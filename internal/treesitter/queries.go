package treesitter

import (
	"fmt"
	"strings"
	"sync"
	"time"

	sitter "github.com/smacker/go-tree-sitter"
)

// QueryType represents different types of tree-sitter queries
type QueryType string

const (
	QueryFunctions    QueryType = "functions"
	QueryMethods      QueryType = "methods"
	QueryClasses      QueryType = "classes"
	QueryStructs      QueryType = "structs"
	QueryInterfaces   QueryType = "interfaces"
	QueryTypes        QueryType = "types"
	QueryVariables    QueryType = "variables"
	QueryConstants    QueryType = "constants"
	QueryImports      QueryType = "imports"
	QueryComments     QueryType = "comments"
	QueryDocStrings   QueryType = "docstrings"
	QueryErrors       QueryType = "errors"
	QueryAll          QueryType = "all"
)

// QueryResult represents the result of a tree-sitter query
type QueryResult struct {
	Type        QueryType                `json:"type"`
	Language    Language                 `json:"language"`
	Matches     []QueryMatch             `json:"matches"`
	ExecuteTime time.Duration            `json:"execute_time"`
	NodeCount   int                      `json:"node_count"`
	Error       string                   `json:"error,omitempty"`
	Metadata    map[string]interface{}   `json:"metadata,omitempty"`
}

// QueryMatch represents a single match from a query
type QueryMatch struct {
	Node        *sitter.Node             `json:"-"`
	Text        string                   `json:"text"`
	StartLine   uint32                   `json:"start_line"`
	StartColumn uint32                   `json:"start_column"`
	EndLine     uint32                   `json:"end_line"`
	EndColumn   uint32                   `json:"end_column"`
	Captures    map[string]string        `json:"captures,omitempty"`
	Context     string                   `json:"context,omitempty"`
	Metadata    map[string]interface{}   `json:"metadata,omitempty"`
}

// QueryPattern represents a tree-sitter query pattern for a specific language
type QueryPattern struct {
	Language Language  `json:"language"`
	Type     QueryType `json:"type"`
	Pattern  string    `json:"pattern"`
	Captures []string  `json:"captures"`
}

// QuerySystem manages tree-sitter queries for multiple languages
type QuerySystem struct {
	parser     *TreeSitterParser
	patterns   map[Language]map[QueryType]*QueryPattern
	queries    map[string]*sitter.Query
	mutex      sync.RWMutex
	cache      map[string]*QueryResult
	cacheSize  int
}

// NewQuerySystem creates a new query system
func NewQuerySystem(parser *TreeSitterParser) *QuerySystem {
	qs := &QuerySystem{
		parser:    parser,
		patterns:  make(map[Language]map[QueryType]*QueryPattern),
		queries:   make(map[string]*sitter.Query),
		cache:     make(map[string]*QueryResult),
		cacheSize: 100,
	}

	// Initialize language-specific patterns
	qs.initializePatterns()

	return qs
}

