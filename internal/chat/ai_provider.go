package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/phildougherty/m8e/internal/ai"
)

// renderMarkdown renders markdown content using glamour with custom code block headers
func (tc *TermChat) renderMarkdown(content string) string {
	if tc.markdownRenderer == nil {
		return content
	}
	
	// Add custom headers for code blocks before rendering
	enhanced := tc.addCodeBlockHeaders(content)
	
	// Render with glamour
	rendered, err := tc.markdownRenderer.Render(enhanced)
	if err != nil {
		// Fallback to original content if rendering fails
		return content
	}
	
	return rendered
}

// addCodeBlockHeaders adds beautiful headers to code blocks
func (tc *TermChat) addCodeBlockHeaders(content string) string {
	// Don't add custom headers - let glamour handle code block styling properly
	// The raw ANSI codes were interfering with glamour's rendering
	return content
}

// chatWithAISilent processes AI response WITH streaming for UI (silent version for UI)
func (tc *TermChat) chatWithAISilent(message string) {
	// Use the actual turn-based conversation system
	// This is what the backup version was doing - we need to delegate to the turn system
	// which handles function calls, streaming, and UI confirmations properly
	
	// Note: We'll implement a simplified interface that calls the turn system
	// The turn system in cmd/turn.go handles all the complex function calling logic
	tc.executeConversationFlowSilent(message)
}

// executeConversationFlowSilent implements the conversation flow without terminal output
func (tc *TermChat) executeConversationFlowSilent(userInput string) {
	// Create a new conversation turn in silent mode
	turn := NewConversationTurnSilent(tc, userInput)
	
	// Execute the turn with automatic continuation
	ctx, cancel := context.WithTimeout(tc.ctx, 10*time.Minute)
	defer cancel()
	
	if err := turn.Execute(ctx); err != nil {
		tc.addMessage("system", fmt.Sprintf("Conversation flow error: %v", err))
	}
}

// executeUIConversationTurn is kept for compatibility but delegates to turn-based system
func (tc *TermChat) executeUIConversationTurn(userRequest string) {
	// Delegate to the turn-based system to avoid duplication
	tc.executeConversationFlowSilent(userRequest)
}

// switchProviderSilent switches AI provider without terminal output
func (tc *TermChat) switchProviderSilent(name string) error {
	if tc.aiManager == nil {
		return fmt.Errorf("AI manager not initialized")
	}
	
	err := tc.aiManager.SwitchProvider(name)
	if err != nil {
		return err
	}
	
	// Update current provider info
	if provider, err := tc.aiManager.GetCurrentProvider(); err == nil {
		tc.currentProvider = provider.Name()
		tc.currentModel = provider.DefaultModel()
	}
	
	return nil
}

// switchModelSilent switches AI model without terminal output
func (tc *TermChat) switchModelSilent(name string) error {
	if tc.aiManager == nil {
		return fmt.Errorf("AI manager not initialized")
	}
	
	// Use the AI manager to properly switch the model within the current provider
	err := tc.aiManager.SwitchModel(name)
	if err != nil {
		return err
	}
	
	// Update local reference
	tc.currentModel = name
	
	return nil
}

