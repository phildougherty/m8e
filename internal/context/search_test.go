package context

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// buildTestTree creates a temp directory tree and returns its root.
//
//	root/
//	  main.go
//	  README.md
//	  .hidden
//	  src/
//	    app.go
//	    util.js
//	  vendor/
//	    lib.go
func buildTestTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	writeFile := func(rel, content string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	writeFile("main.go", "package main")
	writeFile("README.md", "# readme")
	writeFile(".hidden", "secret")
	writeFile("src/app.go", "package src")
	writeFile("src/util.js", "console.log(1)")
	writeFile("vendor/lib.go", "package vendor")

	return root
}

func resultPaths(results []SearchResult) map[string]bool {
	m := make(map[string]bool)
	for _, r := range results {
		m[r.RelativePath] = true
	}
	return m
}

func TestNewFileDiscovery(t *testing.T) {
	root := buildTestTree(t)
	fd, err := NewFileDiscovery(root)
	if err != nil {
		t.Fatalf("NewFileDiscovery error: %v", err)
	}
	if fd.workDir != root {
		t.Errorf("workDir = %q, want %q", fd.workDir, root)
	}
}

func TestSearch_FindsAllNonHidden(t *testing.T) {
	root := buildTestTree(t)
	fd, _ := NewFileDiscovery(root)

	results, err := fd.Search(context.Background(), SearchOptions{Root: root})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}

	paths := resultPaths(results)
	for _, want := range []string{"main.go", "README.md", "src/app.go", "src/util.js", "vendor/lib.go"} {
		if !paths[want] {
			t.Errorf("expected %q in results, got %v", want, paths)
		}
	}
	// Hidden file must be excluded by default.
	if paths[".hidden"] {
		t.Errorf("hidden file should be excluded by default")
	}
}

func TestSearch_IncludeHidden(t *testing.T) {
	root := buildTestTree(t)
	fd, _ := NewFileDiscovery(root)

	results, err := fd.Search(context.Background(), SearchOptions{Root: root, IncludeHidden: true})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if !resultPaths(results)[".hidden"] {
		t.Errorf("expected .hidden to be included with IncludeHidden=true")
	}
}

func TestSearch_ExtensionFilter(t *testing.T) {
	root := buildTestTree(t)
	fd, _ := NewFileDiscovery(root)

	results, err := fd.Search(context.Background(), SearchOptions{Root: root, Extensions: []string{"go"}})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	paths := resultPaths(results)
	for _, want := range []string{"main.go", "src/app.go", "vendor/lib.go"} {
		if !paths[want] {
			t.Errorf("expected %q for go extension filter", want)
		}
	}
	if paths["README.md"] || paths["src/util.js"] {
		t.Errorf("non-go files should be filtered out, got %v", paths)
	}
}

func TestSearch_ExcludePattern(t *testing.T) {
	root := buildTestTree(t)
	fd, _ := NewFileDiscovery(root)

	results, err := fd.Search(context.Background(), SearchOptions{
		Root:    root,
		Exclude: []string{"vendor"},
	})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	paths := resultPaths(results)
	if paths["vendor/lib.go"] {
		t.Errorf("vendor/lib.go should be excluded")
	}
	if !paths["main.go"] {
		t.Errorf("main.go should still be present")
	}
}

