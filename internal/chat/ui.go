package chat

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// NewChatUI creates a new chat UI instance
func NewChatUI(tc *TermChat) *ChatUI {
	// Create the UI instance first
	ui := &ChatUI{
		termChat:     tc,
		input:        "",
		cursor:       0,
		viewport:     []string{},
		statusLine:   "",
		inputFocused: true,
		ready:        false,
	}
	
	// Set up the UI confirmation callback
	tc.uiConfirmationCallback = ui.handleFunctionConfirmation
	
	// Add welcome message and chat history to viewport
	ui.initializeViewport()
	
	return ui
}

// initializeViewport sets up the initial viewport with welcome content
func (ui *ChatUI) initializeViewport() {
	viewport := []string{}
	
	// Add enhanced header with army green styling
	headerStyle := lipgloss.NewStyle().Foreground(ArmyGreen).Bold(true)
	viewport = append(viewport, headerStyle.Render("╭─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╮"))
	viewport = append(viewport, headerStyle.Render("│                                                     Matey AI Chat - Advanced MCP Orchestration Assistant                                                     │"))
	viewport = append(viewport, headerStyle.Render("╰─────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯"))
	
	// Add enhanced provider info with colorful indicators
	providerStyle := lipgloss.NewStyle().Foreground(GoldYellow).Bold(true)
	metaStyle := lipgloss.NewStyle().Foreground(LightGreen)
	
	viewport = append(viewport, fmt.Sprintf("%s %s %s %s %s %s %s %s", 
		providerStyle.Render("Provider:"), metaStyle.Render(ui.termChat.currentProvider),
		providerStyle.Render("Model:"), metaStyle.Render(ui.termChat.currentModel),
		providerStyle.Render("Time:"), metaStyle.Render(time.Now().Format("15:04:05")),
		providerStyle.Render("Mode:"), metaStyle.Render(ui.termChat.approvalMode.GetModeIndicatorNoEmoji())))
	viewport = append(viewport, "")
	
	// Enhanced welcome section with better formatting
	welcomeStyle := lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	bulletStyle := lipgloss.NewStyle().Foreground(GoldYellow)
	highlightStyle := lipgloss.NewStyle().Foreground(LightGreen).Bold(true)
	
	viewport = append(viewport, welcomeStyle.Render("Welcome to Matey AI Chat"))
	viewport = append(viewport, "")
	viewport = append(viewport, "Your expert assistant for Kubernetes-native MCP server orchestration.")
	viewport = append(viewport, "")
	viewport = append(viewport, welcomeStyle.Render("What I can help you with:"))
	viewport = append(viewport, bulletStyle.Render("• Deploy and scale microservices in your Kubernetes cluster"))
	viewport = append(viewport, bulletStyle.Render("• Set up automated backup and monitoring workflows")) 
	viewport = append(viewport, bulletStyle.Render("• Create comprehensive CI/CD pipelines with GitOps integration"))
	viewport = append(viewport, bulletStyle.Render("• Build observability dashboards and alerting systems"))
	viewport = append(viewport, bulletStyle.Render("• Manage MCP server orchestration with full protocol support"))
	viewport = append(viewport, "")
	viewport = append(viewport, welcomeStyle.Render("Quick Commands:"))
	viewport = append(viewport, bulletStyle.Render("• "+highlightStyle.Render("/auto")+" - Smart automation mode (auto-approve safe operations)"))
	viewport = append(viewport, bulletStyle.Render("• "+highlightStyle.Render("/yolo")+" - Maximum autonomy mode (auto-approve everything)"))
	viewport = append(viewport, bulletStyle.Render("• "+highlightStyle.Render("/status")+" - System status and health checks"))
	viewport = append(viewport, bulletStyle.Render("• "+highlightStyle.Render("/help")+" - Complete command reference"))
	viewport = append(viewport, "")
	viewport = append(viewport, welcomeStyle.Render("Keyboard Shortcuts:"))
	viewport = append(viewport, bulletStyle.Render("• "+highlightStyle.Render("Ctrl+Y")+" - Toggle YOLO/Manual mode"))
	viewport = append(viewport, bulletStyle.Render("• "+highlightStyle.Render("Tab")+" - Toggle Auto-edit/Manual mode"))
	viewport = append(viewport, bulletStyle.Render("• "+highlightStyle.Render("Ctrl+R")+" - Toggle verbose/compact output"))
	viewport = append(viewport, bulletStyle.Render("• "+highlightStyle.Render("ESC")+" - Cancel current operation"))
	viewport = append(viewport, "")
	viewport = append(viewport, fmt.Sprintf("Current Mode: %s", ui.createModeIndicator()))
	viewport = append(viewport, "")
	viewport = append(viewport, highlightStyle.Render("Ready to orchestrate your infrastructure! Just tell me what you want to build."))
	viewport = append(viewport, "")
	viewport = append(viewport, ui.createSectionDivider())
	viewport = append(viewport, "")
	
	// Add existing chat history with enhanced formatting
	for _, msg := range ui.termChat.chatHistory {
		timestamp := msg.Timestamp.Format("15:04:05")
		switch msg.Role {
		case "user":
			viewport = append(viewport, ui.createEnhancedBoxHeader("You", timestamp))
			lines := strings.Split(msg.Content, "\n")
			for _, line := range lines {
				viewport = append(viewport, "│ "+line)
			}
			viewport = append(viewport, ui.createBoxFooter())
		case "assistant":
			// Claude Code style: simple, clean output
			rendered := ui.termChat.renderMarkdown(msg.Content)
			lines := strings.Split(rendered, "\n")
			
			// Skip function call XML tags - they're handled by our new system
			for _, line := range lines {
				if strings.HasPrefix(line, "<function_calls>") || strings.HasPrefix(line, "</function_calls>") {
					continue
				}
				// Clean output without boxes - just the content
				if strings.TrimSpace(line) != "" {
					viewport = append(viewport, line)
				}
			}
		case "system":
			viewport = append(viewport, ui.createEnhancedBoxHeader("System", timestamp))
			// Render system messages with markdown too
			rendered := ui.termChat.renderMarkdown(msg.Content)
			lines := strings.Split(rendered, "\n")
			for _, line := range lines {
				if line != "" {
					viewport = append(viewport, "│ "+line)
				} else {
					viewport = append(viewport, "│ ")
				}
			}
			viewport = append(viewport, ui.createBoxFooter())
		}
		// Removed automatic empty line after each historical message
	}
	
	ui.viewport = viewport
}

