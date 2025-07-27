package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/phildougherty/m8e/internal/ai"
	"github.com/phildougherty/m8e/internal/edit"
	"github.com/phildougherty/m8e/internal/mcp"
	appcontext "github.com/phildougherty/m8e/internal/context"
)

// Public accessor methods for turn.go compatibility

// GetSystemContext returns the system context (wrapper for GetOptimizedSystemPrompt)
func (tc *TermChat) GetSystemContext() string {
	return tc.GetOptimizedSystemPrompt()
}

// GetChatHistory returns the chat history
func (tc *TermChat) GetChatHistory() []TermChatMessage {
	return tc.chatHistory
}

// AddMessage adds a message to chat history (public wrapper)
func (tc *TermChat) AddMessage(role, content string) {
	tc.addMessage(role, content)
}

// GetMCPFunctions returns MCP functions (public wrapper)
func (tc *TermChat) GetMCPFunctions() []ai.Function {
	return tc.getMCPFunctions()
}

// GetAIManager returns the AI manager
func (tc *TermChat) GetAIManager() *ai.Manager {
	return tc.aiManager
}


// GetCurrentModel returns the current model
func (tc *TermChat) GetCurrentModel() string {
	return tc.currentModel
}

// GetVerboseMode returns the verbose mode setting
func (tc *TermChat) GetVerboseMode() bool {
	return tc.verboseMode
}

// GetApprovalMode returns the current approval mode
func (tc *TermChat) GetApprovalMode() ApprovalMode {
	return tc.approvalMode
}

// RequestFunctionConfirmation requests confirmation for function execution
func (tc *TermChat) RequestFunctionConfirmation(functionName, arguments string) bool {
	return tc.requestFunctionConfirmation(functionName, arguments)
}

// requestFunctionConfirmation asks the user to confirm a function call
func (tc *TermChat) requestFunctionConfirmation(functionName, arguments string) bool {
	// For now, auto-approve based on approval mode
	switch tc.approvalMode {
	case YOLO:
		return true
	case AUTO_EDIT:
		// Auto-approve safe operations
		return true
	default:
		// Default to manual approval - for UI integration this would use the callback
		if tc.uiConfirmationCallback != nil {
			return tc.uiConfirmationCallback(functionName, arguments)
		}
		return true // Fallback to approve
	}
}

// GetMCPClient returns the MCP client
func (tc *TermChat) GetMCPClient() *mcp.MCPClient {
	return tc.mcpClient
}

// ExecuteNativeToolCall executes a native tool call
func (tc *TermChat) ExecuteNativeToolCall(toolCall interface{}, index int) (interface{}, error) {
	// Convert toolCall to ai.ToolCall type
	var call ai.ToolCall
	switch v := toolCall.(type) {
	case ai.ToolCall:
		call = v
	default:
		return nil, fmt.Errorf("unsupported tool call type: %T", toolCall)
	}

	// Route to specific native function handlers
	switch call.Function.Name {
	case "matey_list_mcp_servers", "list_mcp_servers":
		return tc.executeListMCPServers(call.Function.Arguments)
	case "execute_bash", "bash", "run_command":
		return tc.executeBashCommand(call.Function.Arguments)
	case "deploy_service", "scale_service", "restart_service":
		return tc.executeInfrastructureCommand(call.Function.Name, call.Function.Arguments)
	case "get_logs", "get_metrics", "check_health", "get_service_status", "create_backup":
		return tc.executeMonitoringCommand(call.Function.Name, call.Function.Arguments)
	case "edit_file", "editfile", "edit-file":
		return tc.executeEditFile(call.Function.Arguments)
	case "read_file", "readfile", "read-file":
		return tc.executeReadFile(call.Function.Arguments)
	case "search_files", "searchfiles", "search-files":
		return tc.executeSearchFiles(call.Function.Arguments)
	case "search_in_files", "searchinfiles", "search-in-files":
		return tc.executeSearchInFiles(call.Function.Arguments)
	case "parse_code", "parsecode", "parse-code":
		return tc.executeParseCode(call.Function.Arguments)
	case "create_todo":
		return tc.executeCreateTodo(call.Function.Arguments)
	case "create_todos":
		return tc.executeCreateTodos(call.Function.Arguments)
	case "list_todos":
		return tc.executeListTodos(call.Function.Arguments)
	case "update_todo_status":
		return tc.executeUpdateTodoStatus(call.Function.Arguments)
	case "update_todos":
		return tc.executeUpdateTodos(call.Function.Arguments)
	case "remove_todo":
		return tc.executeRemoveTodo(call.Function.Arguments)
	case "clear_completed_todos":
		return tc.executeClearCompletedTodos(call.Function.Arguments)
	case "get_todo_stats":
		return tc.executeGetTodoStats(call.Function.Arguments)
	default:
		return nil, fmt.Errorf("unknown native function: %s", call.Function.Name)
	}
}

