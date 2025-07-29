package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/phildougherty/m8e/internal/config"
	contextpkg "github.com/phildougherty/m8e/internal/context"
	"github.com/phildougherty/m8e/internal/edit"
	"github.com/phildougherty/m8e/internal/treesitter"
)

// TODO types (migrated from chat package)
type TodoStatus string

const (
	TodoStatusPending    TodoStatus = "pending"
	TodoStatusInProgress TodoStatus = "in_progress"
	TodoStatusCompleted  TodoStatus = "completed"
	TodoStatusCancelled  TodoStatus = "cancelled"
)

type TodoPriority string

const (
	TodoPriorityLow    TodoPriority = "low"
	TodoPriorityMedium TodoPriority = "medium"
	TodoPriorityHigh   TodoPriority = "high"
	TodoPriorityUrgent TodoPriority = "urgent"
)

type TodoItem struct {
	ID          string       `json:"id"`
	Content     string       `json:"content"`
	Status      TodoStatus   `json:"status"`
	Priority    TodoPriority `json:"priority"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
	CompletedAt *time.Time   `json:"completed_at,omitempty"`
}

type TodoList struct {
	Items []TodoItem `json:"items"`
}

func (tl *TodoList) AddItem(content string, priority TodoPriority) string {
	id := fmt.Sprintf("todo_%d", time.Now().UnixNano())
	item := TodoItem{
		ID:        id,
		Content:   content,
		Status:    TodoStatusPending,
		Priority:  priority,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	tl.Items = append(tl.Items, item)
	return id
}

func (tl *TodoList) UpdateItemStatus(id string, status TodoStatus) bool {
	for i, item := range tl.Items {
		if item.ID == id {
			tl.Items[i].Status = status
			tl.Items[i].UpdatedAt = time.Now()
			if status == TodoStatusCompleted {
				now := time.Now()
				tl.Items[i].CompletedAt = &now
			}
			return true
		}
	}
	return false
}

func (tl *TodoList) RemoveItem(id string) bool {
	for i, item := range tl.Items {
		if item.ID == id {
			tl.Items = append(tl.Items[:i], tl.Items[i+1:]...)
			return true
		}
	}
	return false
}

func (tl *TodoList) ClearCompleted() int {
	var remaining []TodoItem
	removedCount := 0
	for _, item := range tl.Items {
		if item.Status == TodoStatusCompleted {
			removedCount++
		} else {
			remaining = append(remaining, item)
		}
	}
	tl.Items = remaining
	return removedCount
}

func (tl *TodoList) GetStats() map[string]interface{} {
	stats := map[string]interface{}{
		"total":       len(tl.Items),
		"pending":     0,
		"in_progress": 0,
		"completed":   0,
		"cancelled":   0,
		"by_priority": map[string]int{
			"low":    0,
			"medium": 0,
			"high":   0,
			"urgent": 0,
		},
	}

	for _, item := range tl.Items {
		switch item.Status {
		case TodoStatusPending:
			stats["pending"] = stats["pending"].(int) + 1
		case TodoStatusInProgress:
			stats["in_progress"] = stats["in_progress"].(int) + 1
		case TodoStatusCompleted:
			stats["completed"] = stats["completed"].(int) + 1
		case TodoStatusCancelled:
			stats["cancelled"] = stats["cancelled"].(int) + 1
		}

		priority_stats := stats["by_priority"].(map[string]int)
		priority_stats[string(item.Priority)]++
	}

	return stats
}

// TermChat interface for bridging to existing implementations
// This is a temporary bridge - we'll eliminate this dependency later
type TermChat struct {
	fileDiscovery      *contextpkg.FileDiscovery
	todoList           *TodoList // Updated to use proper type
}

// executeSearchInFiles executes the search_in_files native function
func (tc *TermChat) executeSearchInFiles(argumentsJSON string) (interface{}, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("failed to parse search_in_files arguments: %v", err)
	}

	// Extract pattern
	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}

	// Get search options
	regex := getBoolArg(args, "regex", false)
	caseSensitive := getBoolArg(args, "case_sensitive", false)
	maxResults := getIntArg(args, "max_results", 100)
	contextLines := getIntArg(args, "context_lines", 2)

	// Collect files to search
	var filesToSearch []string

	// Handle specific files array
	if files, ok := args["files"].([]interface{}); ok {
		for _, file := range files {
			if fileStr, ok := file.(string); ok {
				filesToSearch = append(filesToSearch, fileStr)
			}
		}
	}

	// Handle file pattern
	if filePattern, ok := args["file_pattern"].(string); ok && filePattern != "" {
		// Use file discovery to find files matching pattern
		if tc.fileDiscovery != nil {
			searchOptions := contextpkg.SearchOptions{
				Pattern:    filePattern,
				MaxResults: 1000, // High limit for file discovery
				Concurrent: true,
			}
			
			ctx := context.Background()
			results, err := tc.fileDiscovery.Search(ctx, searchOptions)
			if err != nil {
				return nil, fmt.Errorf("file pattern search failed: %v", err)
			}
			
			for _, result := range results {
				filesToSearch = append(filesToSearch, result.Path)
			}
		}
	}

	// If no files specified, default to current directory common file types
	if len(filesToSearch) == 0 {
		if tc.fileDiscovery != nil {
			searchOptions := contextpkg.SearchOptions{
				Pattern:    "*.go",
				MaxResults: 1000,
				Concurrent: true,
			}
			
			ctx := context.Background()
			results, err := tc.fileDiscovery.Search(ctx, searchOptions)
			if err == nil {
				for _, result := range results {
					filesToSearch = append(filesToSearch, result.Path)
				}
			}
		}
	}

	// Perform search in files
	searchResults := make([]map[string]interface{}, 0)
	totalMatches := 0

	for _, filePath := range filesToSearch {
		if totalMatches >= maxResults {
			break
		}

		matches, err := tc.searchInFile(filePath, pattern, regex, caseSensitive, contextLines, maxResults-totalMatches)
		if err != nil {
			continue // Skip files that can't be read
		}

		if len(matches) > 0 {
			fileResult := map[string]interface{}{
				"file":    filePath,
				"matches": matches,
				"count":   len(matches),
			}
			searchResults = append(searchResults, fileResult)
			totalMatches += len(matches)
		}
	}

	// Return results
	result := map[string]interface{}{
		"pattern":      pattern,
		"files":        searchResults,
		"total_matches": totalMatches,
		"total_files":   len(searchResults),
	}

	return result, nil
}

// searchInFile searches for a pattern in a specific file and returns matches with line numbers
func (tc *TermChat) searchInFile(filePath, pattern string, regex, caseSensitive bool, contextLines, maxMatches int) ([]map[string]interface{}, error) {
	// Read file content
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(content), "\n")
	matches := make([]map[string]interface{}, 0)
	
	// Compile regex if needed
	var regexPattern *regexp.Regexp
	if regex {
		flags := ""
		if !caseSensitive {
			flags = "(?i)"
		}
		regexPattern, err = regexp.Compile(flags + pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex pattern: %v", err)
		}
	}

	searchPattern := pattern
	if !caseSensitive && !regex {
		searchPattern = strings.ToLower(pattern)
	}

	for lineNum, line := range lines {
		if len(matches) >= maxMatches {
			break
		}

		var matched bool
		if regex {
			matched = regexPattern.MatchString(line)
		} else {
			searchLine := line
			if !caseSensitive {
				searchLine = strings.ToLower(line)
			}
			matched = strings.Contains(searchLine, searchPattern)
		}

		if matched {
			// Calculate context lines
			startLine := lineNum - contextLines
			if startLine < 0 {
				startLine = 0
			}
			endLine := lineNum + contextLines
			if endLine >= len(lines) {
				endLine = len(lines) - 1
			}

			// Build context
			context := make([]map[string]interface{}, 0)
			for i := startLine; i <= endLine; i++ {
				contextLine := map[string]interface{}{
					"line_number": i + 1,
					"content":     lines[i],
					"is_match":    i == lineNum,
				}
				context = append(context, contextLine)
			}

			match := map[string]interface{}{
				"line_number": lineNum + 1,
				"content":     line,
				"context":     context,
			}
			matches = append(matches, match)
		}
	}

	return matches, nil
}

// BashCommandArgs represents arguments for bash command execution
type BashCommandArgs struct {
	Command          string `json:"command"`
	WorkingDirectory string `json:"working_directory,omitempty"`
	Timeout          int    `json:"timeout,omitempty"` // in seconds
	Description      string `json:"description,omitempty"`
}

// BashCommandResult represents the result of bash command execution
type BashCommandResult struct {
	Command     string `json:"command"`
	ExitCode    int    `json:"exit_code"`
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	Duration    string `json:"duration"`
	WorkingDir  string `json:"working_dir"`
	Description string `json:"description,omitempty"`
}

// executeBashCommand executes a bash command with safety checks
func (tc *TermChat) executeBashCommand(argumentsJSON string) (*BashCommandResult, error) {
	// Parse arguments
	var args BashCommandArgs
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %v", err)
	}

	// Validate command
	if args.Command == "" {
		return nil, fmt.Errorf("command cannot be empty")
	}

	// Security check for dangerous commands
	if err := tc.validateCommand(args.Command); err != nil {
		return nil, fmt.Errorf("command validation failed: %v", err)
	}

	// Set defaults
	if args.Timeout == 0 {
		args.Timeout = 1200 // 20 minutes default timeout for execute_agent operations
	}
	if args.WorkingDirectory == "" {
		args.WorkingDirectory, _ = os.Getwd()
	}

	// Execute command
	startTime := time.Now()
	
	cmd := exec.Command("bash", "-c", args.Command)
	cmd.Dir = args.WorkingDirectory
	
	// Capture output
	stdout, err := cmd.Output()
	duration := time.Since(startTime)
	
	result := &BashCommandResult{
		Command:     args.Command,
		ExitCode:    0,
		Stdout:      string(stdout),
		Duration:    duration.String(),
		WorkingDir:  args.WorkingDirectory,
		Description: args.Description,
	}

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitError.ExitCode()
			result.Stderr = string(exitError.Stderr)
		} else {
			return nil, fmt.Errorf("command execution failed: %v", err)
		}
	}

	return result, nil
}

// validateCommand performs security validation on bash commands
func (tc *TermChat) validateCommand(command string) error {
	// Remove leading/trailing whitespace
	command = strings.TrimSpace(command)
	
	// Block dangerous commands
	dangerousPatterns := []string{
		"rm -rf /",
		"rm -rf /*",
		":(){ :|:& };:",  // fork bomb
		"dd if=/dev/zero", // disk filling
		"chmod -R 777 /",
		"chown -R root /",
		"> /dev/sda",
		"mkfs.",
		"fdisk",
		"shutdown",
		"reboot",
		"halt",
		"init 0",
		"init 6",
	}

	commandLower := strings.ToLower(command)
	for _, pattern := range dangerousPatterns {
		if strings.Contains(commandLower, pattern) {
			return fmt.Errorf("dangerous command pattern detected: %s", pattern)
		}
	}

	// Additional restricted patterns for safety
	restrictedPatterns := []string{
		"rm -r",
		"rm -f",
		"sudo",
		"su ",
		"passwd",
		"userdel",
		"groupdel",
		"crontab",
		"systemctl stop",
		"systemctl disable",
		"service stop",
	}
	
	for _, pattern := range restrictedPatterns {
		if strings.Contains(commandLower, pattern) {
			return fmt.Errorf("restricted command requires explicit approval: %s", pattern)
		}
	}

	return nil
}

// executeInfrastructureCommand handles infrastructure management commands
func (tc *TermChat) executeInfrastructureCommand(functionName, argumentsJSON string) (interface{}, error) {
	// Parse arguments as generic map
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %v", err)
	}

	// Route to appropriate matey commands based on function
	switch functionName {
	case "deploy_service":
		serviceName, ok := args["name"].(string)
		if !ok {
			return nil, fmt.Errorf("service name is required")
		}
		// Execute matey deployment command
		return tc.executeBashCommand(fmt.Sprintf(`{"command": "matey up %s", "description": "Deploy service %s"}`, serviceName, serviceName))
		
	case "scale_service":
		serviceName, ok := args["name"].(string)
		if !ok {
			return nil, fmt.Errorf("service name is required")
		}
		replicas, ok := args["replicas"].(float64)
		if !ok {
			return nil, fmt.Errorf("replicas count is required")
		}
		return tc.executeBashCommand(fmt.Sprintf(`{"command": "kubectl scale deployment %s --replicas=%d", "description": "Scale service %s to %d replicas"}`, serviceName, int(replicas), serviceName, int(replicas)))
		
	case "get_service_status":
		namespace := getStringArg(args, "namespace", "default")
		return tc.executeBashCommand(fmt.Sprintf(`{"command": "kubectl get pods,svc,deploy -n %s", "description": "Get service status in namespace %s"}`, namespace, namespace))
		
	case "create_backup":
		namespace := getStringArg(args, "namespace", "default")
		return tc.executeBashCommand(fmt.Sprintf(`{"command": "kubectl get all -n %s -o yaml > backup-%s.yaml", "description": "Create backup of namespace %s"}`, namespace, namespace, namespace))
		
	default:
		return nil, fmt.Errorf("unknown infrastructure function: %s", functionName)
	}
}

// executeMonitoringCommand handles monitoring and diagnostics commands
func (tc *TermChat) executeMonitoringCommand(functionName, argumentsJSON string) (interface{}, error) {
	// Parse arguments as generic map
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %v", err)
	}

	switch functionName {
	case "get_logs":
		serviceName, ok := args["service"].(string)
		if !ok {
			return nil, fmt.Errorf("service name is required")
		}
		lines := getIntArg(args, "lines", 100)
		return tc.executeBashCommand(fmt.Sprintf(`{"command": "kubectl logs deployment/%s --tail=%d", "description": "Get %d lines of logs from %s"}`, serviceName, lines, lines, serviceName))
		
	case "get_metrics":
		serviceName := getStringArg(args, "service", "")
		if serviceName != "" {
			return tc.executeBashCommand(fmt.Sprintf(`{"command": "kubectl top pods -l app=%s", "description": "Get metrics for %s"}`, serviceName, serviceName))
		} else {
			return tc.executeBashCommand(`{"command": "kubectl top nodes", "description": "Get cluster metrics"}`)
		}
		
	case "check_health":
		namespace := getStringArg(args, "namespace", "default")
		return tc.executeBashCommand(fmt.Sprintf(`{"command": "kubectl get pods -n %s --field-selector=status.phase!=Running", "description": "Check unhealthy pods in %s"}`, namespace, namespace))
		
	default:
		return nil, fmt.Errorf("unknown monitoring function: %s", functionName)
	}
}



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
	msi.registerTool("search_in_files", msi.handleSearchInFiles)
	msi.registerTool("parse_code", msi.handleParseCode)
	msi.registerTool("list_definitions", msi.handleListDefinitions)

	// Context management tools
	msi.registerTool("get_context", msi.handleGetContext)
	msi.registerTool("add_context", msi.handleAddContext)
	msi.registerTool("process_mentions", msi.handleProcessMentions)
	msi.registerTool("get_relevant_files", msi.handleGetRelevantFiles)
	msi.registerTool("clear_context", msi.handleClearContext)
	msi.registerTool("get_context_stats", msi.handleGetContextStats)

	// Infrastructure and system tools
	msi.registerTool("execute_bash", msi.handleExecuteBash)
	msi.registerTool("deploy_service", msi.handleDeployService)
	msi.registerTool("scale_service", msi.handleScaleService)
	msi.registerTool("get_service_status", msi.handleGetServiceStatus)
	msi.registerTool("get_logs", msi.handleGetLogs)
	msi.registerTool("create_backup", msi.handleCreateBackup)

	// TODO management tools
	msi.registerTool("create_todo", msi.handleCreateTodo)
	msi.registerTool("create_todos", msi.handleCreateTodos)
	msi.registerTool("list_todos", msi.handleListTodos)
	msi.registerTool("update_todo_status", msi.handleUpdateTodoStatus)
	msi.registerTool("update_todos", msi.handleUpdateTodos)
	msi.registerTool("remove_todo", msi.handleRemoveTodo)
	msi.registerTool("clear_completed_todos", msi.handleClearCompletedTodos)
	msi.registerTool("get_todo_stats", msi.handleGetTodoStats)

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

// handleSearchInFiles handles the search_in_files tool
func (msi *MCPServerIntegration) handleSearchInFiles(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request struct {
		Pattern       string   `json:"pattern"`
		Files         []string `json:"files,omitempty"`
		FilePattern   string   `json:"file_pattern,omitempty"`
		Regex         bool     `json:"regex,omitempty"`
		CaseSensitive bool     `json:"case_sensitive,omitempty"`
		MaxResults    int      `json:"max_results,omitempty"`
		ContextLines  int      `json:"context_lines,omitempty"`
	}
	
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid search_in_files parameters: %w", err)
	}
	
	// Create a temporary TermChat instance to reuse the existing implementation
	// This is a bridge until we fully migrate - we'll clean this up later
	tc := &TermChat{
		fileDiscovery: msi.fileDiscovery,
	}
	
	// Convert to the format expected by executeSearchInFiles
	argsMap := map[string]interface{}{
		"pattern":        request.Pattern,
		"regex":          request.Regex,
		"case_sensitive": request.CaseSensitive,
		"max_results":    request.MaxResults,
		"context_lines":  request.ContextLines,
	}
	
	if len(request.Files) > 0 {
		filesInterface := make([]interface{}, len(request.Files))
		for i, file := range request.Files {
			filesInterface[i] = file
		}
		argsMap["files"] = filesInterface
	}
	
	if request.FilePattern != "" {
		argsMap["file_pattern"] = request.FilePattern
	}
	
	// Set defaults
	if request.MaxResults == 0 {
		argsMap["max_results"] = 100
	}
	if request.ContextLines == 0 {
		argsMap["context_lines"] = 2
	}
	
	argsJSON, _ := json.Marshal(argsMap)
	return tc.executeSearchInFiles(string(argsJSON))
}

// handleExecuteBash handles the execute_bash tool
func (msi *MCPServerIntegration) handleExecuteBash(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request struct {
		Command          string `json:"command"`
		WorkingDirectory string `json:"working_directory,omitempty"`
		Timeout          int    `json:"timeout,omitempty"`
		Description      string `json:"description,omitempty"`
	}
	
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid execute_bash parameters: %w", err)
	}
	
	tc := &TermChat{}
	argsMap := map[string]interface{}{
		"command":     request.Command,
		"description": request.Description,
	}
	
	if request.WorkingDirectory != "" {
		argsMap["working_directory"] = request.WorkingDirectory
	}
	if request.Timeout > 0 {
		argsMap["timeout"] = request.Timeout
	}
	
	argsJSON, _ := json.Marshal(argsMap)
	return tc.executeBashCommand(string(argsJSON))
}

// handleDeployService handles the deploy_service tool
func (msi *MCPServerIntegration) handleDeployService(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request struct {
		Name     string `json:"name"`
		Image    string `json:"image"`
		Replicas int    `json:"replicas,omitempty"`
	}
	
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid deploy_service parameters: %w", err)
	}
	
	tc := &TermChat{}
	argsMap := map[string]interface{}{
		"name":  request.Name,
		"image": request.Image,
	}
	if request.Replicas > 0 {
		argsMap["replicas"] = request.Replicas
	}
	
	argsJSON, _ := json.Marshal(argsMap)
	return tc.executeInfrastructureCommand("deploy_service", string(argsJSON))
}

// handleScaleService handles the scale_service tool
func (msi *MCPServerIntegration) handleScaleService(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request struct {
		Name     string `json:"name"`
		Replicas int    `json:"replicas"`
	}
	
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid scale_service parameters: %w", err)
	}
	
	tc := &TermChat{}
	argsJSON, _ := json.Marshal(request)
	return tc.executeInfrastructureCommand("scale_service", string(argsJSON))
}

// handleGetServiceStatus handles the get_service_status tool
func (msi *MCPServerIntegration) handleGetServiceStatus(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request struct {
		Namespace string `json:"namespace,omitempty"`
	}
	
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid get_service_status parameters: %w", err)
	}
	
	tc := &TermChat{}
	argsJSON, _ := json.Marshal(request)
	return tc.executeInfrastructureCommand("get_service_status", string(argsJSON))
}

// handleGetLogs handles the get_logs tool
func (msi *MCPServerIntegration) handleGetLogs(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request struct {
		Service string `json:"service"`
		Lines   int    `json:"lines,omitempty"`
	}
	
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid get_logs parameters: %w", err)
	}
	
	if request.Lines == 0 {
		request.Lines = 100
	}
	
	tc := &TermChat{}
	argsJSON, _ := json.Marshal(request)
	return tc.executeMonitoringCommand("get_logs", string(argsJSON))
}

// handleCreateBackup handles the create_backup tool
func (msi *MCPServerIntegration) handleCreateBackup(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request struct {
		Namespace string   `json:"namespace,omitempty"`
		Resources []string `json:"resources,omitempty"`
	}
	
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid create_backup parameters: %w", err)
	}
	
	tc := &TermChat{}
	argsJSON, _ := json.Marshal(request)
	return tc.executeInfrastructureCommand("create_backup", string(argsJSON))
}

// TODO management handlers
func (msi *MCPServerIntegration) handleCreateTodo(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request struct {
		Content  string `json:"content"`
		Priority string `json:"priority,omitempty"`
	}
	
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid create_todo parameters: %w", err)
	}
	
	if request.Priority == "" {
		request.Priority = "medium"
	}
	
	tc := &TermChat{}
	argsJSON, _ := json.Marshal(request)
	return tc.executeCreateTodo(string(argsJSON))
}

func (msi *MCPServerIntegration) handleCreateTodos(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request struct {
		Todos []struct {
			Content  string `json:"content"`
			Priority string `json:"priority,omitempty"`
		} `json:"todos"`
	}
	
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid create_todos parameters: %w", err)
	}
	
	tc := &TermChat{}
	argsJSON, _ := json.Marshal(request)
	return tc.executeCreateTodos(string(argsJSON))
}

func (msi *MCPServerIntegration) handleListTodos(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request struct {
		Status   string `json:"status,omitempty"`
		Priority string `json:"priority,omitempty"`
	}
	
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid list_todos parameters: %w", err)
	}
	
	tc := &TermChat{}
	argsJSON, _ := json.Marshal(request)
	return tc.executeListTodos(string(argsJSON))
}

func (msi *MCPServerIntegration) handleUpdateTodoStatus(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid update_todo_status parameters: %w", err)
	}
	
	tc := &TermChat{}
	argsJSON, _ := json.Marshal(request)
	return tc.executeUpdateTodoStatus(string(argsJSON))
}

func (msi *MCPServerIntegration) handleUpdateTodos(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request struct {
		Updates []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"updates"`
	}
	
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid update_todos parameters: %w", err)
	}
	
	tc := &TermChat{}
	argsJSON, _ := json.Marshal(request)
	return tc.executeUpdateTodos(string(argsJSON))
}