// ExecuteQuery executes a specific query type on a parse result
func (qs *QuerySystem) ExecuteQuery(result *ParseResult, queryType QueryType) (*QueryResult, error) {
	if result.Tree == nil || result.RootNode == nil {
		return nil, fmt.Errorf("invalid parse result")
	}

	// Check cache
	cacheKey := fmt.Sprintf("%s:%s:%s", result.FilePath, result.Language, queryType)
	if cached := qs.getCached(cacheKey); cached != nil {
		return cached, nil
	}

	// Get query pattern
	pattern, err := qs.getPattern(result.Language, queryType)
	if err != nil {
		return nil, fmt.Errorf("failed to get pattern: %w", err)
	}

	// Get compiled query
	query, err := qs.getQuery(result.Language, pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to get query: %w", err)
	}

	// Execute query
	startTime := time.Now()
	cursor := sitter.NewQueryCursor()
	defer cursor.Close()

	cursor.Exec(query, result.RootNode)

	var matches []QueryMatch
	nodeCount := 0

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		for _, capture := range match.Captures {
			node := capture.Node
			nodeCount++

			text := qs.parser.GetNodeText(node, result.Content)
			startLine, startCol, endLine, endCol := qs.parser.GetNodeLocation(node)

			// Extract captures
			captures := make(map[string]string)
			captureName := query.CaptureNameForId(capture.Index)
			captures[captureName] = text

			// Get context (surrounding lines)
			context := qs.extractContext(node, result.Content, 2)

			queryMatch := QueryMatch{
				Node:        node,
				Text:        text,
				StartLine:   startLine,
				StartColumn: startCol,
				EndLine:     endLine,
				EndColumn:   endCol,
				Captures:    captures,
				Context:     context,
				Metadata: map[string]interface{}{
					"capture_name": captureName,
					"node_type":    node.Type(),
				},
			}

			matches = append(matches, queryMatch)
		}
	}

	queryResult := &QueryResult{
		Type:        queryType,
		Language:    result.Language,
		Matches:     matches,
		ExecuteTime: time.Since(startTime),
		NodeCount:   nodeCount,
		Metadata: map[string]interface{}{
			"pattern":    pattern.Pattern,
			"file_path":  result.FilePath,
			"total_nodes": len(matches),
		},
	}

	// Cache result
	qs.setCached(cacheKey, queryResult)

	return queryResult, nil
}

// ExecuteCustomQuery executes a custom query pattern
func (qs *QuerySystem) ExecuteCustomQuery(result *ParseResult, queryPattern string) (*QueryResult, error) {
	if result.Tree == nil || result.RootNode == nil {
		return nil, fmt.Errorf("invalid parse result")
	}

	// Get language
	language := qs.parser.languages[result.Language]
	if language == nil {
		return nil, fmt.Errorf("language not available: %s", result.Language)
	}

	// Create query
	query, err := sitter.NewQuery([]byte(queryPattern), language)
	if err != nil {
		return nil, fmt.Errorf("failed to create query: %w", err)
	}
	defer query.Close()

	// Execute query
	startTime := time.Now()
	cursor := sitter.NewQueryCursor()
	defer cursor.Close()

	cursor.Exec(query, result.RootNode)

	var matches []QueryMatch
	nodeCount := 0

	for {
		match, ok := cursor.NextMatch()
		if !ok {
			break
		}

		for _, capture := range match.Captures {
			node := capture.Node
			nodeCount++

			text := qs.parser.GetNodeText(node, result.Content)
			startLine, startCol, endLine, endCol := qs.parser.GetNodeLocation(node)

			captures := make(map[string]string)
			captureName := query.CaptureNameForId(capture.Index)
			captures[captureName] = text

			context := qs.extractContext(node, result.Content, 2)

			queryMatch := QueryMatch{
				Node:        node,
				Text:        text,
				StartLine:   startLine,
				StartColumn: startCol,
				EndLine:     endLine,
				EndColumn:   endCol,
				Captures:    captures,
				Context:     context,
				Metadata: map[string]interface{}{
					"capture_name": captureName,
					"node_type":    node.Type(),
				},
			}

			matches = append(matches, queryMatch)
		}
	}

	return &QueryResult{
		Type:        QueryType("custom"),
		Language:    result.Language,
		Matches:     matches,
		ExecuteTime: time.Since(startTime),
		NodeCount:   nodeCount,
		Metadata: map[string]interface{}{
			"pattern":     queryPattern,
			"file_path":   result.FilePath,
			"total_nodes": len(matches),
		},
	}, nil
}

// GetAvailableQueries returns available query types for a language
func (qs *QuerySystem) GetAvailableQueries(language Language) []QueryType {
	qs.mutex.RLock()
	defer qs.mutex.RUnlock()

	patterns, exists := qs.patterns[language]
	if !exists {
		return nil
	}

	var queryTypes []QueryType
	for queryType := range patterns {
		queryTypes = append(queryTypes, queryType)
	}

	return queryTypes
}

