package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/phildougherty/m8e/internal/ai"
)


// ConversationTurn represents a single turn in the conversation
type ConversationTurn struct {
	chat          *TermChat
	messages      []ai.Message
	pendingTools  []ai.ToolCall
	completed     bool
	maxTurns      int
	currentTurn   int
	userRequest   string
	silentMode    bool  // If true, don't print to terminal
}

// NewConversationTurn creates a new conversation turn
func NewConversationTurn(chat *TermChat, userRequest string) *ConversationTurn {
	return &ConversationTurn{
		chat:         chat,
		userRequest:  userRequest,
		maxTurns:     25, // Increased from 15 to allow more auto-continuation turns
		currentTurn:  0,
		completed:    false,
		pendingTools: make([]ai.ToolCall, 0),
		silentMode:   false,
	}
}

// NewConversationTurnSilent creates a new conversation turn in silent mode
func NewConversationTurnSilent(chat *TermChat, userRequest string) *ConversationTurn {
	return &ConversationTurn{
		chat:         chat,
		userRequest:  userRequest,
		maxTurns:     25, // Increased from 15 to allow more auto-continuation turns
		currentTurn:  0,
		completed:    false,
		pendingTools: make([]ai.ToolCall, 0),
		silentMode:   true,
	}
}

// Execute runs the conversation turn with automatic continuation
func (t *ConversationTurn) Execute(ctx context.Context) error {
	// Build initial message context
	t.buildMessageContext()
	
	// Start the conversation flow
	return t.processConversationFlow(ctx)
}

