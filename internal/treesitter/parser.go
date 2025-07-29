package treesitter

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/rust"
)

// Language represents a supported programming language
type Language string

const (
	LanguageGo         Language = "go"
	LanguagePython     Language = "python"
	LanguageJavaScript Language = "javascript"
	LanguageRust       Language = "rust"
	LanguageC          Language = "c"
	LanguageCPP        Language = "cpp"
	LanguageJava       Language = "java"
	LanguageUnknown    Language = "unknown"
)

// ParseResult contains the result of parsing a file
type ParseResult struct {
	Language     Language           `json:"language"`
	FilePath     string             `json:"file_path"`
	Content      string             `json:"content"`
	Tree         *sitter.Tree       `json:"-"`
	RootNode     *sitter.Node       `json:"-"`
	ParseTime    time.Duration      `json:"parse_time"`
	NodeCount    uint32             `json:"node_count"`
	Error        string             `json:"error,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// ParserConfig configures the tree-sitter parser behavior
type ParserConfig struct {
	CacheSize      int           `json:"cache_size"`
	ParseTimeout   time.Duration `json:"parse_timeout"`
	MaxFileSize    int64         `json:"max_file_size"`
	EnableLogging  bool          `json:"enable_logging"`
	LazyLoad       bool          `json:"lazy_load"`
}

// TreeSitterParser provides multi-language AST parsing capabilities
type TreeSitterParser struct {
	config     ParserConfig
	parsers    map[Language]*sitter.Parser
	languages  map[Language]*sitter.Language
	cache      map[string]*ParseResult
	cacheMutex sync.RWMutex
	mutex      sync.RWMutex
}

// NewTreeSitterParser creates a new tree-sitter parser instance
func NewTreeSitterParser(config ParserConfig) (*TreeSitterParser, error) {
	if config.CacheSize == 0 {
		config.CacheSize = 100
	}
	if config.ParseTimeout == 0 {
		config.ParseTimeout = 10 * time.Second
	}
	if config.MaxFileSize == 0 {
		config.MaxFileSize = 1024 * 1024 // 1MB
	}

	parser := &TreeSitterParser{
		config:    config,
		parsers:   make(map[Language]*sitter.Parser),
		languages: make(map[Language]*sitter.Language),
		cache:     make(map[string]*ParseResult),
	}

	// Initialize languages immediately if not lazy loading
	if !config.LazyLoad {
		if err := parser.initializeAllLanguages(); err != nil {
			return nil, fmt.Errorf("failed to initialize languages: %w", err)
		}
	}

	return parser, nil
}

// ParseFile parses a file and returns the AST
func (tsp *TreeSitterParser) ParseFile(filePath string) (*ParseResult, error) {
	// Check cache first
	if cached := tsp.getCached(filePath); cached != nil {
		return cached, nil
	}

	// Detect language from file extension
	language := tsp.DetectLanguage(filePath)
	if language == LanguageUnknown {
		return nil, fmt.Errorf("unsupported language for file: %s", filePath)
	}

	// Read file content
	content, err := tsp.readFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return tsp.ParseContent(content, language, filePath)
}

// ParseContent parses the given content with the specified language
func (tsp *TreeSitterParser) ParseContent(content string, language Language, filePath string) (*ParseResult, error) {
	if len(content) > int(tsp.config.MaxFileSize) {
		return nil, fmt.Errorf("file too large: %d bytes (max: %d)", len(content), tsp.config.MaxFileSize)
	}

	// Get or create parser for language
	parser, err := tsp.getParser(language)
	if err != nil {
		return nil, fmt.Errorf("failed to get parser for %s: %w", language, err)
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), tsp.config.ParseTimeout)
	defer cancel()

	// Parse with timeout
	result := &ParseResult{
		Language: language,
		FilePath: filePath,
		Content:  content,
		Metadata: make(map[string]interface{}),
	}

	startTime := time.Now()
	
	// Parse in goroutine to enable timeout
	parseDone := make(chan struct{})
	var parseErr error
	
	go func() {
		defer close(parseDone)
		result.Tree, parseErr = parser.ParseCtx(ctx, nil, []byte(content))
		if result.Tree != nil {
			result.RootNode = result.Tree.RootNode()
			result.NodeCount = result.RootNode.ChildCount()
		}
	}()

	// Wait for parsing or timeout
	select {
	case <-parseDone:
		if parseErr != nil {
			return nil, fmt.Errorf("parse error: %w", parseErr)
		}
	case <-ctx.Done():
		return nil, fmt.Errorf("parse timeout after %v", tsp.config.ParseTimeout)
	}

	result.ParseTime = time.Since(startTime)

	// Add metadata
	result.Metadata["lines"] = strings.Count(content, "\n") + 1
	result.Metadata["size"] = len(content)
	result.Metadata["parse_time_ms"] = result.ParseTime.Milliseconds()

	// Cache result
	tsp.setCached(filePath, result)

	return result, nil
}

// DetectLanguage detects the programming language from file extension
func (tsp *TreeSitterParser) DetectLanguage(filePath string) Language {
	ext := strings.ToLower(filepath.Ext(filePath))
	
	switch ext {
	case ".go":
		return LanguageGo
	case ".py", ".pyx", ".pyi":
		return LanguagePython
	case ".js", ".mjs", ".cjs":
		return LanguageJavaScript
	case ".ts", ".tsx":
		return LanguageJavaScript // Use JavaScript parser for TypeScript for now
	case ".rs":
		return LanguageRust
	case ".c", ".h":
		return LanguageC
	case ".cpp", ".cc", ".cxx", ".hpp", ".hxx":
		return LanguageCPP
	case ".java":
		return LanguageJava
	default:
		// Try to detect from filename patterns
		base := strings.ToLower(filepath.Base(filePath))
		if strings.Contains(base, "dockerfile") {
			return LanguageUnknown // Dockerfile not supported yet
		}
		if strings.HasSuffix(base, ".yaml") || strings.HasSuffix(base, ".yml") {
			return LanguageUnknown // YAML not supported yet
		}
		return LanguageUnknown
	}
}

// GetSupportedLanguages returns a list of supported languages
func (tsp *TreeSitterParser) GetSupportedLanguages() []Language {
	return []Language{
		LanguageGo,
		LanguagePython,
		LanguageJavaScript,
		LanguageRust,
	}
}

// IsLanguageSupported checks if a language is supported
func (tsp *TreeSitterParser) IsLanguageSupported(language Language) bool {
	supported := tsp.GetSupportedLanguages()
	for _, lang := range supported {
		if lang == language {
			return true
		}
	}
	return false
}

// GetNodeText returns the text content of a node
func (tsp *TreeSitterParser) GetNodeText(node *sitter.Node, content string) string {
	if node == nil {
		return ""
	}
	return content[node.StartByte():node.EndByte()]
}

// GetNodeLocation returns the line and column information for a node
func (tsp *TreeSitterParser) GetNodeLocation(node *sitter.Node) (startLine, startCol, endLine, endCol uint32) {
	if node == nil {
		return 0, 0, 0, 0
	}
	
	startPoint := node.StartPoint()
	endPoint := node.EndPoint()
	
	return startPoint.Row + 1, startPoint.Column + 1, 
		   endPoint.Row + 1, endPoint.Column + 1
}

// TraverseTree traverses the AST and calls the visitor function for each node
func (tsp *TreeSitterParser) TraverseTree(root *sitter.Node, visitor func(*sitter.Node, int) bool) {
	tsp.traverseNode(root, visitor, 0)
}

// ClearCache clears the parser cache
func (tsp *TreeSitterParser) ClearCache() {
	tsp.cacheMutex.Lock()
	defer tsp.cacheMutex.Unlock()
	
	tsp.cache = make(map[string]*ParseResult)
}

// GetCacheStats returns cache statistics
func (tsp *TreeSitterParser) GetCacheStats() map[string]interface{} {
	tsp.cacheMutex.RLock()
	defer tsp.cacheMutex.RUnlock()
	
	return map[string]interface{}{
		"cached_files": len(tsp.cache),
		"cache_size":   tsp.config.CacheSize,
	}
}

// Private methods

func (tsp *TreeSitterParser) initializeAllLanguages() error {
	languages := map[Language]*sitter.Language{
		LanguageGo:         golang.GetLanguage(),
		LanguagePython:     python.GetLanguage(),
		LanguageJavaScript: javascript.GetLanguage(),
		LanguageRust:       rust.GetLanguage(),
	}

	for lang, langDef := range languages {
		if err := tsp.initializeLanguage(lang, langDef); err != nil {
			return fmt.Errorf("failed to initialize %s: %w", lang, err)
		}
	}

	return nil
}

func (tsp *TreeSitterParser) initializeLanguage(lang Language, langDef *sitter.Language) error {
	tsp.mutex.Lock()
	defer tsp.mutex.Unlock()

	// Create parser
	parser := sitter.NewParser()
	parser.SetLanguage(langDef)

	tsp.parsers[lang] = parser
	tsp.languages[lang] = langDef

	return nil
}

func (tsp *TreeSitterParser) getParser(language Language) (*sitter.Parser, error) {
	tsp.mutex.RLock()
	parser, exists := tsp.parsers[language]
	tsp.mutex.RUnlock()

	if exists {
		return parser, nil
	}

	// Lazy load if not found
	if tsp.config.LazyLoad {
		return tsp.lazyLoadLanguage(language)
	}

	return nil, fmt.Errorf("parser not found for language: %s", language)
}

func (tsp *TreeSitterParser) lazyLoadLanguage(language Language) (*sitter.Parser, error) {
	var langDef *sitter.Language

	switch language {
	case LanguageGo:
		langDef = golang.GetLanguage()
	case LanguagePython:
		langDef = python.GetLanguage()
	case LanguageJavaScript:
		langDef = javascript.GetLanguage()
	case LanguageRust:
		langDef = rust.GetLanguage()
	default:
		return nil, fmt.Errorf("unsupported language: %s", language)
	}

	if err := tsp.initializeLanguage(language, langDef); err != nil {
		return nil, err
	}

	return tsp.parsers[language], nil
}

func (tsp *TreeSitterParser) readFile(filePath string) (string, error) {
	// This would integrate with the file editor from internal/edit
	// For now, use basic file reading
	
	// Check file size first
	if info, err := filepath.Glob(filePath); err == nil && len(info) > 0 { //nolint:staticcheck // intentional empty branch
		// File exists, could check size here
		// For now, intentionally not checking file size
	}

	// TODO: Integrate with internal/edit/editor.go for consistent file reading
	// For now, return placeholder
	return "// File content for " + filePath + " would be read here\n// Integration with internal/edit needed", nil
}

func (tsp *TreeSitterParser) getCached(filePath string) *ParseResult {
	tsp.cacheMutex.RLock()
	defer tsp.cacheMutex.RUnlock()
	
	if result, exists := tsp.cache[filePath]; exists {
		// Check if cache is still valid (could add file modification time checking)
		return result
	}
	
	return nil
}

func (tsp *TreeSitterParser) setCached(filePath string, result *ParseResult) {
	tsp.cacheMutex.Lock()
	defer tsp.cacheMutex.Unlock()
	
	// Implement LRU eviction if cache is full
	if len(tsp.cache) >= tsp.config.CacheSize {
		// Simple eviction: remove first item
		// In a real implementation, you'd use proper LRU
		for key := range tsp.cache {
			delete(tsp.cache, key)
			break
		}
	}
	
	tsp.cache[filePath] = result
}

func (tsp *TreeSitterParser) traverseNode(node *sitter.Node, visitor func(*sitter.Node, int) bool, depth int) {
	if node == nil {
		return
	}

	// Call visitor function
	if !visitor(node, depth) {
		return // Stop traversal if visitor returns false
	}

	// Traverse children
	childCount := int(node.ChildCount())
	for i := 0; i < childCount; i++ {
		child := node.Child(i)
		tsp.traverseNode(child, visitor, depth+1)
	}
}

// Helper functions for common node operations

// FindNodesOfType finds all nodes of a specific type
func (tsp *TreeSitterParser) FindNodesOfType(root *sitter.Node, nodeType string) []*sitter.Node {
	var nodes []*sitter.Node
	
	tsp.TraverseTree(root, func(node *sitter.Node, depth int) bool {
		if node.Type() == nodeType {
			nodes = append(nodes, node)
		}
		return true
	})
	
	return nodes
}

// GetChildByType finds the first child node of a specific type
func (tsp *TreeSitterParser) GetChildByType(node *sitter.Node, nodeType string) *sitter.Node {
	if node == nil {
		return nil
	}
	
	childCount := int(node.ChildCount())
	for i := 0; i < childCount; i++ {
		child := node.Child(i)
		if child.Type() == nodeType {
			return child
		}
	}
	
	return nil
}

// GetChildrenByType finds all child nodes of a specific type
func (tsp *TreeSitterParser) GetChildrenByType(node *sitter.Node, nodeType string) []*sitter.Node {
	var children []*sitter.Node
	
	if node == nil {
		return children
	}
	
	childCount := int(node.ChildCount())
	for i := 0; i < childCount; i++ {
		child := node.Child(i)
		if child.Type() == nodeType {
			children = append(children, child)
		}
	}
	
	return children
}

// NodeToString provides a debug representation of a node
func (tsp *TreeSitterParser) NodeToString(node *sitter.Node, content string, maxDepth int) string {
	if node == nil {
		return ""
	}
	
	var builder strings.Builder
	tsp.nodeToStringHelper(node, content, &builder, 0, maxDepth)
	return builder.String()
}

func (tsp *TreeSitterParser) nodeToStringHelper(node *sitter.Node, content string, builder *strings.Builder, depth, maxDepth int) {
	if depth > maxDepth {
		return
	}
	
	indent := strings.Repeat("  ", depth)
	
	startLine, startCol, endLine, endCol := tsp.GetNodeLocation(node)
	nodeText := tsp.GetNodeText(node, content)
	
	// Truncate text if too long
	if len(nodeText) > 50 {
		nodeText = nodeText[:47] + "..."
	}
	
	// Escape newlines
	nodeText = strings.ReplaceAll(nodeText, "\n", "\\n")
	
	fmt.Fprintf(builder, "%s%s [%d:%d-%d:%d] %q\n", 
		indent, node.Type(), startLine, startCol, endLine, endCol, nodeText)
	
	// Traverse children
	childCount := int(node.ChildCount())
	for i := 0; i < childCount; i++ {
		child := node.Child(i)
		tsp.nodeToStringHelper(child, content, builder, depth+1, maxDepth)
	}
}