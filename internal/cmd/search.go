package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	appcontext "github.com/phildougherty/m8e/internal/context"
	"github.com/phildougherty/m8e/internal/treesitter"
	"github.com/phildougherty/m8e/internal/visual"
)

type searchFlags struct {
	pattern      string
	extensions   []string
	fuzzy        bool
	maxResults   int
	maxDepth     int
	showHidden   bool
	gitChanges   bool
	recent       bool
	since        string
	interactive  bool
	definitions  string
	outputFormat string
	includeSize  bool
	includeGit   bool
	sortBy       string
	reverse      bool
}

// NewSearchCommand creates the search command
func NewSearchCommand() *cobra.Command {
	var searchOpts searchFlags

	searchCmd := &cobra.Command{
	Use:   "search [PATTERN]",
	Short: "Intelligent file discovery with fuzzy search and Git integration",
	Long: `Advanced file search with enterprise features:
- Fuzzy matching with relevance scoring
- Git-aware search with status integration
- File type filtering and extension-based search
- Interactive file browser with preview
- Context-aware file discovery
- Tree-sitter integration for code search

Examples:
  matey search main.go                     # Find files matching "main.go"
  matey search --fuzzy "main"              # Fuzzy search for files containing "main"
  matey search --ext go,py --pattern func # Search Go/Python files for "func"
  matey search --recent --since 24h       # Find files modified in last 24 hours
  matey search --interactive               # Launch interactive file browser
  matey search --git-changes               # Show only Git-modified files
  matey search --definitions "Handler"     # Find code definitions matching "Handler"`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSearchCommand(cmd, args, searchOpts)
	},
}

	// Set up flags

	searchCmd.Flags().StringVar(&searchOpts.pattern, "pattern", "", "Search pattern (alternative to positional argument)")
	searchCmd.Flags().StringSliceVar(&searchOpts.extensions, "ext", nil, "File extensions to include (e.g., go,py,js)")
	searchCmd.Flags().BoolVar(&searchOpts.fuzzy, "fuzzy", true, "Enable fuzzy matching")
	searchCmd.Flags().IntVar(&searchOpts.maxResults, "max-results", 100, "Maximum number of results to return")
	searchCmd.Flags().IntVar(&searchOpts.maxDepth, "max-depth", 10, "Maximum directory depth to search")
	searchCmd.Flags().BoolVar(&searchOpts.showHidden, "hidden", false, "Include hidden files and directories")
	searchCmd.Flags().BoolVar(&searchOpts.gitChanges, "git-changes", false, "Show only Git-modified files")
	searchCmd.Flags().BoolVar(&searchOpts.recent, "recent", false, "Search for recently modified files")
	searchCmd.Flags().StringVar(&searchOpts.since, "since", "24h", "Time duration for recent files (e.g., 2h, 1d, 1w)")
	searchCmd.Flags().BoolVar(&searchOpts.interactive, "interactive", false, "Launch interactive file browser")
	searchCmd.Flags().StringVar(&searchOpts.definitions, "definitions", "", "Search for code definitions (functions, types, etc.)")
	searchCmd.Flags().StringVar(&searchOpts.outputFormat, "format", "table", "Output format: table, json, paths")
	searchCmd.Flags().BoolVar(&searchOpts.includeSize, "size", true, "Include file size in output")
	searchCmd.Flags().BoolVar(&searchOpts.includeGit, "git-status", false, "Include Git status in output")
	searchCmd.Flags().StringVar(&searchOpts.sortBy, "sort", "relevance", "Sort by: relevance, name, size, modified")
	searchCmd.Flags().BoolVar(&searchOpts.reverse, "reverse", false, "Reverse sort order")

	return searchCmd
}

