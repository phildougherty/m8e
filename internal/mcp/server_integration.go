package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/phildougherty/m8e/internal/config"
	contextpkg "github.com/phildougherty/m8e/internal/context"
	"github.com/phildougherty/m8e/internal/edit"
	"github.com/phildougherty/m8e/internal/treesitter"
)

// MCPServerIntegration provides enhanced MCP server functionality
type MCPServerIntegration struct {
	config           *config.ComposeConfig
	enhancedTools    *EnhancedTools
	contextTools     *ContextTools
	fileEditor       *edit.FileEditor
	contextManager   *contextpkg.ContextManager
	fileDiscovery    *contextpkg.FileDiscovery
	parser           *treesitter.TreeSitterParser
	mentionProcessor *contextpkg.MentionProcessor
	toolRegistry     map[string]ToolHandler
	mutex            sync.RWMutex
}

// ToolHandler represents a function that handles MCP tool calls
type ToolHandler func(ctx context.Context, params json.RawMessage) (interface{}, error)

// ToolDefinition represents an MCP tool definition
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// NewMCPServerIntegration creates a new MCP server integration
func NewMCPServerIntegration(cfg *config.ComposeConfig) (*MCPServerIntegration, error) {
	// Initialize components
	fileEditor := edit.NewFileEditor(edit.EditorConfig{
		BackupDir:     "/tmp/matey-backups", // Default backup directory
		MaxBackups:    10,
		K8sIntegrated: true,
	})

	contextManager := contextpkg.NewContextManager(
		contextpkg.ContextConfig{
			MaxTokens:          32768,
			TruncationStrategy: contextpkg.TruncateIntelligent,
			RetentionDays:      7,
			PersistToK8s:      true,
			Namespace:         "matey-system", // Default namespace
		},
		nil, // AI provider would be passed here
	)

	fileDiscovery, err := contextpkg.NewFileDiscovery(".")
	if err != nil {
		return nil, fmt.Errorf("failed to create file discovery: %w", err)
	}

	parser, err := treesitter.NewTreeSitterParser(treesitter.ParserConfig{
		CacheSize:     100,
		ParseTimeout:  30,
		MaxFileSize:   1024 * 1024,
		EnableLogging: true,
		LazyLoad:      true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create parser: %w", err)
	}

	mentionProcessor := contextpkg.NewMentionProcessor(
		".",
		fileDiscovery,
		contextManager,
	)

	// Create enhanced tools
	enhancedTools := NewEnhancedTools(
		EnhancedToolConfig{
			EnableVisualDiff:  true,
			EnablePreview:     true,
			AutoApprove:       false,
			MaxFileSize:       1024 * 1024,
			ContextTracking:   true,
			BackupEnabled:     true,
			ValidationEnabled: true,
			DiffMode:          "unified",
		},
		fileEditor,
		contextManager,
		fileDiscovery,
		parser,
		mentionProcessor,
	)

	// Create context tools
	contextTools := NewContextTools(
		ContextToolConfig{
			MaxTokens:          32768,
			TruncationStrategy: "intelligent",
			EnableMentions:     true,
			AutoTrack:          true,
			RelevanceThreshold: 0.5,
			MaxContextItems:    50,
		},
		contextManager,
		fileDiscovery,
		mentionProcessor,
		parser,
	)

	integration := &MCPServerIntegration{
		config:           cfg,
		enhancedTools:    enhancedTools,
		contextTools:     contextTools,
		fileEditor:       fileEditor,
		contextManager:   contextManager,
		fileDiscovery:    fileDiscovery,
		parser:           parser,
		mentionProcessor: mentionProcessor,
		toolRegistry:     make(map[string]ToolHandler),
	}

	// Register tools
	integration.registerTools()

	return integration, nil
}

// registerTools registers all available MCP tools
func (msi *MCPServerIntegration) registerTools() {
	// Enhanced file editing tools
	msi.registerTool("edit_file", msi.handleEditFile)
	msi.registerTool("read_file", msi.handleReadFile)
	msi.registerTool("search_files", msi.handleSearchFiles)
	msi.registerTool("parse_code", msi.handleParseCode)
	msi.registerTool("list_definitions", msi.handleListDefinitions)

	// Context management tools
	msi.registerTool("get_context", msi.handleGetContext)
	msi.registerTool("add_context", msi.handleAddContext)
	msi.registerTool("process_mentions", msi.handleProcessMentions)
	msi.registerTool("get_relevant_files", msi.handleGetRelevantFiles)
	msi.registerTool("clear_context", msi.handleClearContext)
	msi.registerTool("get_context_stats", msi.handleGetContextStats)

	// Diff and visual tools
	msi.registerTool("diff_view", msi.handleDiffView)
	msi.registerTool("file_browser", msi.handleFileBrowser)
	msi.registerTool("interactive_editor", msi.handleInteractiveEditor)
}

// registerTool registers a tool handler
func (msi *MCPServerIntegration) registerTool(name string, handler ToolHandler) {
	msi.mutex.Lock()
	defer msi.mutex.Unlock()
	msi.toolRegistry[name] = handler
}

// GetToolDefinitions returns all available tool definitions
func (msi *MCPServerIntegration) GetToolDefinitions() []ToolDefinition {
	return []ToolDefinition{
		{
			Name:        "edit_file",
			Description: "Edit a file with visual diff preview and approval workflow",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to edit",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "New content for the file",
					},
					"diff_content": map[string]interface{}{
						"type":        "string",
						"description": "SEARCH/REPLACE diff format content",
					},
					"create_file": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to create the file if it doesn't exist",
					},
					"show_preview": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to show a preview of the changes",
					},
					"auto_approve": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to auto-approve changes without visual confirmation",
					},
				},
				"required": []string{"file_path"},
			},
		},
		{
			Name:        "read_file",
			Description: "Read file content with context tracking and line selection",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to read",
					},
					"start_line": map[string]interface{}{
						"type":        "integer",
						"description": "Starting line number (1-based)",
					},
					"end_line": map[string]interface{}{
						"type":        "integer",
						"description": "Ending line number (1-based)",
					},
					"include_context": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to include related context information",
					},
					"track_in_context": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to track this read in the context manager",
					},
				},
				"required": []string{"file_path"},
			},
		},
		{
			Name:        "search_files",
			Description: "Search for files using fuzzy matching and filters",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query for file names/paths",
					},
					"extensions": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "File extensions to filter by",
					},
					"max_results": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results to return",
					},
					"include_content": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to include content previews",
					},
					"fuzzy_search": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to use fuzzy matching",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "parse_code",
			Description: "Parse code structure using tree-sitter for definitions and analysis",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to parse",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Content to parse directly",
					},
					"query_types": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Types of definitions to extract (function, class, etc.)",
					},
					"include_body": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to include function/method bodies",
					},
				},
			},
		},
		{
			Name:        "get_context",
			Description: "Get current context information with filtering options",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"include_files": map[string]interface{}{
						"type":        "boolean",
						"description": "Include file context items",
					},
					"include_edits": map[string]interface{}{
						"type":        "boolean",
						"description": "Include edit context items",
					},
					"include_mentions": map[string]interface{}{
						"type":        "boolean",
						"description": "Include mention context items",
					},
					"filter_types": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Filter by specific context types",
					},
					"max_items": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of context items to return",
					},
				},
			},
		},
		{
			Name:        "process_mentions",
			Description: "Process @-mentions in text and expand file/folder references",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Text containing @-mentions to process",
					},
					"expand_inline": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to expand mentions inline in the text",
					},
					"track_context": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to track processed mentions in context",
					},
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "get_relevant_files",
			Description: "Get files relevant to a query using context and similarity scoring",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Query to find relevant files for",
					},
					"current_file": map[string]interface{}{
						"type":        "string",
						"description": "Current file for context-aware relevance scoring",
					},
					"file_types": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "File types/extensions to consider",
					},
					"max_results": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of relevant files to return",
					},
					"use_context": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to use current context for relevance scoring",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