// getMCPFunctions returns available MCP functions
func (tc *TermChat) getMCPFunctions() []ai.Function {
	// Get real MCP tools from discovery system
	functions := tc.getDiscoveredMCPFunctions()
	
	// Add native functions - these are implemented directly in the chat system
	nativeFunctions := []ai.Function{
		{
			Name:        "execute_bash",
			Description: "Execute bash commands safely with security validation and approval controls",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "The bash command to execute",
					},
					"working_directory": map[string]interface{}{
						"type":        "string",
						"description": "Working directory to run the command in (optional)",
					},
					"timeout": map[string]interface{}{
						"type":        "integer",
						"description": "Timeout in seconds (optional, default: 120)",
						"default":     120,
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Human-readable description of what the command does",
					},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "deploy_service",
			Description: "Deploy a service to Kubernetes cluster",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Service name",
					},
					"image": map[string]interface{}{
						"type":        "string",
						"description": "Docker image",
					},
					"replicas": map[string]interface{}{
						"type":        "integer",
						"description": "Number of replicas",
					},
				},
				"required": []string{"name", "image"},
			},
		},
		{
			Name:        "scale_service",
			Description: "Scale a service in the Kubernetes cluster",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Service name to scale",
					},
					"replicas": map[string]interface{}{
						"type":        "integer",
						"description": "Target number of replicas",
					},
				},
				"required": []string{"name", "replicas"},
			},
		},
		{
			Name:        "get_service_status",
			Description: "Get the status of Kubernetes services/pods/deployments in the cluster (NOT for MCP servers - use list_mcp_servers for that)",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace (optional)",
					},
				},
			},
		},
		{
			Name:        "get_logs",
			Description: "Get logs from a service or pod",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"service": map[string]interface{}{
						"type":        "string",
						"description": "Service name",
					},
					"lines": map[string]interface{}{
						"type":        "integer",
						"description": "Number of log lines to retrieve",
						"default":     100,
					},
				},
				"required": []string{"service"},
			},
		},
		{
			Name:        "create_backup",
			Description: "Create a backup of cluster resources",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Namespace to backup",
					},
					"resources": map[string]interface{}{
						"type":        "array",
						"description": "Resource types to backup",
						"items": map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
		},
		// File Management Tools
		{
			Name:        "edit_file",
			Description: "Edit files with diff preview, backup capabilities, and visual approval workflow",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the file to edit",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "New file content (for full replacement)",
					},
					"diff_content": map[string]interface{}{
						"type":        "string",
						"description": "Diff content in SEARCH/REPLACE format",
					},
					"create_file": map[string]interface{}{
						"type":        "boolean",
						"description": "Create file if it doesn't exist",
						"default":     false,
					},
					"show_preview": map[string]interface{}{
						"type":        "boolean",
						"description": "Show preview before applying changes",
						"default":     false,
					},
					"auto_approve": map[string]interface{}{
						"type":        "boolean",
						"description": "Auto-approve changes without confirmation",
						"default":     false,
					},
				},
				"required": []string{"file_path"},
			},
		},
		{
			Name:        "read_file",
			Description: "Read file contents with optional line range and context tracking",
			Parameters: map[string]interface{}{
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
						"description": "Ending line number (inclusive)",
					},
					"include_context": map[string]interface{}{
						"type":        "boolean",
						"description": "Include context information",
						"default":     false,
					},
					"track_in_context": map[string]interface{}{
						"type":        "boolean",
						"description": "Track this file in context manager",
						"default":     false,
					},
				},
				"required": []string{"file_path"},
			},
		},
		{
			Name:        "search_files",
			Description: "Search for files with intelligent fuzzy matching and content preview",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search pattern or filename",
					},
					"extensions": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "File extensions to filter by",
					},
					"max_results": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results",
						"default":     50,
					},
					"include_content": map[string]interface{}{
						"type":        "boolean",
						"description": "Include content preview",
						"default":     false,
					},
					"fuzzy_search": map[string]interface{}{
						"type":        "boolean",
						"description": "Enable fuzzy matching",
						"default":     true,
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "parse_code",
			Description: "Parse code structure using tree-sitter for function/class definitions",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to the code file to parse (required if content not provided)",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "Code content to parse (required if file_path not provided)",
					},
					"query_types": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Types of definitions to extract (function, class, method, etc.)",
					},
					"include_body": map[string]interface{}{
						"type":        "boolean",
						"description": "Include function/method body content",
						"default":     false,
					},
				},
				"required": []string{},
			},
		},
	}
	
	// Add native functions FIRST (they take priority over external MCP tools)
	// Then add discovered MCP functions that don't conflict
	functionNames := make(map[string]bool)
	finalFunctions := make([]ai.Function, 0)
	
	// First add all native functions (highest priority)
	for _, nativeFn := range nativeFunctions {
		finalFunctions = append(finalFunctions, nativeFn)
		functionNames[nativeFn.Name] = true
	}
	
	// Then add discovered MCP functions that don't conflict with native ones
	for _, fn := range functions {
		if !functionNames[fn.Name] {
			finalFunctions = append(finalFunctions, fn)
			functionNames[fn.Name] = true
		}
	}
	
	return finalFunctions
}

// getDiscoveredMCPFunctions gets real MCP functions from discovery system
func (tc *TermChat) getDiscoveredMCPFunctions() []ai.Function {
	// Use the same discovery system from system_prompt.go
	serverData := tc.getComprehensiveMCPData()
	
	var functions []ai.Function
	for _, server := range serverData {
		// Only include connected servers
		if server.ConnectionStatus != "connected" {
			continue
		}
		
		for _, tool := range server.Tools {
			// Type assert the input schema and clean it for compatibility
			var parameters map[string]interface{}
			if tool.InputSchema != nil {
				if schemaMap, ok := tool.InputSchema.(map[string]interface{}); ok {
					parameters = tc.cleanSchemaForCompatibility(schemaMap)
				} else {
					parameters = map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{},
					}
				}
			} else {
				parameters = map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{},
				}
			}
			
			function := ai.Function{
				Name:        tool.Name,
				Description: fmt.Sprintf("[%s] %s", server.Name, tool.Description),
				Parameters:  parameters,
			}
			functions = append(functions, function)
		}
	}
	
	return functions
}