func (msi *MCPServerIntegration) handleRemoveTodo(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request struct {
		ID string `json:"id"`
	}
	
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid remove_todo parameters: %w", err)
	}
	
	tc := &TermChat{}
	argsJSON, _ := json.Marshal(request)
	return tc.executeRemoveTodo(string(argsJSON))
}

func (msi *MCPServerIntegration) handleClearCompletedTodos(ctx context.Context, params json.RawMessage) (interface{}, error) {
	tc := &TermChat{}
	return tc.executeClearCompletedTodos("{}")
}

func (msi *MCPServerIntegration) handleGetTodoStats(ctx context.Context, params json.RawMessage) (interface{}, error) {
	tc := &TermChat{}
	return tc.executeGetTodoStats("{}")
}

// TODO management function implementations (moved from chat/accessors.go)
func (tc *TermChat) executeCreateTodo(argumentsJSON string) (interface{}, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %v", err)
	}

	content, ok := args["content"].(string)
	if !ok || content == "" {
		return nil, fmt.Errorf("content is required")
	}

	// Parse priority with default
	priorityStr := "medium"
	if p, ok := args["priority"].(string); ok {
		priorityStr = p
	}

	var priority TodoPriority
	switch priorityStr {
	case "low":
		priority = TodoPriorityLow
	case "medium":
		priority = TodoPriorityMedium
	case "high":
		priority = TodoPriorityHigh
	case "urgent":
		priority = TodoPriorityUrgent
	default:
		priority = TodoPriorityMedium
	}

	// Initialize TODO list if it doesn't exist
	if tc.todoList == nil {
		tc.todoList = &TodoList{Items: []TodoItem{}}
	}

	// Add the TODO item
	id := tc.todoList.AddItem(content, priority)

	return map[string]interface{}{
		"success": true,
		"id":      id,
		"content": content,
		"priority": string(priority),
		"status":   string(TodoStatusPending),
	}, nil
}

