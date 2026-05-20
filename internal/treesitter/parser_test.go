package treesitter

import (
	"strings"
	"testing"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
)

func newTestParser(t *testing.T) *TreeSitterParser {
	t.Helper()
	parser, err := NewTreeSitterParser(ParserConfig{})
	if err != nil {
		t.Fatalf("NewTreeSitterParser: %v", err)
	}
	return parser
}

func TestNewTreeSitterParserDefaults(t *testing.T) {
	parser, err := NewTreeSitterParser(ParserConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if parser.config.CacheSize != 100 {
		t.Errorf("CacheSize default = %d, want 100", parser.config.CacheSize)
	}
	if parser.config.ParseTimeout != 10*time.Second {
		t.Errorf("ParseTimeout default = %v, want 10s", parser.config.ParseTimeout)
	}
	if parser.config.MaxFileSize != 1024*1024 {
		t.Errorf("MaxFileSize default = %d, want 1MB", parser.config.MaxFileSize)
	}
	// Non-lazy load should have initialized all four languages.
	if len(parser.parsers) != 4 {
		t.Errorf("expected 4 initialized parsers, got %d", len(parser.parsers))
	}
}

func TestNewTreeSitterParserLazyLoad(t *testing.T) {
	parser, err := NewTreeSitterParser(ParserConfig{LazyLoad: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(parser.parsers) != 0 {
		t.Errorf("lazy load should not pre-initialize parsers, got %d", len(parser.parsers))
	}
	// First parse should lazily load the Go parser.
	res, err := parser.ParseContent("package main\n", LanguageGo, "x.go")
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	if res.RootNode == nil {
		t.Fatal("expected root node after lazy parse")
	}
	if _, ok := parser.parsers[LanguageGo]; !ok {
		t.Error("Go parser was not lazily loaded")
	}
}

func TestDetectLanguage(t *testing.T) {
	parser := newTestParser(t)
	cases := []struct {
		path string
		want Language
	}{
		{"main.go", LanguageGo},
		{"/abs/path/server.go", LanguageGo},
		{"script.py", LanguagePython},
		{"types.pyi", LanguagePython},
		{"app.js", LanguageJavaScript},
		{"mod.mjs", LanguageJavaScript},
		{"component.tsx", LanguageJavaScript},
		{"types.ts", LanguageJavaScript},
		{"lib.rs", LanguageRust},
		{"main.c", LanguageC},
		{"header.h", LanguageC},
		{"engine.cpp", LanguageCPP},
		{"App.java", LanguageJava},
		{"Dockerfile", LanguageUnknown},
		{"config.yaml", LanguageUnknown},
		{"notes.txt", LanguageUnknown},
		{"noext", LanguageUnknown},
		{"data.JSON", LanguageUnknown},
	}
	for _, c := range cases {
		if got := parser.DetectLanguage(c.path); got != c.want {
			t.Errorf("DetectLanguage(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestIsLanguageSupported(t *testing.T) {
	parser := newTestParser(t)
	supported := map[Language]bool{
		LanguageGo: true, LanguagePython: true,
		LanguageJavaScript: true, LanguageRust: true,
		LanguageC: false, LanguageCPP: false,
		LanguageJava: false, LanguageUnknown: false,
	}
	for lang, want := range supported {
		if got := parser.IsLanguageSupported(lang); got != want {
			t.Errorf("IsLanguageSupported(%q) = %v, want %v", lang, got, want)
		}
	}
	if len(parser.GetSupportedLanguages()) != 4 {
		t.Errorf("GetSupportedLanguages len = %d, want 4", len(parser.GetSupportedLanguages()))
	}
}

func TestParseContentGo(t *testing.T) {
	parser := newTestParser(t)
	src := "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	res, err := parser.ParseContent(src, LanguageGo, "main.go")
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	if res.Language != LanguageGo {
		t.Errorf("Language = %q, want go", res.Language)
	}
	if res.FilePath != "main.go" {
		t.Errorf("FilePath = %q", res.FilePath)
	}
	if res.RootNode == nil || res.Tree == nil {
		t.Fatal("expected non-nil tree and root node")
	}
	if res.RootNode.Type() != "source_file" {
		t.Errorf("root type = %q, want source_file", res.RootNode.Type())
	}
	if res.RootNode.HasError() {
		t.Error("valid Go source should not produce parse errors")
	}
	if got := res.Metadata["lines"]; got != 6 {
		t.Errorf("metadata lines = %v, want 6", got)
	}
	if got := res.Metadata["size"]; got != len(src) {
		t.Errorf("metadata size = %v, want %d", got, len(src))
	}
	if res.NodeCount == 0 {
		t.Error("expected non-zero NodeCount")
	}
}

func TestParseContentInvalidSyntax(t *testing.T) {
	parser := newTestParser(t)
	// Syntactically broken Go: tree-sitter is error-tolerant, so it still
	// returns a tree but the root node should report an error.
	src := "package main\nfunc broken( {\n"
	res, err := parser.ParseContent(src, LanguageGo, "broken.go")
	if err != nil {
		t.Fatalf("ParseContent should tolerate invalid syntax, got: %v", err)
	}
	if res.RootNode == nil {
		t.Fatal("expected a (partial) root node for invalid syntax")
	}
	if !res.RootNode.HasError() {
		t.Error("expected HasError() to be true for broken syntax")
	}
}

func TestParseContentEmpty(t *testing.T) {
	parser := newTestParser(t)
	res, err := parser.ParseContent("", LanguageGo, "empty.go")
	if err != nil {
		t.Fatalf("ParseContent empty: %v", err)
	}
	if res.RootNode == nil {
		t.Fatal("expected root node even for empty input")
	}
	if res.RootNode.ChildCount() != 0 {
		t.Errorf("empty file should have 0 children, got %d", res.RootNode.ChildCount())
	}
	if got := res.Metadata["lines"]; got != 1 {
		t.Errorf("empty content lines = %v, want 1", got)
	}
}

func TestParseContentTooLarge(t *testing.T) {
	parser, err := NewTreeSitterParser(ParserConfig{MaxFileSize: 10})
	if err != nil {
		t.Fatalf("NewTreeSitterParser: %v", err)
	}
	_, err = parser.ParseContent(strings.Repeat("x", 50), LanguageGo, "big.go")
	if err == nil {
		t.Fatal("expected error for oversized content")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error = %q, want 'too large'", err.Error())
	}
}

func TestParseContentUnsupportedLanguage(t *testing.T) {
	// Non-lazy parser: unknown language has no registered parser.
	parser := newTestParser(t)
	_, err := parser.ParseContent("whatever", LanguageJava, "App.java")
	if err == nil {
		t.Fatal("expected error for unsupported language")
	}
}

func TestParseFileUnsupportedExtension(t *testing.T) {
	parser := newTestParser(t)
	_, err := parser.ParseFile("notes.txt")
	if err == nil {
		t.Fatal("expected error for unsupported file extension")
	}
	if !strings.Contains(err.Error(), "unsupported language") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestParseContentCaching(t *testing.T) {
	parser := newTestParser(t)
	src := "package main\n"
	r1, err := parser.ParseContent(src, LanguageGo, "cached.go")
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	stats := parser.GetCacheStats()
	if stats["cached_files"] != 1 {
		t.Errorf("cached_files = %v, want 1", stats["cached_files"])
	}
	// ParseFile hits the cache and returns the identical pointer.
	r2, err := parser.ParseFile("cached.go")
	if err != nil {
		t.Fatalf("ParseFile cached: %v", err)
	}
	if r1 != r2 {
		t.Error("expected cached result to be the same pointer")
	}
	parser.ClearCache()
	if parser.GetCacheStats()["cached_files"] != 0 {
		t.Error("ClearCache did not empty the cache")
	}
}

func TestCacheEviction(t *testing.T) {
	parser, err := NewTreeSitterParser(ParserConfig{CacheSize: 2})
	if err != nil {
		t.Fatalf("NewTreeSitterParser: %v", err)
	}
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		if _, err := parser.ParseContent("package main\n", LanguageGo, name); err != nil {
			t.Fatalf("ParseContent %s: %v", name, err)
		}
	}
	// Cache size capped at 2; eviction keeps it from exceeding the limit.
	if n := parser.GetCacheStats()["cached_files"].(int); n > 2 {
		t.Errorf("cache size = %d, want <= 2", n)
	}
}

func TestGetNodeTextAndLocation(t *testing.T) {
	parser := newTestParser(t)
	src := "package main\n\nfunc Hello() {}\n"
	res, err := parser.ParseContent(src, LanguageGo, "h.go")
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	funcs := parser.FindNodesOfType(res.RootNode, "function_declaration")
	if len(funcs) != 1 {
		t.Fatalf("expected 1 function_declaration, got %d", len(funcs))
	}
	text := parser.GetNodeText(funcs[0], src)
	if text != "func Hello() {}" {
		t.Errorf("GetNodeText = %q, want %q", text, "func Hello() {}")
	}
	startLine, startCol, endLine, endCol := parser.GetNodeLocation(funcs[0])
	if startLine != 3 {
		t.Errorf("startLine = %d, want 3", startLine)
	}
	if startCol != 1 {
		t.Errorf("startCol = %d, want 1", startCol)
	}
	if endLine != 3 {
		t.Errorf("endLine = %d, want 3", endLine)
	}
	if endCol <= startCol {
		t.Errorf("endCol %d should be > startCol %d", endCol, startCol)
	}

	// nil node guards.
	if parser.GetNodeText(nil, src) != "" {
		t.Error("GetNodeText(nil) should be empty")
	}
	if sl, sc, el, ec := parser.GetNodeLocation(nil); sl|sc|el|ec != 0 {
		t.Error("GetNodeLocation(nil) should be all zero")
	}
}

func TestFindNodesAndChildHelpers(t *testing.T) {
	parser := newTestParser(t)
	src := "package main\n\nfunc A() {}\nfunc B() {}\n"
	res, err := parser.ParseContent(src, LanguageGo, "f.go")
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	funcs := parser.FindNodesOfType(res.RootNode, "function_declaration")
	if len(funcs) != 2 {
		t.Fatalf("FindNodesOfType found %d functions, want 2", len(funcs))
	}

	// GetChildByType: each function_declaration has an identifier child.
	id := parser.GetChildByType(funcs[0], "identifier")
	if id == nil {
		t.Fatal("expected identifier child")
	}
	if name := parser.GetNodeText(id, src); name != "A" {
		t.Errorf("first function name = %q, want A", name)
	}

	// GetChildrenByType on the root: two function_declaration children.
	children := parser.GetChildrenByType(res.RootNode, "function_declaration")
	if len(children) != 2 {
		t.Errorf("GetChildrenByType = %d, want 2", len(children))
	}

	if parser.GetChildByType(nil, "identifier") != nil {
		t.Error("GetChildByType(nil) should be nil")
	}
	if parser.GetChildrenByType(nil, "identifier") != nil {
		t.Error("GetChildrenByType(nil) should be nil")
	}
}

func TestTraverseTreeStopsOnFalse(t *testing.T) {
	parser := newTestParser(t)
	src := "package main\nfunc A() {}\n"
	res, err := parser.ParseContent(src, LanguageGo, "t.go")
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	// Returning false at the root prevents descent into children.
	visited := 0
	parser.TraverseTree(res.RootNode, func(n *sitter.Node, depth int) bool {
		visited++
		return false
	})
	if visited != 1 {
		t.Errorf("visited = %d, want 1 (traversal should stop)", visited)
	}

	// Full traversal visits more than one node.
	visited = 0
	parser.TraverseTree(res.RootNode, func(n *sitter.Node, depth int) bool {
		visited++
		return true
	})
	if visited < 3 {
		t.Errorf("full traversal visited %d nodes, expected several", visited)
	}
}

func TestNodeToString(t *testing.T) {
	parser := newTestParser(t)
	src := "package main\n"
	res, err := parser.ParseContent(src, LanguageGo, "n.go")
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	out := parser.NodeToString(res.RootNode, src, 2)
	if !strings.Contains(out, "source_file") {
		t.Errorf("NodeToString output missing source_file: %q", out)
	}
	if !strings.Contains(out, "package_clause") {
		t.Errorf("NodeToString output missing package_clause: %q", out)
	}
	if parser.NodeToString(nil, src, 2) != "" {
		t.Error("NodeToString(nil) should be empty")
	}
}

func TestParsePythonAndRustAndJS(t *testing.T) {
	parser := newTestParser(t)
	cases := []struct {
		lang     Language
		src      string
		rootType string
	}{
		{LanguagePython, "def foo():\n    pass\n", "module"},
		{LanguageRust, "fn main() {}\n", "source_file"},
		{LanguageJavaScript, "function foo() {}\n", "program"},
	}
	for _, c := range cases {
		res, err := parser.ParseContent(c.src, c.lang, "x")
		if err != nil {
			t.Fatalf("ParseContent %s: %v", c.lang, err)
		}
		if res.RootNode.Type() != c.rootType {
			t.Errorf("%s root type = %q, want %q", c.lang, res.RootNode.Type(), c.rootType)
		}
		if res.RootNode.HasError() {
			t.Errorf("%s: valid source should not have parse errors", c.lang)
		}
	}
}