// cleanSchemaForCompatibility removes oneOf/allOf/anyOf constructs that some AI models don't support
func (tc *TermChat) cleanSchemaForCompatibility(schema map[string]interface{}) map[string]interface{} {
	cleaned := make(map[string]interface{})
	
	// Copy all fields except the problematic ones
	for key, value := range schema {
		switch key {
		case "oneOf", "allOf", "anyOf":
			// Skip these fields as they cause issues with some AI models
			continue
		default:
			cleaned[key] = value
		}
	}
	
	// Ensure we have basic schema structure
	if cleaned["type"] == nil {
		cleaned["type"] = "object"
	}
	if cleaned["properties"] == nil {
		cleaned["properties"] = map[string]interface{}{}
	}
	
	return cleaned
}

// discoverMCPServerFunctions discovers available functions from MCP servers
func (tc *TermChat) discoverMCPServerFunctions() []ai.Function {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	servers, err := tc.mcpClient.ListServers(ctx)
	if err != nil {
		// Silent failure - don't spam logs with errors
		return []ai.Function{}
	}
	
	var mcpFunctions []ai.Function
	for _, server := range servers {
		for _, tool := range server.Tools {
			// Convert MCP tool to AI function format
			var parameters map[string]interface{}
			if schema, ok := tool.InputSchema.(map[string]interface{}); ok {
				parameters = schema
			} else {
				// Default parameters if schema is not available
				parameters = map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{},
				}
			}
			
			function := ai.Function{
				Name:        fmt.Sprintf("%s_%s", server.Name, tool.Name),
				Description: fmt.Sprintf("[%s] %s", server.Name, tool.Description),
				Parameters:  parameters,
			}
			mcpFunctions = append(mcpFunctions, function)
		}
	}
	
	return mcpFunctions
}

// executeUIConversationRound executes one round of AI conversation for UI with concurrent function execution
func (tc *TermChat) executeUIConversationRound(messages []ai.Message) ([]ai.ToolCall, error) {
	// Create AI message placeholder
	tc.AddMessage("assistant", "")
	aiMsgIndex := len(tc.chatHistory) - 1
	
	// Start AI response in UI
	if uiProgram != nil {
		uiProgram.Send(AiStreamMsg{Content: ""}) // Empty content signals start of AI response
	}
	
	// Get MCP functions
	mcpFunctions := tc.GetMCPFunctions()
	
	// Stream the AI response
	stream, err := tc.aiManager.StreamChatWithFallback(tc.ctx, messages, ai.StreamOptions{
		Model:     tc.currentModel,
		Functions: mcpFunctions,
	})
	if err != nil {
		return nil, fmt.Errorf("AI streaming failed: %v", err)
	}
	
	// Process streaming response with concurrent function execution
	accumulatedFunctionCalls := make(map[string]*ai.ToolCall)
	thinkProcessor := ai.NewThinkProcessor()
	
	// Channel for concurrent function execution results
	functionResultChan := make(chan string, 10)
	var activeFunctionCalls int64
	
	// Start goroutine to handle function results
	go func() {
		for result := range functionResultChan {
			// Stream function result immediately to UI
			if uiProgram != nil {
				uiProgram.Send(AiStreamMsg{Content: result})
			}
			// Add result to chat history
			tc.chatHistory[aiMsgIndex].Content += result
		}
	}()
	
	for response := range stream {
		if response.Error != nil {
			tc.chatHistory[aiMsgIndex].Content += fmt.Sprintf("\nError: %v", response.Error)
			break
		}
		
		// Process content
		if response.Content != "" {
			regularContent := response.Content
			if strings.Contains(response.Content, "<think>") || strings.Contains(response.Content, "</think>") {
				regularContent, _ = ai.ProcessStreamContentWithThinks(thinkProcessor, response.Content)
			}
			
			tc.chatHistory[aiMsgIndex].Content += regularContent
			
			// Send streaming content to UI
			if uiProgram != nil {
				uiProgram.Send(AiStreamMsg{Content: regularContent})
			}
		} else if len(response.ToolCalls) > 0 {
			// If we get tool calls but no content, add explanatory text
			if tc.chatHistory[aiMsgIndex].Content == "" {
				tc.chatHistory[aiMsgIndex].Content = "I'll help you with that."
				if uiProgram != nil {
					uiProgram.Send(AiStreamMsg{Content: "I'll help you with that."})
				}
			}
		}
		
		// Handle tool calls with immediate concurrent execution
		if len(response.ToolCalls) > 0 {
			for _, toolCall := range response.ToolCalls {
				if toolCall.Type == "function" {
					// Use function name as key to ensure consistent accumulation
					callId := toolCall.Function.Name
					if callId == "" {
						// For empty function names, use the first non-empty function name we've seen
						for _, existing := range accumulatedFunctionCalls {
							if existing.Function.Name != "" {
								callId = existing.Function.Name
								break
							}
						}
						if callId == "" {
							callId = "unknown"
						}
					}
					
					if existing, exists := accumulatedFunctionCalls[callId]; exists {
						// Accumulate arguments for existing function call
						existing.Function.Arguments += toolCall.Function.Arguments
						// Update function name if it was empty before
						if existing.Function.Name == "" && toolCall.Function.Name != "" {
							existing.Function.Name = toolCall.Function.Name
						}
					} else {
						// New function call
						newCall := &ai.ToolCall{
							ID:   toolCall.ID,
							Type: toolCall.Type,
							Function: ai.FunctionCall{
								Name:      toolCall.Function.Name,
								Arguments: toolCall.Function.Arguments,
							},
						}
						accumulatedFunctionCalls[callId] = newCall
						
						// Execute function call immediately if we have complete data
						if toolCall.Function.Name != "" && toolCall.Function.Arguments != "" {
							atomic.AddInt64(&activeFunctionCalls, 1)
							go tc.executeUIToolCallConcurrent(*newCall, aiMsgIndex, functionResultChan, &activeFunctionCalls)
						}
					}
				}
			}
		}
	}
	
	// Close the function result channel and wait for any remaining function calls
	go func() {
		// Wait for all function calls to complete with timeout
		timeout := time.After(30 * time.Second)
		for atomic.LoadInt64(&activeFunctionCalls) > 0 {
			select {
			case <-timeout:
				// Function call timeout
				if uiProgram != nil {
					uiProgram.Send(AiStreamMsg{Content: "\n⏰ Function call timeout"})
				}
				tc.chatHistory[aiMsgIndex].Content += "\n⏰ Function call timeout"
				atomic.StoreInt64(&activeFunctionCalls, 0)
			case <-time.After(100 * time.Millisecond):
				// Check again
			}
		}
		close(functionResultChan)
	}()
	
	// Signal end of AI response to UI
	if uiProgram != nil {
		uiProgram.Send(AiResponseMsg{Content: tc.chatHistory[aiMsgIndex].Content})
	}
	
	// Return empty since we're handling function calls concurrently
	return []ai.ToolCall{}, nil
}

