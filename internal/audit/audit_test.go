package audit

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/logging"
)

func testLogger() *logging.Logger {
	return logging.NewLogger("error")
}

func mkEntry(id, event, user string, success bool, ts time.Time) *AuditEntry {
	return &AuditEntry{
		ID:        id,
		Timestamp: ts,
		Event:     event,
		UserID:    user,
		Success:   success,
		Details:   map[string]interface{}{"k": "v"},
	}
}

// --- helpers ---------------------------------------------------------------

func TestMatchesFilter(t *testing.T) {
	now := time.Now()
	yes := true
	entry := AuditEntry{
		Event:     "oauth.token.issued",
		UserID:    "alice",
		ClientID:  "cli",
		Success:   true,
		Timestamp: now,
	}

	tests := []struct {
		name   string
		filter *AuditFilter
		want   bool
	}{
		{"nil filter matches", nil, true},
		{"event match", &AuditFilter{Event: "oauth.token.issued"}, true},
		{"event mismatch", &AuditFilter{Event: "other"}, false},
		{"user match", &AuditFilter{UserID: "alice"}, true},
		{"user mismatch", &AuditFilter{UserID: "bob"}, false},
		{"client match", &AuditFilter{ClientID: "cli"}, true},
		{"success match", &AuditFilter{Success: &yes}, true},
		{"start after entry", &AuditFilter{StartTime: now.Add(time.Hour)}, false},
		{"end before entry", &AuditFilter{EndTime: now.Add(-time.Hour)}, false},
		{"within range", &AuditFilter{StartTime: now.Add(-time.Hour), EndTime: now.Add(time.Hour)}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesFilter(entry, tt.filter); got != tt.want {
				t.Fatalf("matchesFilter = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPaginate(t *testing.T) {
	mk := func(n int) []AuditEntry {
		out := make([]AuditEntry, n)
		for i := range out {
			out[i] = AuditEntry{ID: fmt.Sprintf("e%d", i)}
		}

		return out
	}

	tests := []struct {
		name               string
		entries            []AuditEntry
		limit, offset      int
		wantLen, wantTotal int
	}{
		{"basic", mk(10), 3, 0, 3, 10},
		{"offset", mk(10), 3, 8, 2, 10},
		{"offset past end", mk(10), 5, 100, 0, 10},
		{"negative limit means no limit", mk(10), -1, 0, 10, 10},
		{"limit larger than data", mk(2), 50, 0, 2, 2},
		{"empty", mk(0), 10, 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, total, err := paginate(tt.entries, tt.limit, tt.offset)
			if err != nil {
				t.Fatalf("paginate err: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("len = %d, want %d", len(got), tt.wantLen)
			}
			if total != tt.wantTotal {
				t.Errorf("total = %d, want %d", total, tt.wantTotal)
			}
		})
	}
}

func TestComputeStats(t *testing.T) {
	now := time.Now()
	entries := []AuditEntry{
		*mkEntry("1", "login", "a", true, now),
		*mkEntry("2", "login", "b", false, now),
		*mkEntry("3", "logout", "a", true, now),
	}

	stats := computeStats(entries)
	if stats.TotalEntries != 3 {
		t.Errorf("TotalEntries = %d, want 3", stats.TotalEntries)
	}
	if stats.EventCounts["login"] != 2 {
		t.Errorf("login count = %d, want 2", stats.EventCounts["login"])
	}
	wantRate := float64(2) / float64(3) * PercentageMultiplier
	if stats.SuccessRate != wantRate {
		t.Errorf("SuccessRate = %v, want %v", stats.SuccessRate, wantRate)
	}

	empty := computeStats(nil)
	if empty.SuccessRate != 0 || empty.TotalEntries != 0 {
		t.Errorf("empty stats = %+v", empty)
	}
}

// --- memory backend --------------------------------------------------------

func TestMemoryBackendRoundTrip(t *testing.T) {
	b := newMemoryBackend(3)
	now := time.Now()

	for i := 0; i < 5; i++ {
		if err := b.Store(mkEntry(fmt.Sprintf("e%d", i), "ev", "u", true, now.Add(time.Duration(i)*time.Second))); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}

	// maxEntries=3 -> only the 3 newest retained.
	got, total, err := b.Query(10, 0, nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3 (ring trimmed)", total)
	}
	// newest-first
	if got[0].ID != "e4" {
		t.Errorf("newest = %s, want e4", got[0].ID)
	}
}

func TestMemoryBackendCleanup(t *testing.T) {
	b := newMemoryBackend(10)
	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now()
	_ = b.Store(mkEntry("old", "ev", "u", true, old))
	_ = b.Store(mkEntry("new", "ev", "u", true, recent))

	if err := b.Cleanup(time.Now().Add(-24 * time.Hour)); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	_, total, _ := b.Query(10, 0, nil)
	if total != 1 {
		t.Fatalf("after cleanup total = %d, want 1", total)
	}
}

// --- file backend ----------------------------------------------------------

func TestFileBackendRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")

	b, err := newFileBackend(backendConfig{filePath: path, maxFileSize: 1 << 20, maxBackups: 3})
	if err != nil {
		t.Fatalf("newFileBackend: %v", err)
	}
	defer func() { _ = b.Close() }()

	now := time.Now()
	for i := 0; i < 4; i++ {
		e := mkEntry(fmt.Sprintf("e%d", i), "evt", "alice", i%2 == 0, now.Add(time.Duration(i)*time.Second))
		if err := b.Store(e); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}

	// File should exist with 0600 perms.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != auditFileMode {
		t.Errorf("file mode = %o, want %o", info.Mode().Perm(), auditFileMode)
	}

	// NDJSON: one line per entry.
	f, _ := os.Open(path)
	lines := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.TrimSpace(sc.Text()) != "" {
			lines++
		}
	}
	_ = f.Close()
	if lines != 4 {
		t.Errorf("file has %d lines, want 4", lines)
	}

	// Round-trip via Query, newest-first.
	got, total, err := b.Query(10, 0, nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total != 4 {
		t.Fatalf("total = %d, want 4", total)
	}
	if got[0].ID != "e3" {
		t.Errorf("newest = %s, want e3", got[0].ID)
	}
	if got[0].Details["k"] != "v" {
		t.Errorf("details not round-tripped: %+v", got[0].Details)
	}

	// Filter by success.
	yes := true
	filtered, ftotal, err := b.Query(10, 0, &AuditFilter{Success: &yes})
	if err != nil {
		t.Fatalf("filtered Query: %v", err)
	}
	if ftotal != 2 {
		t.Errorf("success-filtered total = %d, want 2", ftotal)
	}
	for _, e := range filtered {
		if !e.Success {
			t.Errorf("filter returned unsuccessful entry %s", e.ID)
		}
	}
}

func TestFileBackendRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")

	// Tiny max size forces rotation almost every write.
	b, err := newFileBackend(backendConfig{filePath: path, maxFileSize: 200, maxBackups: 2})
	if err != nil {
		t.Fatalf("newFileBackend: %v", err)
	}
	defer func() { _ = b.Close() }()

	now := time.Now()
	for i := 0; i < 20; i++ {
		e := mkEntry(fmt.Sprintf("entry-%02d", i), "rotation.test", "user", true, now.Add(time.Duration(i)*time.Millisecond))
		if err := b.Store(e); err != nil {
			t.Fatalf("Store %d: %v", i, err)
		}
	}

	// Backups must be capped at maxBackups.
	backups, _ := filepath.Glob(path + ".*")
	if len(backups) > 2 {
		t.Errorf("found %d backups, want <= 2", len(backups))
	}
	if len(backups) == 0 {
		t.Error("expected at least one rotated backup file")
	}

	// Even after rotation+pruning, Query must still return entries (from the
	// retained backups plus the active file) and remain ordered newest-first.
	got, total, err := b.Query(100, 0, nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total == 0 {
		t.Fatal("expected entries after rotation, got 0")
	}
	for i := 1; i < len(got); i++ {
		if got[i-1].Timestamp.Before(got[i].Timestamp) {
			t.Errorf("entries not newest-first at index %d", i)
		}
	}
}

func TestFileBackendCleanup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")

	b, err := newFileBackend(backendConfig{filePath: path, maxFileSize: 150, maxBackups: 10})
	if err != nil {
		t.Fatalf("newFileBackend: %v", err)
	}
	defer func() { _ = b.Close() }()

	oldTs := time.Now().Add(-72 * time.Hour)
	// Write several old entries; small maxFileSize rotates them into backups.
	for i := 0; i < 10; i++ {
		_ = b.Store(mkEntry(fmt.Sprintf("old-%d", i), "ev", "u", true, oldTs))
	}
	// A recent entry in the active file.
	_ = b.Store(mkEntry("recent", "ev", "u", true, time.Now()))

	beforeBackups, _ := filepath.Glob(path + ".*")
	if len(beforeBackups) == 0 {
		t.Skip("no rotation occurred, cannot exercise backup cleanup")
	}

	if err := b.Cleanup(time.Now().Add(-24 * time.Hour)); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	afterBackups, _ := filepath.Glob(path + ".*")
	if len(afterBackups) >= len(beforeBackups) {
		t.Errorf("cleanup removed no old backups: before=%d after=%d", len(beforeBackups), len(afterBackups))
	}

	// The recent entry in the active file must survive.
	_, total, _ := b.Query(100, 0, nil)
	if total == 0 {
		t.Error("recent entry should survive cleanup")
	}
}

func TestFileBackendCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "deep", "audit.log")

	b, err := newFileBackend(backendConfig{filePath: path, maxFileSize: 1 << 20, maxBackups: 1})
	if err != nil {
		t.Fatalf("newFileBackend should create parent dirs: %v", err)
	}
	defer func() { _ = b.Close() }()

	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Errorf("parent dir not created: %v", err)
	}
}

