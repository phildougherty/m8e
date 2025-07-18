package cmd

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
			Content: t.chat.getSystemContext(),
		},
	}
	
	// Add recent chat history (last 10 messages to keep context manageable)
	historyLimit := 10
	startIdx := len(t.chat.chatHistory) - historyLimit
	if startIdx < 0 {
		startIdx = 0
	}
	
	for i := startIdx; i < len(t.chat.chatHistory); i++ {
		msg := t.chat.chatHistory[i]
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
		fmt.Printf("\nReached maximum turns (%d) for this request. Task may need to be broken down into smaller parts.\n", t.maxTurns)
	}
	
	return nil
}

// executeConversationRound executes one round of AI conversation
func (t *ConversationTurn) executeConversationRound(ctx context.Context) error {
	// Create AI message placeholder
	t.chat.addMessage("assistant", "")
	aiMsgIndex := len(t.chat.chatHistory) - 1
	
	fmt.Printf("\n\033[1;32m✓ AI\033[0m \033[90m%s\033[0m", time.Now().Format("15:04:05"))
	if t.currentTurn > 1 {
		fmt.Printf(" \033[90m(turn %d)\033[0m", t.currentTurn)
	}
	fmt.Println()
	
	// Get MCP functions
	mcpFunctions := t.chat.getMCPFunctions()
	
	// Stream the AI response
	streamCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	
	stream, err := t.chat.aiManager.StreamChatWithFallback(streamCtx, t.messages, ai.StreamOptions{
		Model:     t.chat.currentModel,
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
			t.chat.chatHistory[aiMsgIndex].Content += fmt.Sprintf("\nError: %v", response.Error)
			break
		}
		
		// Process content
		if response.Content != "" {
			regularContent := response.Content
			if strings.Contains(response.Content, "<think>") || strings.Contains(response.Content, "</think>") {
				regularContent, _ = ai.ProcessStreamContentWithThinks(thinkProcessor, response.Content)
			}
			
			t.chat.chatHistory[aiMsgIndex].Content += regularContent
			
			if !t.chat.verboseMode {
				fmt.Print(regularContent)
			}
		}
		
		// Collect tool calls
		if len(response.ToolCalls) > 0 {
			for _, toolCall := range response.ToolCalls {
				callId := toolCall.ID
				if callId == "" {
					callId = fmt.Sprintf("call_%d", len(accumulatedFunctionCalls))
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
		Content:   t.chat.chatHistory[aiMsgIndex].Content,
		ToolCalls: t.pendingTools,
	}
	t.messages = append(t.messages, currentAIMessage)
	
	return nil
}

// executeToolsAndContinue executes pending tools and adds results to conversation
func (t *ConversationTurn) executeToolsAndContinue(ctx context.Context) error {
	aiMsgIndex := len(t.chat.chatHistory) - 1
	var toolResults []ai.Message
	
	// Execute all pending tools
	for _, toolCall := range t.pendingTools {
		if toolCall.Type != "function" {
			continue
		}
		
		// Check approval mode for this specific tool
		if t.chat.approvalMode.ShouldConfirm(toolCall.Function.Name) {
			confirmed := t.chat.requestFunctionConfirmation(toolCall.Function.Name, toolCall.Function.Arguments)
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
		
		// Execute the tool
		t.executeToolCall(toolCall, aiMsgIndex)
		
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
		
		// Get the tool result for conversation context
		toolCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		result, err := t.chat.mcpClient.CallTool(toolCtx, t.findServerForTool(toolCall.Function.Name), toolCall.Function.Name, parsedArgs)
		cancel()
		
		var resultContent string
		if err != nil {
			resultContent = fmt.Sprintf("Error: %v", err)
		} else if result != nil {
			// Handle MCP tool result - convert to string
			resultContent = fmt.Sprintf("Result: %+v", result)
		} else {
			resultContent = "Function executed successfully"
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
func (t *ConversationTurn) executeToolCall(toolCall ai.ToolCall, aiMsgIndex int) {
	// Delegate to existing tool execution logic
	t.chat.executeNativeToolCall(toolCall, aiMsgIndex)
}

func (t *ConversationTurn) findServerForTool(toolName string) string {
	return t.chat.findServerForTool(toolName)
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
	
	// If response is substantial but doesn't have clear completion, continue cautiously
	return len(content) > 50 && t.currentTurn < 3
}