// FindServerForTool finds the appropriate server for a tool
func (tc *TermChat) FindServerForTool(toolName string) string {
	serverData := tc.getComprehensiveMCPData()
	
	for _, server := range serverData {
		if server.ConnectionStatus != "connected" {
			continue
		}
		
		for _, tool := range server.Tools {
			if tool.Name == toolName {
				return server.Name
			}
		}
	}
	
	return ""
}

// BashCommandArgs represents the arguments for bash command execution
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
	
	// Block dangerous commands based on approval mode
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

	// Additional validation based on approval mode
	approvalMode := tc.approvalMode
	if approvalMode == DEFAULT {
		// In manual mode, be more restrictive
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
		
	case "restart_service":
		serviceName, ok := args["name"].(string)
		if !ok {
			return nil, fmt.Errorf("service name is required")
		}
		return tc.executeBashCommand(fmt.Sprintf(`{"command": "kubectl rollout restart deployment %s", "description": "Restart service %s"}`, serviceName, serviceName))
		
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

	// Route to appropriate monitoring commands
	switch functionName {
	case "get_logs":
		serviceName, ok := args["service"].(string)
		if !ok {
			return nil, fmt.Errorf("service name is required")
		}
		lines := 100 // default
		if l, ok := args["lines"].(float64); ok {
			lines = int(l)
		}
		return tc.executeBashCommand(fmt.Sprintf(`{"command": "kubectl logs deployment/%s --tail=%d", "description": "Get logs for service %s"}`, serviceName, lines, serviceName))
		
	case "get_metrics":
		serviceName, ok := args["service"].(string)
		if !ok {
			return nil, fmt.Errorf("service name is required")
		}
		return tc.executeBashCommand(fmt.Sprintf(`{"command": "kubectl top pods -l app=%s", "description": "Get metrics for service %s"}`, serviceName, serviceName))
		
	case "check_health":
		serviceName, ok := args["service"].(string)
		if !ok {
			return nil, fmt.Errorf("service name is required")
		}
		return tc.executeBashCommand(fmt.Sprintf(`{"command": "kubectl get pods -l app=%s -o wide", "description": "Check health status for service %s"}`, serviceName, serviceName))
		
	default:
		return nil, fmt.Errorf("unknown monitoring function: %s", functionName)
	}
}

// MCPServerListResult represents the result of listing MCP servers
type MCPServerListResult struct {
	Servers []MCPServerInfo `json:"servers"`
	Total   int             `json:"total"`
	Status  string          `json:"status"`
}

// MCPServerInfo represents information about an MCP server
type MCPServerInfo struct {
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	Tools       []string `json:"tools"`
	Description string   `json:"description"`
}