// --- database backend (no live DB needed) ----------------------------------

func TestDatabaseInsertStmtParameterized(t *testing.T) {
	b := &databaseBackend{table: "audit_log"}
	stmt := b.insertStmt()

	// Must use bind parameters $1..$10, never interpolated values.
	for i := 1; i <= 10; i++ {
		if !strings.Contains(stmt, fmt.Sprintf("$%d", i)) {
			t.Errorf("insert stmt missing bind param $%d: %s", i, stmt)
		}
	}
	if !strings.Contains(stmt, "INSERT INTO audit_log") {
		t.Errorf("insert stmt has wrong table: %s", stmt)
	}
	if strings.Contains(stmt, "VALUES (audit_") {
		t.Errorf("insert stmt appears to interpolate values: %s", stmt)
	}
}

func TestBuildWhereClause(t *testing.T) {
	yes := true
	start := time.Now().Add(-time.Hour)

	tests := []struct {
		name      string
		filter    *AuditFilter
		wantArgs  int
		wantParts []string
	}{
		{"nil filter", nil, 0, nil},
		{"empty filter", &AuditFilter{}, 0, nil},
		{"event only", &AuditFilter{Event: "login"}, 1, []string{"event = $1", " WHERE "}},
		{
			"multi-field",
			&AuditFilter{Event: "login", UserID: "alice", Success: &yes, StartTime: start},
			4,
			[]string{"event = $1", "user_id = $2", "success = $3", "ts >= $4", " AND "},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clause, args := buildWhereClause(tt.filter)
			if len(args) != tt.wantArgs {
				t.Errorf("args = %d, want %d", len(args), tt.wantArgs)
			}
			for _, part := range tt.wantParts {
				if !strings.Contains(clause, part) {
					t.Errorf("clause %q missing %q", clause, part)
				}
			}
			// Filter values must never appear literally in the SQL text.
			if tt.filter != nil && tt.filter.Event != "" && strings.Contains(clause, "'"+tt.filter.Event+"'") {
				t.Errorf("clause interpolated a literal value: %s", clause)
			}
		})
	}
}