// GetPattern returns a query pattern for a language and query type
func (qs *QuerySystem) GetPattern(language Language, queryType QueryType) (*QueryPattern, error) {
	return qs.getPattern(language, queryType)
}

// AddCustomPattern adds a custom query pattern
func (qs *QuerySystem) AddCustomPattern(language Language, queryType QueryType, pattern string, captures []string) error {
	qs.mutex.Lock()
	defer qs.mutex.Unlock()

	if qs.patterns[language] == nil {
		qs.patterns[language] = make(map[QueryType]*QueryPattern)
	}

	qs.patterns[language][queryType] = &QueryPattern{
		Language: language,
		Type:     queryType,
		Pattern:  pattern,
		Captures: captures,
	}

	return nil
}

// ClearCache clears the query result cache
func (qs *QuerySystem) ClearCache() {
	qs.mutex.Lock()
	defer qs.mutex.Unlock()

	qs.cache = make(map[string]*QueryResult)
}

// GetCacheStats returns cache statistics
func (qs *QuerySystem) GetCacheStats() map[string]interface{} {
	qs.mutex.RLock()
	defer qs.mutex.RUnlock()

	return map[string]interface{}{
		"cached_queries": len(qs.cache),
		"cache_size":     qs.cacheSize,
		"compiled_queries": len(qs.queries),
	}
}

// Private methods

func (qs *QuerySystem) initializePatterns() {
	// Initialize Go patterns
	qs.initializeGoPatterns()
	
	// Initialize Python patterns
	qs.initializePythonPatterns()
	
	// Initialize JavaScript patterns
	qs.initializeJavaScriptPatterns()
	
	// Initialize Rust patterns
	qs.initializeRustPatterns()
}

func (qs *QuerySystem) initializeGoPatterns() {
	goPatterns := map[QueryType]*QueryPattern{
		QueryFunctions: {
			Language: LanguageGo,
			Type:     QueryFunctions,
			Pattern: `
				(function_declaration
					name: (identifier) @function.name
					parameters: (parameter_list) @function.params
					result: (parameter_list)? @function.result
				) @function.def
			`,
			Captures: []string{"function.name", "function.params", "function.result", "function.def"},
		},
		QueryMethods: {
			Language: LanguageGo,
			Type:     QueryMethods,
			Pattern: `
				(method_declaration
					receiver: (parameter_list) @method.receiver
					name: (field_identifier) @method.name
					parameters: (parameter_list) @method.params
					result: (parameter_list)? @method.result
				) @method.def
			`,
			Captures: []string{"method.receiver", "method.name", "method.params", "method.result", "method.def"},
		},
		QueryStructs: {
			Language: LanguageGo,
			Type:     QueryStructs,
			Pattern: `
				(type_declaration
					(type_spec
						name: (type_identifier) @struct.name
						type: (struct_type) @struct.body
					)
				) @struct.def
			`,
			Captures: []string{"struct.name", "struct.body", "struct.def"},
		},
		QueryInterfaces: {
			Language: LanguageGo,
			Type:     QueryInterfaces,
			Pattern: `
				(type_declaration
					(type_spec
						name: (type_identifier) @interface.name
						type: (interface_type) @interface.body
					)
				) @interface.def
			`,
			Captures: []string{"interface.name", "interface.body", "interface.def"},
		},
		QueryImports: {
			Language: LanguageGo,
			Type:     QueryImports,
			Pattern: `
				(import_declaration
					(import_spec
						path: (interpreted_string_literal) @import.path
						name: (package_identifier)? @import.alias
					)
				) @import.def
			`,
			Captures: []string{"import.path", "import.alias", "import.def"},
		},
		QueryConstants: {
			Language: LanguageGo,
			Type:     QueryConstants,
			Pattern: `
				(const_declaration
					(const_spec
						name: (identifier) @const.name
						value: (_)? @const.value
					)
				) @const.def
			`,
			Captures: []string{"const.name", "const.value", "const.def"},
		},
		QueryVariables: {
			Language: LanguageGo,
			Type:     QueryVariables,
			Pattern: `
				(var_declaration
					(var_spec
						name: (identifier) @var.name
						type: (_)? @var.type
						value: (_)? @var.value
					)
				) @var.def
			`,
			Captures: []string{"var.name", "var.type", "var.value", "var.def"},
		},
		QueryComments: {
			Language: LanguageGo,
			Type:     QueryComments,
			Pattern: `
				(comment) @comment
			`,
			Captures: []string{"comment"},
		},
	}

	qs.patterns[LanguageGo] = goPatterns
}

