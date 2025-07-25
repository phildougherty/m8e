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
		// TODO Management Tools
		{
			Name:        "create_todo",
			Description: "Create a new TODO item for tracking tasks and progress",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content": map[string]interface{}{
						"type":        "string",
						"description": "TODO item description",
					},
					"priority": map[string]interface{}{
						"type":        "string",
						"description": "Priority level: low, medium, high, urgent",
						"enum":        []string{"low", "medium", "high", "urgent"},
						"default":     "medium",
					},
				},
				"required": []string{"content"},
			},
		},
		{
			Name:        "create_todos",
			Description: "Create multiple TODO items in a single call for multi-step tasks",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"todos": map[string]interface{}{
						"type":        "array",
						"description": "Array of TODO items to create",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"content": map[string]interface{}{
									"type":        "string",
									"description": "TODO item description",
								},
								"priority": map[string]interface{}{
									"type":        "string",
									"description": "Priority level: low, medium, high, urgent",
									"enum":        []string{"low", "medium", "high", "urgent"},
									"default":     "medium",
								},
							},
							"required": []string{"content"},
						},
					},
				},
				"required": []string{"todos"},
			},
		},
		{
			Name:        "list_todos",
			Description: "List all TODO items with optional filtering by status or priority",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"status": map[string]interface{}{
						"type":        "string",
						"description": "Filter by status: pending, in_progress, completed, cancelled",
						"enum":        []string{"pending", "in_progress", "completed", "cancelled"},
					},
					"priority": map[string]interface{}{
						"type":        "string",
						"description": "Filter by priority: low, medium, high, urgent",
						"enum":        []string{"low", "medium", "high", "urgent"},
					},
				},
			},
		},
		{
			Name:        "update_todo_status",
			Description: "Update the status of a TODO item",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "string",
						"description": "TODO item ID",
					},
					"status": map[string]interface{}{
						"type":        "string",
						"description": "New status: pending, in_progress, completed, cancelled",
						"enum":        []string{"pending", "in_progress", "completed", "cancelled"},
					},
				},
				"required": []string{"id", "status"},
			},
		},
		{
			Name:        "update_todos",
			Description: "Update multiple TODO items in a single call for efficient bulk status changes",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"updates": map[string]interface{}{
						"type":        "array",
						"description": "Array of TODO status updates",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"id": map[string]interface{}{
									"type":        "string",
									"description": "TODO item ID",
								},
								"status": map[string]interface{}{
									"type":        "string",
									"description": "New status: pending, in_progress, completed, cancelled",
									"enum":        []string{"pending", "in_progress", "completed", "cancelled"},
								},
							},
							"required": []string{"id", "status"},
						},
					},
				},
				"required": []string{"updates"},
			},
		},
		{
			Name:        "remove_todo",
			Description: "Remove a TODO item from the list",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"id": map[string]interface{}{
						"type":        "string",
						"description": "TODO item ID",
					},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "clear_completed_todos",
			Description: "Remove all completed TODO items from the list",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "get_todo_stats",
			Description: "Get statistics about TODO items (counts by status and priority)",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
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
			Name:        "search_in_files",
			Description: "Search for text patterns within specific files or file patterns, returns matches with line numbers",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{
						"type":        "string",
						"description": "Text pattern or regex to search for",
					},
					"files": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Specific file paths to search in",
					},
					"file_pattern": map[string]interface{}{
						"type":        "string",
						"description": "Glob pattern for files to search (e.g., '*.go', 'internal/**/*.go')",
					},
					"regex": map[string]interface{}{
						"type":        "boolean",
						"description": "Treat pattern as regex",
						"default":     false,
					},
					"case_sensitive": map[string]interface{}{
						"type":        "boolean",
						"description": "Case sensitive search",
						"default":     false,
					},
					"max_results": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results per file",
						"default":     100,
					},
					"context_lines": map[string]interface{}{
						"type":        "integer",
						"description": "Number of context lines to show around matches",
						"default":     2,
					},
				},
				"required": []string{"pattern"},
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
		
		// Function call will be shown in the enhanced format after execution
		
		// Parse arguments
		parsedArgs := tc.parseArguments(toolCall.Function.Arguments)
		
		// Try native function execution first, then fall back to MCP
		var result interface{}
		var err error
		
		// Check if this is a native function
		nativeFunctions := []string{
			"execute_bash", "bash", "run_command", 
			"deploy_service", "scale_service", "restart_service", 
			"get_logs", "get_metrics", "check_health",
			"create_todo", "create_todos", "list_todos", "update_todo_status", "update_todos", "remove_todo", "clear_completed_todos", "get_todo_stats",
		}
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
				// Parse arguments for enhanced formatting
				var args map[string]interface{}
				if toolCall.Function.Arguments != "" {
					json.Unmarshal([]byte(toolCall.Function.Arguments), &args)
				}
				
				// Use our enhanced formatting for file editing tools
				if tc.isFileEditingTool(toolCall.Function.Name) && result != nil {
					duration := time.Millisecond * 50 // Placeholder duration
					statusMsg = tc.formatToolResultWithArgs(toolCall.Function.Name, "native", result, duration, args)
				} else {
					// Use the enhanced Claude Code style formatting
					duration := time.Millisecond * 50
					statusMsg = tc.formatToolResultWithArgs(toolCall.Function.Name, "native", result, duration, args)
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
	return tc.formatToolResultWithArgs(toolName, serverName, result, duration, nil)
}