func runSearchCommand(cmd *cobra.Command, args []string, searchOpts searchFlags) error {
	// Determine search pattern
	pattern := searchOpts.pattern
	if len(args) > 0 {
		pattern = args[0]
	}

	if pattern == "" && !searchOpts.recent && !searchOpts.gitChanges && !searchOpts.interactive {
		return fmt.Errorf("search pattern is required (or use --recent, --git-changes, or --interactive)")
	}

	// Initialize file discovery
	fileDiscovery, err := appcontext.NewFileDiscovery(".")
	if err != nil {
		return fmt.Errorf("failed to initialize file discovery: %w", err)
	}

	// Handle interactive mode
	if searchOpts.interactive {
		return runInteractiveSearch(fileDiscovery, pattern, searchOpts)
	}

	// Handle definitions search
	if searchOpts.definitions != "" {
		return runDefinitionsSearch(searchOpts.definitions)
	}

	// Handle recent files search
	if searchOpts.recent {
		return runRecentFilesSearch(fileDiscovery, searchOpts)
	}

	// Handle Git changes search
	if searchOpts.gitChanges {
		return runGitChangesSearch(fileDiscovery, pattern, searchOpts)
	}

	// Run standard file search
	return runStandardSearch(fileDiscovery, pattern, searchOpts)
}

func runStandardSearch(fileDiscovery *appcontext.FileDiscovery, pattern string, opts searchFlags) error {
	// Configure search options
	searchOptions := appcontext.SearchOptions{
		Pattern:          pattern,
		Extensions:       opts.extensions,
		MaxResults:       opts.maxResults,
		MaxDepth:         opts.maxDepth,
		IncludeHidden:    opts.showHidden,
		RespectGitignore: true,
		IncludeGitStatus: opts.includeGit,
		Concurrent:       true,
		Timeout:          30 * time.Second,
	}

	// Perform search
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results, err := fileDiscovery.Search(ctx, searchOptions)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	// Sort results
	sortResults(results, opts)

	// Display results
	return displaySearchResults(results, pattern, opts)
}

func runRecentFilesSearch(fileDiscovery *appcontext.FileDiscovery, opts searchFlags) error {
	// Parse duration
	duration, err := time.ParseDuration(opts.since)
	if err != nil {
		return fmt.Errorf("invalid duration format: %s", opts.since)
	}

	since := time.Now().Add(-duration)

	// Find recent files
	results, err := fileDiscovery.FindRecentFiles(since, opts.maxResults)
	if err != nil {
		return fmt.Errorf("failed to find recent files: %w", err)
	}

	// Sort by modification time (newest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].ModTime.After(results[j].ModTime)
	})

	fmt.Printf("📅 Files modified since %s (%d found):\n\n", formatSearchDuration(duration), len(results))
	return displaySearchResults(results, "", opts)
}