func (qs *QuerySystem) initializePythonPatterns() {
	pythonPatterns := map[QueryType]*QueryPattern{
		QueryFunctions: {
			Language: LanguagePython,
			Type:     QueryFunctions,
			Pattern: `
				(function_definition
					name: (identifier) @function.name
					parameters: (parameters) @function.params
					body: (block) @function.body
				) @function.def
			`,
			Captures: []string{"function.name", "function.params", "function.body", "function.def"},
		},
		QueryClasses: {
			Language: LanguagePython,
			Type:     QueryClasses,
			Pattern: `
				(class_definition
					name: (identifier) @class.name
					superclasses: (argument_list)? @class.bases
					body: (block) @class.body
				) @class.def
			`,
			Captures: []string{"class.name", "class.bases", "class.body", "class.def"},
		},
		QueryImports: {
			Language: LanguagePython,
			Type:     QueryImports,
			Pattern: `
				[
					(import_statement
						name: (dotted_name) @import.module
					) @import.def
					(import_from_statement
						module_name: (dotted_name) @import.from
						name: (dotted_name) @import.name
					) @import.def
				]
			`,
			Captures: []string{"import.module", "import.from", "import.name", "import.def"},
		},
		QueryDocStrings: {
			Language: LanguagePython,
			Type:     QueryDocStrings,
			Pattern: `
				(expression_statement
					(string) @docstring
				)
			`,
			Captures: []string{"docstring"},
		},
	}

	qs.patterns[LanguagePython] = pythonPatterns
}

func (qs *QuerySystem) initializeJavaScriptPatterns() {
	jsPatterns := map[QueryType]*QueryPattern{
		QueryFunctions: {
			Language: LanguageJavaScript,
			Type:     QueryFunctions,
			Pattern: `
				[
					(function_declaration
						name: (identifier) @function.name
						parameters: (formal_parameters) @function.params
						body: (statement_block) @function.body
					) @function.def
					(arrow_function
						parameters: (formal_parameters) @function.params
						body: (_) @function.body
					) @function.def
				]
			`,
			Captures: []string{"function.name", "function.params", "function.body", "function.def"},
		},
		QueryClasses: {
			Language: LanguageJavaScript,
			Type:     QueryClasses,
			Pattern: `
				(class_declaration
					name: (identifier) @class.name
					superclass: (class_heritage)? @class.extends
					body: (class_body) @class.body
				) @class.def
			`,
			Captures: []string{"class.name", "class.extends", "class.body", "class.def"},
		},
		QueryMethods: {
			Language: LanguageJavaScript,
			Type:     QueryMethods,
			Pattern: `
				(method_definition
					name: (property_identifier) @method.name
					parameters: (formal_parameters) @method.params
					body: (statement_block) @method.body
				) @method.def
			`,
			Captures: []string{"method.name", "method.params", "method.body", "method.def"},
		},
		QueryImports: {
			Language: LanguageJavaScript,
			Type:     QueryImports,
			Pattern: `
				[
					(import_statement
						source: (string) @import.source
						(import_clause
							(named_imports) @import.names
						)?
					) @import.def
					(import_statement
						source: (string) @import.source
						(import_clause
							(identifier) @import.default
						)?
					) @import.def
				]
			`,
			Captures: []string{"import.source", "import.names", "import.default", "import.def"},
		},
		QueryVariables: {
			Language: LanguageJavaScript,
			Type:     QueryVariables,
			Pattern: `
				(variable_declaration
					(variable_declarator
						name: (identifier) @var.name
						value: (_)? @var.value
					)
				) @var.def
			`,
			Captures: []string{"var.name", "var.value", "var.def"},
		},
	}

	qs.patterns[LanguageJavaScript] = jsPatterns
}