// FormatToolResultWithArgs formats tool execution results with access to original arguments (exported for testing)
func (tc *TermChat) FormatToolResultWithArgs(toolName, serverName string, result interface{}, duration time.Duration, args map[string]interface{}) string {
	return tc.formatToolResultWithArgs(toolName, serverName, result, duration, args)
}

// formatToolResultWithArgs formats tool execution results with access to original arguments
func (tc *TermChat) formatToolResultWithArgs(toolName, serverName string, result interface{}, duration time.Duration, args map[string]interface{}) string {
	
	// Calculate token count estimate (rough approximation)
	resultStr := fmt.Sprintf("%+v", result)
	tokenCount := len(strings.Fields(resultStr)) // Simple word count as token estimate
	
	// Check if this is a file editing tool result with diff preview
	if tc.isFileEditingTool(toolName) {
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
				
				// Check if it's a dry run
				if dryRun, isDryRun := resultMap["dry_run"].(bool); isDryRun && dryRun {
					formattedDiff += "\n\x1b[33m[DRY RUN] Changes not applied\x1b[0m"
				}
				
				// Add the visual diff content
				return fmt.Sprintf("\n%s\n\n%s", statusLine, formattedDiff)
			}
		}
	}
	
	// Claude Code style: show tool with key results
	return tc.formatToolResultWithDetailsAndArgs(toolName, serverName, result, duration, tokenCount, args)
}

// formatToolResultWithDetailsAndArgs intelligently formats tool results based on tool type and content
func (tc *TermChat) formatToolResultWithDetailsAndArgs(toolName, serverName string, result interface{}, duration time.Duration, tokenCount int, args map[string]interface{}) string {
	bullet := "\x1b[32m*\x1b[0m"
	
	// Special handling for execute_bash to show the command
	var displayName string
	if (toolName == "execute_bash" || toolName == "bash" || toolName == "run_command") && args != nil {
		if command, ok := args["command"].(string); ok {
			// Truncate long commands
			if len(command) > 50 {
				command = command[:47] + "..."
			}
			displayName = fmt.Sprintf("%s(%s)", toolName, command)
		} else {
			displayName = fmt.Sprintf("%s(%s)", toolName, serverName)
		}
	} else {
		displayName = fmt.Sprintf("%s(%s)", toolName, serverName)
	}
	
	statusLine := fmt.Sprintf("\n%s \x1b[32m%s\x1b[0m", bullet, displayName)
	
	summary := tc.extractAnyContent(result, toolName)
	if summary != "" {
		return statusLine + fmt.Sprintf("\n  \x1b[90m⎿\x1b[0m  %s", summary)
	}
	
	return statusLine
}

