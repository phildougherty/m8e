package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/memory"
)

func newTestMentionProcessor(t *testing.T, workDir string) *MentionProcessor {
	t.Helper()
	fd, err := NewFileDiscovery(workDir)
	if err != nil {
		t.Fatalf("NewFileDiscovery error: %v", err)
	}
	cm := NewContextManager(ContextConfig{MaxTokens: 100000}, nil)
	return NewMentionProcessor(workDir, fd, cm)
}

func findMention(mentions []Mention, mt MentionType) (Mention, bool) {
	for _, m := range mentions {
		if m.Type == mt {
			return m, true
		}
	}
	return Mention{}, false
}

func TestParseMentions_FileWithLineRange(t *testing.T) {
	mp := newTestMentionProcessor(t, t.TempDir())

	mentions, err := mp.ParseMentions("look at @/src/main.go:10-20 please")
	if err != nil {
		t.Fatalf("ParseMentions error: %v", err)
	}
	m, ok := findMention(mentions, MentionTypeFile)
	if !ok {
		t.Fatalf("expected a file mention")
	}
	if m.Path != "/src/main.go" {
		t.Errorf("path = %q, want /src/main.go", m.Path)
	}
	if len(m.Lines) != 2 || m.Lines[0] != 10 || m.Lines[1] != 20 {
		t.Errorf("lines = %v, want [10 20]", m.Lines)
	}
	if m.Raw != "@/src/main.go:10-20" {
		t.Errorf("raw = %q", m.Raw)
	}
}

func TestParseMentions_FileSingleLine(t *testing.T) {
	mp := newTestMentionProcessor(t, t.TempDir())
	mentions, _ := mp.ParseMentions("@/a/b.go:42")
	m, ok := findMention(mentions, MentionTypeFile)
	if !ok {
		t.Fatalf("expected file mention")
	}
	if len(m.Lines) != 1 || m.Lines[0] != 42 {
		t.Errorf("lines = %v, want [42]", m.Lines)
	}
}

func TestParseMentions_Directory(t *testing.T) {
	mp := newTestMentionProcessor(t, t.TempDir())
	mentions, _ := mp.ParseMentions("contents of @/src/dir/")
	m, ok := findMention(mentions, MentionTypeDirectory)
	if !ok {
		t.Fatalf("expected directory mention")
	}
	if m.Path != "/src/dir/" {
		t.Errorf("path = %q, want /src/dir/", m.Path)
	}
}

func TestParseMentions_SpecialTypes(t *testing.T) {
	mp := newTestMentionProcessor(t, t.TempDir())

	tests := []struct {
		text     string
		wantType MentionType
		wantMeta string // metadata key expected to be present
	}{
		{"@problems:production", MentionTypeProblems, "namespace"},
		{"@logs:proxy", MentionTypeLogs, "service"},
		{"@git-changes:main", MentionTypeGitChanges, "branch"},
		{"@def:MyFunc", MentionTypeDefinition, ""},
		{"@memory:recent", MentionTypeMemory, ""},
		{"@workflow:deploy", MentionTypeWorkflow, ""},
	}
	for _, tt := range tests {
		t.Run(string(tt.wantType), func(t *testing.T) {
			mentions, err := mp.ParseMentions(tt.text)
			if err != nil {
				t.Fatalf("ParseMentions error: %v", err)
			}
			m, ok := findMention(mentions, tt.wantType)
			if !ok {
				t.Fatalf("expected mention of type %q in %q", tt.wantType, tt.text)
			}
			if tt.wantMeta != "" {
				if m.Metadata == nil || m.Metadata[tt.wantMeta] == nil {
					t.Errorf("expected metadata key %q, got %v", tt.wantMeta, m.Metadata)
				}
			}
		})
	}
}

func TestParseMentions_DefinitionPath(t *testing.T) {
	mp := newTestMentionProcessor(t, t.TempDir())
	mentions, _ := mp.ParseMentions("find @def:CalculateTotal")
	m, ok := findMention(mentions, MentionTypeDefinition)
	if !ok {
		t.Fatalf("expected definition mention")
	}
	if m.Path != "CalculateTotal" {
		t.Errorf("definition path = %q, want CalculateTotal", m.Path)
	}
}