// CallTool handles MCP tool calls
func (msi *MCPServerIntegration) CallTool(ctx context.Context, name string, params json.RawMessage) (interface{}, error) {
	msi.mutex.RLock()
	handler, exists := msi.toolRegistry[name]
	msi.mutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}

	return handler(ctx, params)
}

// Tool handlers

func (msi *MCPServerIntegration) handleEditFile(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request EditFileRequest
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid edit_file parameters: %w", err)
	}

	return msi.enhancedTools.EditFile(ctx, request)
}

func (msi *MCPServerIntegration) handleReadFile(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request ReadFileRequest
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid read_file parameters: %w", err)
	}

	return msi.enhancedTools.ReadFile(ctx, request)
}

func (msi *MCPServerIntegration) handleSearchFiles(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request SearchFilesRequest
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid search_files parameters: %w", err)
	}

	return msi.enhancedTools.SearchFiles(ctx, request)
}

func (msi *MCPServerIntegration) handleParseCode(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request ParseCodeRequest
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid parse_code parameters: %w", err)
	}

	return msi.enhancedTools.ParseCode(ctx, request)
}

func (msi *MCPServerIntegration) handleListDefinitions(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request ListDefinitionsRequest
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid list_definitions parameters: %w", err)
	}

	return msi.enhancedTools.ListDefinitions(ctx, request)
}