// executeListMCPServers lists all available MCP servers
func (tc *TermChat) executeListMCPServers(argumentsJSON string) (*MCPServerListResult, error) {
	if tc.mcpClient == nil {
		return &MCPServerListResult{
			Servers: []MCPServerInfo{},
			Total:   0,
			Status:  "MCP client not available",
		}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get servers from MCP client
	servers, err := tc.mcpClient.ListServers(ctx)
	if err != nil {
		return &MCPServerListResult{
			Servers: []MCPServerInfo{},
			Total:   0,
			Status:  fmt.Sprintf("Error listing servers: %v", err),
		}, nil
	}

	// Convert to our result format
	serverInfos := make([]MCPServerInfo, 0, len(servers))
	for _, server := range servers {
		toolNames := make([]string, 0, len(server.Tools))
		for _, tool := range server.Tools {
			toolNames = append(toolNames, tool.Name)
		}

		serverInfo := MCPServerInfo{
			Name:        server.Name,
			Status:      "running", // Assume running if we can list it
			Tools:       toolNames,
			Description: server.Description,
		}
		serverInfos = append(serverInfos, serverInfo)
	}

	result := &MCPServerListResult{
		Servers: serverInfos,
		Total:   len(serverInfos),
		Status:  "success",
	}

	return result, nil
}

// executeEditFile executes file editing with visual diff support
func (tc *TermChat) executeEditFile(argumentsJSON string) (interface{}, error) {
	// Parse arguments - support both direct content and edits array format
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("failed to parse edit_file arguments: %v", err)
	}
	
	// Extract basic parameters
	filePath, ok := args["path"].(string)
	if !ok {
		return nil, fmt.Errorf("missing required 'path' parameter")
	}
	
	// Check for dry run mode
	dryRun, _ := args["dryRun"].(bool)
	
	// Check for edits array (Claude Code style)
	var diffContent strings.Builder
	if edits, hasEdits := args["edits"].([]interface{}); hasEdits {
		for i, edit := range edits {
			editMap, ok := edit.(map[string]interface{})
			if !ok {
				continue
			}
			
			oldText, hasOld := editMap["oldText"].(string)
			newText, hasNew := editMap["newText"].(string)
			
			if hasOld && hasNew {
				if i > 0 {
					diffContent.WriteString("\n\n")
				}
				diffContent.WriteString("------- SEARCH\n")
				diffContent.WriteString(oldText)
				diffContent.WriteString("\n=======\n")
				diffContent.WriteString(newText) 
				diffContent.WriteString("\n+++++++ REPLACE")
			}
		}
	}
	
	// Fallback to direct content or diff_content
	if diffContent.Len() == 0 {
		if content, hasContent := args["content"].(string); hasContent {
			// Simple content replacement - create search/replace for entire file
			diffContent.WriteString("------- SEARCH\n")
			// TODO: Read current file content for search part
			diffContent.WriteString("\n=======\n")
			diffContent.WriteString(content)
			diffContent.WriteString("\n+++++++ REPLACE")
		} else if diff, hasDiff := args["diff_content"].(string); hasDiff {
			diffContent.WriteString(diff)
		}
	}
	
	if diffContent.Len() == 0 {
		return nil, fmt.Errorf("no content, diff_content, or edits provided")
	}
	
	// Create file editor instance
	editorConfig := edit.EditorConfig{
		BackupDir:     "/tmp/matey-edit-backups",
		MaxBackups:    10,
		ValidateFunc:  nil, // No custom validation for now
		K8sIntegrated: true,
	}
	fileEditor := edit.NewFileEditor(editorConfig)
	
	// Create enhanced tools instance with visual diff enabled
	config := mcp.EnhancedToolConfig{
		EnableVisualDiff:  true,
		EnablePreview:     true,
		AutoApprove:       tc.approvalMode == YOLO, // Auto-approve in YOLO mode
		MaxFileSize:       1024 * 1024, // 1MB
		ContextTracking:   true,
		BackupEnabled:     true,
		ValidationEnabled: true,
		DiffMode:          "unified",
	}
	
	// Initialize enhanced tools with proper file editor
	enhancedTools := mcp.NewEnhancedTools(config, fileEditor, nil, nil, nil, nil)
	
	// Debug: Check if fileEditor was properly set
	if fileEditor == nil {
		return nil, fmt.Errorf("debug: fileEditor is nil after creation")
	}
	
	// Debug: Check if file exists for better error reporting
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// File doesn't exist - this is OK for dry run mode in some cases
		if !dryRun {
			return nil, fmt.Errorf("debug: file does not exist: %s", filePath)
		}
	}
	
	// Create edit request  
	editRequest := mcp.EditFileRequest{
		FilePath:    filePath,
		DiffContent: diffContent.String(),
		ShowPreview: true,
		AutoApprove: config.AutoApprove && !dryRun, // Don't auto-approve dry runs
	}
	
	// Handle dry run mode
	if dryRun {
		// For dry run, try to read the file to create a proper diff preview
		var originalContent string
		if fileEditor != nil {
			originalContent, _ = fileEditor.ReadFile(filePath)
		}
		
		// Generate a visual diff preview for dry run
		var diffPreview string
		if originalContent != "" {
			// Use the visual diff preview functionality
			diffPreview = fmt.Sprintf("SEARCH/REPLACE Preview for: %s\n%s", filePath, diffContent.String())
		} else {
			diffPreview = fmt.Sprintf("New file preview for: %s\n%s", filePath, diffContent.String())
		}
		
		result := map[string]interface{}{
			"success":      true,
			"file_path":    filePath,
			"applied":      false,
			"dry_run":      true,
			"diff_preview": diffPreview,
			"message":      "Dry run mode - changes not applied",
		}
		return result, nil
	}
	
	// Execute the edit with visual diff
	ctx := context.Background()
	response, err := enhancedTools.EditFile(ctx, editRequest)
	if err != nil {
		return nil, fmt.Errorf("edit failed: %v", err)
	}
	
	// Return structured response that includes diff preview
	result := map[string]interface{}{
		"success":      response.Success,
		"file_path":    response.FilePath,
		"applied":      response.Applied,
		"preview":      response.Preview,
		"diff_preview": response.DiffPreview,
		"backup":       response.Backup,
	}
	
	if response.Error != "" {
		result["error"] = response.Error
	}
	
	return result, nil
}