// createModeIndicator creates a colorful mode indicator
func (ui *ChatUI) createModeIndicator() string {
	mode := ui.termChat.approvalMode
	var modeStyle lipgloss.Style
	var icon string
	
	switch mode {
	case YOLO:
		modeStyle = lipgloss.NewStyle().Foreground(Red).Bold(true)
		icon = "[YOLO]"
	case AUTO_EDIT:
		modeStyle = lipgloss.NewStyle().Foreground(Yellow).Bold(true)
		icon = "[AUTO]"
	default:
		modeStyle = lipgloss.NewStyle().Foreground(LightGreen).Bold(true)
		icon = "[MANUAL]"
	}
	
	return fmt.Sprintf("%s %s - %s", icon, modeStyle.Render(mode.GetModeIndicatorNoEmoji()), mode.Description())
}

// createSectionDivider creates a visually appealing section divider
func (ui *ChatUI) createSectionDivider() string {
	dividerStyle := lipgloss.NewStyle().Foreground(Brown)
	return dividerStyle.Render("─────────────────────────────────────────────────────────────────────")
}

// createEnhancedBoxHeader creates an enhanced box header with improved styling
func (ui *ChatUI) createEnhancedBoxHeader(title, timestamp string) string {
	width := 140
	if ui.width > 0 {
		width = ui.width - 2
	}
	
	var headerStyle lipgloss.Style
	switch {
	case strings.Contains(title, "You"):
		headerStyle = lipgloss.NewStyle().Foreground(Brown).Bold(true)
	case strings.Contains(title, "AI"):
		headerStyle = lipgloss.NewStyle().Foreground(LightGreen).Bold(true)
	case strings.Contains(title, "System"):
		headerStyle = lipgloss.NewStyle().Foreground(ArmyGreen).Bold(true)
	case strings.Contains(title, "Function"):
		headerStyle = lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	default:
		headerStyle = lipgloss.NewStyle().Foreground(Brown).Bold(true)
	}
	
	// Build header content
	content := title
	if timestamp != "" {
		content += " " + timestamp
	}
	
	// Calculate available space for dashes to match footer width
	// Footer: "└" + strings.Repeat("─", width-2) + "┘" = width total
	// Header: "┌─ " + content + " " + dashes + "┐" = width total
	// So: 3 + len(content) + 1 + dashes + 1 = width
	// Therefore: dashes = width - 5 - len(content)
	dashSpace := width - 5 - len(content)
	if dashSpace < 0 {
		dashSpace = 0
	}
	
	header := "┌─ " + content + " " + strings.Repeat("─", dashSpace) + "┐"
	
	return headerStyle.Render(header)
}

