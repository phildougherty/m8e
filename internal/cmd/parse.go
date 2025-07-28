package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	sitter "github.com/smacker/go-tree-sitter"

	"github.com/phildougherty/m8e/internal/treesitter"
)

type parseFlags struct {
	language        string
	outputFormat    string
	showDefinitions bool
	showMetrics     bool
	queryType       string
	interactive     bool
	cacheSize       int
	maxFileSize     int64
}

// NewParseCommand creates the parse command with full tree-sitter integration
func NewParseCommand() *cobra.Command {
	var parseOpts parseFlags

	parseCmd := &cobra.Command{
		Use:   "parse [FILE]",
		Short: "Advanced code analysis using tree-sitter parsing",
		Long: `Analyze code files with enterprise tree-sitter features:
- Multi-language AST parsing (Go, Python, JavaScript, Rust)
- Code definition extraction and analysis
- Custom query execution and pattern matching
- Syntax validation and error detection
- Performance metrics and caching

Examples:
  matey parse src/main.go                      # Parse Go file with default output
  matey parse --format=json lib.py            # JSON output for Python file
  matey parse --definitions handler.go        # Extract code definitions only
  matey parse --query=functions *.go          # Find all functions in Go files
  matey parse --metrics --language=python app.py  # Show parsing metrics`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runParseCommand(args[0], parseOpts)
		},
	}

	// Core parsing flags
	parseCmd.Flags().StringVar(&parseOpts.language, "language", "", "Override language detection (go, python, javascript, rust)")
	parseCmd.Flags().StringVar(&parseOpts.outputFormat, "format", "text", "Output format: text, json, tree")
	parseCmd.Flags().BoolVar(&parseOpts.showDefinitions, "definitions", false, "Extract and show code definitions")
	parseCmd.Flags().BoolVar(&parseOpts.showMetrics, "metrics", false, "Show parsing performance metrics")
	parseCmd.Flags().StringVar(&parseOpts.queryType, "query", "", "Execute predefined query: functions, classes, imports, all")
	parseCmd.Flags().BoolVar(&parseOpts.interactive, "interactive", false, "Launch interactive AST explorer")
	
	// Performance tuning flags
	parseCmd.Flags().IntVar(&parseOpts.cacheSize, "cache-size", 100, "Parser cache size")
	parseCmd.Flags().Int64Var(&parseOpts.maxFileSize, "max-file-size", 1048576, "Maximum file size in bytes (1MB default)")

	return parseCmd
}

func runParseCommand(filePath string, opts parseFlags) error {
	// Initialize parser with configuration
	config := treesitter.ParserConfig{
		CacheSize:     opts.cacheSize,
		ParseTimeout:  10 * time.Second,
		MaxFileSize:   opts.maxFileSize,
		EnableLogging: false,
		LazyLoad:      true,
	}

	parser, err := treesitter.NewTreeSitterParser(config)
	if err != nil {
		return fmt.Errorf("failed to initialize tree-sitter parser: %w", err)
	}

	// Override language detection if specified
	var detectedLanguage treesitter.Language
	if opts.language != "" {
		detectedLanguage = parseLanguageString(opts.language)
	} else {
		detectedLanguage = parser.DetectLanguage(filePath)
	}

	fmt.Printf("🔍 Parsing %s (detected language: %s)\n", filePath, detectedLanguage)

	// Parse the file
	parseStart := time.Now()
	result, err := parser.ParseFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to parse file: %w", err)
	}
	parseTime := time.Since(parseStart)

	// Handle interactive mode
	if opts.interactive {
		return runInteractiveParse(parser, result)
	}

	// Handle definitions extraction
	if opts.showDefinitions {
		return showDefinitions(parser, result, opts.outputFormat)
	}

	// Handle query execution
	if opts.queryType != "" {
		return executeQuery(parser, result, opts.queryType, opts.outputFormat)
	}

	// Show general parse results
	switch opts.outputFormat {
	case "json":
		return showJSONOutput(result, parseTime, opts.showMetrics)
	case "tree":
		return showTreeOutput(result, parser)
	default:
		return showTextOutput(result, parseTime, opts.showMetrics)
	}
}

func parseLanguageString(lang string) treesitter.Language {
	switch strings.ToLower(lang) {
	case "go", "golang":
		return treesitter.LanguageGo
	case "python", "py":
		return treesitter.LanguagePython
	case "javascript", "js", "typescript", "ts":
		return treesitter.LanguageJavaScript
	case "rust", "rs":
		return treesitter.LanguageRust
	default:
		return treesitter.LanguageUnknown
	}
}

