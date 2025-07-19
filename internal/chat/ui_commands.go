package chat

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// handleSlashCommand handles slash commands like /auto, /status, etc.
func (m *ChatUI) handleSlashCommand(command string) tea.Cmd {
	return func() tea.Msg {
		// Add command to viewport with enhanced styling
		timestamp := time.Now().Format("15:04:05")
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("💻 Command", timestamp))
		m.viewport = append(m.viewport, "│ "+m.createHighlightText(command))
		m.viewport = append(m.viewport, m.createBoxFooter())
		m.viewport = append(m.viewport, "")
		
		// Parse the command to get the base command and arguments
		parts := strings.Fields(command)
		if len(parts) == 0 {
			return nil
		}
		
		baseCommand := parts[0]
		
		switch baseCommand {
		case "/auto":
			return m.handleAutoCommand(timestamp)
		case "/manual":
			return m.handleManualCommand(timestamp)
		case "/yolo":
			return m.handleYoloCommand(timestamp)
		case "/clear":
			return m.handleClearCommand(timestamp)
		case "/status":
			return m.handleStatusCommand(timestamp)
		case "/help":
			return m.handleHelpCommand(timestamp)
		case "/exit", "/quit":
			return tea.Quit
		case "/provider":
			return m.handleProviderCommand(parts, timestamp)
		case "/providers":
			return m.handleProvidersCommand(timestamp)
		case "/model":
			return m.handleModelCommand(parts, timestamp)
		case "/models":
			return m.handleModelsCommand(timestamp)
		default:
			return m.handleUnknownCommand(baseCommand, timestamp)
		}
	}
}

// handleAutoCommand handles the /auto command
func (m *ChatUI) handleAutoCommand(timestamp string) tea.Msg {
	m.termChat.approvalMode = AUTO_EDIT
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("⚙️ System", timestamp))
	m.viewport = append(m.viewport, m.createSuccessMessage("AUTO-EDIT MODE ACTIVATED ⚡"))
	m.viewport = append(m.viewport, m.createInfoMessage("Smart automation enabled - I'll auto-approve safe operations"))
	m.viewport = append(m.viewport, m.createInfoMessage("Ready to orchestrate your Kubernetes infrastructure!"))
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	return nil
}

// handleManualCommand handles the /manual command
func (m *ChatUI) handleManualCommand(timestamp string) tea.Msg {
	m.termChat.approvalMode = DEFAULT
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("⚙️ System", timestamp))
	m.viewport = append(m.viewport, m.createSuccessMessage("MANUAL MODE ACTIVATED 🔒"))
	m.viewport = append(m.viewport, m.createInfoMessage("I'll ask for confirmation before executing any actions"))
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	return nil
}

// handleYoloCommand handles the /yolo command
func (m *ChatUI) handleYoloCommand(timestamp string) tea.Msg {
	m.termChat.approvalMode = YOLO
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("⚙️ System", timestamp))
	m.viewport = append(m.viewport, m.createSuccessMessage("YOLO MODE ACTIVATED 🔥"))
	m.viewport = append(m.viewport, m.createInfoMessage("Maximum autonomy enabled - I'll execute all actions immediately"))
	m.viewport = append(m.viewport, m.createWarningMessage("Use with caution in production environments"))
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	return nil
}

// handleClearCommand handles the /clear command
func (m *ChatUI) handleClearCommand(timestamp string) tea.Msg {
	m.viewport = []string{}
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("⚙️ System", timestamp))
	m.viewport = append(m.viewport, m.createSuccessMessage("Chat history cleared 🧹"))
	m.viewport = append(m.viewport, m.createInfoMessage("Ready for a fresh start!"))
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	return nil
}

// handleStatusCommand handles the /status command
func (m *ChatUI) handleStatusCommand(timestamp string) tea.Msg {
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("📊 System Status", timestamp))
	
	// Enhanced status with colorful formatting
	m.viewport = append(m.viewport, m.createEmphasizedText("│ 🎯 Current Status:"))
	m.viewport = append(m.viewport, fmt.Sprintf("│   %s %s", 
		m.createHighlightText("🔗 Provider:"), m.termChat.currentProvider))
	m.viewport = append(m.viewport, fmt.Sprintf("│   %s %s", 
		m.createHighlightText("🤖 Model:"), m.termChat.currentModel))
	m.viewport = append(m.viewport, fmt.Sprintf("│   %s %d", 
		m.createHighlightText("💬 Messages:"), len(m.termChat.chatHistory)))
	m.viewport = append(m.viewport, fmt.Sprintf("│   %s %s - %s", 
		m.createHighlightText("🎮 Approval Mode:"), 
		m.termChat.approvalMode.GetModeIndicatorNoEmoji(), 
		m.termChat.approvalMode.Description()))
	
	outputMode := "Compact"
	if m.termChat.verboseMode {
		outputMode = "Verbose"
	}
	m.viewport = append(m.viewport, fmt.Sprintf("│   %s %s", 
		m.createHighlightText("📋 Output Mode:"), outputMode))
	
	m.viewport = append(m.viewport, "│")
	m.viewport = append(m.viewport, m.createEmphasizedText("│ ⌨️  Keyboard Shortcuts:"))
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("ESC")+" - Cancel operation, return to input")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("Ctrl+C")+" - Exit chat")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("Ctrl+L")+" - Clear screen")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("Ctrl+Y")+" - Toggle YOLO/MANUAL mode")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("Tab")+" - Toggle AUTO-EDIT/MANUAL mode")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("Ctrl+R")+" - Toggle verbose/compact output")
	
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	return nil
}

