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
		maxTurns:     10, // Prevent infinite loops
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
		maxTurns:     10, // Prevent infinite loops
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
		if !t.shouldContinue() {
			t.completed = true
			break
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
		fmt.Printf("\n\033[1;32m✓ AI\033[0m \033[90m%s\033[0m", time.Now().Format("15:04:05"))
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
		if t.silentMode && GetUIProgram() != nil {
			// Show function call start - Claude Code style with color
			funcCallMsg := fmt.Sprintf("\x1b[90m→\x1b[0m \x1b[32m%s\x1b[0m", toolCall.Function.Name)
			if formattedArgs := t.formatFunctionArgs(toolCall.Function.Arguments); formattedArgs != "" {
				funcCallMsg += fmt.Sprintf(" %s", formattedArgs)
			}
			GetUIProgram().Send(aiStreamMsg{content: funcCallMsg})
		}
		
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
			// Claude Code style - clean status line only with color
			var statusMsg string
			if err != nil {
				statusMsg = fmt.Sprintf("\x1b[31m✗\x1b[0m \x1b[32m%s\x1b[0m \x1b[90mfailed |\x1b[0m \x1b[31m%v\x1b[0m", toolCall.Function.Name, err)
			} else {
				statusMsg = fmt.Sprintf("\x1b[32m✓\x1b[0m \x1b[32m%s\x1b[0m \x1b[90mcompleted\x1b[0m", toolCall.Function.Name)
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

// formatFunctionArgs formats function arguments in a human-readable way with color
func (t *ConversationTurn) formatFunctionArgs(arguments string) string {
	if arguments == "" || arguments == "{}" {
		return ""
	}
	
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		// If not valid JSON, just return truncated string
		cleaned := strings.ReplaceAll(arguments, "\n", " ")
		if len(cleaned) > 120 {
			cleaned = cleaned[:117] + "..."
		}
		return cleaned
	}
	
	// Format key arguments nicely with color
	var parts []string
	for key, value := range args {
		switch v := value.(type) {
		case string:
			if len(v) > 80 {
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
	if len(result) > 140 {
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
	
	// Check for completion markers first
	completionMarkers := []string{
		"complete", "finished", "done", "ready", "successfully created",
		"all set", "configured", "deployed successfully", "task completed",
		"workflow created", "setup complete",
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
	}
	
	for _, indicator := range continuationIndicators {
		if strings.Contains(content, indicator) {
			return true
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