func TestSearch_IncludePattern(t *testing.T) {
	// Include patterns are applied via shouldInclude. Test it directly: the
	// Search-level walk also applies includes to directories (pruning
	// traversal), so a unit check on shouldInclude is the meaningful assertion
	// for include-pattern matching of files.
	root := buildTestTree(t)
	fd, _ := NewFileDiscovery(root)

	include, err := fd.compilePatterns([]string{"*.md"})
	if err != nil {
		t.Fatalf("compilePatterns error: %v", err)
	}

	mdInfo, err := os.Stat(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	goInfo, err := os.Stat(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	opts := SearchOptions{Root: root}
	if !fd.shouldInclude(filepath.Join(root, "README.md"), "README.md", mdInfo, opts, include, nil) {
		t.Errorf("README.md should match include *.md")
	}
	if fd.shouldInclude(filepath.Join(root, "main.go"), "main.go", goInfo, opts, include, nil) {
		t.Errorf("main.go should not match include *.md")
	}
}

func TestSearch_ConcurrentMatchesSequential(t *testing.T) {
	root := buildTestTree(t)
	fd, _ := NewFileDiscovery(root)

	seq, err := fd.Search(context.Background(), SearchOptions{Root: root, Concurrent: false})
	if err != nil {
		t.Fatalf("sequential search error: %v", err)
	}
	conc, err := fd.Search(context.Background(), SearchOptions{Root: root, Concurrent: true})
	if err != nil {
		t.Fatalf("concurrent search error: %v", err)
	}

	if len(seq) != len(conc) {
		t.Errorf("sequential (%d) and concurrent (%d) result counts differ", len(seq), len(conc))
	}
	seqPaths, concPaths := resultPaths(seq), resultPaths(conc)
	for p := range seqPaths {
		if !concPaths[p] {
			t.Errorf("concurrent search missing %q", p)
		}
	}
}

func TestSearch_FuzzyPattern(t *testing.T) {
	root := buildTestTree(t)
	fd, _ := NewFileDiscovery(root)

	results, err := fd.Search(context.Background(), SearchOptions{Root: root, Pattern: "appgo"})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) == 0 {
		t.Fatalf("expected fuzzy matches for 'appgo'")
	}
	// src/app.go should be the top fuzzy match.
	if results[0].RelativePath != "src/app.go" {
		t.Errorf("top fuzzy result = %q, want src/app.go", results[0].RelativePath)
	}
}

func TestSearch_MaxResultsLimit(t *testing.T) {
	root := buildTestTree(t)
	fd, _ := NewFileDiscovery(root)

	// Use concurrent mode: it stops collecting at MaxResults*2 without
	// erroring, then the final slice is trimmed to MaxResults.
	results, err := fd.Search(context.Background(), SearchOptions{Root: root, MaxResults: 3, Concurrent: true})
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}
}

func TestSearchByExtension(t *testing.T) {
	root := buildTestTree(t)
	fd, _ := NewFileDiscovery(root)

	results, err := fd.SearchByExtension([]string{"js"}, 100)
	if err != nil {
		t.Fatalf("SearchByExtension error: %v", err)
	}
	paths := resultPaths(results)
	if !paths["src/util.js"] {
		t.Errorf("expected src/util.js, got %v", paths)
	}
	if paths["main.go"] {
		t.Errorf("go file should not appear in js extension search")
	}
}

