package chat

import (
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

// ExecuteNativeToolCall executes a native tool call (stub for compatibility)
func (tc *TermChat) ExecuteNativeToolCall(toolCall interface{}, index int) {
	// This is a stub - the actual implementation would be more complex
}

// FindServerForTool finds the appropriate server for a tool
func (tc *TermChat) FindServerForTool(toolName string) string {
	// This is a stub - the actual implementation would query MCP servers
	return "default-server"
}