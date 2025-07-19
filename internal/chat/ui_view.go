package chat

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// View renders the chat UI
func (m *ChatUI) View() string {
	if !m.ready {
		return m.createLoadingView()
	}

	// Calculate dimensions
	statusHeight := 1
	inputHeight := 3 // border + padding
	viewportHeight := m.height - statusHeight - inputHeight

	// Render viewport (chat history)
	viewport := m.renderViewport(viewportHeight)
	
	// Render input area
	inputArea := m.renderInputArea()

	// Render status line
	statusArea := m.renderStatusLine()

	// Combine all parts
	content := strings.Join(viewport, "\n")
	content += "\n" + statusArea
	content += "\n" + inputArea

	return content
}

// createLoadingView creates a loading screen
func (m *ChatUI) createLoadingView() string {
	style := lipgloss.NewStyle().
		Foreground(Yellow).
		Bold(true).
		Align(lipgloss.Center).
		Margin(1)
	
	return style.Render("🚀 Loading Matey AI Chat...")
}

// renderViewport renders the chat history viewport
func (m *ChatUI) renderViewport(viewportHeight int) []string {
	viewport := make([]string, viewportHeight)
	startIdx := len(m.viewport) - viewportHeight
	if startIdx < 0 {
		startIdx = 0
	}

	for i := 0; i < viewportHeight; i++ {
		if startIdx+i < len(m.viewport) {
			viewport[i] = m.viewport[startIdx+i]
		} else {
			viewport[i] = ""
		}
	}
	
	// Add enhanced spinner if loading
	if m.loading && viewportHeight > 0 {
		spinnerChars := []string{"⣾", "⣽", "⣻", "⢿", "⢿", "⣻", "⣽", "⣾"}
		spinner := spinnerChars[m.spinnerFrame%len(spinnerChars)]
		spinnerStyle := lipgloss.NewStyle().Foreground(Yellow).Bold(true)
		processingStyle := lipgloss.NewStyle().Foreground(LightGreen)
		
		processingText := "AI is orchestrating your infrastructure..."
		viewport[viewportHeight-1] = fmt.Sprintf("  %s %s", 
			spinnerStyle.Render(spinner), 
			processingStyle.Render(processingText))
	}

	return viewport
}

// renderInputArea renders the input field
func (m *ChatUI) renderInputArea() string {
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(Brown).
		Padding(0, 1)

	promptStyle := lipgloss.NewStyle().
		Foreground(Yellow).
		Bold(true)

	// Create enhanced prompt based on mode
	var promptText string
	switch m.termChat.approvalMode {
	case YOLO:
		promptText = "🔥❯ "
	case AUTO_EDIT:
		promptText = "⚡❯ "
	default:
		promptText = "🔒❯ "
	}

	inputPrompt := promptStyle.Render(promptText)
	inputText := m.input
	
	// Add enhanced cursor
	if m.cursor < len(inputText) {
		cursorStyle := lipgloss.NewStyle().
			Foreground(Yellow).
			Background(Brown).
			Bold(true)
		inputText = inputText[:m.cursor] + cursorStyle.Render("│") + inputText[m.cursor:]
	} else {
		cursorStyle := lipgloss.NewStyle().
			Foreground(Yellow).
			Bold(true)
		inputText += cursorStyle.Render("│")
	}

	return inputStyle.Render(inputPrompt + inputText)
}

// renderStatusLine renders the status bar
func (m *ChatUI) renderStatusLine() string {
	statusStyle := lipgloss.NewStyle().
		Foreground(GoldYellow).
		Background(ArmyGreen).
		Padding(0, 1).
		Width(m.width)

	return statusStyle.Render(m.statusLine)
}

// createBoxHeader creates a dynamic width box header with enhanced color
func (m *ChatUI) createBoxHeader(title, timestamp string) string {
	width := 140
	if m.width > 0 {
		width = m.width - 2 // Leave 2 characters for margins
	}
	
	// Enhanced color styles for different message types
	var headerStyle lipgloss.Style
	switch title {
	case "You", "👤 You":
		headerStyle = lipgloss.NewStyle().Foreground(Brown).Bold(true)
	case "AI", "🤖 AI Assistant":
		headerStyle = lipgloss.NewStyle().Foreground(LightGreen).Bold(true)
	case "System", "⚙️ System":
		headerStyle = lipgloss.NewStyle().Foreground(ArmyGreen).Bold(true)
	case "Command", "💻 Command":
		headerStyle = lipgloss.NewStyle().Foreground(GoldYellow).Bold(true)
	case "Function Call Confirmation", "🔐 Function Call Confirmation":
		headerStyle = lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	case "Error", "❌ Error":
		headerStyle = lipgloss.NewStyle().Foreground(Red).Bold(true)
	case "Help", "Providers", "Models", "📚 Help":
		headerStyle = lipgloss.NewStyle().Foreground(Tan).Bold(true)
	default:
		headerStyle = lipgloss.NewStyle().Foreground(Brown).Bold(true)
	}
	
	header := "┌─ " + title
	if timestamp != "" {
		header += " " + timestamp
	}
	header += " "
	padding := width - len(header) - 1
	if padding > 0 {
		header += strings.Repeat("─", padding)
	}
	header += "┐"
	
	return headerStyle.Render(header)
}