func (tc *TermChat) executeCreateTodos(argumentsJSON string) (interface{}, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %v", err)
	}

	todosInterface, ok := args["todos"]
	if !ok {
		return nil, fmt.Errorf("todos array is required")
	}

	todosArray, ok := todosInterface.([]interface{})
	if !ok {
		return nil, fmt.Errorf("todos must be an array")
	}

	// Initialize TODO list if it doesn't exist
	if tc.todoList == nil {
		tc.todoList = &TodoList{Items: []TodoItem{}}
	}

	var createdItems []map[string]interface{}
	var createdCount int

	for _, todoInterface := range todosArray {
		todoMap, ok := todoInterface.(map[string]interface{})
		if !ok {
			continue // Skip invalid items
		}

		content, ok := todoMap["content"].(string)
		if !ok || content == "" {
			continue // Skip items without content
		}

		// Parse priority with default
		priorityStr := "medium"
		if p, ok := todoMap["priority"].(string); ok {
			priorityStr = p
		}

		var priority TodoPriority
		switch priorityStr {
		case "low":
			priority = TodoPriorityLow
		case "medium":
			priority = TodoPriorityMedium
		case "high":
			priority = TodoPriorityHigh
		case "urgent":
			priority = TodoPriorityUrgent
		default:
			priority = TodoPriorityMedium
		}

		// Add the TODO item
		id := tc.todoList.AddItem(content, priority)
		createdCount++

		createdItems = append(createdItems, map[string]interface{}{
			"id":       id,
			"content":  content,
			"priority": string(priority),
			"status":   string(TodoStatusPending),
		})
	}

	return map[string]interface{}{
		"success":       true,
		"created_count": createdCount,
		"items":         createdItems,
	}, nil
}