// executeUITools executes pending tools and returns results
func (tc *TermChat) executeUITools(pendingTools []ai.ToolCall) ([]ai.Message, error) {
	var toolResults []ai.Message
	
	// Execute all pending tools
	for _, toolCall := range pendingTools {
		if toolCall.Type != "function" {
			continue
		}
		
		// Check approval mode for this specific tool
		if tc.approvalMode.ShouldConfirm(toolCall.Function.Name) {
			confirmed := tc.RequestFunctionConfirmation(toolCall.Function.Name, toolCall.Function.Arguments)
			if !confirmed {
				// User rejected - add rejection to conversation and continue
				rejectionResult := ai.Message{
					Role:       "tool",
					Content:    fmt.Sprintf("Function call %s was rejected by user", toolCall.Function.Name),
					ToolCallId: toolCall.ID,
				}
				toolResults = append(toolResults, rejectionResult)
				continue
			}
		}
		
		// Show function call execution in UI - Claude Code style
		if uiProgram != nil {
			// Show function name with formatted arguments and color
			funcCallMsg := fmt.Sprintf("\x1b[90m→\x1b[0m \x1b[32m%s\x1b[0m", toolCall.Function.Name)
			if formattedArgs := tc.formatFunctionArgs(toolCall.Function.Arguments, toolCall.Function.Name); formattedArgs != "" {
				funcCallMsg += fmt.Sprintf(" %s", formattedArgs)
			}
			uiProgram.Send(AiStreamMsg{Content: "\n" + funcCallMsg})
		}
		
		// Parse arguments
		parsedArgs := tc.parseArguments(toolCall.Function.Arguments)
		
		// Try native function execution first, then fall back to MCP
		var result interface{}
		var err error
		
		// Check if this is a native function
		nativeFunctions := []string{"execute_bash", "bash", "run_command", "deploy_service", "scale_service", "restart_service", "get_logs", "get_metrics", "check_health"}
		isNative := false
		for _, nativeFunc := range nativeFunctions {
			if toolCall.Function.Name == nativeFunc {
				isNative = true
				break
			}
		}
		
		if isNative {
			// Execute native function
			result, err = tc.ExecuteNativeToolCall(toolCall, len(tc.chatHistory)-1)
		} else {
			// Execute MCP tool
			result, err = tc.mcpClient.CallTool(tc.ctx, tc.FindServerForTool(toolCall.Function.Name), toolCall.Function.Name, parsedArgs)
		}
		
		var resultContent string
		if err != nil {
			resultContent = fmt.Sprintf("Error: %v", err)
		} else if result != nil {
			// Handle result - convert to string
			resultContent = fmt.Sprintf("Result: %+v", result)
		} else {
			resultContent = "Function executed successfully"
		}
		
		// Show function call result in UI
		if uiProgram != nil {
			// Claude Code style - just show clean status line with color
			var statusMsg string
			if err != nil {
				statusMsg = fmt.Sprintf("\x1b[31mx\x1b[0m \x1b[32m%s\x1b[0m \x1b[90mfailed |\x1b[0m \x1b[31m%v\x1b[0m", toolCall.Function.Name, err)
			} else {
				// Use our enhanced formatting for file editing tools
				if tc.isFileEditingTool(toolCall.Function.Name) && result != nil {
					duration := time.Millisecond * 50 // Placeholder duration
					statusMsg = tc.formatToolResult(toolCall.Function.Name, "native", result, duration)
				} else {
					statusMsg = fmt.Sprintf("\x1b[32m+\x1b[0m \x1b[32m%s\x1b[0m \x1b[90mcompleted\x1b[0m", toolCall.Function.Name)
				}
			}
			uiProgram.Send(AiStreamMsg{Content: statusMsg + "\n"})
		}
		
		// Add tool result to conversation
		toolResult := ai.Message{
			Role:       "tool",
			Content:    resultContent,
			ToolCallId: toolCall.ID,
		}
		toolResults = append(toolResults, toolResult)
	}
	
	return toolResults, nil
}