func runGitChangesSearch(fileDiscovery *appcontext.FileDiscovery, pattern string, opts searchFlags) error {
	// Configure search for Git changes
	searchOptions := appcontext.SearchOptions{
		Pattern:          pattern,
		Extensions:       opts.extensions,
		MaxResults:       opts.maxResults,
		IncludeHidden:    false,
		RespectGitignore: false, // We want to see changed files even if they're normally ignored
		IncludeGitStatus: true,
		Concurrent:       true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results, err := fileDiscovery.Search(ctx, searchOptions)
	if err != nil {
		return fmt.Errorf("Git search failed: %w", err)
	}

	// Filter for files with Git changes
	var changedFiles []appcontext.SearchResult
	for _, result := range results {
		if result.GitStatus != "" && result.GitStatus != "  " {
			changedFiles = append(changedFiles, result)
		}
	}

	fmt.Printf("🔄 Git-modified files (%d found):\n\n", len(changedFiles))
	return displaySearchResults(changedFiles, pattern, opts)
}

func runDefinitionsSearch(query string) error {
	// Initialize tree-sitter parser
	parser, err := treesitter.NewTreeSitterParser(treesitter.ParserConfig{
		CacheSize:   50,
		MaxFileSize: 1024 * 1024, // 1MB
	})
	if err != nil {
		return fmt.Errorf("failed to initialize parser: %w", err)
	}

	// Find source files to search
	fileDiscovery, err := appcontext.NewFileDiscovery(".")
	if err != nil {
		return fmt.Errorf("failed to initialize file discovery: %w", err)
	}

	// Search for source files
	searchOptions := appcontext.SearchOptions{
		Extensions:    []string{"go", "py", "js", "ts", "rs"},
		MaxResults:    500,
		IncludeHidden: false,
		Concurrent:    true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	files, err := fileDiscovery.Search(ctx, searchOptions)
	if err != nil {
		return fmt.Errorf("failed to find source files: %w", err)
	}

	fmt.Printf("🔍 Searching for definitions matching '%s' in %d files...\n\n", query, len(files))

	var definitions []DefinitionResult
	queryLower := strings.ToLower(query)

	// Search each file for definitions
	for _, file := range files {
		if file.Type != "file" {
			continue
		}

		result, err := parser.ParseFile(file.Path)
		if err != nil {
			continue // Skip files that can't be parsed
		}

		// Extract definitions using tree-sitter
		defExtractor := treesitter.NewDefinitionExtractor(parser)
		fileDefs, err := defExtractor.ExtractDefinitions(result)
		if err != nil {
			continue
		}

		// Filter definitions matching query
		for _, def := range fileDefs {
			if strings.Contains(strings.ToLower(def.Name), queryLower) {
				definitions = append(definitions, DefinitionResult{
					Name:         def.Name,
					Type:         string(def.Type),
					FilePath:     file.RelativePath,
					Line:         int(def.StartLine),
					Column:       int(def.StartColumn),
					Signature:    def.Signature,
					DocString:    def.DocString,
				})
			}
		}
	}

	// Sort definitions by relevance
	sort.Slice(definitions, func(i, j int) bool {
		return strings.Contains(strings.ToLower(definitions[i].Name), queryLower) &&
			!strings.Contains(strings.ToLower(definitions[j].Name), queryLower)
	})

	return displayDefinitionResults(definitions, query)
}

func runInteractiveSearch(fileDiscovery *appcontext.FileDiscovery, initialPattern string, opts searchFlags) error {
	// Initialize tree-sitter parser for preview
	parser, err := treesitter.NewTreeSitterParser(treesitter.ParserConfig{
		CacheSize:   50,
		MaxFileSize: 1024 * 1024,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize parser: %w", err)
	}

	// Configure file view
	config := visual.FileViewConfig{
		RootPath:         ".",
		Mode:             visual.FileViewModeSearch,
		ShowHidden:       opts.showHidden,
		Extensions:       opts.extensions,
		EnablePreview:    true,
		MaxPreviewLines:  20,
		PreviewSyntax:    true,
	}

	// Run interactive file browser
	result, err := visual.RunFileView(config, fileDiscovery, parser)
	if err != nil {
		return fmt.Errorf("interactive search failed: %w", err)
	}

	// Handle result
	if result.SelectedFile != nil {
		fmt.Printf("📄 Selected file: %s\n", result.SelectedFile.Path)
	} else {
		fmt.Println("No file selected")
	}

	return nil
}

func sortResults(results []appcontext.SearchResult, opts searchFlags) {
	sort.Slice(results, func(i, j int) bool {
		switch opts.sortBy {
		case "name":
			result := strings.Compare(results[i].RelativePath, results[j].RelativePath)
			return opts.reverse != (result < 0)
		case "size":
			result := results[i].Size < results[j].Size
			return opts.reverse != result
		case "modified":
			result := results[i].ModTime.Before(results[j].ModTime)
			return opts.reverse != result
		case "relevance":
			fallthrough
		default:
			// Sort by score (higher is better), then by name
			if results[i].Score != results[j].Score {
				result := results[i].Score > results[j].Score
				return opts.reverse != result
			}
			result := strings.Compare(results[i].RelativePath, results[j].RelativePath)
			return opts.reverse != (result < 0)
		}
	})
}

func displaySearchResults(results []appcontext.SearchResult, pattern string, opts searchFlags) error {
	if len(results) == 0 {
		fmt.Println("No files found matching the search criteria")
		return nil
	}

	switch opts.outputFormat {
	case "json":
		return displaySearchJSONResults(results)
	case "paths":
		return displayPathsResults(results)
	default:
		return displaySearchTableResults(results, pattern, opts)
	}
}

func displaySearchTableResults(results []appcontext.SearchResult, pattern string, opts searchFlags) error {
	if pattern != "" {
		fmt.Printf("🔍 Search results for '%s' (%d found):\n\n", pattern, len(results))
	}

	// Calculate column widths
	maxPathLen := 4 // "Path"
	maxSizeLen := 4 // "Size"
	maxGitLen := 3  // "Git"

	for _, result := range results {
		if len(result.RelativePath) > maxPathLen {
			maxPathLen = len(result.RelativePath)
		}
		sizeStr := formatFileSize(result.Size)
		if len(sizeStr) > maxSizeLen {
			maxSizeLen = len(sizeStr)
		}
		if len(result.GitStatus) > maxGitLen {
			maxGitLen = len(result.GitStatus)
		}
	}

	// Limit column widths
	if maxPathLen > 60 {
		maxPathLen = 60
	}

	// Print header
	headerFormat := fmt.Sprintf("%%-%ds", maxPathLen)
	fmt.Printf(headerFormat, "Path")

	if opts.includeSize {
		fmt.Printf("  %%-%ds", maxSizeLen)
		fmt.Printf(fmt.Sprintf("%%-%ds", maxSizeLen), "Size")
	}

	if opts.includeGit {
		fmt.Printf("  %%-%ds", maxGitLen)
		fmt.Printf(fmt.Sprintf("%%-%ds", maxGitLen), "Git")
	}

	fmt.Printf("  Modified\n")

	// Print separator
	fmt.Print(strings.Repeat("─", maxPathLen))
	if opts.includeSize {
		fmt.Printf("  %s", strings.Repeat("─", maxSizeLen))
	}
	if opts.includeGit {
		fmt.Printf("  %s", strings.Repeat("─", maxGitLen))
	}
	fmt.Printf("  ────────────\n")

	// Print results
	for _, result := range results {
		// Truncate path if necessary
		path := result.RelativePath
		if len(path) > maxPathLen {
			path = "..." + path[len(path)-maxPathLen+3:]
		}

		fmt.Printf(headerFormat, path)

		if opts.includeSize {
			sizeStr := formatFileSize(result.Size)
			fmt.Printf(fmt.Sprintf("  %%-%ds", maxSizeLen), sizeStr)
		}

		if opts.includeGit {
			gitStatus := result.GitStatus
			if gitStatus == "" {
				gitStatus = "--"
			}
			fmt.Printf(fmt.Sprintf("  %%-%ds", maxGitLen), gitStatus)
		}

		fmt.Printf("  %s", result.ModTime.Format("2006-01-02 15:04"))

		// Show score if relevant
		if result.Score > 0 {
			fmt.Printf(" (score: %d)", result.Score)
		}

		fmt.Println()
	}

	return nil
}

func displaySearchJSONResults(results []appcontext.SearchResult) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(results)
}

func displayPathsResults(results []appcontext.SearchResult) error {
	for _, result := range results {
		fmt.Println(result.RelativePath)
	}
	return nil
}

type DefinitionResult struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	FilePath  string `json:"file_path"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	Signature string `json:"signature,omitempty"`
	DocString string `json:"doc_string,omitempty"`
}

func displayDefinitionResults(definitions []DefinitionResult, query string) error {
	if len(definitions) == 0 {
		fmt.Printf("No definitions found matching '%s'\n", query)
		return nil
	}

	fmt.Printf("📋 Found %d definitions matching '%s':\n\n", len(definitions), query)

	// Group by file
	fileGroups := make(map[string][]DefinitionResult)
	for _, def := range definitions {
		fileGroups[def.FilePath] = append(fileGroups[def.FilePath], def)
	}

	// Display grouped results
	for filePath, defs := range fileGroups {
		fmt.Printf("📄 %s:\n", filePath)
		for _, def := range defs {
			fmt.Printf("  %s %s", formatDefinitionType(def.Type), def.Name)
			if def.Signature != "" {
				fmt.Printf(" %s", def.Signature)
			}
			fmt.Printf(" (line %d:%d)", def.Line, def.Column)
			if def.DocString != "" {
				fmt.Printf("\n    %s", truncateString(def.DocString, 80))
			}
			fmt.Println()
		}
		fmt.Println()
	}

	return nil
}

func formatDefinitionType(defType string) string {
	switch defType {
	case "function":
		return "🔧"
	case "method":
		return "⚙️"
	case "type":
		return "📦"
	case "struct":
		return "🏗️"
	case "interface":
		return "🔌"
	case "constant":
		return "🔒"
	case "variable":
		return "📋"
	default:
		return "❓"
	}
}

func formatFileSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%dB", size)
	} else if size < 1024*1024 {
		return fmt.Sprintf("%.1fK", float64(size)/1024)
	} else {
		return fmt.Sprintf("%.1fM", float64(size)/(1024*1024))
	}
}

func formatSearchDuration(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%.0fm", d.Minutes())
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%.1fh", d.Hours())
	} else {
		return fmt.Sprintf("%.1fd", d.Hours()/24)
	}
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}