// handleHelpCommand handles the /help command
func (m *ChatUI) handleHelpCommand(timestamp string) tea.Msg {
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("📚 Help & Commands", timestamp))
	
	m.viewport = append(m.viewport, m.createEmphasizedText("│ 🚀 Available Commands:"))
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("/auto")+"     - Enable auto-edit mode (smart automation)")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("/manual")+"   - Enable manual mode (confirm all actions)")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("/yolo")+"     - Enable YOLO mode (maximum autonomy)")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("/status")+"   - Show system status and health")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("/clear")+"    - Clear chat history")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("/help")+"     - Show this help message")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("/exit")+"     - Exit chat")
	m.viewport = append(m.viewport, "│")
	
	m.viewport = append(m.viewport, m.createEmphasizedText("│ ⌨️  Keyboard Shortcuts:"))
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("ESC")+"       - Cancel current operation")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("Ctrl+C")+"    - Exit chat")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("Ctrl+L")+"    - Clear screen")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("Ctrl+Y")+"    - Toggle YOLO/MANUAL mode")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("Tab")+"       - Toggle AUTO-EDIT/MANUAL mode")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("Ctrl+R")+"    - Toggle verbose/compact output")
	m.viewport = append(m.viewport, "│")
	
	m.viewport = append(m.viewport, m.createEmphasizedText("│ 🤖 AI Provider Commands:"))
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("/provider <name>")+" - Switch AI provider")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("/providers")+"       - List available providers")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("/model <name>")+"    - Switch AI model")
	m.viewport = append(m.viewport, "│   "+m.createHighlightText("/models")+"          - List available models")
	m.viewport = append(m.viewport, "│")
	
	m.viewport = append(m.viewport, m.createEmphasizedText("│ 💡 Pro Tips:"))
	m.viewport = append(m.viewport, "│   • Use AUTO mode for safe operations like status checks")
	m.viewport = append(m.viewport, "│   • Use YOLO mode when you want maximum speed and trust the AI")
	m.viewport = append(m.viewport, "│   • Try asking: 'Deploy a microservice' or 'Show cluster health'")
	
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	return nil
}

// handleProviderCommand handles the /provider command
func (m *ChatUI) handleProviderCommand(parts []string, timestamp string) tea.Msg {
	if len(parts) > 1 {
		// Switch provider using silent method to avoid terminal output
		if err := m.termChat.switchProviderSilent(parts[1]); err != nil {
			m.viewport = append(m.viewport, m.createEnhancedBoxHeader("❌ Error", timestamp))
			m.viewport = append(m.viewport, m.createErrorMessage("Failed to switch provider: "+err.Error()))
			m.viewport = append(m.viewport, m.createBoxFooter())
		} else {
			m.viewport = append(m.viewport, m.createEnhancedBoxHeader("⚙️ System", timestamp))
			m.viewport = append(m.viewport, m.createSuccessMessage("Switched to provider: "+parts[1]))
			m.viewport = append(m.viewport, m.createInfoMessage("Current model: "+m.termChat.currentModel))
			m.viewport = append(m.viewport, m.createBoxFooter())
		}
	} else {
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("❌ Error", timestamp))
		m.viewport = append(m.viewport, m.createErrorMessage("Usage: /provider <name>"))
		m.viewport = append(m.viewport, m.createInfoMessage("Available: ollama, openai, claude, openrouter"))
		m.viewport = append(m.viewport, m.createBoxFooter())
	}
	m.viewport = append(m.viewport, "")
	return nil
}

// handleProvidersCommand handles the /providers command
func (m *ChatUI) handleProvidersCommand(timestamp string) tea.Msg {
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("🤖 Available Providers", timestamp))
	m.viewport = append(m.viewport, m.createEmphasizedText("│ 🚀 AI Providers:"))
	m.viewport = append(m.viewport, "│   • "+m.createHighlightText("openrouter")+" - Claude Sonnet 4 and premium models")
	m.viewport = append(m.viewport, "│   • "+m.createHighlightText("ollama")+"     - Local models (llama3, qwen3, etc.)")
	m.viewport = append(m.viewport, "│   • "+m.createHighlightText("claude")+"     - Direct Anthropic API")
	m.viewport = append(m.viewport, "│   • "+m.createHighlightText("openai")+"     - GPT-4 and OpenAI models")
	m.viewport = append(m.viewport, "│")
	m.viewport = append(m.viewport, "│ "+m.createEmphasizedText("Current provider: ")+m.createHighlightText(m.termChat.currentProvider))
	m.viewport = append(m.viewport, "│")
	m.viewport = append(m.viewport, m.createInfoMessage("Use /provider <name> to switch providers"))
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	return nil
}

