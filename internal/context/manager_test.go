package context

import (
	"strings"
	"testing"
	"time"
)

func newTestManager(cfg ContextConfig) *ContextManager {
	return NewContextManager(cfg, nil)
}

func TestNewContextManager_Defaults(t *testing.T) {
	cm := newTestManager(ContextConfig{})

	if cm.config.MaxTokens != 32768 {
		t.Errorf("default MaxTokens = %d, want 32768", cm.config.MaxTokens)
	}
	if cm.config.TruncationStrategy != TruncateIntelligent {
		t.Errorf("default strategy = %q, want intelligent", cm.config.TruncationStrategy)
	}
	if cm.config.RetentionDays != 7 {
		t.Errorf("default retention = %d, want 7", cm.config.RetentionDays)
	}
	if cm.config.TokenEstimator == nil {
		t.Errorf("expected default token estimator")
	}
	if cm.config.TypePriorities == nil {
		t.Errorf("expected default type priorities")
	}
	// Default token estimator: ~4 chars per token.
	if got := cm.config.TokenEstimator("12345678"); got != 2 {
		t.Errorf("token estimator(8 chars) = %d, want 2", got)
	}
}

func TestNewContextManager_PreservesProvidedConfig(t *testing.T) {
	cfg := ContextConfig{
		MaxTokens:          5000,
		TruncationStrategy: TruncateOldest,
		RetentionDays:      30,
	}
	cm := newTestManager(cfg)
	if cm.config.MaxTokens != 5000 {
		t.Errorf("MaxTokens = %d, want 5000", cm.config.MaxTokens)
	}
	if cm.config.TruncationStrategy != TruncateOldest {
		t.Errorf("strategy = %q, want oldest", cm.config.TruncationStrategy)
	}
	if cm.config.RetentionDays != 30 {
		t.Errorf("retention = %d, want 30", cm.config.RetentionDays)
	}
}

func TestAddContext_AndWindow(t *testing.T) {
	cm := newTestManager(ContextConfig{MaxTokens: 10000})

	err := cm.AddContext(ContextTypeFile, "/main.go", "package main", map[string]interface{}{"k": "v"})
	if err != nil {
		t.Fatalf("AddContext error: %v", err)
	}

	window := cm.GetCurrentWindow()
	if len(window.Items) != 1 {
		t.Fatalf("expected 1 item in window, got %d", len(window.Items))
	}
	item := window.Items[0]
	if item.Type != ContextTypeFile {
		t.Errorf("item type = %q, want file", item.Type)
	}
	if item.FilePath != "/main.go" {
		t.Errorf("item file path = %q", item.FilePath)
	}
	if item.Content != "package main" {
		t.Errorf("item content = %q", item.Content)
	}
	if item.TokenCount != len("package main")/4 {
		t.Errorf("token count = %d, want %d", item.TokenCount, len("package main")/4)
	}
	if window.Truncated {
		t.Errorf("window should not be truncated")
	}
}

func TestAddContext_DeduplicatesAndUpdates(t *testing.T) {
	cm := newTestManager(ContextConfig{MaxTokens: 10000})

	// Same type+path+content -> same ID -> dedup.
	_ = cm.AddContext(ContextTypeFile, "/a.go", "content", nil)
	_ = cm.AddContext(ContextTypeFile, "/a.go", "content", nil)

	if len(cm.items) != 1 {
		t.Fatalf("expected 1 deduplicated item, got %d", len(cm.items))
	}
	for _, item := range cm.items {
		if item.AccessCount != 2 {
			t.Errorf("expected access count 2 after re-add, got %d", item.AccessCount)
		}
	}

	// Different content -> different ID -> new item.
	_ = cm.AddContext(ContextTypeFile, "/a.go", "different content", nil)
	if len(cm.items) != 2 {
		t.Errorf("expected 2 items after content change, got %d", len(cm.items))
	}
}

func TestGetContext(t *testing.T) {
	cm := newTestManager(ContextConfig{MaxTokens: 10000})
	_ = cm.AddContext(ContextTypeFile, "/x.go", "hello", nil)

	var id string
	for k := range cm.items {
		id = k
	}

	item, err := cm.GetContext(id)
	if err != nil {
		t.Fatalf("GetContext error: %v", err)
	}
	if item.Content != "hello" {
		t.Errorf("content = %q, want hello", item.Content)
	}
	// Access count incremented from 1 to 2.
	if item.AccessCount != 2 {
		t.Errorf("access count = %d, want 2", item.AccessCount)
	}

	if _, err := cm.GetContext("nonexistent"); err == nil {
		t.Errorf("expected error for missing context id")
	}
}