func showDefinitions(parser *treesitter.TreeSitterParser, result *treesitter.ParseResult, outputFormat string) error {
	// Extract definitions using the definition extractor
	extractor := treesitter.NewDefinitionExtractor(parser)
	definitions, err := extractor.ExtractDefinitions(result)
	if err != nil {
		return fmt.Errorf("failed to extract definitions: %w", err)
	}

	switch outputFormat {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(map[string]interface{}{
			"file":        result.FilePath,
			"language":    result.Language,
			"definitions": definitions,
			"count":       len(definitions),
		})
	default:
		return showDefinitionsText(definitions, result.FilePath)
	}
}

func showDefinitionsText(definitions []treesitter.Definition, filePath string) error {
	if len(definitions) == 0 {
		fmt.Printf("No definitions found in %s\n", filePath)
		return nil
	}

	fmt.Printf("📋 Found %d definitions in %s:\n\n", len(definitions), filePath)

	// Group by type for better organization
	typeGroups := make(map[treesitter.DefinitionType][]treesitter.Definition)
	for _, def := range definitions {
		typeGroups[def.Type] = append(typeGroups[def.Type], def)
	}

	// Display grouped results
	for defType, defs := range typeGroups {
		fmt.Printf("%s %s (%d):\n", formatDefinitionTypeIcon(defType), strings.Title(string(defType)), len(defs))
		for _, def := range defs {
			fmt.Printf("  📍 %s", def.Name)
			if def.Signature != "" {
				fmt.Printf(" %s", def.Signature)
			}
			fmt.Printf(" (line %d:%d)", def.StartLine, def.StartColumn)
			if def.DocString != "" {
				truncated := truncateParseString(def.DocString, 60)
				fmt.Printf("\n    💬 %s", truncated)
			}
			fmt.Println()
		}
		fmt.Println()
	}

	return nil
}

func executeQuery(parser *treesitter.TreeSitterParser, result *treesitter.ParseResult, queryType, outputFormat string) error {
	fmt.Printf("🔍 Executing %s query on %s...\n\n", queryType, result.FilePath)

	// For now, filter definitions by type based on query
	extractor := treesitter.NewDefinitionExtractor(parser)
	allDefinitions, err := extractor.ExtractDefinitions(result)
	if err != nil {
		return fmt.Errorf("failed to extract definitions for query: %w", err)
	}

	var filteredDefs []treesitter.Definition
	switch queryType {
	case "functions":
		for _, def := range allDefinitions {
			if def.Type == treesitter.DefFunction || def.Type == treesitter.DefMethod {
				filteredDefs = append(filteredDefs, def)
			}
		}
	case "classes":
		for _, def := range allDefinitions {
			if def.Type == treesitter.DefClass || def.Type == treesitter.DefStruct || def.Type == treesitter.DefInterface {
				filteredDefs = append(filteredDefs, def)
			}
		}
	case "imports":
		for _, def := range allDefinitions {
			if def.Type == treesitter.DefImport || def.Type == treesitter.DefPackage {
				filteredDefs = append(filteredDefs, def)
			}
		}
	case "all":
		filteredDefs = allDefinitions
	default:
		return fmt.Errorf("unknown query type: %s. Available: functions, classes, imports, all", queryType)
	}

	switch outputFormat {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(map[string]interface{}{
			"query":       queryType,
			"file":        result.FilePath,
			"language":    result.Language,
			"results":     filteredDefs,
			"count":       len(filteredDefs),
		})
	default:
		return showDefinitionsText(filteredDefs, result.FilePath)
	}
}