// Helper method to parse arguments
func (tc *TermChat) parseArguments(arguments string) map[string]interface{} {
	if arguments == "" || arguments == "{}" {
		return map[string]interface{}{}
	}
	
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return map[string]interface{}{}
	}
	
	return args
}

// isFileEditingTool checks if the function is related to file editing and should show full diffs
func (tc *TermChat) isFileEditingTool(functionName string) bool {
	fileEditTools := []string{
		"edit_file", "editfile", "edit-file",
		"write_file", "writefile", "write-file", 
		"create_file", "createfile", "create-file",
		"modify_file", "modifyfile", "modify-file",
		"update_file", "updatefile", "update-file",
		"patch_file", "patchfile", "patch-file",
		"diff_file", "difffile", "diff-file",
		"apply_diff", "applydiff", "apply-diff",
		"file_edit", "fileedit", "file-edit",
	}
	
	for _, tool := range fileEditTools {
		if strings.EqualFold(functionName, tool) {
			return true
		}
	}
	return false
}

// formatFunctionArgs formats function arguments in a human-readable way with color
func (tc *TermChat) formatFunctionArgs(arguments, functionName string) string {
	if arguments == "" || arguments == "{}" {
		return ""
	}
	
	// Check if this is a file editing tool that should show full diffs
	isFileEditTool := tc.isFileEditingTool(functionName)
	
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		// If not valid JSON, return truncated string unless it's a file edit tool
		cleaned := strings.ReplaceAll(arguments, "\n", " ")
		if !isFileEditTool && len(cleaned) > 60 {
			cleaned = cleaned[:57] + "..."
		}
		return cleaned
	}
	
	// Format key arguments nicely with color
	var parts []string
	for key, value := range args {
		switch v := value.(type) {
		case string:
			// For file edit tools, don't truncate content that might be diffs
			if !isFileEditTool && len(v) > 40 {
				v = v[:37] + "..."
			}
			parts = append(parts, fmt.Sprintf("\x1b[36m%s\x1b[0m=\x1b[33m\"%s\"\x1b[0m", key, v))
		case float64, int:
			parts = append(parts, fmt.Sprintf("\x1b[36m%s\x1b[0m=\x1b[35m%v\x1b[0m", key, v))
		default:
			parts = append(parts, fmt.Sprintf("\x1b[36m%s\x1b[0m=\x1b[37m%v\x1b[0m", key, v))
		}
	}
	
	result := strings.Join(parts, " ")
	// For file edit tools, don't truncate the overall result to preserve diffs
	if !isFileEditTool && len(result) > 70 {
		result = result[:67] + "..."
	}
	return result
}

