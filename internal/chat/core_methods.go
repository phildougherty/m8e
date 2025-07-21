package chat

import (
	"fmt"
	"strings"
)

// processInput handles user input
func (tc *TermChat) processInput(input string) {
	// Handle commands
	if strings.HasPrefix(input, "/") {
		if tc.handleCommand(input) {
			return // Command processed successfully, including exit commands
		}
		return // Command processed (even if not found)
	}

	// Add user message and print it
	tc.addMessage("user", input)
	tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])

	// Reset turn counter for new user input
	tc.currentTurns = 0

	// Use conversation flow architecture
	tc.executeConversationFlow(input)
}

// executeConversationFlow implements the conversation flow
func (tc *TermChat) executeConversationFlow(userInput string) {
	// Simple AI chat implementation
	tc.chatWithAI(userInput)
}

// printMessage prints a single message with proper markdown rendering
func (tc *TermChat) printMessage(msg TermChatMessage) {
	// Skip terminal output if UI program is active to prevent duplication
	if GetUIProgram() != nil {
		return
	}
	
	timestamp := msg.Timestamp.Format("15:04:05")
	
	switch msg.Role {
	case "user":
		fmt.Printf("\n\033[1;34m❯ You\033[0m \033[90m%s\033[0m\n%s\n", timestamp, msg.Content)
	case "assistant":
		// For AI messages, always render markdown and hide raw markdown from user
		fmt.Printf("\n\033[1;32mAI\033[0m \033[90m%s\033[0m\n%s\n", timestamp, tc.renderMarkdown(msg.Content))
	case "system":
		// For system messages, always render markdown  
		fmt.Printf("\n\033[1;33m● System\033[0m \033[90m%s\033[0m\n%s\n", timestamp, tc.renderMarkdown(msg.Content))
	}
}

// chatWithAI sends a message to AI and handles the response
func (tc *TermChat) chatWithAI(message string) {
	// Use the turn-based conversation system
	tc.executeConversationFlowSilent(message)
}



// handleCommand processes slash commands
func (tc *TermChat) handleCommand(command string) bool {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return false
	}
	
	baseCommand := parts[0]
	
	switch baseCommand {
	case "/help":
		tc.showHelp()
		return true
	case "/status":
		tc.showStatus()
		return true
	case "/auto":
		tc.approvalMode = AUTO_EDIT
		tc.addMessage("system", "AUTO-EDIT MODE ACTIVATED - Smart automation enabled")
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
		return true
	case "/manual":
		tc.approvalMode = DEFAULT
		tc.addMessage("system", "MANUAL MODE ACTIVATED - Will ask for confirmation")
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
		return true
	case "/yolo":
		tc.approvalMode = YOLO
		tc.addMessage("system", "YOLO MODE ACTIVATED - Maximum autonomy enabled")
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
		return true
	case "/clear":
		tc.chatHistory = []TermChatMessage{}
		fmt.Println("\033[2J\033[H") // Clear screen
		tc.addMessage("system", "Chat history cleared")
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
		return true
	case "/exit", "/quit":
		return true
	case "/provider":
		if len(parts) > 1 {
			if err := tc.switchProviderSilent(parts[1]); err != nil {
				tc.addMessage("system", "Failed to switch provider: "+err.Error())
			} else {
				tc.addMessage("system", "Switched to provider: "+parts[1])
			}
			tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
		}
		return true
	case "/model":
		if len(parts) > 1 {
			if err := tc.switchModelSilent(parts[1]); err != nil {
				tc.addMessage("system", "Failed to switch model: "+err.Error())
			} else {
				tc.addMessage("system", "Switched to model: "+parts[1])
			}
			tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
		}
		return true
	default:
		tc.addMessage("system", "Unknown command: "+baseCommand+". Type /help for available commands.")
		tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
		return true
	}
}

// showHelp displays help information
func (tc *TermChat) showHelp() {
	helpText := `# Matey AI Chat Help

## Available Commands:
• /auto     - Enable auto-edit mode (smart automation)
• /manual   - Enable manual mode (confirm all actions)
• /yolo     - Enable YOLO mode (maximum autonomy)
• /status   - Show system status
• /clear    - Clear chat history
• /help     - Show this help message
• /exit     - Exit chat

## AI Provider Commands:
• /provider <name> - Switch AI provider (ollama, openai, claude, openrouter)
• /model <name>    - Switch AI model

## Keyboard Shortcuts (Enhanced UI):
• ESC       - Cancel current operation
• Ctrl+C    - Exit chat
• Ctrl+L    - Clear screen
• Ctrl+Y    - Toggle YOLO/MANUAL mode
• Tab       - Toggle AUTO-EDIT/MANUAL mode
• Ctrl+R    - Toggle verbose/compact output
• Ctrl+V    - Paste from clipboard
• Alt+V     - Toggle voice mode
• Ctrl+T    - Voice recording
• Ctrl+N    - Skip to next TTS response
• Ctrl+I    - Interrupt TTS playback
• Ctrl+Q    - Show TTS queue status

Ready to orchestrate your Kubernetes infrastructure!`

	tc.addMessage("system", helpText)
	tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
}

// showStatus displays current system status
func (tc *TermChat) showStatus() {
	outputMode := "Compact"
	if tc.verboseMode {
		outputMode = "Verbose"
	}

	statusText := fmt.Sprintf(`# System Status

**Provider:** %s
**Model:** %s
**Messages:** %d
**Approval Mode:** %s - %s
**Output Mode:** %s

**Available Modes:**
• MANUAL - Ask for confirmation before actions
• AUTO - Auto-approve safe operations
• YOLO - Auto-approve everything (use with caution)

System ready for infrastructure orchestration!`,
		tc.currentProvider,
		tc.currentModel,
		len(tc.chatHistory),
		tc.approvalMode.GetModeIndicatorNoEmoji(),
		tc.approvalMode.Description(),
		outputMode)

	tc.addMessage("system", statusText)
	tc.printMessage(tc.chatHistory[len(tc.chatHistory)-1])
}