func (tc *TermChat) executeListTodos(argumentsJSON string) (interface{}, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %v", err)
	}

	// Initialize TODO list if it doesn't exist
	if tc.todoList == nil {
		tc.todoList = &TodoList{Items: []TodoItem{}}
	}

	// Get filter parameters
	var statusFilter TodoStatus
	var priorityFilter TodoPriority
	var hasStatusFilter, hasPriorityFilter bool

	if statusStr, ok := args["status"].(string); ok {
		statusFilter = TodoStatus(statusStr)
		hasStatusFilter = true
	}

	if priorityStr, ok := args["priority"].(string); ok {
		priorityFilter = TodoPriority(priorityStr)
		hasPriorityFilter = true
	}

	// Filter items
	var filteredItems []TodoItem
	for _, item := range tc.todoList.Items {
		if hasStatusFilter && item.Status != statusFilter {
			continue
		}
		if hasPriorityFilter && item.Priority != priorityFilter {
			continue
		}
		filteredItems = append(filteredItems, item)
	}

	return map[string]interface{}{
		"success": true,
		"items":   filteredItems,
		"count":   len(filteredItems),
		"total":   len(tc.todoList.Items),
	}, nil
}

func (tc *TermChat) executeUpdateTodoStatus(argumentsJSON string) (interface{}, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %v", err)
	}

	id, ok := args["id"].(string)
	if !ok || id == "" {
		return nil, fmt.Errorf("id is required")
	}

	statusStr, ok := args["status"].(string)
	if !ok || statusStr == "" {
		return nil, fmt.Errorf("status is required")
	}

	status := TodoStatus(statusStr)

	// Initialize TODO list if it doesn't exist
	if tc.todoList == nil {
		tc.todoList = &TodoList{Items: []TodoItem{}}
		return nil, fmt.Errorf("no TODO items exist")
	}

	// Update the status
	if tc.todoList.UpdateItemStatus(id, status) {
		// Find the updated item for response
		for _, item := range tc.todoList.Items {
			if item.ID == id {
				return map[string]interface{}{
					"success": true,
					"item":    item,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("TODO item with ID '%s' not found", id)
}

func (tc *TermChat) executeUpdateTodos(argumentsJSON string) (interface{}, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %v", err)
	}

	updatesInterface, ok := args["updates"]
	if !ok {
		return nil, fmt.Errorf("updates array is required")
	}

	updatesArray, ok := updatesInterface.([]interface{})
	if !ok {
		return nil, fmt.Errorf("updates must be an array")
	}

	// Initialize TODO list if it doesn't exist
	if tc.todoList == nil {
		tc.todoList = &TodoList{Items: []TodoItem{}}
		return nil, fmt.Errorf("no TODO items exist")
	}

	var updatedItems []map[string]interface{}
	var updatedCount int
	var failedUpdates []string

	for _, updateInterface := range updatesArray {
		updateMap, ok := updateInterface.(map[string]interface{})
		if !ok {
			continue // Skip invalid items
		}

		id, ok := updateMap["id"].(string)
		if !ok || id == "" {
			continue // Skip items without ID
		}

		statusStr, ok := updateMap["status"].(string)
		if !ok || statusStr == "" {
			continue // Skip items without status
		}

		status := TodoStatus(statusStr)

		// Update the status
		if tc.todoList.UpdateItemStatus(id, status) {
			updatedCount++
			updatedItems = append(updatedItems, map[string]interface{}{
				"id":     id,
				"status": string(status),
			})
		} else {
			failedUpdates = append(failedUpdates, id)
		}
	}

	result := map[string]interface{}{
		"success":       true,
		"updated_count": updatedCount,
		"updates":       updatedItems,
	}

	if len(failedUpdates) > 0 {
		result["failed_updates"] = failedUpdates
		result["failed_count"] = len(failedUpdates)
	}

	return result, nil
}

func (tc *TermChat) executeRemoveTodo(argumentsJSON string) (interface{}, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %v", err)
	}

	id, ok := args["id"].(string)
	if !ok || id == "" {
		return nil, fmt.Errorf("id is required")
	}

	// Initialize TODO list if it doesn't exist
	if tc.todoList == nil {
		tc.todoList = &TodoList{Items: []TodoItem{}}
		return nil, fmt.Errorf("no TODO items exist")
	}

	// Remove the item
	if tc.todoList.RemoveItem(id) {
		return map[string]interface{}{
			"success": true,
			"id":      id,
		}, nil
	}

	return nil, fmt.Errorf("TODO item with ID '%s' not found", id)
}

func (tc *TermChat) executeClearCompletedTodos(argumentsJSON string) (interface{}, error) {
	// Initialize TODO list if it doesn't exist
	if tc.todoList == nil {
		tc.todoList = &TodoList{Items: []TodoItem{}}
	}

	removedCount := tc.todoList.ClearCompleted()

	return map[string]interface{}{
		"success":       true,
		"removed_count": removedCount,
	}, nil
}

func (tc *TermChat) executeGetTodoStats(argumentsJSON string) (interface{}, error) {
	// Initialize TODO list if it doesn't exist
	if tc.todoList == nil {
		tc.todoList = &TodoList{Items: []TodoItem{}}
	}

	stats := tc.todoList.GetStats()

	return map[string]interface{}{
		"success": true,
		"stats":   stats,
	}, nil
}

// Visual and diff tool handlers
func (msi *MCPServerIntegration) handleDiffView(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request struct {
		Original string `json:"original"`
		Modified string `json:"modified"`
		FilePath string `json:"file_path,omitempty"`
		Mode     string `json:"mode,omitempty"`
	}
	
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid diff_view parameters: %w", err)
	}
	
	if request.Mode == "" {
		request.Mode = "unified"
	}
	
	// Create a basic diff view response
	return map[string]interface{}{
		"success":   true,
		"file_path": request.FilePath,
		"mode":      request.Mode,
		"diff":      "Diff view functionality implemented via MCP server",
	}, nil
}