// formatToolResult formats tool execution results in Claude Code style
func (tc *TermChat) formatToolResult(toolName, serverName string, result interface{}, duration time.Duration) string {
	// Debug: Always print when called
	fmt.Printf("DEBUG: formatToolResult called - toolName=%s, serverName=%s\n", toolName, serverName)
	
	// Calculate token count estimate (rough approximation)
	resultStr := fmt.Sprintf("%+v", result)
	tokenCount := len(strings.Fields(resultStr)) // Simple word count as token estimate
	
	// Check if this is a file editing tool result with diff preview
	if tc.isFileEditingTool(toolName) {
		fmt.Printf("DEBUG: Tool %s identified as file editing tool\n", toolName)
		if resultMap, ok := result.(map[string]interface{}); ok {
			if diffPreview, hasDiff := resultMap["diff_preview"].(string); hasDiff && diffPreview != "" {
				// Show the visual diff for file editing tools
				statusLine := fmt.Sprintf("\x1b[32m+\x1b[0m \x1b[32m%s\x1b[0m \x1b[90m(%s) | %v | ~%d tokens\x1b[0m", 
					toolName, serverName, duration.Truncate(time.Millisecond), tokenCount)
				
				// Format the diff with enhanced visual display
				var filePath string
				if fp, hasPath := resultMap["file_path"].(string); hasPath {
					filePath = fp
				}
				
				formattedDiff := tc.formatEnhancedVisualDiff(diffPreview, filePath)
				
				// Debug: Print to see if this is being called
				fmt.Printf("DEBUG: Enhanced diff generated:\n%s\n", formattedDiff)
				
				// Check if it's a dry run
				if dryRun, isDryRun := resultMap["dry_run"].(bool); isDryRun && dryRun {
					formattedDiff += "\n\x1b[33m[DRY RUN] Changes not applied\x1b[0m"
				}
				
				// Add the visual diff content
				return fmt.Sprintf("\n%s\n\n%s", statusLine, formattedDiff)
			}
		}
	}
	
	// Claude Code style: just show the status line, no verbose output
	statusLine := fmt.Sprintf("\x1b[32m+\x1b[0m \x1b[32m%s\x1b[0m \x1b[90m(%s) | %v | ~%d tokens\x1b[0m", 
		toolName, serverName, duration.Truncate(time.Millisecond), tokenCount)
	
	// Return just the status line - no verbose content for intermediate function calls
	return fmt.Sprintf("\n%s", statusLine)
}

// formatEnhancedVisualDiff creates a colored diff display that bypasses markdown
func (tc *TermChat) formatEnhancedVisualDiff(diffPreview, filePath string) string {
	// Parse the SEARCH/REPLACE format
	lines := strings.Split(diffPreview, "\n")
	var searchText, replaceText string
	var inSearch, inReplace bool
	
	for _, line := range lines {
		switch {
		case strings.Contains(line, "------- SEARCH"):
			inSearch = true
			inReplace = false
		case strings.Contains(line, "======="):
			inSearch = false
			inReplace = true
		case strings.Contains(line, "+++++++ REPLACE"):
			inReplace = false
		case inSearch:
			if searchText != "" {
				searchText += "\n"
			}
			searchText += line
		case inReplace:
			if replaceText != "" {
				replaceText += "\n"
			}
			replaceText += line
		}
	}
	
	// Read the original file to find line numbers
	var searchLineStart int = -1
	
	if filePath != "" {
		if content, err := os.ReadFile(filePath); err == nil {
			originalLines := strings.Split(string(content), "\n")
			
			// Find where the search text appears in the file
			searchLines := strings.Split(searchText, "\n")
			if len(searchLines) > 0 {
				firstSearchLine := strings.TrimSpace(searchLines[0])
				for i, line := range originalLines {
					if strings.TrimSpace(line) == firstSearchLine {
						// Verify this is the right match by checking subsequent lines
						match := true
						for j := 1; j < len(searchLines) && i+j < len(originalLines); j++ {
							if strings.TrimSpace(originalLines[i+j]) != strings.TrimSpace(searchLines[j]) {
								match = false
								break
							}
						}
						if match {
							searchLineStart = i + 1 // 1-indexed
							break
						}
					}
				}
			}
		}
	}
	
	// Build colored diff with ANSI escape codes
	var result strings.Builder
	
	// Header with file path
	result.WriteString("\x1b[36m╭─ ")
	if filePath != "" {
		result.WriteString(fmt.Sprintf("\x1b[37m%s\x1b[36m ", filePath))
	}
	result.WriteString("(diff preview)")
	result.WriteString(strings.Repeat("─", max(0, 50-len(filePath))))
	result.WriteString("╮\x1b[0m\n")
	
	// Show the diff with line numbers and colors
	searchLines := strings.Split(searchText, "\n")
	replaceLines := strings.Split(replaceText, "\n")
	
	// Add hunk header
	if searchLineStart > 0 {
		result.WriteString(fmt.Sprintf("\x1b[36m│\x1b[0m \x1b[90m@@ -%d,%d +%d,%d @@\x1b[0m\n", 
			searchLineStart, len(searchLines), 
			searchLineStart, len(replaceLines)))
	}
	
	// Show removed lines in red
	for i, line := range searchLines {
		lineNum := ""
		if searchLineStart > 0 {
			lineNum = fmt.Sprintf("%3d", searchLineStart+i)
		} else {
			lineNum = "  ?"
		}
		result.WriteString(fmt.Sprintf("\x1b[36m│\x1b[0m \x1b[31m%s\x1b[91m -%s\x1b[0m\n", lineNum, line))
	}
	
	// Show added lines in green
	for i, line := range replaceLines {
		lineNum := ""
		if searchLineStart > 0 {
			lineNum = fmt.Sprintf("%3d", searchLineStart+i)
		} else {
			lineNum = "  ?"
		}
		result.WriteString(fmt.Sprintf("\x1b[36m│\x1b[0m \x1b[32m%s\x1b[92m +%s\x1b[0m\n", lineNum, line))
	}
	
	result.WriteString("\x1b[36m╰")
	result.WriteString(strings.Repeat("─", 60))
	result.WriteString("╯\x1b[0m")
	
	return result.String()
}