// handleModelCommand handles the /model command
func (m *ChatUI) handleModelCommand(parts []string, timestamp string) tea.Msg {
	if len(parts) > 1 {
		// Switch model using silent method to avoid terminal output
		if err := m.termChat.switchModelSilent(parts[1]); err != nil {
			m.viewport = append(m.viewport, m.createEnhancedBoxHeader("❌ Error", timestamp))
			m.viewport = append(m.viewport, m.createErrorMessage("Failed to switch model: "+err.Error()))
			m.viewport = append(m.viewport, m.createBoxFooter())
		} else {
			m.viewport = append(m.viewport, m.createEnhancedBoxHeader("⚙️ System", timestamp))
			m.viewport = append(m.viewport, m.createSuccessMessage("Switched to model: "+parts[1]))
			m.viewport = append(m.viewport, m.createInfoMessage("Provider: "+m.termChat.currentProvider))
			m.viewport = append(m.viewport, m.createBoxFooter())
		}
	} else {
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("❌ Error", timestamp))
		m.viewport = append(m.viewport, m.createErrorMessage("Usage: /model <name>"))
		m.viewport = append(m.viewport, m.createBoxFooter())
	}
	m.viewport = append(m.viewport, "")
	return nil
}

// handleModelsCommand handles the /models command
func (m *ChatUI) handleModelsCommand(timestamp string) tea.Msg {
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("🎯 Available Models", timestamp))
	m.viewport = append(m.viewport, m.createEmphasizedText("│ 🤖 Available Models:"))
	
	switch m.termChat.currentProvider {
	case "openrouter":
		m.viewport = append(m.viewport, "│   • "+m.createHighlightText("anthropic/claude-sonnet-4")+" - Latest Claude Sonnet")
		m.viewport = append(m.viewport, "│   • "+m.createHighlightText("anthropic/claude-3.5-sonnet")+" - Claude 3.5 Sonnet")
		m.viewport = append(m.viewport, "│   • "+m.createHighlightText("openai/gpt-4")+" - GPT-4")
		m.viewport = append(m.viewport, "│   • "+m.createHighlightText("meta-llama/llama-3.1-70b-instruct")+" - Llama 3.1 70B")
	case "ollama":
		m.viewport = append(m.viewport, "│   • "+m.createHighlightText("llama3")+" - Llama 3 8B")
		m.viewport = append(m.viewport, "│   • "+m.createHighlightText("qwen3")+" - Qwen 3")
		m.viewport = append(m.viewport, "│   • "+m.createHighlightText("mistral")+" - Mistral 7B")
		m.viewport = append(m.viewport, "│   • "+m.createHighlightText("codellama")+" - Code Llama")
	case "claude":
		m.viewport = append(m.viewport, "│   • "+m.createHighlightText("claude-3-sonnet-20240229")+" - Claude 3 Sonnet")
		m.viewport = append(m.viewport, "│   • "+m.createHighlightText("claude-3-haiku-20240307")+" - Claude 3 Haiku")
	case "openai":
		m.viewport = append(m.viewport, "│   • "+m.createHighlightText("gpt-4")+" - GPT-4")
		m.viewport = append(m.viewport, "│   • "+m.createHighlightText("gpt-4-turbo")+" - GPT-4 Turbo")
		m.viewport = append(m.viewport, "│   • "+m.createHighlightText("gpt-3.5-turbo")+" - GPT-3.5 Turbo")
	default:
		m.viewport = append(m.viewport, m.createErrorMessage("Unknown provider. Switch to a known provider first."))
	}
	
	m.viewport = append(m.viewport, "│")
	m.viewport = append(m.viewport, "│ "+m.createEmphasizedText("Current model: ")+m.createHighlightText(m.termChat.currentModel))
	m.viewport = append(m.viewport, "│")
	m.viewport = append(m.viewport, m.createInfoMessage("Use /model <name> to switch models"))
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	return nil
}

// handleUnknownCommand handles unknown commands
func (m *ChatUI) handleUnknownCommand(command, timestamp string) tea.Msg {
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("❌ Error", timestamp))
	m.viewport = append(m.viewport, m.createErrorMessage("Unknown command: "+command))
	m.viewport = append(m.viewport, m.createInfoMessage("Type /help for available commands"))
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	return nil
}