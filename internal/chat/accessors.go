package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/phildougherty/m8e/internal/ai"
	"github.com/phildougherty/m8e/internal/edit"
	"github.com/phildougherty/m8e/internal/mcp"
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
		fmt.Printf("DEBUG: Executing INTERNAL edit_file function\n")
		return tc.executeEditFile(call.Function.Arguments)
	case "read_file", "readfile", "read-file":
		return tc.executeReadFile(call.Function.Arguments)
	case "search_files", "searchfiles", "search-files":
		return tc.executeSearchFiles(call.Function.Arguments)
	case "parse_code", "parsecode", "parse-code":
		return tc.executeParseCode(call.Function.Arguments)
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
		args.Timeout = 120 // 2 minutes default timeout
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

	enhancedTools := mcp.NewEnhancedTools(config, fileEditor, nil, nil, nil, nil)

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

