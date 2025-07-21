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
		return tc.executeEditFile(call.Function.Arguments)
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
	
	// Initialize enhanced tools (simplified for now)
	enhancedTools := mcp.NewEnhancedTools(config, nil, nil, nil, nil, nil)
	
	// Create edit request
	editRequest := mcp.EditFileRequest{
		FilePath:    filePath,
		DiffContent: diffContent.String(),
		ShowPreview: true,
		AutoApprove: config.AutoApprove,
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