func TestParseMentions_OrderedByPosition(t *testing.T) {
	mp := newTestMentionProcessor(t, t.TempDir())
	mentions, _ := mp.ParseMentions("@logs first then @problems second")
	if len(mentions) < 2 {
		t.Fatalf("expected at least 2 mentions, got %d", len(mentions))
	}
	if mentions[0].Type != MentionTypeLogs {
		t.Errorf("first mention = %q, want logs (appears first in text)", mentions[0].Type)
	}
}

func TestParseMentions_NoMentions(t *testing.T) {
	mp := newTestMentionProcessor(t, t.TempDir())
	mentions, err := mp.ParseMentions("just some plain text with no mentions")
	if err != nil {
		t.Fatalf("ParseMentions error: %v", err)
	}
	if len(mentions) != 0 {
		t.Errorf("expected 0 mentions, got %d", len(mentions))
	}
}

func TestProcessFileMention_RealFile(t *testing.T) {
	dir := t.TempDir()
	content := "line1\nline2\nline3\nline4\nline5"
	abs := filepath.Join(dir, "code.txt")
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	mp := newTestMentionProcessor(t, dir)

	// The @file mention regex always captures absolute paths, so the mention
	// Path here is the real absolute temp path.
	mention := Mention{Type: MentionTypeFile, Raw: "@" + abs, Path: abs}
	processed, err := mp.ProcessMention(mention)
	if err != nil {
		t.Fatalf("ProcessMention error: %v", err)
	}
	if processed.Content != content {
		t.Errorf("content = %q, want %q", processed.Content, content)
	}
	if processed.TokenCount != len(content)/4 {
		t.Errorf("token count = %d, want %d", processed.TokenCount, len(content)/4)
	}
	if processed.Metadata["path"] == nil {
		t.Errorf("expected path metadata")
	}
	// The file context should have been registered with the context manager.
	items, _ := mp.contextManager.GetContextByFile(abs)
	if len(items) != 1 {
		t.Errorf("expected file context to be added to manager, got %d items", len(items))
	}
}

func TestProcessFileMention_LineRange(t *testing.T) {
	dir := t.TempDir()
	content := "alpha\nbravo\ncharlie\ndelta\necho"
	abs := filepath.Join(dir, "lines.txt")
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	mp := newTestMentionProcessor(t, dir)

	mention := Mention{Type: MentionTypeFile, Raw: "@" + abs + ":2-4", Path: abs, Lines: []int{2, 4}}
	processed, err := mp.ProcessMention(mention)
	if err != nil {
		t.Fatalf("ProcessMention error: %v", err)
	}
	// Lines 2-4 with line-number prefixes.
	if !strings.Contains(processed.Content, "2: bravo") {
		t.Errorf("expected '2: bravo' in content, got %q", processed.Content)
	}
	if !strings.Contains(processed.Content, "4: delta") {
		t.Errorf("expected '4: delta' in content, got %q", processed.Content)
	}
	if strings.Contains(processed.Content, "alpha") || strings.Contains(processed.Content, "echo") {
		t.Errorf("content should be limited to lines 2-4, got %q", processed.Content)
	}
}

func TestProcessFileMention_MissingFile(t *testing.T) {
	dir := t.TempDir()
	mp := newTestMentionProcessor(t, dir)
	abs := filepath.Join(dir, "nope.txt")
	mention := Mention{Type: MentionTypeFile, Raw: "@" + abs, Path: abs}
	_, err := mp.ProcessMention(mention)
	if err == nil {
		t.Errorf("expected error for missing file")
	}
}