func TestFindRecentFiles(t *testing.T) {
	root := buildTestTree(t)
	fd, _ := NewFileDiscovery(root)

	// Touch one file to make it clearly recent, and backdate another.
	recentFile := filepath.Join(root, "src", "app.go")
	now := time.Now()
	if err := os.Chtimes(recentFile, now, now); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	oldFile := filepath.Join(root, "main.go")
	past := now.Add(-72 * time.Hour)
	if err := os.Chtimes(oldFile, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	results, err := fd.FindRecentFiles(now.Add(-1*time.Hour), 100)
	if err != nil {
		t.Fatalf("FindRecentFiles error: %v", err)
	}
	paths := resultPaths(results)
	if !paths["src/app.go"] {
		t.Errorf("expected recent file src/app.go in results")
	}
	if paths["main.go"] {
		t.Errorf("backdated main.go should be excluded from recent files")
	}
}

func TestGetDirectoryStructure(t *testing.T) {
	root := buildTestTree(t)
	fd, _ := NewFileDiscovery(root)

	structure, err := fd.GetDirectoryStructure(root, 3)
	if err != nil {
		t.Fatalf("GetDirectoryStructure error: %v", err)
	}
	// Top level file present.
	if _, ok := structure["main.go"]; !ok {
		t.Errorf("expected main.go in directory structure")
	}
	// Sub-directory present with trailing slash.
	src, ok := structure["src/"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected src/ subdirectory in structure")
	}
	if _, ok := src["app.go"]; !ok {
		t.Errorf("expected src/app.go nested in structure")
	}
	// Hidden entries skipped.
	if _, ok := structure[".hidden"]; ok {
		t.Errorf("hidden files should be skipped from directory structure")
	}
}

func TestGlobToRegex(t *testing.T) {
	fd := &FileDiscovery{}
	tests := []struct {
		glob    string
		match   []string
		noMatch []string
	}{
		{"*.go", []string{"main.go", "x.go"}, []string{"main.js", "go"}},
		{"test?.go", []string{"test1.go", "testA.go"}, []string{"test.go", "test12.go"}},
		{"exact.txt", []string{"exact.txt"}, []string{"exactxtxt", "exact.txtx"}},
	}
	for _, tt := range tests {
		t.Run(tt.glob, func(t *testing.T) {
			patterns, err := fd.compilePatterns([]string{tt.glob})
			if err != nil {
				t.Fatalf("compilePatterns error: %v", err)
			}
			re := patterns[0]
			for _, m := range tt.match {
				if !re.MatchString(m) {
					t.Errorf("glob %q should match %q", tt.glob, m)
				}
			}
			for _, nm := range tt.noMatch {
				if re.MatchString(nm) {
					t.Errorf("glob %q should NOT match %q", tt.glob, nm)
				}
			}
		})
	}
}

func TestGitignorePatternToRegex(t *testing.T) {
	fd := &FileDiscovery{}

	re, err := fd.gitignorePatternToRegex("*.log")
	if err != nil {
		t.Fatalf("gitignorePatternToRegex error: %v", err)
	}
	if !re.MatchString("error.log") {
		t.Errorf("*.log should match error.log")
	}

	dirRe, err := fd.gitignorePatternToRegex("node_modules/")
	if err != nil {
		t.Fatalf("gitignorePatternToRegex error: %v", err)
	}
	if !dirRe.MatchString("node_modules/pkg/index.js") {
		t.Errorf("node_modules/ should match nested paths")
	}
	if !dirRe.MatchString("node_modules") {
		t.Errorf("node_modules/ should match the bare directory")
	}
}

func TestGitignore_RespectedInSearch(t *testing.T) {
	root := buildTestTree(t)
	// Add a .gitignore that excludes *.js files.
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.js\n# comment\n\n"), 0o644); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}
	fd, _ := NewFileDiscovery(root)
	// The .gitignore is loaded only if it's a git repo; load patterns directly
	// to exercise isGitIgnored regardless of repo presence.
	if err := fd.loadGitignore(); err != nil {
		t.Fatalf("loadGitignore error: %v", err)
	}

	if !fd.isGitIgnored("src/util.js") {
		t.Errorf("expected src/util.js to be gitignored by *.js pattern")
	}
	if fd.isGitIgnored("main.go") {
		t.Errorf("main.go should not be gitignored")
	}
	// Comment and blank lines must not have been compiled into patterns.
	if len(fd.gitignore) != 1 {
		t.Errorf("expected 1 gitignore pattern (comments/blanks skipped), got %d: %v", len(fd.gitignore), fd.gitignore)
	}
}

func TestGetFileType(t *testing.T) {
	root := buildTestTree(t)
	fd := &FileDiscovery{}

	dirInfo, err := os.Stat(filepath.Join(root, "src"))
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if fd.getFileType(dirInfo) != "directory" {
		t.Errorf("expected directory type")
	}

	fileInfo, err := os.Stat(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if fd.getFileType(fileInfo) != "file" {
		t.Errorf("expected file type")
	}
}

func TestCompilePatterns_Invalid(t *testing.T) {
	fd := &FileDiscovery{}
	// A glob with an unbalanced bracket compiles to invalid regex.
	_, err := fd.compilePatterns([]string{"[unclosed"})
	if err == nil {
		t.Errorf("expected error for invalid pattern")
	}
}

func TestSortResults_FilesBeforeDirectories(t *testing.T) {
	fd := &FileDiscovery{}
	results := []SearchResult{
		{RelativePath: "zdir", Type: "directory"},
		{RelativePath: "afile", Type: "file"},
	}
	fd.sortResults(results, "")
	if results[0].Type != "file" {
		t.Errorf("expected files sorted before directories, got %q first", results[0].Type)
	}
}

func TestGetGitInfo_NonGitDir(t *testing.T) {
	root := buildTestTree(t)
	fd, _ := NewFileDiscovery(root)

	// Not a git repo: GetRepoRoot falls back to workDir, branch is empty.
	if fd.GetRepoRoot() != root {
		t.Errorf("GetRepoRoot = %q, want %q", fd.GetRepoRoot(), root)
	}
	if fd.GetGitBranch() != "" {
		t.Errorf("GetGitBranch should be empty for non-git dir, got %q", fd.GetGitBranch())
	}
	if _, err := fd.GetRecentCommits(5); err == nil {
		t.Errorf("GetRecentCommits should error for non-git dir")
	}
}