func TestGetContextByFile(t *testing.T) {
	cm := newTestManager(ContextConfig{MaxTokens: 10000})
	_ = cm.AddContext(ContextTypeFile, "/shared.go", "content one", nil)
	_ = cm.AddContext(ContextTypeEdit, "/shared.go", "content two", nil)
	_ = cm.AddContext(ContextTypeFile, "/other.go", "elsewhere", nil)

	items, err := cm.GetContextByFile("/shared.go")
	if err != nil {
		t.Fatalf("GetContextByFile error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items for /shared.go, got %d", len(items))
	}
	// Edit has higher priority than File, so it should be first.
	if items[0].Type != ContextTypeEdit {
		t.Errorf("expected edit item first (higher priority), got %q", items[0].Type)
	}

	none, _ := cm.GetContextByFile("/missing.go")
	if len(none) != 0 {
		t.Errorf("expected no items for missing file, got %d", len(none))
	}
}

func TestRemoveContext(t *testing.T) {
	cm := newTestManager(ContextConfig{MaxTokens: 10000})
	_ = cm.AddContext(ContextTypeFile, "/r.go", "remove me", nil)
	var id string
	for k := range cm.items {
		id = k
	}

	if err := cm.RemoveContext(id); err != nil {
		t.Fatalf("RemoveContext error: %v", err)
	}
	if len(cm.items) != 0 {
		t.Errorf("expected 0 items after removal, got %d", len(cm.items))
	}
	if len(cm.GetCurrentWindow().Items) != 0 {
		t.Errorf("expected empty window after removal")
	}
}

func TestRemoveContextByFile(t *testing.T) {
	cm := newTestManager(ContextConfig{MaxTokens: 10000})
	_ = cm.AddContext(ContextTypeFile, "/keep.go", "keep", nil)
	_ = cm.AddContext(ContextTypeFile, "/del.go", "delete a", nil)
	_ = cm.AddContext(ContextTypeEdit, "/del.go", "delete b", nil)

	if err := cm.RemoveContextByFile("/del.go"); err != nil {
		t.Fatalf("RemoveContextByFile error: %v", err)
	}
	if len(cm.items) != 1 {
		t.Errorf("expected 1 item remaining, got %d", len(cm.items))
	}
	remaining, _ := cm.GetContextByFile("/keep.go")
	if len(remaining) != 1 {
		t.Errorf("expected /keep.go to survive")
	}
}

func TestUpdateFilePath(t *testing.T) {
	cm := newTestManager(ContextConfig{MaxTokens: 10000})
	_ = cm.AddContext(ContextTypeFile, "/old.go", "content", nil)

	if err := cm.UpdateFilePath("/old.go", "/new.go"); err != nil {
		t.Fatalf("UpdateFilePath error: %v", err)
	}
	old, _ := cm.GetContextByFile("/old.go")
	if len(old) != 0 {
		t.Errorf("expected no items under old path")
	}
	updated, _ := cm.GetContextByFile("/new.go")
	if len(updated) != 1 {
		t.Errorf("expected item to move to new path")
	}
}

func TestCleanupExpired(t *testing.T) {
	cm := newTestManager(ContextConfig{MaxTokens: 10000, RetentionDays: 7})
	_ = cm.AddContext(ContextTypeFile, "/fresh.go", "fresh", nil)
	_ = cm.AddContext(ContextTypeFile, "/stale.go", "stale", nil)

	// Backdate the stale item beyond the retention window.
	for _, item := range cm.items {
		if item.FilePath == "/stale.go" {
			old := time.Now().AddDate(0, 0, -30)
			item.Timestamp = old
			item.LastAccess = old
		}
	}

	if err := cm.CleanupExpired(); err != nil {
		t.Fatalf("CleanupExpired error: %v", err)
	}
	if len(cm.items) != 1 {
		t.Errorf("expected 1 item after cleanup, got %d", len(cm.items))
	}
	fresh, _ := cm.GetContextByFile("/fresh.go")
	if len(fresh) != 1 {
		t.Errorf("expected fresh item to survive cleanup")
	}
}