// max returns the larger of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// executeUIToolCallConcurrent executes a tool call concurrently for streaming UI
func (tc *TermChat) executeUIToolCallConcurrent(toolCall ai.ToolCall, aiMsgIndex int, resultChan chan<- string, activeFunctionCalls *int64) {
	defer func() {
		atomic.AddInt64(activeFunctionCalls, -1) // Decrement counter when done
		if r := recover(); r != nil {
			// Send error to result channel
			select {
			case resultChan <- fmt.Sprintf("\nFunction call crashed: %v", r):
			default:
			}
		}
	}()
	
	if tc.mcpClient == nil && !tc.isNativeFunction(toolCall.Function.Name) {
		select {
		case resultChan <- "\nMCP client not available":
		default:
		}
		return
	}
	
	// Check approval mode for this specific tool - use non-blocking confirmation
	if tc.approvalMode.ShouldConfirm(toolCall.Function.Name) {
		confirmed := tc.requestFunctionConfirmationNonBlocking(toolCall.Function.Name, toolCall.Function.Arguments)
		if !confirmed {
			// User rejected - send rejection message
			select {
			case resultChan <- fmt.Sprintf("\nFunction call %s was rejected by user", toolCall.Function.Name):
			default:
			}
			return
		}
	}
	
	// Record start time for duration tracking
	startTime := time.Now()
	
	// Parse arguments
	parsedArgs := tc.parseArguments(toolCall.Function.Arguments)
	
	// Try native function execution first, then fall back to MCP
	var result interface{}
	var err error
	var serverName string
	
	if tc.isNativeFunction(toolCall.Function.Name) {
		// Show minimal function call progress like Claude Code
		funcCallMsg := fmt.Sprintf("\x1b[90m→\x1b[0m \x1b[32m%s\x1b[0m \x1b[90m(native)\x1b[0m", toolCall.Function.Name)
		if formattedArgs := tc.formatFunctionArgs(toolCall.Function.Arguments, toolCall.Function.Name); formattedArgs != "" {
			funcCallMsg += fmt.Sprintf(" %s", formattedArgs)
		}
		select {
		case resultChan <- fmt.Sprintf("\n%s", funcCallMsg):
		default:
		}
		
		// Execute native function
		result, err = tc.ExecuteNativeToolCall(toolCall, aiMsgIndex)
		serverName = "native"
	} else {
		// Execute MCP tool using discovery system
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		
		serverName = tc.FindServerForTool(toolCall.Function.Name)
		if serverName == "" {
			duration := time.Since(startTime)
			select {
			case resultChan <- fmt.Sprintf("\n\x1b[31mx\x1b[0m \x1b[32m%s\x1b[0m \x1b[90m(no server found) | %v\x1b[0m", toolCall.Function.Name, duration.Truncate(time.Millisecond)):
			default:
			}
			return
		}
		
		// Show minimal function call progress like Claude Code
		funcCallMsg := fmt.Sprintf("\x1b[90m→\x1b[0m \x1b[32m%s\x1b[0m \x1b[90m(%s)\x1b[0m", toolCall.Function.Name, serverName)
		if formattedArgs := tc.formatFunctionArgs(toolCall.Function.Arguments, toolCall.Function.Name); formattedArgs != "" {
			funcCallMsg += fmt.Sprintf(" %s", formattedArgs)
		}
		select {
		case resultChan <- fmt.Sprintf("\n%s", funcCallMsg):
		default:
		}
		
		// Use the discovery endpoint structure for tool calls
		result, err = tc.callDiscoveredTool(ctx, serverName, toolCall.Function.Name, parsedArgs)
	}
	
	duration := time.Since(startTime)
	
	// Format result like Claude Code - only show the actual output
	var resultContent string
	if err != nil {
		resultContent = fmt.Sprintf("\n\x1b[31mx\x1b[0m \x1b[32m%s\x1b[0m \x1b[90m(%s) | %v |\x1b[0m \x1b[31m%s\x1b[0m", 
			toolCall.Function.Name, serverName, duration.Truncate(time.Millisecond), err.Error())
	} else {
		// Parse and format the actual tool result
		resultContent = tc.formatToolResult(toolCall.Function.Name, serverName, result, duration)
	}
	
	// Send result to UI
	select {
	case resultChan <- resultContent:
	default:
	}
}