// createFunctionCallHeader creates a colorful function call header
func (ui *ChatUI) createFunctionCallHeader() string {
	style := lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	return "│ " + style.Render("🔧 [Function Call Starting]")
}

// createFunctionCallFooter creates a colorful function call footer
func (ui *ChatUI) createFunctionCallFooter() string {
	style := lipgloss.NewStyle().Foreground(LightGreen).Bold(true)
	return "│ " + style.Render("✅ [Function Call Complete]")
}

// Run starts the chat UI
func (ui *ChatUI) Run() error {
	p := tea.NewProgram(ui, tea.WithAltScreen())
	// Set the global program reference so other goroutines can send messages
	uiProgram = p
	_, err := p.Run()
	return err
}

// Init initializes the chat UI
func (m *ChatUI) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		m.updateStatus(),
	)
}

// handleFunctionConfirmation handles function confirmation requests from the turn system
func (m *ChatUI) handleFunctionConfirmation(functionName, arguments string) bool {
	// This is called from the turn system goroutine and needs to be handled synchronously
	// We'll use a channel to communicate with the UI
	resultChan := make(chan bool, 1)
	
	// Create a confirmation callback that will be called when the user responds
	callback := func(approved bool) {
		resultChan <- approved
	}
	
	// Send the confirmation request through the Bubbletea message system
	if uiProgram != nil {
		uiProgram.Send(functionConfirmationMsg{
			functionName: functionName,
			arguments:    arguments,
			callback:     callback,
		})
	} else {
		// If UI program is not available, auto-approve
		return true
	}
	
	// Wait for the user to respond with timeout
	select {
	case result := <-resultChan:
		return result
	case <-m.termChat.ctx.Done():
		return false
	case <-time.After(30 * time.Second):
		// Timeout - auto-approve to prevent blocking
		return true
	}
}

// updateStatus creates a command to update the status line
func (m *ChatUI) updateStatus() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		// Get current mode with enhanced formatting
		mode := m.termChat.approvalMode.GetModeIndicatorNoEmoji()
		
		// Calculate total tokens (rough estimate based on message length)
		totalTokens := 0
		for _, msg := range m.termChat.chatHistory {
			// Rough estimate: 1 token per 4 characters
			totalTokens += len(msg.Content) / 4
		}
		
		// Create enhanced status with colors
		statusText := fmt.Sprintf("%s | %s | %s | %d tokens | %s",
			time.Now().Format("15:04:05"), 
			m.termChat.currentProvider, 
			m.termChat.currentModel,
			totalTokens,
			mode)
		return statusUpdateMsg{status: statusText}
	})
}