func TestTruncation_OldestStrategy(t *testing.T) {
	// Each item is "aaaa" = 4 chars = 1 token. Limit of 2 tokens -> 2 items kept.
	cm := newTestManager(ContextConfig{
		MaxTokens:          2,
		TruncationStrategy: TruncateOldest,
	})
	_ = cm.AddContext(ContextTypeFile, "/1.go", "aaaa", nil)
	time.Sleep(2 * time.Millisecond)
	_ = cm.AddContext(ContextTypeFile, "/2.go", "bbbb", nil)
	time.Sleep(2 * time.Millisecond)
	_ = cm.AddContext(ContextTypeFile, "/3.go", "cccc", nil)

	window := cm.GetCurrentWindow()
	if len(window.Items) != 2 {
		t.Fatalf("expected 2 items within token limit, got %d", len(window.Items))
	}
	if !window.Truncated {
		t.Errorf("expected window to be marked truncated")
	}
	if window.Strategy != "oldest" {
		t.Errorf("strategy = %q, want oldest", window.Strategy)
	}
	if window.TotalTokens != 2 {
		t.Errorf("total tokens = %d, want 2", window.TotalTokens)
	}
	// Oldest strategy keeps newest first; /1.go (oldest) should be dropped.
	for _, item := range window.Items {
		if item.FilePath == "/1.go" {
			t.Errorf("oldest item /1.go should have been truncated")
		}
	}
}

func TestTruncation_ByPriorityStrategy(t *testing.T) {
	cm := newTestManager(ContextConfig{
		MaxTokens:          1, // only room for one 4-char item
		TruncationStrategy: TruncateByPriority,
	})
	// Edit type has highest default priority (1.0), Log lowest (0.3).
	_ = cm.AddContext(ContextTypeLog, "", "llll", nil)
	_ = cm.AddContext(ContextTypeEdit, "/e.go", "eeee", nil)

	window := cm.GetCurrentWindow()
	if len(window.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(window.Items))
	}
	if window.Items[0].Type != ContextTypeEdit {
		t.Errorf("expected highest-priority edit item to be kept, got %q", window.Items[0].Type)
	}
	if window.Strategy != "by_priority" {
		t.Errorf("strategy = %q, want by_priority", window.Strategy)
	}
}

func TestTruncation_ByTypeStrategy(t *testing.T) {
	cm := newTestManager(ContextConfig{
		MaxTokens:          1,
		TruncationStrategy: TruncateByType,
	})
	_ = cm.AddContext(ContextTypeLog, "", "llll", nil)
	_ = cm.AddContext(ContextTypeFile, "/f.go", "ffff", nil)

	window := cm.GetCurrentWindow()
	if window.Strategy != "by_type" {
		t.Errorf("strategy = %q, want by_type", window.Strategy)
	}
	if len(window.Items) != 1 {
		t.Fatalf("expected 1 item within limit, got %d", len(window.Items))
	}
	// File (0.8) outranks Log (0.3).
	if window.Items[0].Type != ContextTypeFile {
		t.Errorf("expected file item kept under by_type, got %q", window.Items[0].Type)
	}
}

func TestTruncation_LRUStrategy(t *testing.T) {
	cm := newTestManager(ContextConfig{
		MaxTokens:          1,
		TruncationStrategy: TruncateLRU,
	})
	_ = cm.AddContext(ContextTypeFile, "/old.go", "oooo", nil)
	time.Sleep(5 * time.Millisecond)
	_ = cm.AddContext(ContextTypeFile, "/new.go", "nnnn", nil)

	window := cm.GetCurrentWindow()
	if len(window.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(window.Items))
	}
	if window.Items[0].FilePath != "/new.go" {
		t.Errorf("LRU should keep most recently accessed /new.go, got %q", window.Items[0].FilePath)
	}
}

func TestUpdateMaxTokens_Retruncates(t *testing.T) {
	cm := newTestManager(ContextConfig{
		MaxTokens:          100,
		TruncationStrategy: TruncateOldest,
	})
	_ = cm.AddContext(ContextTypeFile, "/1.go", "aaaa", nil)
	_ = cm.AddContext(ContextTypeFile, "/2.go", "bbbb", nil)
	_ = cm.AddContext(ContextTypeFile, "/3.go", "cccc", nil)

	if len(cm.GetCurrentWindow().Items) != 3 {
		t.Fatalf("expected all 3 items initially")
	}

	cm.UpdateMaxTokens(1)
	window := cm.GetCurrentWindow()
	if len(window.Items) != 1 {
		t.Errorf("expected 1 item after shrinking max tokens, got %d", len(window.Items))
	}
	if window.MaxTokens != 1 {
		t.Errorf("window MaxTokens = %d, want 1", window.MaxTokens)
	}
}