func TestProcessFileMention_LineOutOfRange(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "tiny.txt")
	if err := os.WriteFile(abs, []byte("only one line"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	mp := newTestMentionProcessor(t, dir)
	mention := Mention{Type: MentionTypeFile, Raw: "@" + abs + ":99", Path: abs, Lines: []int{99}}
	_, err := mp.ProcessMention(mention)
	if err == nil {
		t.Errorf("expected error for line number exceeding file length")
	}
}

func TestProcessDirectoryMention(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"one.go", "two.go", "three.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	mp := newTestMentionProcessor(t, dir)

	mention := Mention{Type: MentionTypeDirectory, Raw: "@" + dir + "/", Path: dir + "/"}
	processed, err := mp.ProcessMention(mention)
	if err != nil {
		t.Fatalf("ProcessMention error: %v", err)
	}
	if !strings.Contains(processed.Content, "one.go") {
		t.Errorf("expected one.go in directory listing, got %q", processed.Content)
	}
	if !strings.Contains(processed.Content, "subdir/") {
		t.Errorf("expected subdir/ with trailing slash, got %q", processed.Content)
	}
	if processed.Metadata["total_files"].(int) != 4 {
		t.Errorf("total_files = %v, want 4", processed.Metadata["total_files"])
	}
}

func TestProcessDirectoryMention_WithLimit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10; i++ {
		name := filepath.Join(dir, string(rune('a'+i))+".txt")
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	mp := newTestMentionProcessor(t, dir)

	mention := Mention{
		Type:     MentionTypeDirectory,
		Raw:      "@" + dir + "/:3",
		Path:     dir + "/",
		Metadata: map[string]interface{}{"limit": 3},
	}
	processed, err := mp.ProcessMention(mention)
	if err != nil {
		t.Fatalf("ProcessMention error: %v", err)
	}
	if !strings.Contains(processed.Content, "... (truncated)") {
		t.Errorf("expected truncation marker with limit, got %q", processed.Content)
	}
	if processed.Metadata["shown_files"].(int) != 3 {
		t.Errorf("shown_files = %v, want 3", processed.Metadata["shown_files"])
	}
}

func TestProcessDirectoryMention_NotADirectory(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "afile.txt")
	if err := os.WriteFile(abs, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	mp := newTestMentionProcessor(t, dir)
	mention := Mention{Type: MentionTypeDirectory, Raw: "@" + abs + "/", Path: abs}
	_, err := mp.ProcessMention(mention)
	if err == nil {
		t.Errorf("expected error when directory mention points at a file")
	}
}

func TestProcessMention_UnknownType(t *testing.T) {
	mp := newTestMentionProcessor(t, t.TempDir())
	_, err := mp.ProcessMention(Mention{Type: MentionType("bogus"), Raw: "@bogus"})
	if err == nil {
		t.Errorf("expected error for unknown mention type")
	}
}

// These mention types previously returned hardcoded placeholders. They are
// now wired to real backends (Kubernetes, tree-sitter, memory) — so calling
// them with no injected clients in a unit-test environment returns an honest
// error or a "required arg missing" message instead of fake-success content.
// Dispatch routing is still asserted: every type lands in its own handler and
// preserves Mention.Type. The mention content/error must be informative.
func TestProcessMention_RealHandlersDispatch(t *testing.T) {
	mp := newTestMentionProcessor(t, t.TempDir())
	// Definition mention does NOT need a network — point at an empty tempdir.
	cases := []struct {
		name        string
		mention     Mention
		mustError   bool   // true: handler must surface an error (no fake-success)
		contentHint string // substring expected in either Content or Error
	}{
		{
			name:        "logs_missing_service",
			mention:     Mention{Type: MentionTypeLogs, Raw: "@logs"},
			mustError:   true,
			contentHint: "service name required",
		},
		{
			name:        "memory_missing_query",
			mention:     Mention{Type: MentionTypeMemory, Raw: "@memory"},
			mustError:   true,
			contentHint: "query required",
		},
		{
			name:        "git_changes_dispatch",
			mention:     Mention{Type: MentionTypeGitChanges, Raw: "@git-changes"},
			mustError:   false,
			contentHint: "Git changes",
		},
		{
			name:        "definition_empty_path",
			mention:     Mention{Type: MentionTypeDefinition, Raw: "@def:", Path: ""},
			mustError:   true,
			contentHint: "name required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			processed, err := mp.ProcessMention(tc.mention)
			if tc.mustError && err == nil {
				t.Fatalf("expected error from %s; got content=%q", tc.name, processed.Content)
			}
			if processed.Type != tc.mention.Type {
				t.Errorf("dispatch changed type from %q to %q", tc.mention.Type, processed.Type)
			}
			haystack := processed.Content
			if err != nil {
				haystack += " " + err.Error()
			}
			if tc.contentHint != "" && !strings.Contains(haystack, tc.contentHint) {
				t.Errorf("expected substring %q in content/error, got: content=%q err=%v", tc.contentHint, processed.Content, err)
			}
		})
	}
}

