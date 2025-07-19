package chat

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Update handles messages and updates the model
func (m *ChatUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case statusUpdateMsg:
		m.statusLine = msg.status
		return m, m.updateStatus()

	case aiStreamMsg:
		return m.handleAIStreamMessage(msg)

	case aiResponseMsg:
		return m.handleAIResponseMessage(msg)
	
	case spinnerMsg:
		if m.loading {
			m.spinnerFrame = (m.spinnerFrame + 1) % 4
			return m, m.spinnerTick()
		}
		return m, nil

	case startSpinnerMsg:
		// Start the spinner for AI continuation after function approval
		m.loading = true
		m.spinnerFrame = 0
		return m, m.spinnerTick()

	case functionConfirmationMsg:
		return m.handleFunctionConfirmationMessage(msg)

	case userInputMsg:
		return m.handleUserInputMessage(msg)
	}

	return m, nil
}

// handleAIStreamMessage handles streaming AI content
func (m *ChatUI) handleAIStreamMessage(msg aiStreamMsg) (tea.Model, tea.Cmd) {
	if msg.content == "" {
		// Start new AI response box with enhanced styling
		m.loading = true
		timestamp := time.Now().Format("15:04:05")
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("🤖 AI Assistant", timestamp))
		m.viewport = append(m.viewport, "│ ")
	} else {
		// Handle streaming content with improved formatting
		if len(m.viewport) > 0 {
			lines := strings.Split(msg.content, "\n")
			for i, line := range lines {
				if i == 0 {
					// First line - append to current line
					lastIndex := len(m.viewport) - 1
					if strings.HasPrefix(m.viewport[lastIndex], "│ ") {
						m.viewport[lastIndex] += line
					} else {
						m.viewport = append(m.viewport, "│ "+line)
					}
				} else {
					// Subsequent lines - create new lines
					if line != "" {
						m.viewport = append(m.viewport, "│ "+line)
					} else {
						m.viewport = append(m.viewport, "│ ")
					}
				}
			}
		}
	}
	return m, nil
}

// handleAIResponseMessage handles complete AI responses
func (m *ChatUI) handleAIResponseMessage(msg aiResponseMsg) (tea.Model, tea.Cmd) {
	// Stop loading spinner
	m.loading = false
	
	if msg.content == "" {
		// Just close the response box and preserve streaming content
		m.viewport = append(m.viewport, m.createBoxFooter())
		m.viewport = append(m.viewport, "")
	} else {
		// Replace streaming content with final rendered content
		for i := len(m.viewport) - 1; i >= 0; i-- {
			if strings.Contains(m.viewport[i], "🤖 AI Assistant") {
				// Found the AI response box, replace content
				boxStart := i
				m.viewport = m.viewport[:boxStart+1]
				
				// Render markdown with enhanced function call formatting
				rendered := m.termChat.renderMarkdown(msg.content)
				lines := strings.Split(rendered, "\n")
				for _, line := range lines {
					// Enhanced function call detection and formatting
					if strings.Contains(line, "<function_calls>") {
						m.viewport = append(m.viewport, m.createEnhancedFunctionCallStart())
					} else if strings.Contains(line, "</function_calls>") {
						m.viewport = append(m.viewport, m.createEnhancedFunctionCallEnd())
					} else if strings.Contains(line, "<invoke") {
						m.viewport = append(m.viewport, m.createEnhancedFunctionInvoke(line))
					} else if strings.Contains(line, "</invoke>") {
						m.viewport = append(m.viewport, m.createEnhancedFunctionInvokeEnd())
					} else if strings.Contains(line, "<parameter") {
						m.viewport = append(m.viewport, m.createEnhancedFunctionParameter(line))
					} else if line != "" {
						m.viewport = append(m.viewport, "│ "+line)
					}
				}
				break
			}
		}
	}
	
	// Close the AI response box
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	
	// Keep viewport to reasonable size
	if len(m.viewport) > 1000 {
		m.viewport = m.viewport[500:]
	}
	return m, nil
}

// handleFunctionConfirmationMessage handles function confirmation requests
func (m *ChatUI) handleFunctionConfirmationMessage(msg functionConfirmationMsg) (tea.Model, tea.Cmd) {
	// Enter confirmation mode
	m.confirmationMode = true
	m.pendingFunctionCall = &FunctionConfirmation{
		FunctionName: msg.functionName,
		Arguments:    msg.arguments,
		Callback:     msg.callback,
	}
	m.loading = false // Stop spinner
	
	// Add enhanced confirmation prompt to viewport
	m.viewport = append(m.viewport, "")
	m.viewport = append(m.viewport, m.createEnhancedFunctionConfirmationBox(msg.functionName, msg.arguments))
	return m, nil
}

// handleUserInputMessage handles user input
func (m *ChatUI) handleUserInputMessage(msg userInputMsg) (tea.Model, tea.Cmd) {
	// Add user message to viewport with enhanced formatting
	timestamp := time.Now().Format("15:04:05")
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("👤 You", timestamp))
	// Add user message with indentation
	lines := strings.Split(msg.input, "\n")
	for _, line := range lines {
		m.viewport = append(m.viewport, "│ "+line)
	}
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	
	// Start loading spinner
	m.loading = true
	m.spinnerFrame = 0
	
	// Process the input and return command for AI response
	return m, tea.Batch(
		m.processAIResponseCommand(msg.input),
		m.spinnerTick(),
	)
}