func TestDatabaseSchemaDDLIdempotent(t *testing.T) {
	// auditSchemaDDL is the exact string initSchema executes; verify it is
	// idempotent without needing a live DB.
	schema := auditSchemaDDL("audit_log")
	if !strings.Contains(schema, "CREATE TABLE IF NOT EXISTS audit_log") {
		t.Errorf("schema missing idempotent table guard: %s", schema)
	}
	if strings.Count(schema, "CREATE INDEX IF NOT EXISTS") < 4 {
		t.Errorf("schema missing idempotent index guards: %s", schema)
	}
	if !strings.Contains(schema, "details      JSONB") {
		t.Errorf("schema missing JSONB details column: %s", schema)
	}
}

// --- top-level AuditLogger wiring -------------------------------------------

func TestNewAuditLoggerUnknownBackendIsLoudNotSilent(t *testing.T) {
	cfg := &config.AuditConfig{
		Enabled:   true,
		Storage:   "cassandra", // not implemented
		Events:    []string{"login"},
		Retention: config.RetentionConfig{MaxEntries: 10, MaxAge: "1h"},
	}

	// WithError variant must return an error, not a memory fallback.
	if _, err := NewAuditLoggerWithError(cfg, testLogger()); err == nil {
		t.Fatal("expected error for unknown backend, got nil")
	}

	// The compatibility constructor must return a DISABLED logger, never a
	// silent in-memory downgrade.
	al := NewAuditLogger(cfg, testLogger())
	if al == nil {
		t.Fatal("NewAuditLogger returned nil")
	}
	if al.enabled {
		t.Error("logger must be disabled when backend init fails, not silently downgraded")
	}
	al.Log("login", "u", "c", "ip", "ua", true, nil, nil)
	got, total, err := al.GetEntries(10, 0, nil)
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if total != 0 || len(got) != 0 {
		t.Errorf("disabled logger recorded entries: total=%d", total)
	}
}

func TestNewAuditLoggerFileBackendEndToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	t.Setenv("M8E_AUDIT_FILE_PATH", path)

	cfg := &config.AuditConfig{
		Enabled:   true,
		Storage:   "file",
		Events:    []string{"oauth.user.login"},
		Retention: config.RetentionConfig{MaxEntries: 100, MaxAge: "24h"},
	}

	al, err := NewAuditLoggerWithError(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewAuditLoggerWithError: %v", err)
	}
	defer func() { _ = al.Shutdown() }()

	al.LogUserLogin("alice", "10.0.0.1", "test-agent", true, nil)
	// Unregistered event must be ignored.
	al.Log("unregistered.event", "bob", "", "", "", true, nil, nil)

	got, total, err := al.GetEntries(10, 0, nil)
	if err != nil {
		t.Fatalf("GetEntries: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1 (only registered event)", total)
	}
	if got[0].UserID != "alice" || got[0].Event != "oauth.user.login" {
		t.Errorf("unexpected entry: %+v", got[0])
	}

	stats := al.GetStats()
	if stats.TotalEntries != 1 {
		t.Errorf("stats total = %d, want 1", stats.TotalEntries)
	}
}

// --- live DB integration (skipped without DSN) -----------------------------

func TestDatabaseBackendIntegration(t *testing.T) {
	dsn := os.Getenv("M8E_AUDIT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("M8E_AUDIT_TEST_DATABASE_URL not set; skipping live PostgreSQL test")
	}

	cfg := backendConfig{databaseURL: dsn, tableName: "audit_log_test"}
	b, err := newDatabaseBackend(cfg, testLogger())
	if err != nil {
		t.Fatalf("newDatabaseBackend: %v", err)
	}
	defer func() { _ = b.Close() }()

	now := time.Now().UTC().Truncate(time.Microsecond)
	entry := mkEntry("it-1", "oauth.user.login", "alice", true, now)
	if err := b.Store(entry); err != nil {
		t.Fatalf("Store: %v", err)
	}

	got, total, err := b.Query(10, 0, &AuditFilter{UserID: "alice"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if total < 1 {
		t.Fatalf("total = %d, want >= 1", total)
	}
	found := false
	for _, e := range got {
		if e.ID == "it-1" {
			found = true
			if e.Details["k"] != "v" {
				t.Errorf("details not round-tripped: %+v", e.Details)
			}
		}
	}
	if !found {
		t.Error("stored entry not returned by Query")
	}

	if err := b.Cleanup(time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
}