func TestExpandText(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(abs, []byte("HELLO WORLD"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	mp := newTestMentionProcessor(t, dir)

	expanded, mentions, err := mp.ExpandText("file says: @" + abs + " done")
	if err != nil {
		t.Fatalf("ExpandText error: %v", err)
	}
	if !strings.Contains(expanded, "HELLO WORLD") {
		t.Errorf("expected expanded text to contain file content, got %q", expanded)
	}
	if !strings.Contains(expanded, "--- End") {
		t.Errorf("expected delimiter markers in expanded text, got %q", expanded)
	}
	// The file mention must be among the processed mentions and resolved.
	var fileMention *Mention
	for i := range mentions {
		if mentions[i].Type == MentionTypeFile {
			fileMention = &mentions[i]
		}
	}
	if fileMention == nil {
		t.Fatalf("expected a processed file mention")
	}
	if fileMention.Content != "HELLO WORLD" {
		t.Errorf("file mention content = %q, want HELLO WORLD", fileMention.Content)
	}
}

func TestExpandText_MissingFileKeepsRaw(t *testing.T) {
	dir := t.TempDir()
	mp := newTestMentionProcessor(t, dir)
	original := "ref @" + filepath.Join(dir, "does-not-exist.txt") + " here"
	expanded, mentions, err := mp.ExpandText(original)
	if err != nil {
		t.Fatalf("ExpandText error: %v", err)
	}
	// The unresolvable file mention contributes no content, so its raw token
	// is left untouched in the expanded text.
	if !strings.Contains(expanded, filepath.Join(dir, "does-not-exist.txt")) {
		t.Errorf("expected unresolvable mention raw token preserved, got %q", expanded)
	}
	var fileMention *Mention
	for i := range mentions {
		if mentions[i].Type == MentionTypeFile {
			fileMention = &mentions[i]
		}
	}
	if fileMention == nil {
		t.Fatalf("expected a file mention to be processed")
	}
	if fileMention.Error == "" {
		t.Errorf("expected the file mention to carry an error, got %+v", *fileMention)
	}
}

func TestGetSupportedMentions(t *testing.T) {
	mp := newTestMentionProcessor(t, t.TempDir())
	supported := mp.GetSupportedMentions()
	for _, key := range []string{"@/path/file.ext", "@problems", "@logs", "@git-changes", "@def:functionName"} {
		if _, ok := supported[key]; !ok {
			t.Errorf("expected %q in supported mentions", key)
		}
	}
}

// -----------------------------------------------------------------------------
// Tests for the real implementations of the five previously-stubbed mentions.
// Each one is exercised with a fake k8s client / memory store / tree-sitter
// parser to drive the happy path, plus a failure path that asserts no
// fake-success content is returned.
// -----------------------------------------------------------------------------

func newPod(name, namespace string, phase corev1.PodPhase, waitingReason string, ready bool) *corev1.Pod {
	cs := corev1.ContainerStatus{Name: "main", Ready: ready}
	if waitingReason != "" {
		cs.State.Waiting = &corev1.ContainerStateWaiting{Reason: waitingReason, Message: "container is waiting"}
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: map[string]string{"app": name}},
		Status: corev1.PodStatus{
			Phase:             phase,
			ContainerStatuses: []corev1.ContainerStatus{cs},
		},
	}
}

func TestProcessProblemsMention_HappyPath(t *testing.T) {
	healthy := newPod("matey-proxy", "matey", corev1.PodRunning, "", true)
	crash := newPod("matey-memory", "matey", corev1.PodRunning, "CrashLoopBackOff", false)
	pending := newPod("matey-task-scheduler", "matey", corev1.PodPending, "ImagePullBackOff", false)
	cs := k8sfake.NewSimpleClientset(healthy, crash, pending)

	mp := newTestMentionProcessor(t, t.TempDir())
	mp.SetKubernetesClients(cs, nil)
	mp.SetNamespace("matey")

	m := Mention{Type: MentionTypeProblems, Raw: "@problems"}
	processed, err := mp.ProcessMention(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(processed.Content, "matey/matey-memory") {
		t.Errorf("expected problem pod matey/matey-memory in content, got %q", processed.Content)
	}
	if !strings.Contains(processed.Content, "CrashLoopBackOff") {
		t.Errorf("expected CrashLoopBackOff reason in content, got %q", processed.Content)
	}
	if !strings.Contains(processed.Content, "ImagePullBackOff") {
		t.Errorf("expected ImagePullBackOff reason in content, got %q", processed.Content)
	}
	if strings.Contains(processed.Content, "matey/matey-proxy") {
		t.Errorf("healthy pod should not be reported as a problem, got %q", processed.Content)
	}
	if processed.Metadata["problem_count"].(int) != 2 {
		t.Errorf("problem_count = %v, want 2", processed.Metadata["problem_count"])
	}
}

func TestProcessProblemsMention_NoProblems(t *testing.T) {
	pod := newPod("matey-proxy", "matey", corev1.PodRunning, "", true)
	cs := k8sfake.NewSimpleClientset(pod)

	mp := newTestMentionProcessor(t, t.TempDir())
	mp.SetKubernetesClients(cs, nil)
	mp.SetNamespace("matey")

	processed, err := mp.ProcessMention(Mention{Type: MentionTypeProblems, Raw: "@problems"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(processed.Content, "No problem pods detected") {
		t.Errorf("expected explicit empty-problem message; got %q", processed.Content)
	}
}

// Failure path: no clientset injected, kube.LoadConfig will fail without a
// kubeconfig pointing at a reachable cluster. We point KUBECONFIG at an empty
// file to force a deterministic failure, then assert the error is honest and
// does NOT contain fake-success strings.
func TestProcessProblemsMention_HonestFailure(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig.yaml")
	if err := os.WriteFile(kubeconfig, []byte("invalid: yaml: ::: not a kubeconfig"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	t.Setenv("KUBECONFIG", kubeconfig)

	mp := newTestMentionProcessor(t, dir)
	mp.SetNamespace("matey")
	processed, err := mp.ProcessMention(Mention{Type: MentionTypeProblems, Raw: "@problems"})
	if err == nil {
		t.Fatalf("expected error when k8s config invalid; got content=%q", processed.Content)
	}
	if !strings.Contains(processed.Content, "@problems:") {
		t.Errorf("expected honest '@problems:' error prefix, got %q", processed.Content)
	}
	if strings.Contains(processed.Content, "Running (1/1)") {
		t.Errorf("must not return placeholder fake content; got %q", processed.Content)
	}
}

func TestProcessLogsMention_HappyPath(t *testing.T) {
	pod := newPod("proxy", "matey", corev1.PodRunning, "", true)
	cs := k8sfake.NewSimpleClientset(pod)

	mp := newTestMentionProcessor(t, t.TempDir())
	mp.SetKubernetesClients(cs, nil)
	mp.SetNamespace("matey")

	m := Mention{
		Type:     MentionTypeLogs,
		Raw:      "@logs:proxy",
		Metadata: map[string]interface{}{"service": "proxy"},
	}
	processed, err := mp.ProcessMention(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The fake clientset returns a canned "fake logs" stream from GetLogs;
	// we assert the wrapper context (namespace/pod) is present.
	if !strings.Contains(processed.Content, "matey/proxy") {
		t.Errorf("expected matey/proxy in logs header, got %q", processed.Content)
	}
	if processed.Metadata["pod"] != "proxy" {
		t.Errorf("expected pod metadata=proxy, got %v", processed.Metadata["pod"])
	}
}

func TestProcessLogsMention_NoPodFound(t *testing.T) {
	cs := k8sfake.NewSimpleClientset()
	mp := newTestMentionProcessor(t, t.TempDir())
	mp.SetKubernetesClients(cs, nil)
	mp.SetNamespace("matey")

	processed, err := mp.ProcessMention(Mention{
		Type:     MentionTypeLogs,
		Raw:      "@logs:proxy",
		Metadata: map[string]interface{}{"service": "proxy"},
	})
	if err == nil {
		t.Fatalf("expected error when no pods found, got content=%q", processed.Content)
	}
	if !strings.Contains(processed.Content, "no pod found matching") {
		t.Errorf("expected honest no-pod error, got %q", processed.Content)
	}
	if strings.Contains(processed.Content, "Service started successfully") {
		t.Errorf("must not return placeholder fake content; got %q", processed.Content)
	}
}

func TestProcessDefinitionMention_HappyPath(t *testing.T) {
	dir := t.TempDir()
	src := `package demo

func CalculateTotal(a, b int) int {
	return a + b
}

type Cart struct {
	Items []string
}
`
	if err := os.WriteFile(filepath.Join(dir, "demo.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	// Throwaway file in a pruned directory must not appear.
	if err := os.Mkdir(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "ignored.go"),
		[]byte("package x\nfunc CalculateTotal() {}\n"), 0o644); err != nil {
		t.Fatalf("write ignored: %v", err)
	}

	mp := newTestMentionProcessor(t, dir)
	m := Mention{Type: MentionTypeDefinition, Raw: "@def:CalculateTotal", Path: "CalculateTotal"}
	processed, err := mp.ProcessMention(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(processed.Content, "demo.go") {
		t.Errorf("expected hit in demo.go, got %q", processed.Content)
	}
	if !strings.Contains(processed.Content, "CalculateTotal") {
		t.Errorf("expected CalculateTotal in content, got %q", processed.Content)
	}
	if strings.Contains(processed.Content, "node_modules") {
		t.Errorf("node_modules should have been pruned, got %q", processed.Content)
	}
	if processed.Metadata["match_count"].(int) < 1 {
		t.Errorf("match_count should be >= 1, got %v", processed.Metadata["match_count"])
	}
}

func TestProcessDefinitionMention_NoMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "demo.go"),
		[]byte("package demo\nfunc Other() {}\n"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	mp := newTestMentionProcessor(t, dir)
	processed, err := mp.ProcessMention(Mention{Type: MentionTypeDefinition, Raw: "@def:Missing", Path: "Missing"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(processed.Content, "no matches found") {
		t.Errorf("expected explicit no-match message; got %q", processed.Content)
	}
	if strings.Contains(processed.Content, "would use tree-sitter") {
		t.Errorf("must not return placeholder fake content; got %q", processed.Content)
	}
}

// fakeMemoryStore satisfies the MemoryStore interface for tests so we don't
// have to spin up PostgreSQL.
type fakeMemoryStore struct {
	results []memory.SearchResult
	err     error
}

func (f *fakeMemoryStore) SearchNodes(query string) ([]memory.SearchResult, error) {
	return f.results, f.err
}

func TestProcessMemoryMention_HappyPath(t *testing.T) {
	store := &fakeMemoryStore{
		results: []memory.SearchResult{
			{
				Entity: memory.Entity{
					Name:         "Phil",
					EntityType:   "person",
					Observations: []string{"works on m8e"},
				},
				Relevance: 0.9,
				Matches:   []string{"works on m8e"},
			},
		},
	}
	mp := newTestMentionProcessor(t, t.TempDir())
	mp.SetMemoryStore(store)

	m := Mention{Type: MentionTypeMemory, Raw: "@memory:phil", Path: "phil"}
	processed, err := mp.ProcessMention(m)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(processed.Content, "Phil [person]") {
		t.Errorf("expected entity header in content, got %q", processed.Content)
	}
	if !strings.Contains(processed.Content, "works on m8e") {
		t.Errorf("expected matched observation in content, got %q", processed.Content)
	}
	if processed.Metadata["result_count"].(int) != 1 {
		t.Errorf("result_count = %v, want 1", processed.Metadata["result_count"])
	}
}

func TestProcessMemoryMention_StoreNotConfigured(t *testing.T) {
	mp := newTestMentionProcessor(t, t.TempDir())
	processed, err := mp.ProcessMention(Mention{Type: MentionTypeMemory, Raw: "@memory:phil", Path: "phil"})
	if err == nil {
		t.Fatalf("expected error when memory store not configured, got content=%q", processed.Content)
	}
	if !strings.Contains(processed.Content, "memory store is not configured") {
		t.Errorf("expected honest not-configured error, got %q", processed.Content)
	}
	if strings.Contains(processed.Content, "PostgreSQL knowledge graph") {
		t.Errorf("must not return placeholder fake content; got %q", processed.Content)
	}
}

func TestProcessMemoryMention_SearchError(t *testing.T) {
	store := &fakeMemoryStore{err: errStub("db down")}
	mp := newTestMentionProcessor(t, t.TempDir())
	mp.SetMemoryStore(store)
	processed, err := mp.ProcessMention(Mention{Type: MentionTypeMemory, Raw: "@memory:phil", Path: "phil"})
	if err == nil {
		t.Fatalf("expected propagated error, got content=%q", processed.Content)
	}
	if !strings.Contains(processed.Content, "search failed") {
		t.Errorf("expected search failed in content, got %q", processed.Content)
	}
}

type errStub string

func (e errStub) Error() string { return string(e) }

func TestProcessWorkflowMention_HappyPath(t *testing.T) {
	endTime := time.Now()
	startTime := endTime.Add(-2 * time.Minute)
	dur := endTime.Sub(startTime)
	scheduler := &crd.MCPTaskScheduler{
		ObjectMeta: metav1.ObjectMeta{Name: "matey-scheduler", Namespace: "matey"},
		Spec: crd.MCPTaskSchedulerSpec{
			Workflows: []crd.WorkflowDefinition{
				{Name: "nightly-backup", Schedule: "0 2 * * *", Enabled: true},
				{Name: "hourly-sync", Schedule: "@hourly", Enabled: false},
			},
		},
		Status: crd.MCPTaskSchedulerStatus{
			WorkflowExecutions: []crd.WorkflowExecution{
				{
					ID:           "run-1",
					WorkflowName: "nightly-backup",
					StartTime:    startTime,
					EndTime:      &endTime,
					Duration:     &dur,
					Phase:        crd.WorkflowPhaseSucceeded,
				},
			},
		},
	}
	scheme := newSchemeWithCRDs()
	c := ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(scheduler).Build()

	mp := newTestMentionProcessor(t, t.TempDir())
	mp.SetKubernetesClients(nil, c)
	mp.SetNamespace("matey")

	processed, err := mp.ProcessMention(Mention{Type: MentionTypeWorkflow, Raw: "@workflow"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(processed.Content, "nightly-backup") {
		t.Errorf("expected nightly-backup in content, got %q", processed.Content)
	}
	if !strings.Contains(processed.Content, "hourly-sync") {
		t.Errorf("expected hourly-sync in content, got %q", processed.Content)
	}
	if !strings.Contains(processed.Content, "Succeeded") {
		t.Errorf("expected Succeeded phase in content, got %q", processed.Content)
	}
	if !strings.Contains(processed.Content, "lastRun(time=") {
		t.Errorf("expected lastRun in content, got %q", processed.Content)
	}
}

func TestProcessWorkflowMention_NameFilter(t *testing.T) {
	scheduler := &crd.MCPTaskScheduler{
		ObjectMeta: metav1.ObjectMeta{Name: "matey-scheduler", Namespace: "matey"},
		Spec: crd.MCPTaskSchedulerSpec{
			Workflows: []crd.WorkflowDefinition{
				{Name: "a", Enabled: true},
				{Name: "b", Enabled: true},
			},
		},
	}
	scheme := newSchemeWithCRDs()
	c := ctrlfake.NewClientBuilder().WithScheme(scheme).WithObjects(scheduler).Build()
	mp := newTestMentionProcessor(t, t.TempDir())
	mp.SetKubernetesClients(nil, c)
	mp.SetNamespace("matey")
	processed, err := mp.ProcessMention(Mention{Type: MentionTypeWorkflow, Raw: "@workflow:a", Path: "a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(processed.Content, " a: ") {
		t.Errorf("expected workflow 'a' in content, got %q", processed.Content)
	}
	if strings.Contains(processed.Content, " b: ") {
		t.Errorf("workflow 'b' should be filtered out, got %q", processed.Content)
	}
}

func TestProcessWorkflowMention_HonestFailure(t *testing.T) {
	dir := t.TempDir()
	kubeconfig := filepath.Join(dir, "kubeconfig.yaml")
	if err := os.WriteFile(kubeconfig, []byte("not a kubeconfig"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
	t.Setenv("KUBECONFIG", kubeconfig)
	mp := newTestMentionProcessor(t, dir)
	mp.SetNamespace("matey")
	processed, err := mp.ProcessMention(Mention{Type: MentionTypeWorkflow, Raw: "@workflow"})
	if err == nil {
		t.Fatalf("expected error with bogus kubeconfig, got content=%q", processed.Content)
	}
	if !strings.Contains(processed.Content, "@workflow:") {
		t.Errorf("expected honest '@workflow:' prefix, got %q", processed.Content)
	}
	if strings.Contains(processed.Content, "would retrieve workflow status") {
		t.Errorf("must not return placeholder fake content; got %q", processed.Content)
	}
}