// Enhanced function call formatting methods
func (m *ChatUI) createEnhancedFunctionCallStart() string {
	style := lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	return "│ " + style.Render("🔧 ╭─ Function Call ─╮")
}

func (m *ChatUI) createEnhancedFunctionCallEnd() string {
	style := lipgloss.NewStyle().Foreground(LightGreen).Bold(true)
	return "│ " + style.Render("✅ ╰─ Call Complete ─╯")
}

func (m *ChatUI) createEnhancedFunctionInvoke(line string) string {
	// Extract function name from invoke line
	funcName := "unknown"
	if strings.Contains(line, "name=") {
		parts := strings.Split(line, "name=")
		if len(parts) > 1 {
			namePart := strings.Split(parts[1], ">")[0]
			funcName = strings.Trim(namePart, "\"")
		}
	}
	
	style := lipgloss.NewStyle().Foreground(GoldYellow).Bold(true)
	return "│ " + style.Render(fmt.Sprintf("  🎯 Invoking: %s", funcName))
}

func (m *ChatUI) createEnhancedFunctionInvokeEnd() string {
	style := lipgloss.NewStyle().Foreground(LightGreen)
	return "│ " + style.Render("  ✓ Invocation complete")
}

func (m *ChatUI) createEnhancedFunctionParameter(line string) string {
	// Extract parameter name from parameter line
	paramName := "unknown"
	if strings.Contains(line, "name=") {
		parts := strings.Split(line, "name=")
		if len(parts) > 1 {
			namePart := strings.Split(parts[1], ">")[0]
			paramName = strings.Trim(namePart, "\"")
		}
	}
	
	style := lipgloss.NewStyle().Foreground(Tan)
	return "│ " + style.Render(fmt.Sprintf("    📋 Parameter: %s", paramName))
}

// createEnhancedFunctionConfirmationBox creates a colorful function confirmation box
func (m *ChatUI) createEnhancedFunctionConfirmationBox(functionName, arguments string) string {
	var result strings.Builder
	
	// Header with enhanced styling
	headerStyle := lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	result.WriteString(headerStyle.Render("┌─ 🔐 Function Call Confirmation ─┐"))
	result.WriteString("\n")
	
	// Function details with colorful formatting
	funcStyle := lipgloss.NewStyle().Foreground(GoldYellow).Bold(true)
	argStyle := lipgloss.NewStyle().Foreground(Tan)
	
	result.WriteString(fmt.Sprintf("│ %s %s", funcStyle.Render("🎯 Function:"), functionName))
	result.WriteString("\n")
	
	if arguments != "" && arguments != "{}" {
		result.WriteString(fmt.Sprintf("│ %s %s", argStyle.Render("📋 Arguments:"), arguments))
		result.WriteString("\n")
	}
	
	result.WriteString("│")
	result.WriteString("\n")
	
	// Enhanced options with icons and colors
	optionStyle := lipgloss.NewStyle().Foreground(LightGreen).Bold(true)
	choiceStyle := lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	
	result.WriteString("│ " + optionStyle.Render("🎮 Options:"))
	result.WriteString("\n")
	result.WriteString("│   " + choiceStyle.Render("[y]") + " ✅ Proceed once")
	result.WriteString("\n")
	result.WriteString("│   " + choiceStyle.Render("[n]") + " ❌ Cancel")
	result.WriteString("\n")
	result.WriteString("│   " + choiceStyle.Render("[a]") + " ⚡ Auto-approve similar functions")
	result.WriteString("\n")
	result.WriteString("│   " + choiceStyle.Render("[Y]") + " 🔥 YOLO mode (auto-approve everything)")
	result.WriteString("\n")
	result.WriteString("│")
	result.WriteString("\n")
	
	promptStyle := lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	result.WriteString("│ " + promptStyle.Render("⏱️  Your choice [y/n/a/Y]: "))
	result.WriteString("\n")
	
	// Footer
	footerStyle := lipgloss.NewStyle().Foreground(Brown)
	result.WriteString(footerStyle.Render("└─────────────────────────────────┘"))
	
	return result.String()
}

// spinnerTick creates animation for loading spinner
func (m *ChatUI) spinnerTick() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(time.Time) tea.Msg {
		return spinnerMsg{}
	})
}

// processAIResponseCommand processes the AI response using Bubbletea Commands
func (m *ChatUI) processAIResponseCommand(input string) tea.Cmd {
	return func() tea.Msg {
		// This runs in a Bubbletea-managed goroutine
		// Use a simplified version that captures AI response without terminal output
		return m.processAIResponseSilent(input)
	}
}

// processAIResponseSilent processes AI response WITH streaming for UI
func (m *ChatUI) processAIResponseSilent(input string) tea.Msg {
	// Add user message first
	m.termChat.addMessage("user", input)
	
	// Use the turn-based system that supports UI function confirmations
	m.termChat.chatWithAISilent(input)
	
	// Return nil - the streaming messages will handle the UI updates
	// and the aiResponseMsg will be sent separately when streaming is complete
	return nil
}