func TestGetStats(t *testing.T) {
	cm := newTestManager(ContextConfig{MaxTokens: 10000})
	_ = cm.AddContext(ContextTypeFile, "/a.go", "aaaaaaaa", nil) // 2 tokens
	_ = cm.AddContext(ContextTypeEdit, "/b.go", "bbbbbbbb", nil) // 2 tokens

	stats := cm.GetStats()
	if stats["total_items"].(int) != 2 {
		t.Errorf("total_items = %v, want 2", stats["total_items"])
	}
	if stats["total_tokens"].(int) != 4 {
		t.Errorf("total_tokens = %v, want 4", stats["total_tokens"])
	}
	if stats["max_tokens"].(int) != 10000 {
		t.Errorf("max_tokens = %v, want 10000", stats["max_tokens"])
	}
	breakdown := stats["type_breakdown"].(map[ContextType]int)
	if breakdown[ContextTypeFile] != 1 || breakdown[ContextTypeEdit] != 1 {
		t.Errorf("type_breakdown = %v", breakdown)
	}
	util := stats["utilization"].(float64)
	if util <= 0 || util > 1 {
		t.Errorf("utilization = %v, expected between 0 and 1", util)
	}
}

func TestCalculatePriority(t *testing.T) {
	cm := newTestManager(ContextConfig{MaxTokens: 10000})

	// Edit type gets base 1.0 + 0.3 edit boost + 0.1 small-file boost.
	editPriority := cm.calculatePriority(ContextTypeEdit, "/x.txt", "small", nil)
	if editPriority < 1.3 {
		t.Errorf("edit priority = %v, expected >= 1.3", editPriority)
	}

	// Go file gets +0.2 boost over a same-type non-go file.
	goPriority := cm.calculatePriority(ContextTypeFile, "/x.go", "small", nil)
	txtPriority := cm.calculatePriority(ContextTypeFile, "/x.txt", "small", nil)
	if goPriority <= txtPriority {
		t.Errorf("go file priority (%v) should exceed txt priority (%v)", goPriority, txtPriority)
	}

	// Metadata priority is additive.
	withMeta := cm.calculatePriority(ContextTypeFile, "/x.txt", "small", map[string]interface{}{"priority": 5.0})
	if withMeta < txtPriority+5.0 {
		t.Errorf("expected metadata priority to add 5.0, got %v vs base %v", withMeta, txtPriority)
	}

	// Large content does not get the small-file boost.
	large := cm.calculatePriority(ContextTypeFile, "/x.txt", strings.Repeat("z", 2000), nil)
	if large >= txtPriority {
		t.Errorf("large file priority (%v) should be below small file priority (%v)", large, txtPriority)
	}
}

func TestGenerateID_StableAndDistinct(t *testing.T) {
	cm := newTestManager(ContextConfig{MaxTokens: 10000})
	id1 := cm.generateID(ContextTypeFile, "/a.go", "content")
	id2 := cm.generateID(ContextTypeFile, "/a.go", "content")
	id3 := cm.generateID(ContextTypeFile, "/a.go", "other content")
	id4 := cm.generateID(ContextTypeEdit, "/a.go", "content")

	if id1 != id2 {
		t.Errorf("generateID not stable: %q != %q", id1, id2)
	}
	if id1 == id3 {
		t.Errorf("generateID should differ for different content")
	}
	if id1 == id4 {
		t.Errorf("generateID should differ for different type")
	}
	if len(id1) != 16 {
		t.Errorf("id length = %d, want 16", len(id1))
	}
}

func TestDefaultTypePriorities_Ordering(t *testing.T) {
	p := defaultTypePriorities()
	if p[ContextTypeEdit] <= p[ContextTypeFile] {
		t.Errorf("edit should outrank file")
	}
	if p[ContextTypeFile] <= p[ContextTypeLog] {
		t.Errorf("file should outrank log")
	}
	if p[ContextTypeLog] != 0.3 {
		t.Errorf("log priority = %v, want 0.3", p[ContextTypeLog])
	}
}