// extractAnyContent tries to extract readable content from any result structure
func (tc *TermChat) extractAnyContent(result interface{}, toolName string) string {
	// First check if it's a map - use extractMapSummary for structured data
	if resultMap, ok := result.(map[string]interface{}); ok {
		return tc.extractMapSummary(resultMap, toolName)
	}
	
	// Convert to string and try to parse it
	resultStr := fmt.Sprintf("%+v", result)
	
	// Check if it looks like a Go struct with Content field
	if strings.Contains(resultStr, "Content:[") && strings.Contains(resultStr, "Text:") {
		// Extract the Text content using string parsing
		return tc.parseGoStructContent(resultStr, toolName)
	}
	
	return ""
}

// parseGoStructContent parses Go struct string representation to extract readable content
func (tc *TermChat) parseGoStructContent(structStr, toolName string) string {
	// Look for Text: pattern and extract the content
	textStart := strings.Index(structStr, "Text:")
	if textStart == -1 {
		return ""
	}
	
	// Find the content after "Text:"
	textStart += 5 // Length of "Text:"
	remaining := structStr[textStart:]
	
	// Find the end of the text content (either " map[" or end of meaningful content)
	var textEnd int
	if mapIndex := strings.Index(remaining, " map["); mapIndex != -1 {
		textEnd = mapIndex
	} else if braceIndex := strings.Index(remaining, "}"); braceIndex != -1 {
		textEnd = braceIndex
	} else {
		textEnd = len(remaining)
	}
	
	if textEnd > 0 {
		content := strings.TrimSpace(remaining[:textEnd])
		
		// Clean up the extracted content
		if strings.HasPrefix(content, "Knowledge graph:") {
			// For memory tools, extract key information
			return tc.extractMemoryInfo(content)
		} else if strings.Contains(strings.ToLower(content), "phase:") && strings.Contains(strings.ToLower(content), "running") {
			return "memory service: running"
		} else if len(content) > 0 {
			return tc.truncateString(content, 80)
		}
	}
	
	return ""
}