// buildMessageContext constructs the conversation context
func (t *ConversationTurn) buildMessageContext() {
	// Start with system context
	t.messages = []ai.Message{
		{
			Role:    "system",
			Content: t.chat.GetSystemContext(),
		},
	}
	
	// Add context manager content if available
	if t.chat.contextManager != nil {
		window := t.chat.contextManager.GetCurrentWindow()
		if len(window.Items) > 0 {
			var contextContent strings.Builder
			contextContent.WriteString("# Context Files\n\nThe following files have been added to your context for reference:\n\n")
			
			for _, item := range window.Items {
				if item.Type == "file" && item.FilePath != "" {
					contextContent.WriteString(fmt.Sprintf("## %s\n\n```\n%s\n```\n\n", item.FilePath, item.Content))
				}
			}
			
			t.messages = append(t.messages, ai.Message{
				Role:    "system",
				Content: contextContent.String(),
			})
		}
	}
	
	// Add recent chat history (last 10 messages to keep context manageable)
	historyLimit := 10
	startIdx := len(t.chat.GetChatHistory()) - historyLimit
	if startIdx < 0 {
		startIdx = 0
	}
	
	for i := startIdx; i < len(t.chat.GetChatHistory()); i++ {
		msg := t.chat.GetChatHistory()[i]
		if msg.Role == "user" || msg.Role == "assistant" {
			t.messages = append(t.messages, ai.Message{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}
	
	// Add the current user request
	t.messages = append(t.messages, ai.Message{
		Role:    "user",
		Content: t.userRequest,
	})
}

// processConversationFlow handles the main conversation flow with automatic continuation
func (t *ConversationTurn) processConversationFlow(ctx context.Context) error {
	for !t.completed && t.currentTurn < t.maxTurns {
		t.currentTurn++
		
		// Execute one round of conversation
		if err := t.executeConversationRound(ctx); err != nil {
			return fmt.Errorf("conversation round %d failed: %v", t.currentTurn, err)
		}
		
		// Check if we should continue
		shouldContinue := t.shouldContinue()
		if !shouldContinue {
			t.completed = true
			break
		}
		
		// Strict enforcement: don't exceed maxTurns even with pending operations
		if t.currentTurn >= t.maxTurns {
			if !t.silentMode {
				fmt.Printf("\n\033[90mReached maximum turns (%d). Stopping auto-continuation.\033[0m\n", t.maxTurns)
			}
			t.completed = true
			break
		}
		
		// Show auto-continuation feedback if we're continuing due to tool call limits
		if t.detectToolCallLimit() && !t.silentMode {
			fmt.Printf("\n\033[90m▶ Auto-continuing after tool call limit (turn %d/%d)...\033[0m\n", t.currentTurn+1, t.maxTurns)
		} else if t.detectToolCallLimit() && t.silentMode && GetUIProgram() != nil {
			// Show in UI
			GetUIProgram().Send(aiStreamMsg{content: fmt.Sprintf("\n\033[90m▶ Auto-continuing after tool call limit (turn %d/%d)...\033[0m\n", t.currentTurn+1, t.maxTurns)})
		}
		
		// If we have pending tools, execute them and continue
		if len(t.pendingTools) > 0 {
			if err := t.executeToolsAndContinue(ctx); err != nil {
				return fmt.Errorf("tool execution failed: %v", err)
			}
		}
	}
	
	if t.currentTurn >= t.maxTurns {
		if !t.silentMode {
			fmt.Printf("\nReached maximum turns (%d) for this request. Task may need to be broken down into smaller parts.\n", t.maxTurns)
		}
	}
	
	return nil
}

// executeConversationRound executes one round of AI conversation
func (t *ConversationTurn) executeConversationRound(ctx context.Context) error {
	// Create AI message placeholder
	t.chat.AddMessage("assistant", "")
	aiMsgIndex := len(t.chat.GetChatHistory()) - 1
	
	// Only print to terminal if not in silent mode
	if !t.silentMode {
		fmt.Printf("\n\033[1;32mAI\033[0m \033[90m%s\033[0m", time.Now().Format("15:04:05"))
		if t.currentTurn > 1 {
			fmt.Printf(" \033[90m(turn %d)\033[0m", t.currentTurn)
		}
		fmt.Println()
	} else {
		// In silent mode, start the AI response box in UI
		if GetUIProgram() != nil {
			GetUIProgram().Send(aiStreamMsg{content: ""}) // Empty content signals start of AI response
		}
	}
	
	// Get MCP functions
	mcpFunctions := t.chat.GetMCPFunctions()
	
	// Stream the AI response
	streamCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	
	stream, err := t.chat.GetAIManager().StreamChatWithFallback(streamCtx, t.messages, ai.StreamOptions{
		Model:     t.chat.GetCurrentModel(),
		Functions: mcpFunctions,
	})
	if err != nil {
		return fmt.Errorf("AI streaming failed: %v", err)
	}
	
	// Process streaming response and collect tool calls
	t.pendingTools = make([]ai.ToolCall, 0)
	accumulatedFunctionCalls := make(map[string]*ai.ToolCall)
	
	thinkProcessor := ai.NewThinkProcessor()
	
	for response := range stream {
		if response.Error != nil {
			t.chat.GetChatHistory()[aiMsgIndex].Content += fmt.Sprintf("\nError: %v", response.Error)
			break
		}
		
		// Process content
		if response.Content != "" {
			regularContent := response.Content
			if strings.Contains(response.Content, "<think>") || strings.Contains(response.Content, "</think>") {
				regularContent, _ = ai.ProcessStreamContentWithThinks(thinkProcessor, response.Content)
			}
			
			t.chat.GetChatHistory()[aiMsgIndex].Content += regularContent
			
			// Handle output based on mode
			if !t.silentMode {
				// Terminal mode - only print if not verbose mode
				if !t.chat.GetVerboseMode() {
					fmt.Print(regularContent)
				}
			} else {
				// Silent mode (UI) - send raw content for streaming (markdown will be rendered at the end)
				if GetUIProgram() != nil {
					GetUIProgram().Send(aiStreamMsg{content: regularContent})
				}
			}
		} else if len(response.ToolCalls) > 0 && t.silentMode {
			// In silent mode, tool calls without content don't need generic filler text
			// The function calls themselves will be visible to the user
		}
		
		// Debug: Check if we're getting empty responses
		if response.Content == "" && len(response.ToolCalls) == 0 && !response.Finished {
			// This might indicate a streaming issue
			continue
		}
		
		// Collect tool calls
		if len(response.ToolCalls) > 0 {
			for _, toolCall := range response.ToolCalls {
				
				callId := toolCall.ID
				if callId == "" {
					// For chunks without ID, use the function name if available
					if toolCall.Function.Name != "" {
						callId = toolCall.Function.Name
					} else {
						// Find the most recent function call to accumulate to
						for existingId, existingCall := range accumulatedFunctionCalls {
							if existingCall.Function.Name != "" {
								callId = existingId
								break
							}
						}
						if callId == "" {
							callId = fmt.Sprintf("call_%d", len(accumulatedFunctionCalls))
						}
					}
				}
				
				if existing, exists := accumulatedFunctionCalls[callId]; exists {
					existing.Function.Arguments += toolCall.Function.Arguments
					if existing.Function.Name == "" && toolCall.Function.Name != "" {
						existing.Function.Name = toolCall.Function.Name
					}
				} else {
					accumulatedFunctionCalls[callId] = &ai.ToolCall{
						ID:   toolCall.ID,
						Type: toolCall.Type,
						Function: ai.FunctionCall{
							Name:      toolCall.Function.Name,
							Arguments: toolCall.Function.Arguments,
						},
					}
				}
			}
		}
	}
	
	// Convert accumulated function calls to pending tools
	for _, call := range accumulatedFunctionCalls {
		if call.Function.Name != "" {
			t.pendingTools = append(t.pendingTools, *call)
		}
	}
	
	// Add the AI response to message context for next turn
	currentAIMessage := ai.Message{
		Role:      "assistant",
		Content:   t.chat.GetChatHistory()[aiMsgIndex].Content,
		ToolCalls: t.pendingTools,
	}
	t.messages = append(t.messages, currentAIMessage)
	
	// In silent mode, signal end of AI response to UI
	if t.silentMode && GetUIProgram() != nil {
		GetUIProgram().Send(aiResponseMsg{content: t.chat.GetChatHistory()[aiMsgIndex].Content})
	}
	
	return nil
}

// executeToolsAndContinue executes pending tools and adds results to conversation
func (t *ConversationTurn) executeToolsAndContinue(ctx context.Context) error {
	aiMsgIndex := len(t.chat.GetChatHistory()) - 1
	var toolResults []ai.Message
	
	// Execute all pending tools
	for _, toolCall := range t.pendingTools {
		if toolCall.Type != "function" {
			continue
		}
		
		// Check approval mode for this specific tool
		if t.chat.GetApprovalMode().ShouldConfirm(toolCall.Function.Name) {
			confirmed := t.chat.RequestFunctionConfirmation(toolCall.Function.Name, toolCall.Function.Arguments)
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
		
		// In silent mode, show function call execution in UI
		// Function call will be shown in enhanced format after execution
		
		// Parse arguments and validate for specific functions
		parsedArgs := t.parseArguments(toolCall.Function.Arguments)
		
		// Special validation for create_workflow to provide helpful error messages
		if toolCall.Function.Name == "create_workflow" {
			if len(parsedArgs) == 0 {
				// Provide a helpful error that the AI can learn from
				toolResult := ai.Message{
					Role:       "tool",
					Content:    "ERROR: create_workflow requires arguments. MUST include 'name' (string) and 'steps' (array). Example: {\"name\": \"my-workflow\", \"steps\": [{\"name\": \"step1\", \"tool\": \"some_tool\"}]}",
					ToolCallId: toolCall.ID,
				}
				toolResults = append(toolResults, toolResult)
				continue
			}
			if _, hasName := parsedArgs["name"]; !hasName {
				toolResult := ai.Message{
					Role:       "tool", 
					Content:    "ERROR: create_workflow missing required 'name' parameter. Must be a string.",
					ToolCallId: toolCall.ID,
				}
				toolResults = append(toolResults, toolResult)
				continue
			}
			if _, hasSteps := parsedArgs["steps"]; !hasSteps {
				toolResult := ai.Message{
					Role:       "tool",
					Content:    "ERROR: create_workflow missing required 'steps' parameter. Must be an array of step objects.",
					ToolCallId: toolCall.ID,
				}
				toolResults = append(toolResults, toolResult)
				continue
			}
		}
		
		// Try native function execution first, then fall back to MCP
		var result interface{}
		var err error
		
		// Check if this is a native function
		nativeFunctions := []string{"execute_bash", "bash", "run_command", "deploy_service", "scale_service", "restart_service", "get_logs", "get_metrics", "check_health", "edit_file", "editfile", "edit-file"}
		isNative := false
		for _, nativeFunc := range nativeFunctions {
			if toolCall.Function.Name == nativeFunc {
				isNative = true
				break
			}
		}
		
		if isNative {
			// Execute native function
			result, err = t.executeToolCall(toolCall, aiMsgIndex)
		} else {
			// Execute MCP tool
			toolCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			result, err = t.chat.GetMCPClient().CallTool(toolCtx, t.findServerForTool(toolCall.Function.Name), toolCall.Function.Name, parsedArgs)
			cancel()
		}
		
		var resultContent string
		if err != nil {
			resultContent = fmt.Sprintf("Error: %v", err)
		} else if result != nil {
			// Handle MCP tool result - convert to string
			resultContent = fmt.Sprintf("Result: %+v", result)
		} else {
			resultContent = "Function executed successfully"
		}
		
		// In silent mode, show function call result in UI
		if t.silentMode && GetUIProgram() != nil {
			// Enhanced formatting for file editing tools, Claude Code style for others
			var statusMsg string
			if err != nil {
				statusMsg = fmt.Sprintf("\x1b[31mx\x1b[0m \x1b[32m%s\x1b[0m \x1b[90mfailed |\x1b[0m \x1b[31m%v\x1b[0m", toolCall.Function.Name, err)
			} else {
				// Use enhanced formatting for file editing tools
				if t.isFileEditingTool(toolCall.Function.Name) && result != nil {
					duration := time.Millisecond * 50 // Placeholder duration
					statusMsg = t.chat.formatToolResult(toolCall.Function.Name, "native", result, duration)
				} else {
					// Use the enhanced Claude Code style formatting
					duration := time.Millisecond * 50
					statusMsg = t.chat.formatToolResult(toolCall.Function.Name, "native", result, duration)
				}
			}
			GetUIProgram().Send(aiStreamMsg{content: statusMsg})
		}
		
		// Add tool result to conversation
		toolResult := ai.Message{
			Role:       "tool",
			Content:    resultContent,
			ToolCallId: toolCall.ID,
		}
		toolResults = append(toolResults, toolResult)
	}
	
	// Add all tool results to message context
	for _, result := range toolResults {
		t.messages = append(t.messages, result)
	}
	
	// Clear pending tools since they're now executed
	t.pendingTools = make([]ai.ToolCall, 0)
	
	return nil
}

// Helper methods
func (t *ConversationTurn) executeToolCall(toolCall ai.ToolCall, aiMsgIndex int) (interface{}, error) {
	// Delegate to existing tool execution logic
	return t.chat.ExecuteNativeToolCall(toolCall, aiMsgIndex)
}

func (t *ConversationTurn) findServerForTool(toolName string) string {
	return t.chat.FindServerForTool(toolName)
}

func (t *ConversationTurn) parseArguments(arguments string) map[string]interface{} {
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
func (t *ConversationTurn) isFileEditingTool(functionName string) bool {
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
func (t *ConversationTurn) formatFunctionArgs(arguments, functionName string) string {
	if arguments == "" || arguments == "{}" {
		return ""
	}
	
	// Check if this is a file editing tool that should show full diffs
	isFileEditTool := t.isFileEditingTool(functionName)
	
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		// If not valid JSON, return truncated string unless it's a file edit tool
		cleaned := strings.ReplaceAll(arguments, "\n", " ")
		if !isFileEditTool && len(cleaned) > 120 {
			cleaned = cleaned[:117] + "..."
		}
		return cleaned
	}
	
	// Format key arguments nicely with color
	var parts []string
	for key, value := range args {
		switch v := value.(type) {
		case string:
			// For file edit tools, don't truncate content that might be diffs
			if !isFileEditTool && len(v) > 80 {
				v = v[:77] + "..."
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
	if !isFileEditTool && len(result) > 140 {
		result = result[:137] + "..."
	}
	return result
}

// shouldContinue determines if the conversation should continue automatically
func (t *ConversationTurn) shouldContinue() bool {
	if len(t.pendingTools) > 0 {
		return true // Always continue if we have pending tools
	}
	
	// Check if the last AI response indicates continuation is needed
	if len(t.messages) == 0 {
		return false
	}
	
	lastMessage := t.messages[len(t.messages)-1]
	// If the last message is a tool result, we should continue to let the AI present the results
	if lastMessage.Role == "tool" {
		return true
	}
	
	// Check if the AI response was cut off while generating tool calls (malformed function calls)
	if lastMessage.Role == "assistant" && t.detectMalformedToolCalls(lastMessage.Content) {
		return true
	}
	
	if lastMessage.Role != "assistant" {
		return false
	}
	
	content := strings.ToLower(lastMessage.Content)
	
	// Stop if there are repeated errors - indicates a stuck loop
	errorCount := 0
	for i := len(t.messages) - 1; i >= 0 && i >= len(t.messages)-3; i-- {
		if t.messages[i].Role == "assistant" && strings.Contains(strings.ToLower(t.messages[i].Content), "error") {
			errorCount++
		}
	}
	
	if errorCount >= 2 {
		return false // Stop if we've had multiple recent errors
	}
	
	// Check for tool call limit detection - key addition for auto-continuation
	if t.detectToolCallLimit() {
		return true // Auto-continue after hitting tool call limits
	}
	
	// Check for completion markers first - more comprehensive detection
	completionMarkers := []string{
		"complete", "finished", "done", "ready", "successfully created",
		"all set", "configured", "deployed successfully", "task completed",
		"workflow created", "setup complete", "problem resolved", "issue fixed",
		"working perfectly", "service is responding", "now properly connected",
		"configuration reloaded", "proxy reloaded successfully", "discovery refreshed",
		"everything is working", "issue resolved", "fixed the issue", "solution applied",
	}
	
	for _, marker := range completionMarkers {
		if strings.Contains(content, marker) {
			return false // Task appears complete
		}
	}
	
	// Patterns that indicate the AI should continue
	continuationIndicators := []string{
		"let me try", "let me create", "i'll create", "let me set up",
		"next, i will", "now i'll", "next step is to", "i'll now",
		"let me continue", "continuing with", "next, let me",
		"now let me", "i should also", "additionally, i will",
		"i need to", "let me check", "let me examine", "i'll examine",
		"let me investigate", "i'll investigate", "let me look",
		"let me search", "i'll search", "let me get", "i'll get",
		"let me find", "i'll find", "let me also", "i'll also",
		"let me now", "i'll now check", "i'll now get", "i'll now search",
		"let me get a comprehensive", "let me get a complete",
	}
	
	for _, indicator := range continuationIndicators {
		if strings.Contains(content, indicator) {
			return true
		}
	}
	
	// Check if the AI indicated it will do something next but didn't actually do it
	// This handles cases like "Let me get a comprehensive view..." but no tool call followed
	actionIndicators := []string{
		"let me get a comprehensive view", "let me get a complete view", 
		"let me search more", "let me check for", "let me look for",
		"i'll search for", "i'll check for", "i'll look for",
		"let me also search", "let me also check", "let me also look",
	}
	
	for _, indicator := range actionIndicators {
		if strings.Contains(content, indicator) {
			// Check if there were recent tool calls but AI didn't follow through on the stated action
			if t.hasRecentToolCalls() && !t.hasVeryRecentToolCalls() {
				return true // AI said it would do something but didn't
			}
		}
	}
	
	// Don't continue if we're just explaining or showing status
	if strings.Contains(content, "here's") || strings.Contains(content, "this shows") {
		return false
	}
	
	// Don't continue unless there's a clear indication the AI wants to continue
	// This prevents unnecessary empty responses
	return false
}

// detectToolCallLimit detects if the AI response was cut off due to tool call limits
func (t *ConversationTurn) detectToolCallLimit() bool {
	// Look at recent tool execution patterns to detect tool call limits
	toolCallCount := 0
	recentToolMessages := 0
	
	// Count tool calls and tool results in recent messages
	for i := len(t.messages) - 1; i >= 0 && recentToolMessages < 25; i-- {
		msg := t.messages[i]
		
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			toolCallCount += len(msg.ToolCalls)
		}
		
		if msg.Role == "tool" || (msg.Role == "assistant" && len(msg.ToolCalls) > 0) {
			recentToolMessages++
		}
		
		// If we encounter a user message, we've reached the start of this conversation turn
		if msg.Role == "user" {
			break
		}
	}
	
	// Detect tool call limit patterns:
	// 1. High number of recent tool calls (9+ suggests hitting limits, was 8+ but too aggressive)
	// 2. AI response ends abruptly without clear completion
	if toolCallCount >= 9 {
		lastMessage := t.messages[len(t.messages)-1]
		if lastMessage.Role == "assistant" {
			content := strings.ToLower(lastMessage.Content)
			
			// First check if this looks like completion despite tool calls
			completionIndicators := []string{
				"working perfectly", "problem resolved", "issue fixed", "successfully",
				"everything is working", "service is responding", "now connected",
				"reloaded successfully", "configuration updated", "discovery working",
			}
			
			for _, indicator := range completionIndicators {
				if strings.Contains(content, indicator) {
					return false // Don't continue if it seems complete
				}
			}
			
			// Signs the AI was cut off mid-investigation:
			// - Response ends with investigation language
			// - No clear completion or summary
			// - Appears to be in the middle of diagnostic work
			cutoffIndicators := []string{
				"let me check", "let me examine", "let me look", "let me try",
				"i'll check", "i'll examine", "i'll look", "i'll try",
				"checking", "examining", "looking", "investigating",
				"i need to", "now i need", "next i need", "i should",
				"now let me", "let me verify", "i'll verify",
			}
			
			for _, indicator := range cutoffIndicators {
				if strings.Contains(content, indicator) {
					return true
				}
			}
			
			// Also check if the response seems incomplete:
			// - Very short response after many tool calls
			// - Ends abruptly without conclusion
			if len(strings.TrimSpace(content)) < 50 && toolCallCount >= 10 {
				return true
			}
		}
	}
	
	// Special case: if the last message was a tool result and there's no AI response after,
	// and we had many tool calls, the AI might need to continue processing
	if len(t.messages) >= 2 {
		lastMessage := t.messages[len(t.messages)-1]
		if lastMessage.Role == "tool" && toolCallCount >= 9 {
			// But don't continue if the tool result suggests completion
			toolContent := strings.ToLower(lastMessage.Content)
			successIndicators := []string{
				"successfully", "working", "running", "connected", "reloaded",
				"discovery", "responding", "available", "ready",
			}
			
			hasSuccess := false
			for _, indicator := range successIndicators {
				if strings.Contains(toolContent, indicator) {
					hasSuccess = true
					break
				}
			}
			
			// Only continue if tool result doesn't indicate success
			if !hasSuccess {
				return true
			}
		}
	}
	
	return false
}

// detectMalformedToolCalls detects if the AI response contains malformed tool calls indicating cutoff
func (t *ConversationTurn) detectMalformedToolCalls(content string) bool {
	// Look for patterns that suggest malformed JSON or cut-off tool calls
	malformedPatterns := []string{
		`{"path": ".", "pattern": "glucose"}{"path":`, // Multiple calls concatenated
		`": ".","pattern"`, // Broken JSON structure
		`{"": ".",pattern":`, // Malformed key-value pairs
		`}{"path":`, // Multiple JSON objects without proper separation
		`","pattern":ulin`, // Cut-off arguments
		`"pattern": "blood{"`, // Broken nested JSON
	}
	
	contentLower := strings.ToLower(content)
	for _, pattern := range malformedPatterns {
		if strings.Contains(contentLower, strings.ToLower(pattern)) {
			return true
		}
	}
	
	// Also check for JSON syntax errors after function names
	if strings.Contains(contentLower, "search_files") || strings.Contains(contentLower, "execute_bash") {
		// Look for malformed JSON patterns
		if strings.Contains(content, `{"`) && strings.Contains(content, `}{"`) {
			// Multiple JSON objects smashed together
			return true
		}
		
		if strings.Count(content, `"`) % 2 != 0 {
			// Odd number of quotes suggests incomplete JSON
			return true
		}
		
		// Check for incomplete function arguments
		if strings.Contains(content, `": ".,"`) || strings.Contains(content, `"": ".,`) {
			return true
		}
	}
	
	return false
}

// hasRecentToolCalls checks if there were any tool calls in recent conversation
func (t *ConversationTurn) hasRecentToolCalls() bool {
	for i := len(t.messages) - 1; i >= 0 && i >= len(t.messages)-10; i-- {
		msg := t.messages[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			return true
		}
		if msg.Role == "tool" {
			return true
		}
		// Stop at user message (start of this turn)
		if msg.Role == "user" {
			break
		}
	}
	return false
}

// hasVeryRecentToolCalls checks if there were tool calls in the last 2 messages
func (t *ConversationTurn) hasVeryRecentToolCalls() bool {
	if len(t.messages) < 2 {
		return false
	}
	
	for i := len(t.messages) - 1; i >= len(t.messages) - 2; i-- {
		msg := t.messages[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			return true
		}
		if msg.Role == "tool" {
			return true
		}
	}
	return false
}