// executeReadFile executes the read_file native function
func (tc *TermChat) executeReadFile(argumentsJSON string) (interface{}, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("failed to parse read_file arguments: %v", err)
	}

	// Extract file path
	filePath, ok := args["file_path"].(string)
	if !ok || filePath == "" {
		return nil, fmt.Errorf("file_path is required")
	}

	// Create read request
	readRequest := mcp.ReadFileRequest{
		FilePath:         filePath,
		IncludeContext:   getBoolArg(args, "include_context", false),
		TrackInContext:   getBoolArg(args, "track_in_context", false),
	}

	// Handle line range
	if startLine, ok := args["start_line"].(float64); ok {
		readRequest.StartLine = int(startLine)
	}
	if endLine, ok := args["end_line"].(float64); ok {
		readRequest.EndLine = int(endLine)
	}

	// Create enhanced tools instance
	config := mcp.EnhancedToolConfig{
		EnableVisualDiff:  tc.approvalMode == YOLO,
		EnablePreview:     true,
		AutoApprove:       tc.approvalMode == YOLO,
		MaxFileSize:       1024 * 1024,
		ContextTracking:   true,
		BackupEnabled:     true,
		ValidationEnabled: true,
	}

	fileEditor := edit.NewFileEditor(edit.EditorConfig{
		MaxBackups:    10,
		K8sIntegrated: false,
	})

	enhancedTools := mcp.NewEnhancedTools(config, fileEditor, nil, nil, nil, nil)

	// Execute the read
	ctx := context.Background()
	response, err := enhancedTools.ReadFile(ctx, readRequest)
	if err != nil {
		return nil, fmt.Errorf("read failed: %v", err)
	}

	// Return response
	result := map[string]interface{}{
		"success":    response.Success,
		"file_path":  response.FilePath,
		"content":    response.Content,
		"language":   response.Language,
		"line_count": response.LineCount,
		"size":       response.Size,
	}

	if response.Context != "" {
		result["context"] = response.Context
	}
	if response.Error != "" {
		result["error"] = response.Error
	}

	return result, nil
}