func (qs *QuerySystem) initializeRustPatterns() {
	rustPatterns := map[QueryType]*QueryPattern{
		QueryFunctions: {
			Language: LanguageRust,
			Type:     QueryFunctions,
			Pattern: `
				(function_item
					name: (identifier) @function.name
					parameters: (parameters) @function.params
					return_type: (type_identifier)? @function.return
					body: (block) @function.body
				) @function.def
			`,
			Captures: []string{"function.name", "function.params", "function.return", "function.body", "function.def"},
		},
		QueryStructs: {
			Language: LanguageRust,
			Type:     QueryStructs,
			Pattern: `
				(struct_item
					name: (type_identifier) @struct.name
					body: (field_declaration_list) @struct.fields
				) @struct.def
			`,
			Captures: []string{"struct.name", "struct.fields", "struct.def"},
		},
		QueryMethods: {
			Language: LanguageRust,
			Type:     QueryMethods,
			Pattern: `
				(impl_item
					type: (type_identifier) @impl.type
					body: (declaration_list
						(function_item
							name: (identifier) @method.name
							parameters: (parameters) @method.params
							body: (block) @method.body
						) @method.def
					)
				)
			`,
			Captures: []string{"impl.type", "method.name", "method.params", "method.body", "method.def"},
		},
	}

	qs.patterns[LanguageRust] = rustPatterns
}

func (qs *QuerySystem) getPattern(language Language, queryType QueryType) (*QueryPattern, error) {
	qs.mutex.RLock()
	defer qs.mutex.RUnlock()

	langPatterns, exists := qs.patterns[language]
	if !exists {
		return nil, fmt.Errorf("no patterns available for language: %s", language)
	}

	pattern, exists := langPatterns[queryType]
	if !exists {
		return nil, fmt.Errorf("pattern not found for %s:%s", language, queryType)
	}

	return pattern, nil
}

func (qs *QuerySystem) getQuery(language Language, pattern *QueryPattern) (*sitter.Query, error) {
	// Create cache key
	key := fmt.Sprintf("%s:%s", language, pattern.Type)

	qs.mutex.RLock()
	query, exists := qs.queries[key]
	qs.mutex.RUnlock()

	if exists {
		return query, nil
	}

	// Compile query
	lang := qs.parser.languages[language]
	if lang == nil {
		return nil, fmt.Errorf("language not available: %s", language)
	}

	compiledQuery, err := sitter.NewQuery([]byte(pattern.Pattern), lang)
	if err != nil {
		return nil, fmt.Errorf("failed to compile query: %w", err)
	}

	// Cache compiled query
	qs.mutex.Lock()
	qs.queries[key] = compiledQuery
	qs.mutex.Unlock()

	return compiledQuery, nil
}

func (qs *QuerySystem) extractContext(node *sitter.Node, content string, contextLines int) string {
	if node == nil || contextLines <= 0 {
		return ""
	}

	startLine, _, endLine, _ := qs.parser.GetNodeLocation(node)
	lines := strings.Split(content, "\n")

	// Calculate context boundaries
	contextStart := int(startLine) - contextLines - 1
	contextEnd := int(endLine) + contextLines - 1

	if contextStart < 0 {
		contextStart = 0
	}
	if contextEnd >= len(lines) {
		contextEnd = len(lines) - 1
	}

	if contextStart > contextEnd {
		return ""
	}

	contextLines_slice := lines[contextStart:contextEnd+1]
	return strings.Join(contextLines_slice, "\n")
}