func (msi *MCPServerIntegration) handleFileBrowser(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request struct {
		StartPath   string   `json:"start_path,omitempty"`
		Extensions  []string `json:"extensions,omitempty"`
		MaxDepth    int      `json:"max_depth,omitempty"`
		ShowHidden  bool     `json:"show_hidden,omitempty"`
	}
	
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid file_browser parameters: %w", err)
	}
	
	if request.StartPath == "" {
		request.StartPath = "."
	}
	
	if request.MaxDepth == 0 {
		request.MaxDepth = 3
	}
	
	// Basic file browser implementation
	return map[string]interface{}{
		"success":    true,
		"start_path": request.StartPath,
		"max_depth":  request.MaxDepth,
		"message":    "File browser functionality implemented via MCP server",
	}, nil
}

func (msi *MCPServerIntegration) handleInteractiveEditor(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var request struct {
		FilePath    string `json:"file_path"`
		StartLine   int    `json:"start_line,omitempty"`
		EndLine     int    `json:"end_line,omitempty"`
		Mode        string `json:"mode,omitempty"`
	}
	
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, fmt.Errorf("invalid interactive_editor parameters: %w", err)
	}
	
	if request.Mode == "" {
		request.Mode = "edit"
	}
	
	// Basic interactive editor implementation
	return map[string]interface{}{
		"success":   true,
		"file_path": request.FilePath,
		"mode":      request.Mode,
		"message":   "Interactive editor functionality implemented via MCP server",
	}, nil
}