// executeSearchFiles executes the search_files native function
func (tc *TermChat) executeSearchFiles(argumentsJSON string) (interface{}, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("failed to parse search_files arguments: %v", err)
	}

	// Extract query
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, fmt.Errorf("query is required")
	}

	// Create search request
	searchRequest := mcp.SearchFilesRequest{
		Query:          query,
		MaxResults:     getIntArg(args, "max_results", 50),
		IncludeContent: getBoolArg(args, "include_content", false),
		FuzzySearch:    getBoolArg(args, "fuzzy_search", true),
	}

	// Handle extensions array
	if extensions, ok := args["extensions"].([]interface{}); ok {
		for _, ext := range extensions {
			if extStr, ok := ext.(string); ok {
				searchRequest.Extensions = append(searchRequest.Extensions, extStr)
			}
		}
	}

	// Create enhanced tools instance
	config := mcp.EnhancedToolConfig{
		EnableVisualDiff:  false,
		EnablePreview:     true,
		MaxFileSize:       1024 * 1024,
		ContextTracking:   false,
	}

	fileEditor := edit.NewFileEditor(edit.EditorConfig{
		MaxBackups:    10,
		K8sIntegrated: false,
	})

	// Use existing components from TermChat if available, otherwise create new ones
	var fileDiscovery *appcontext.FileDiscovery
	var contextManager *appcontext.ContextManager
	var mentionProcessor *appcontext.MentionProcessor

	if tc.fileDiscovery != nil {
		fileDiscovery = tc.fileDiscovery
	} else {
		// Fallback: create new file discovery
		workingDir, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %v", err)
		}
		fileDiscovery, err = appcontext.NewFileDiscovery(workingDir)
		if err != nil {
			return nil, fmt.Errorf("failed to create file discovery: %v", err)
		}
	}

	contextManager = tc.contextManager
	mentionProcessor = tc.mentionProcessor

	enhancedTools := mcp.NewEnhancedTools(config, fileEditor, contextManager, fileDiscovery, nil, mentionProcessor)

	// Execute the search
	ctx := context.Background()
	response, err := enhancedTools.SearchFiles(ctx, searchRequest)
	if err != nil {
		return nil, fmt.Errorf("search failed: %v", err)
	}

	// Return response
	result := map[string]interface{}{
		"success": response.Success,
		"query":   response.Query,
		"results": response.Results,
		"count":   response.Count,
	}

	if response.Error != "" {
		result["error"] = response.Error
	}

	return result, nil
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
			searchOptions := appcontext.SearchOptions{
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
			searchOptions := appcontext.SearchOptions{
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

// executeParseCode executes the parse_code native function
func (tc *TermChat) executeParseCode(argumentsJSON string) (interface{}, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsJSON), &args); err != nil {
		return nil, fmt.Errorf("failed to parse parse_code arguments: %v", err)
	}

	// Create parse request
	parseRequest := mcp.ParseCodeRequest{
		IncludeBody: getBoolArg(args, "include_body", false),
	}

	// Handle file_path or content
	if filePath, ok := args["file_path"].(string); ok && filePath != "" {
		parseRequest.FilePath = filePath
	} else if content, ok := args["content"].(string); ok && content != "" {
		parseRequest.Content = content
	} else {
		return nil, fmt.Errorf("either file_path or content is required")
	}

	// Handle query_types array
	if queryTypes, ok := args["query_types"].([]interface{}); ok {
		for _, qType := range queryTypes {
			if qTypeStr, ok := qType.(string); ok {
				parseRequest.QueryTypes = append(parseRequest.QueryTypes, qTypeStr)
			}
		}
	}

	// Create enhanced tools instance
	config := mcp.EnhancedToolConfig{
		EnableVisualDiff: false,
		EnablePreview:    true,
		MaxFileSize:      1024 * 1024,
	}

	fileEditor := edit.NewFileEditor(edit.EditorConfig{
		MaxBackups:    10,
		K8sIntegrated: false,
	})

	enhancedTools := mcp.NewEnhancedTools(config, fileEditor, nil, nil, nil, nil)

	// Execute the parse
	ctx := context.Background()
	response, err := enhancedTools.ParseCode(ctx, parseRequest)
	if err != nil {
		return nil, fmt.Errorf("parse failed: %v", err)
	}

	// Return response
	result := map[string]interface{}{
		"success":     response.Success,
		"file_path":   response.FilePath,
		"language":    response.Language,
		"definitions": response.Definitions,
	}

	if response.Structure != nil {
		result["structure"] = response.Structure
	}
	if response.Error != "" {
		result["error"] = response.Error
	}

	return result, nil
}