func (qs *QuerySystem) getCached(key string) *QueryResult {
	qs.mutex.RLock()
	defer qs.mutex.RUnlock()

	return qs.cache[key]
}

func (qs *QuerySystem) setCached(key string, result *QueryResult) {
	qs.mutex.Lock()
	defer qs.mutex.Unlock()

	// Simple LRU eviction
	if len(qs.cache) >= qs.cacheSize {
		// Remove first item
		for k := range qs.cache {
			delete(qs.cache, k)
			break
		}
	}

	qs.cache[key] = result
}

// QueryBuilder helps build complex queries programmatically
type QueryBuilder struct {
	language Language
	patterns []string
	captures []string
}

// NewQueryBuilder creates a new query builder
func NewQueryBuilder(language Language) *QueryBuilder {
	return &QueryBuilder{
		language: language,
		patterns: make([]string, 0),
		captures: make([]string, 0),
	}
}

// AddPattern adds a pattern to the query
func (qb *QueryBuilder) AddPattern(pattern string) *QueryBuilder {
	qb.patterns = append(qb.patterns, pattern)
	return qb
}

// AddCapture adds a capture to the query
func (qb *QueryBuilder) AddCapture(capture string) *QueryBuilder {
	qb.captures = append(qb.captures, capture)
	return qb
}

// Build builds the final query string
func (qb *QueryBuilder) Build() string {
	if len(qb.patterns) == 0 {
		return ""
	}

	if len(qb.patterns) == 1 {
		return qb.patterns[0]
	}

	// Combine multiple patterns with alternation
	return fmt.Sprintf("[\n%s\n]", strings.Join(qb.patterns, "\n"))
}

// Helper functions for common query operations

// FindNodesByName finds all nodes with a specific name
func (qs *QuerySystem) FindNodesByName(result *ParseResult, name string) ([]QueryMatch, error) {
	// Create a custom query to find nodes by name
	pattern := fmt.Sprintf(`(identifier) @name (#eq? @name "%s")`, name)
	
	queryResult, err := qs.ExecuteCustomQuery(result, pattern)
	if err != nil {
		return nil, err
	}

	return queryResult.Matches, nil
}

// FindNodesByType finds all nodes of a specific type
func (qs *QuerySystem) FindNodesByType(result *ParseResult, nodeType string) ([]QueryMatch, error) {
	// Create a custom query to find nodes by type
	pattern := fmt.Sprintf(`(%s) @node`, nodeType)
	
	queryResult, err := qs.ExecuteCustomQuery(result, pattern)
	if err != nil {
		return nil, err
	}

	return queryResult.Matches, nil
}

// GetAllDefinitions returns all definitions using the appropriate query types
func (qs *QuerySystem) GetAllDefinitions(result *ParseResult) (map[QueryType][]QueryMatch, error) {
	definitions := make(map[QueryType][]QueryMatch)
	
	// Get available query types for this language
	queryTypes := qs.GetAvailableQueries(result.Language)
	
	// Execute each definition-related query
	definitionTypes := []QueryType{QueryFunctions, QueryMethods, QueryClasses, QueryStructs, QueryInterfaces, QueryTypes, QueryVariables, QueryConstants}
	
	for _, queryType := range definitionTypes {
		// Check if this query type is available
		available := false
		for _, available_type := range queryTypes {
			if available_type == queryType {
				available = true
				break
			}
		}
		
		if !available {
			continue
		}
		
		queryResult, err := qs.ExecuteQuery(result, queryType)
		if err != nil {
			// Log error but continue with other queries
			continue
		}
		
		definitions[queryType] = queryResult.Matches
	}
	
	return definitions, nil
}