func runInteractiveParse(parser *treesitter.TreeSitterParser, result *treesitter.ParseResult) error {
	fmt.Printf("🎮 Interactive AST Explorer for %s\n", result.FilePath)
	fmt.Printf("Language: %s | Nodes: %d\n", result.Language, result.NodeCount)
	fmt.Printf("═══════════════════════════════════════════════════════════════\n")
	
	if result.RootNode != nil {
		fmt.Printf("Root Node Type: %s\n", result.RootNode.Type())
		fmt.Printf("Child Count: %d\n", result.RootNode.ChildCount())
		
		// Show first few child nodes
		childCount := result.RootNode.ChildCount()
		if childCount > 0 {
			fmt.Printf("\nFirst 5 child nodes:\n")
			maxChildren := childCount
			if maxChildren > 5 {
				maxChildren = 5
			}
			
			for i := uint32(0); i < maxChildren; i++ {
				child := result.RootNode.Child(int(i))
				if child != nil {
					startLine, startCol, endLine, endCol := parser.GetNodeLocation(child)
					fmt.Printf("  %d. %s (line %d:%d - %d:%d)\n", 
						i+1, child.Type(), startLine+1, startCol+1, endLine+1, endCol+1)
				}
			}
		}
	}

	fmt.Printf("\n💡 Interactive mode limited in CLI. Use the MCP tools for full AST exploration.\n")
	return nil
}

func showJSONOutput(result *treesitter.ParseResult, parseTime time.Duration, showMetrics bool) error {
	output := map[string]interface{}{
		"language":     result.Language,
		"file_path":    result.FilePath,
		"node_count":   result.NodeCount,
		"has_errors":   result.Error != "",
	}

	if result.Error != "" {
		output["error"] = result.Error
	}

	if showMetrics {
		output["metrics"] = map[string]interface{}{
			"parse_time":     parseTime.String(),
			"parse_time_ms":  parseTime.Milliseconds(),
			"content_size":   len(result.Content),
		}
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

func showTreeOutput(result *treesitter.ParseResult, parser *treesitter.TreeSitterParser) error {
	fmt.Printf("🌳 AST Tree for %s:\n", result.FilePath)
	fmt.Printf("═══════════════════════════════════════════════════════════════\n")

	if result.RootNode == nil {
		fmt.Printf("No AST available\n")
		return nil
	}

	// Show tree structure (limited depth to avoid overwhelming output)
	showTreeNode(result.RootNode, parser, result.Content, 0, 3)
	return nil
}

func showTreeNode(node *sitter.Node, parser *treesitter.TreeSitterParser, content string, depth, maxDepth int) {
	if depth > maxDepth {
		return
	}

	indent := strings.Repeat("  ", depth)
	fmt.Printf("%s%s", indent, node.Type())

	// Show node text for leaf nodes or small nodes
	if node.ChildCount() == 0 || len(parser.GetNodeText(node, content)) < 50 {
		text := strings.TrimSpace(parser.GetNodeText(node, content))
		if text != "" && len(text) < 50 {
			fmt.Printf(" \"%s\"", text)
		}
	}

	startLine, startCol, _, _ := parser.GetNodeLocation(node)
	fmt.Printf(" (line %d:%d)\n", startLine+1, startCol+1)

	// Recursively show children
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child != nil {
			showTreeNode(child, parser, content, depth+1, maxDepth)
		}
	}
}

func showTextOutput(result *treesitter.ParseResult, parseTime time.Duration, showMetrics bool) error {
	fmt.Printf("📄 Parse Results for: %s\n", result.FilePath)
	fmt.Printf("═══════════════════════════════════════════════════════════════\n")
	fmt.Printf("Language: %s\n", result.Language)
	fmt.Printf("Node count: %d\n", result.NodeCount)

	if result.Error != "" {
		fmt.Printf("❌ Parse Error: %s\n", result.Error)
	} else {
		fmt.Printf("✅ Parsed successfully!\n")
	}

	if showMetrics {
		fmt.Printf("\n📊 Performance Metrics:\n")
		fmt.Printf("  Parse time: %v\n", parseTime)
		fmt.Printf("  Content size: %d bytes\n", len(result.Content))
		fmt.Printf("  Nodes per ms: %.2f\n", float64(result.NodeCount)/float64(parseTime.Milliseconds()))
	}

	return nil
}

func formatDefinitionTypeIcon(defType treesitter.DefinitionType) string {
	switch defType {
	case treesitter.DefFunction:
		return "🔧"
	case treesitter.DefMethod:
		return "⚙️"
	case treesitter.DefClass:
		return "🏛️"
	case treesitter.DefStruct:
		return "🏗️"
	case treesitter.DefInterface:
		return "🔌"
	case treesitter.DefType:
		return "📦"
	case treesitter.DefVariable:
		return "📋"
	case treesitter.DefConstant:
		return "🔒"
	case treesitter.DefImport:
		return "📥"
	case treesitter.DefPackage:
		return "📦"
	default:
		return "❓"
	}
}

func truncateParseString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}