func (msi *MCPServerIntegration) handleGetContext(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request GetContextRequest
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid get_context parameters: %w", err)
	}

	return msi.contextTools.GetContext(ctx, request)
}

func (msi *MCPServerIntegration) handleAddContext(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request AddContextRequest
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid add_context parameters: %w", err)
	}

	return msi.contextTools.AddContext(ctx, request)
}

func (msi *MCPServerIntegration) handleProcessMentions(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request ProcessMentionsRequest
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid process_mentions parameters: %w", err)
	}

	return msi.contextTools.ProcessMentions(ctx, request)
}

func (msi *MCPServerIntegration) handleGetRelevantFiles(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request GetRelevantFilesRequest
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid get_relevant_files parameters: %w", err)
	}

	return msi.contextTools.GetRelevantFiles(ctx, request)
}

func (msi *MCPServerIntegration) handleClearContext(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request ClearContextRequest
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid clear_context parameters: %w", err)
	}

	return msi.contextTools.ClearContext(ctx, request)
}

func (msi *MCPServerIntegration) handleGetContextStats(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request GetContextStatsRequest
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid get_context_stats parameters: %w", err)
	}

	return msi.contextTools.GetContextStats(ctx, request)
}

func (msi *MCPServerIntegration) handleDiffView(ctx context.Context, params json.RawMessage) (interface{}, error) {
	// Implementation for diff_view tool would go here
	// This would integrate with the visual diff components
	return map[string]interface{}{
		"success": false,
		"error":   "diff_view tool not yet implemented",
	}, nil
}

func (msi *MCPServerIntegration) handleFileBrowser(ctx context.Context, params json.RawMessage) (interface{}, error) {
	// Implementation for file_browser tool would go here
	// This would integrate with the visual file browser
	return map[string]interface{}{
		"success": false,
		"error":   "file_browser tool not yet implemented",
	}, nil
}

func (msi *MCPServerIntegration) handleInteractiveEditor(ctx context.Context, params json.RawMessage) (interface{}, error) {
	// Implementation for interactive_editor tool would go here
	// This would integrate with the visual editor
	return map[string]interface{}{
		"success": false,
		"error":   "interactive_editor tool not yet implemented",
	}, nil
}

// GetStats returns statistics about the MCP server integration
func (msi *MCPServerIntegration) GetStats() map[string]interface{} {
	msi.mutex.RLock()
	defer msi.mutex.RUnlock()

	stats := map[string]interface{}{
		"registered_tools": len(msi.toolRegistry),
		"components": map[string]bool{
			"enhanced_tools":    msi.enhancedTools != nil,
			"context_tools":     msi.contextTools != nil,
			"file_editor":       msi.fileEditor != nil,
			"context_manager":   msi.contextManager != nil,
			"file_discovery":    msi.fileDiscovery != nil,
			"parser":            msi.parser != nil,
			"mention_processor": msi.mentionProcessor != nil,
		},
	}

	// Add context manager stats if available
	if msi.contextManager != nil {
		stats["context_stats"] = msi.contextManager.GetStats()
	}

	// Add parser stats if available
	if msi.parser != nil {
		stats["parser_stats"] = msi.parser.GetCacheStats()
	}

	return stats
}

// Shutdown gracefully shuts down the MCP server integration
func (msi *MCPServerIntegration) Shutdown(ctx context.Context) error {
	log.Println("Shutting down MCP server integration...")

	// Cleanup context manager
	if msi.contextManager != nil {
		if err := msi.contextManager.CleanupExpired(); err != nil {
			log.Printf("Error during context cleanup: %v", err)
		}
	}

	// Clear parser cache
	if msi.parser != nil {
		msi.parser.ClearCache()
	}

	log.Println("MCP server integration shutdown complete")
	return nil
}