// extractMapSummary extracts a summary from a map result
func (tc *TermChat) extractMapSummary(m map[string]interface{}, toolName string) string {
	// Handle specific tools that return structured data
	switch toolName {
	// Web and search tools
	case "search_web":
		if query, ok := m["query"].(string); ok {
			if count, ok := m["count"].(float64); ok {
				return fmt.Sprintf("Found %d results for \"%s\"", int(count), tc.truncateString(query, 30))
			}
		}
		return "Web search completed"
	
	// File operations
	case "read_file", "read_workspace_file":
		if path, ok := m["path"].(string); ok {
			if lines, ok := m["lines"].(float64); ok {
				return fmt.Sprintf("Read %d lines from %s", int(lines), tc.getFileName(path))
			}
			return fmt.Sprintf("Read file %s", tc.getFileName(path))
		}
		return "File read"
	
	case "list_files", "glob", "search_files", "list_workspace_files":
		if count, ok := m["count"].(float64); ok {
			return fmt.Sprintf("Found %d files", int(count))
		}
		if files, ok := m["files"].([]interface{}); ok {
			return fmt.Sprintf("Found %d files", len(files))
		}
		return "Files listed"
	
	case "edit_file", "write_file", "create_file":
		if path, ok := m["path"].(string); ok {
			return fmt.Sprintf("Modified %s", tc.getFileName(path))
		}
		return "File modified"
	
	case "parse_code":
		if language, ok := m["language"].(string); ok {
			if count, ok := m["definitions_count"].(float64); ok {
				return fmt.Sprintf("Parsed %s code: %d definitions", language, int(count))
			}
			return fmt.Sprintf("Parsed %s code", language)
		}
		return "Code parsed"
	
	// Command execution
	case "execute_bash", "bash", "run_command":
		if stdout, ok := m["stdout"].(string); ok {
			// Show first line of output for bash commands
			lines := strings.Split(strings.TrimSpace(stdout), "\n")
			if len(lines) > 0 && lines[0] != "" {
				return tc.truncateString(lines[0], 80)
			}
		}
		if exitCode, ok := m["exit_code"].(float64); ok {
			if int(exitCode) == 0 {
				return "Command executed successfully"
			}
			// Show stderr if available on failure
			if stderr, ok := m["stderr"].(string); ok && stderr != "" {
				lines := strings.Split(strings.TrimSpace(stderr), "\n")
				if len(lines) > 0 {
					return fmt.Sprintf("Failed (exit %d): %s", int(exitCode), tc.truncateString(lines[0], 60))
				}
			}
			return fmt.Sprintf("Command failed (exit %d)", int(exitCode))
		}
		return "Command executed"
	
	// Memory/Knowledge Graph tools
	case "read_graph":
		if entityCount, ok := m["entityCount"].(float64); ok {
			if relationCount, ok := m["relationCount"].(float64); ok {
				return fmt.Sprintf("Graph: %d entities, %d relations", int(entityCount), int(relationCount))
			}
		}
		return "Knowledge graph retrieved"
	
	case "search_nodes":
		if count, ok := m["count"].(float64); ok {
			if query, ok := m["query"].(string); ok {
				return fmt.Sprintf("Found %d nodes for \"%s\"", int(count), tc.truncateString(query, 30))
			}
		}
		return "Node search completed"
	
	case "create_entities":
		if entities, ok := m["entities"].([]interface{}); ok {
			return fmt.Sprintf("Created %d entities", len(entities))
		}
		return "Entities created"
	
	case "delete_entities":
		if count, ok := m["deletedEntities"].([]interface{}); ok {
			return fmt.Sprintf("Deleted %d entities", len(count))
		}
		return "Entities deleted"
	
	case "add_observations":
		if message, ok := m["message"].(string); ok {
			return tc.truncateString(message, 80)
		}
		return "Observations added"
	
	case "delete_observations":
		if message, ok := m["message"].(string); ok {
			return tc.truncateString(message, 80)
		}
		return "Observations deleted"
	
	case "create_relations", "delete_relations":
		if relations, ok := m["relations"].([]interface{}); ok {
			action := "Created"
			if toolName == "delete_relations" {
				action = "Deleted"
			}
			return fmt.Sprintf("%s %d relations", action, len(relations))
		}
		return "Relations updated"
	
	case "open_nodes":
		if entities, ok := m["entities"].([]interface{}); ok {
			return fmt.Sprintf("Retrieved %d nodes", len(entities))
		}
		return "Nodes retrieved"
	
	case "memory_health_check":
		if status, ok := m["status"].(string); ok {
			return fmt.Sprintf("Memory system: %s", status)
		}
		return "Health check completed"
	
	case "memory_stats":
		if entities, ok := m["entities"].(float64); ok {
			if observations, ok := m["observations"].(float64); ok {
				return fmt.Sprintf("Memory: %d entities, %d observations", int(entities), int(observations))
			}
		}
		return "Memory statistics retrieved"
	
	// TODO system case handling
	case "create_todo":
		if content, ok := m["content"].(string); ok {
			if id, ok := m["id"].(string); ok {
				return fmt.Sprintf("Created TODO: %s (ID: %s)", tc.truncateString(content, 50), id)
			}
			return fmt.Sprintf("Created TODO: %s", tc.truncateString(content, 50))
		}
		return "TODO created"
	
	case "create_todos":
		// Handle both int and float64 for created_count
		var count float64
		var hasCount bool
		if countInt, ok := m["created_count"].(int); ok {
			count = float64(countInt)
			hasCount = true
		} else if countFloat, ok := m["created_count"].(float64); ok {
			count = countFloat
			hasCount = true
		}
		
		if hasCount {
			if items, ok := m["items"].([]interface{}); ok {
				var result strings.Builder
				result.WriteString(fmt.Sprintf("Created %d TODOs:", int(count)))
				
				for i, item := range items {
					if itemMap, ok := item.(map[string]interface{}); ok {
						content, _ := itemMap["content"].(string)
						priority, _ := itemMap["priority"].(string)
						status, _ := itemMap["status"].(string)
						
						// Format status like Claude Code
						var statusIcon string
						switch status {
						case "pending":
							statusIcon = "[ ]"
						case "in_progress":
							statusIcon = "[▶]"
						case "completed":
							statusIcon = "[✓]"
						default:
							statusIcon = "[ ]"
						}
						
						// Format priority like Claude Code
						var priorityLabel string
						switch priority {
						case "urgent":
							priorityLabel = "(urgent)"
						case "high":
							priorityLabel = "(high)"
						case "medium":
							priorityLabel = "(medium)"
						case "low":
							priorityLabel = "(low)"
						default:
							priorityLabel = "(medium)"
						}
						
						result.WriteString(fmt.Sprintf("\n%d. %s %s %s", 
							i+1, statusIcon, content, priorityLabel))
					}
				}
				
				return result.String()
			}
			return fmt.Sprintf("Created %d TODOs", int(count))
		}
		return "Bulk TODO creation completed"
	
	case "list_todos":
		if items, ok := m["items"].([]interface{}); ok {
			if len(items) == 0 {
				return "No TODO items found"
			}
			
			// Format the full list of TODO items
			var result strings.Builder
			result.WriteString(fmt.Sprintf("TODO List (%d items):\n", len(items)))
			
			for i, item := range items {
				if itemMap, ok := item.(map[string]interface{}); ok {
					id, _ := itemMap["id"].(string)
					content, _ := itemMap["content"].(string)
					status, _ := itemMap["status"].(string)
					priority, _ := itemMap["priority"].(string)
					
					// Format status
					var statusLabel string
					switch status {
					case "pending":
						statusLabel = "PENDING"
					case "in_progress":
						statusLabel = "ACTIVE"
					case "completed":
						statusLabel = "DONE"
					case "cancelled":
						statusLabel = "CANCELLED"
					default:
						statusLabel = "UNKNOWN"
					}
					
					// Format priority
					var priorityLabel string
					switch priority {
					case "urgent":
						priorityLabel = "URGENT"
					case "high":
						priorityLabel = "HIGH"
					case "medium":
						priorityLabel = "MED"
					case "low":
						priorityLabel = "LOW"
					default:
						priorityLabel = "MED"
					}
					
					result.WriteString(fmt.Sprintf("%d. [%s] [%s] %s (%s)\n", 
						i+1, statusLabel, priorityLabel, tc.truncateString(content, 60), id[:8]))
				}
			}
			
			return strings.TrimSuffix(result.String(), "\n")
		}
		return "TODO list retrieved"
	
	case "update_todo_status":
		if status, ok := m["status"].(string); ok {
			if id, ok := m["id"].(string); ok {
				// Find the TODO item to show its content instead of ID
				if tc.todoList != nil {
					for _, item := range tc.todoList.Items {
						if item.ID == id {
							return fmt.Sprintf("Updated TODO (%s) to %s", tc.truncateString(item.Content, 50), status)
						}
					}
				}
				return fmt.Sprintf("Updated TODO %s to %s", id, status)
			}
			return fmt.Sprintf("Updated TODO status to %s", status)
		}
		return "TODO status updated"
	
	case "update_todos":
		if count, ok := m["updated_count"].(float64); ok {
			if updates, ok := m["updates"].([]interface{}); ok {
				var summaries []string
				for i, update := range updates {
					if i >= 3 {
						summaries = append(summaries, "...")
						break
					}
					if updateMap, ok := update.(map[string]interface{}); ok {
						if id, ok := updateMap["id"].(string); ok {
							if status, ok := updateMap["status"].(string); ok {
								// Find the TODO content to show instead of ID
								content := id // fallback to ID
								if tc.todoList != nil {
									for _, item := range tc.todoList.Items {
										if item.ID == id {
											content = tc.truncateString(item.Content, 20)
											break
										}
									}
								}
								summaries = append(summaries, fmt.Sprintf("(%s)→%s", content, status))
							}
						}
					}
				}
				return fmt.Sprintf("Updated %d TODOs: %s", int(count), strings.Join(summaries, ", "))
			}
			return fmt.Sprintf("Updated %d TODOs", int(count))
		}
		return "Bulk TODO updates completed"
	
	case "remove_todo":
		if id, ok := m["id"].(string); ok {
			return fmt.Sprintf("Removed TODO: %s", id)
		}
		return "TODO removed"
	
	case "clear_completed_todos":
		if count, ok := m["cleared_count"].(float64); ok {
			return fmt.Sprintf("Cleared %d completed TODOs", int(count))
		}
		return "Completed TODOs cleared"
	
	case "get_todo_stats":
		if totalCount, ok := m["total_count"].(float64); ok {
			if pendingCount, ok := m["pending_count"].(float64); ok {
				return fmt.Sprintf("TODO stats: %d total, %d pending", int(totalCount), int(pendingCount))
			}
			return fmt.Sprintf("TODO stats: %d total items", int(totalCount))
		}
		return "TODO statistics retrieved"
	
	// Cluster management tools
	case "matey_ps":
		if servers, ok := m["servers"].([]interface{}); ok {
			return fmt.Sprintf("Found %d MCP servers", len(servers))
		}
		return "Server status retrieved"
	
	case "matey_up", "matey_down":
		if services, ok := m["services"].([]interface{}); ok {
			action := "Started"
			if toolName == "matey_down" {
				action = "Stopped"
			}
			return fmt.Sprintf("%s %d services", action, len(services))
		}
		return "Services updated"
	
	case "matey_logs":
		if service, ok := m["service"].(string); ok {
			return fmt.Sprintf("Logs from %s", service)
		}
		return "Logs retrieved"
	
	case "matey_inspect":
		if resourceType, ok := m["type"].(string); ok {
			if name, ok := m["name"].(string); ok {
				return fmt.Sprintf("Inspected %s: %s", resourceType, name)
			}
			return fmt.Sprintf("Inspected %s", resourceType)
		}
		return "Resource inspected"
	
	case "get_cluster_state":
		if resources, ok := m["resources"].(map[string]interface{}); ok {
			return fmt.Sprintf("Cluster state: %d resource types", len(resources))
		}
		return "Cluster state retrieved"
	
	case "apply_config":
		if applied, ok := m["applied"].([]interface{}); ok {
			return fmt.Sprintf("Applied %d resources", len(applied))
		}
		return "Configuration applied"
	
	// Workflow management
	case "create_workflow":
		if name, ok := m["name"].(string); ok {
			if schedule, ok := m["schedule"].(string); ok {
				return fmt.Sprintf("Created workflow '%s' (%s)", name, schedule)
			}
			return fmt.Sprintf("Created workflow '%s'", name)
		}
		return "Workflow created"
	
	case "list_workflows":
		if workflows, ok := m["workflows"].([]interface{}); ok {
			if len(workflows) == 0 {
				return "No workflows found"
			}
			// Extract first few workflow names for summary
			var names []string
			for i, wf := range workflows {
				if i >= 3 { break } // Show max 3 workflow names
				if wfMap, ok := wf.(map[string]interface{}); ok {
					if name, ok := wfMap["name"].(string); ok {
						names = append(names, name)
					}
				}
			}
			if len(names) > 0 {
				nameList := strings.Join(names, ", ")
				if len(workflows) > 3 {
					nameList += fmt.Sprintf(" (+%d more)", len(workflows)-3)
				}
				return fmt.Sprintf("%d workflows: %s", len(workflows), nameList)
			}
			return fmt.Sprintf("Found %d workflows", len(workflows))
		}
		return "Workflows listed"
	
	case "get_workflow":
		if name, ok := m["name"].(string); ok {
			if status, ok := m["status"].(string); ok {
				return fmt.Sprintf("Workflow '%s': %s", name, status)
			}
			return fmt.Sprintf("Retrieved workflow '%s'", name)
		}
		return "Workflow retrieved"
	
	case "delete_workflow":
		if name, ok := m["name"].(string); ok {
			return fmt.Sprintf("Deleted workflow '%s'", name)
		}
		return "Workflow deleted"
	
	case "execute_workflow":
		if name, ok := m["workflow"].(string); ok {
			return fmt.Sprintf("Executed workflow '%s'", name)
		}
		return "Workflow executed"
	
	case "workflow_logs":
		if workflow, ok := m["workflow"].(string); ok {
			return fmt.Sprintf("Logs from workflow '%s'", workflow)
		}
		return "Workflow logs retrieved"
	
	case "pause_workflow", "resume_workflow":
		if name, ok := m["name"].(string); ok {
			action := "Paused"
			if toolName == "resume_workflow" {
				action = "Resumed"
			}
			return fmt.Sprintf("%s workflow '%s'", action, name)
		}
		return "Workflow updated"
	
	case "workflow_templates":
		if templates, ok := m["templates"].([]interface{}); ok {
			return fmt.Sprintf("Found %d workflow templates", len(templates))
		}
		return "Workflow templates listed"
	
	// Service management
	case "start_service", "stop_service":
		if service, ok := m["service"].(string); ok {
			action := "Started"
			if toolName == "stop_service" {
				action = "Stopped"
			}
			return fmt.Sprintf("%s service '%s'", action, service)
		}
		return "Service updated"
	
	case "reload_proxy":
		if discovered, ok := m["discovered"].(float64); ok {
			return fmt.Sprintf("Proxy reloaded, discovered %d servers", int(discovered))
		}
		return "Proxy configuration reloaded"
	
	// Status tools
	case "memory_status", "task_scheduler_status":
		if status, ok := m["status"].(string); ok {
			service := "Memory"
			if toolName == "task_scheduler_status" {
				service = "Task scheduler"
			}
			return fmt.Sprintf("%s service: %s", service, status)
		}
		return "Status retrieved"
	
	case "memory_start", "memory_stop", "task_scheduler_start", "task_scheduler_stop":
		service := "Memory"
		action := "Started"
		if strings.Contains(toolName, "task_scheduler") {
			service = "Task scheduler"
		}
		if strings.Contains(toolName, "stop") {
			action = "Stopped"
		}
		return fmt.Sprintf("%s service %s", service, strings.ToLower(action))
	
	// Toolbox management
	case "list_toolboxes":
		if toolboxes, ok := m["toolboxes"].([]interface{}); ok {
			return fmt.Sprintf("Found %d toolboxes", len(toolboxes))
		}
		return "Toolboxes listed"
	
	case "get_toolbox":
		if name, ok := m["name"].(string); ok {
			return fmt.Sprintf("Retrieved toolbox '%s'", name)
		}
		return "Toolbox retrieved"
	
	// Configuration tools
	case "validate_config":
		if valid, ok := m["valid"].(bool); ok {
			if valid {
				return "Configuration is valid"
			}
			return "Configuration validation failed"
		}
		return "Configuration validated"
	
	case "create_config":
		if servers, ok := m["servers"].([]interface{}); ok {
			return fmt.Sprintf("Created config for %d servers", len(servers))
		}
		return "Configuration created"
	
	case "install_matey":
		if installed, ok := m["installed"].([]interface{}); ok {
			return fmt.Sprintf("Installed %d CRDs", len(installed))
		}
		return "Matey CRDs installed"
	
	// Resource inspection tools (inspect_*)
	case "inspect_mcpserver", "inspect_mcpmemory", "inspect_mcptaskscheduler", "inspect_mcpproxy", "inspect_mcptoolbox":
		if name, ok := m["name"].(string); ok {
			resourceType := strings.TrimPrefix(toolName, "inspect_")
			return fmt.Sprintf("Inspected %s: %s", resourceType, name)
		}
		return "Resource inspected"
	
	case "inspect_all":
		if resources, ok := m["resources"].([]interface{}); ok {
			return fmt.Sprintf("Inspected %d resources", len(resources))
		}
		return "All resources inspected"
	
	// Workspace management
	case "mount_workspace":
		if workspace, ok := m["workspace"].(string); ok {
			return fmt.Sprintf("Mounted workspace '%s'", workspace)
		}
		return "Workspace mounted"
	
	case "unmount_workspace":
		if workspace, ok := m["workspace"].(string); ok {
			return fmt.Sprintf("Unmounted workspace '%s'", workspace)
		}
		return "Workspace unmounted"
	
	case "list_mounted_workspaces":
		if workspaces, ok := m["workspaces"].([]interface{}); ok {
			return fmt.Sprintf("Found %d mounted workspaces", len(workspaces))
		}
		return "Mounted workspaces listed"
	
	case "get_workspace_stats":
		if pvcs, ok := m["total_pvcs"].(float64); ok {
			return fmt.Sprintf("Workspace stats: %d PVCs", int(pvcs))
		}
		return "Workspace statistics retrieved"
	
	// Context management
	case "get_context":
		if items, ok := m["items"].([]interface{}); ok {
			return fmt.Sprintf("Context: %d items", len(items))
		}
		return "Context retrieved"
	
	case "add_context":
		if added, ok := m["added_items"].([]interface{}); ok {
			return fmt.Sprintf("Added %d context items", len(added))
		}
		return "Context items added"
	
	case "process_mentions":
		if mentions, ok := m["mentions"].([]interface{}); ok {
			return fmt.Sprintf("Processed %d mentions", len(mentions))
		}
		return "Mentions processed"
	
	case "get_relevant_files":
		if files, ok := m["files"].([]interface{}); ok {
			return fmt.Sprintf("Found %d relevant files", len(files))
		}
		return "Relevant files retrieved"
	
	case "clear_context":
		if cleared, ok := m["cleared_count"].(float64); ok {
			return fmt.Sprintf("Cleared %d context items", int(cleared))
		}
		return "Context cleared"
	
	case "get_context_stats":
		if totalItems, ok := m["total_items"].(float64); ok {
			return fmt.Sprintf("Context stats: %d total items", int(totalItems))
		}
		return "Context statistics retrieved"
	
	default:
		// Generic extraction for unknown tools
		if message, ok := m["message"].(string); ok {
			return tc.truncateString(message, 80)
		}
		if success, ok := m["success"].(bool); ok && success {
			return "Completed successfully"
		}
		if err, ok := m["error"].(string); ok {
			return fmt.Sprintf("Error: %s", tc.truncateString(err, 60))
		}
		return "Operation completed"
	}
}