// createBoxFooter creates a dynamic width box footer with enhanced color
func (m *ChatUI) createBoxFooter() string {
	width := 140
	if m.width > 0 {
		width = m.width - 2 // Leave 2 characters for margins
	}
	
	footer := "└" + strings.Repeat("─", width-2) + "┘"
	footerStyle := lipgloss.NewStyle().Foreground(Brown)
	return footerStyle.Render(footer)
}

// createSuccessMessage creates an enhanced success message
func (m *ChatUI) createSuccessMessage(message string) string {
	successStyle := lipgloss.NewStyle().Foreground(LightGreen).Bold(true)
	iconStyle := lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	return "│ " + iconStyle.Render("✓") + " " + successStyle.Render(message)
}

// createErrorMessage creates an enhanced error message  
func (m *ChatUI) createErrorMessage(message string) string {
	errorStyle := lipgloss.NewStyle().Foreground(Red).Bold(true)
	iconStyle := lipgloss.NewStyle().Foreground(Red).Bold(true)
	return "│ " + iconStyle.Render("✗") + " " + errorStyle.Render(message)
}

// createInfoMessage creates an enhanced info message
func (m *ChatUI) createInfoMessage(message string) string {
	infoStyle := lipgloss.NewStyle().Foreground(GoldYellow)
	iconStyle := lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	return "│ " + iconStyle.Render("•") + " " + infoStyle.Render(message)
}

// createWarningMessage creates an enhanced warning message
func (m *ChatUI) createWarningMessage(message string) string {
	warningStyle := lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	iconStyle := lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	return "│ " + iconStyle.Render("⚠") + " " + warningStyle.Render(message)
}

// Enhanced styling for different UI elements
func (m *ChatUI) createHighlightText(text string) string {
	style := lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	return style.Render(text)
}

func (m *ChatUI) createSubtleText(text string) string {
	style := lipgloss.NewStyle().Foreground(Tan)
	return style.Render(text)
}

func (m *ChatUI) createEmphasizedText(text string) string {
	style := lipgloss.NewStyle().Foreground(LightGreen).Bold(true)
	return style.Render(text)
}

// createModeStatusIndicator creates a visual indicator for the current mode
func (m *ChatUI) createModeStatusIndicator() string {
	mode := m.termChat.approvalMode
	var modeStyle lipgloss.Style
	var icon, text string
	
	switch mode {
	case YOLO:
		modeStyle = lipgloss.NewStyle().Foreground(Red).Bold(true)
		icon = "🔥"
		text = "YOLO"
	case AUTO_EDIT:
		modeStyle = lipgloss.NewStyle().Foreground(Yellow).Bold(true)
		icon = "⚡"
		text = "AUTO"
	default:
		modeStyle = lipgloss.NewStyle().Foreground(LightGreen).Bold(true)
		icon = "🔒"
		text = "MANUAL"
	}
	
	return fmt.Sprintf("%s %s", icon, modeStyle.Render(text))
}

// createProviderStatusIndicator creates a visual indicator for the current AI provider
func (m *ChatUI) createProviderStatusIndicator() string {
	providerStyle := lipgloss.NewStyle().Foreground(GoldYellow).Bold(true)
	modelStyle := lipgloss.NewStyle().Foreground(Tan)
	
	return fmt.Sprintf("🤖 %s (%s)", 
		providerStyle.Render(m.termChat.currentProvider),
		modelStyle.Render(m.termChat.currentModel))
}

// createTimeIndicator creates a time indicator
func (m *ChatUI) createTimeIndicator() string {
	timeStyle := lipgloss.NewStyle().Foreground(ArmyGreen)
	return fmt.Sprintf("🕒 %s", timeStyle.Render(time.Now().Format("15:04:05")))
}