// isNativeFunction checks if a function is handled natively
func (tc *TermChat) isNativeFunction(functionName string) bool {
	nativeFunctions := []string{
		"execute_bash", "bash", "run_command", 
		"deploy_service", "scale_service", "restart_service", 
		"get_logs", "get_metrics", "check_health",
		"get_service_status", "create_backup",
		"edit_file", "editfile", "edit-file", // File editing tools as native
		"read_file", "readfile", "read-file", // File reading tools as native
		"search_files", "searchfiles", "search-files", // File search tools as native
		"parse_code", "parsecode", "parse-code", // Code parsing tools as native
	}
	
	for _, nativeFunc := range nativeFunctions {
		if functionName == nativeFunc {
			return true
		}
	}
	return false
}


// callDiscoveredTool calls a tool on a discovered MCP server
func (tc *TermChat) callDiscoveredTool(ctx context.Context, serverName, toolName string, arguments map[string]interface{}) (interface{}, error) {
	// Use the mcp.robotrad.io endpoint structure
	endpointURL := fmt.Sprintf("https://mcp.robotrad.io/servers/%s/tools/call", serverName)
	
	// Create MCP JSON-RPC request  
	mcpRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": arguments,
		},
	}
	
	reqBytes, err := json.Marshal(mcpRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}
	
	req, err := http.NewRequestWithContext(ctx, "POST", endpointURL, strings.NewReader(string(reqBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call tool: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(respBytes))
	}
	
	var mcpResponse struct {
		Jsonrpc string      `json:"jsonrpc"`
		ID      int         `json:"id"`
		Result  interface{} `json:"result"`
		Error   interface{} `json:"error,omitempty"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&mcpResponse); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	
	if mcpResponse.Error != nil {
		return nil, fmt.Errorf("MCP error: %v", mcpResponse.Error)
	}
	
	return mcpResponse.Result, nil
}

// requestFunctionConfirmationNonBlocking requests confirmation without blocking the UI thread
func (tc *TermChat) requestFunctionConfirmationNonBlocking(functionName, arguments string) bool {
	switch tc.approvalMode {
	case YOLO:
		return true
	case AUTO_EDIT:
		// Auto-approve safe operations (read-only functions)
		safeFunctions := []string{
			"get_logs", "get_metrics", "check_health", "get_service_status",
			"list_workflows", "get_workflow", "get_current_glucose", "get_glucose_history",
			"matey_ps", "matey_logs", "matey_inspect", "get_cluster_state",
			"list_toolboxes", "get_toolbox", "memory_status", "task_scheduler_status",
			"workflow_logs", "workflow_templates", "validate_config", "read_file",
			"list_directory", "get_file_info", "list_allowed_directories",
		}
		for _, safe := range safeFunctions {
			if functionName == safe || strings.HasSuffix(functionName, "_"+safe) {
				return true
			}
		}
		// For non-safe functions in AUTO_EDIT mode, use the UI callback
		if tc.uiConfirmationCallback != nil {
			return tc.uiConfirmationCallback(functionName, arguments)
		}
		return false
	default:
		// Default to manual approval - use the UI callback
		if tc.uiConfirmationCallback != nil {
			return tc.uiConfirmationCallback(functionName, arguments)
		}
		return true // Fallback to approve if no callback available
	}
}

// requestFunctionConfirmationAsync requests confirmation asynchronously for concurrent execution
func (tc *TermChat) requestFunctionConfirmationAsync(functionName, arguments string, callback func(bool)) {
	// Handle approval modes that don't need confirmation
	switch tc.approvalMode {
	case YOLO:
		callback(true)
		return
	case AUTO_EDIT:
		// Auto-approve safe operations
		safeFunctions := []string{
			"get_logs", "get_metrics", "check_health", "get_service_status",
			"list_mcp_servers", "list_workflows", "get_current_glucose",
			"matey_list_mcp_servers",
		}
		for _, safe := range safeFunctions {
			if functionName == safe || strings.HasSuffix(functionName, "_"+safe) {
				callback(true)
				return
			}
		}
		// Fall through to manual confirmation for non-safe functions
	}
	
	// For manual confirmation, use the UI callback system
	if tc.uiConfirmationCallback != nil {
		// The UI callback should handle the confirmation asynchronously
		result := tc.uiConfirmationCallback(functionName, arguments)
		callback(result)
	} else {
		// Fallback: auto-approve if no UI system available
		callback(true)
	}
}