// extractArraySummary extracts a summary from an array result
func (tc *TermChat) extractArraySummary(arr []interface{}, toolName string) string {
	if len(arr) == 0 {
		return "Empty result"
	}
	return fmt.Sprintf("Returned %d items", len(arr))
}

// extractFromString extracts meaningful content from string results
func (tc *TermChat) extractFromString(s string, toolName string) string {
	if s == "" {
		return ""
	}
	
	// For certain tools, extract key information
	switch toolName {
	case "execute_bash", "bash", "run_command":
		lines := strings.Split(strings.TrimSpace(s), "\n")
		if len(lines) > 0 && lines[0] != "" {
			return tc.truncateString(lines[0], 60)
		}
		return "Command completed"
	default:
		return tc.truncateString(s, 80)
	}
}

// truncateString truncates text to specified length with ellipsis
func (tc *TermChat) truncateString(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen-3] + "..."
}

// getFileName extracts filename from a file path
func (tc *TermChat) getFileName(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return path
}

// extractMemoryInfo extracts key memory statistics from text
func (tc *TermChat) extractMemoryInfo(text string) string {
	// Look for patterns like "15 entities" or "4 relationships"
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		
		// Look for entity/relationship counts
		if strings.Contains(lower, "entities") && (strings.Contains(lower, "contains") || strings.Contains(lower, "found")) {
			return tc.truncateString(line, 100)
		}
		if strings.Contains(lower, "knowledge graph") {
			return tc.truncateString(line, 100)
		}
		if strings.Contains(lower, "phase:") && strings.Contains(lower, "running") {
			return "memory service: running"
		}
	}
	
	// Fallback to first non-empty line
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" && len(line) > 10 {
			return tc.truncateString(line, 100)
		}
	}
	
	return "completed"
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
		// Function call will be shown in enhanced format after execution
		
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
		
		// Function call will be shown in enhanced format after execution
		
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
		// Parse arguments for enhanced formatting
		var args map[string]interface{}
		if toolCall.Function.Arguments != "" {
			json.Unmarshal([]byte(toolCall.Function.Arguments), &args)
		}
		
		// Parse and format the actual tool result
		resultContent = tc.formatToolResultWithArgs(toolCall.Function.Name, serverName, result, duration, args)
	}
	
	// Send result to UI
	select {
	case resultChan <- resultContent:
	default:
	}
}

// isNativeFunction checks if a function is handled natively
func (tc *TermChat) isNativeFunction(functionName string) bool {
	// All tools now route through MCP discovery - native tools are handled by matey-mcp-server
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