// Helper functions
func getBoolArg(args map[string]interface{}, key string, defaultValue bool) bool {
	if val, ok := args[key].(bool); ok {
		return val
	}
	return defaultValue
}

func getIntArg(args map[string]interface{}, key string, defaultValue int) int {
	if val, ok := args[key].(float64); ok {
		return int(val)
	}
	return defaultValue
}

// TODO Management Functions

// executeCreateTodo creates a new TODO item
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
		"success":  true,
		"id":       id,
		"content":  content,
		"priority": string(priority),
		"status":   string(TodoStatusPending),
	}, nil
}

// executeListTodos lists TODO items with optional filtering
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

// executeUpdateTodoStatus updates the status of a TODO item
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
		return nil, fmt.Errorf("TODO item not found: %s", id)
	}

	// Update the status
	if !tc.todoList.UpdateItemStatus(id, status) {
		return nil, fmt.Errorf("TODO item not found: %s", id)
	}

	return map[string]interface{}{
		"success": true,
		"id":      id,
		"status":  string(status),
	}, nil
}

// executeRemoveTodo removes a TODO item from the list
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
		return nil, fmt.Errorf("TODO item not found: %s", id)
	}

	// Remove the item
	if !tc.todoList.RemoveItem(id) {
		return nil, fmt.Errorf("TODO item not found: %s", id)
	}

	return map[string]interface{}{
		"success": true,
		"id":      id,
	}, nil
}

// executeClearCompletedTodos removes all completed TODO items
func (tc *TermChat) executeClearCompletedTodos(argumentsJSON string) (interface{}, error) {
	// Initialize TODO list if it doesn't exist
	if tc.todoList == nil {
		tc.todoList = &TodoList{Items: []TodoItem{}}
	}

	// Count and remove completed items
	var remainingItems []TodoItem
	clearedCount := 0
	for _, item := range tc.todoList.Items {
		if item.Status == TodoStatusCompleted {
			clearedCount++
		} else {
			remainingItems = append(remainingItems, item)
		}
	}

	tc.todoList.Items = remainingItems

	return map[string]interface{}{
		"success":       true,
		"cleared_count": clearedCount,
		"remaining":     len(remainingItems),
	}, nil
}

// executeGetTodoStats gets statistics about TODO items
func (tc *TermChat) executeGetTodoStats(argumentsJSON string) (interface{}, error) {
	// Initialize TODO list if it doesn't exist
	if tc.todoList == nil {
		tc.todoList = &TodoList{Items: []TodoItem{}}
	}

	// Count by status
	statusCounts := map[string]int{
		"pending":     0,
		"in_progress": 0,
		"completed":   0,
		"cancelled":   0,
	}

	// Count by priority
	priorityCounts := map[string]int{
		"low":    0,
		"medium": 0,
		"high":   0,
		"urgent": 0,
	}

	for _, item := range tc.todoList.Items {
		statusCounts[string(item.Status)]++
		priorityCounts[string(item.Priority)]++
	}

	return map[string]interface{}{
		"success":         true,
		"total_count":     len(tc.todoList.Items),
		"pending_count":   statusCounts["pending"],
		"in_progress_count": statusCounts["in_progress"],
		"completed_count": statusCounts["completed"],
		"cancelled_count": statusCounts["cancelled"],
		"status_counts":   statusCounts,
		"priority_counts": priorityCounts,
	}, nil
}

// executeCreateTodos creates multiple TODO items in bulk
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

// executeUpdateTodos updates multiple TODO items in bulk
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

