package chat

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// handleKeyPress processes keyboard input
func (m *ChatUI) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	
	case "esc":
		return m.handleEscapeKey()

	case "enter":
		return m.handleEnterKey()

	case "backspace":
		if m.cursor > 0 {
			m.input = m.input[:m.cursor-1] + m.input[m.cursor:]
			m.cursor--
		}
		return m, nil

	case "left":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case "right":
		if m.cursor < len(m.input) {
			m.cursor++
		}
		return m, nil

	case "ctrl+l": // Clear screen
		m.viewport = []string{}
		return m, nil

	case "ctrl+y": // Toggle YOLO/DEFAULT mode
		return m.handleModeToggle(YOLO)

	case "tab": // Toggle AUTO_EDIT/DEFAULT mode
		return m.handleModeToggle(AUTO_EDIT)

	case "ctrl+r": // Toggle verbose/compact output
		return m.handleVerboseToggle()

	default:
		// Regular character input
		if len(msg.String()) == 1 {
			char := msg.String()
			m.input = m.input[:m.cursor] + char + m.input[m.cursor:]
			m.cursor++
		}
		return m, nil
	}
}

// handleEscapeKey handles the escape key press
func (m *ChatUI) handleEscapeKey() (tea.Model, tea.Cmd) {
	// Cancel any ongoing operations and return to input
	if m.confirmationMode {
		return m.handleConfirmationCancel()
	}
	
	// Stop any loading/spinner
	if m.loading {
		return m.handleLoadingCancel()
	}
	
	// Clear input and show general message
	m.input = ""
	m.cursor = 0
	m.inputFocused = true
	
	m.viewport = append(m.viewport, "")
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("⚙️ System", time.Now().Format("15:04:05")))
	m.viewport = append(m.viewport, m.createInfoMessage("Input cleared - Ready for new input"))
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	
	return m, nil
}

// handleConfirmationCancel handles canceling function confirmation
func (m *ChatUI) handleConfirmationCancel() (tea.Model, tea.Cmd) {
	m.confirmationMode = false
	if m.pendingFunctionCall != nil {
		callback := m.pendingFunctionCall.Callback
		functionName := m.pendingFunctionCall.FunctionName
		m.pendingFunctionCall = nil
		
		m.viewport = append(m.viewport, m.createErrorMessage("Function '"+functionName+"' cancelled (ESC pressed)"))
		m.viewport = append(m.viewport, m.createInfoMessage("Ready for new input"))
		m.viewport = append(m.viewport, m.createBoxFooter())
		m.viewport = append(m.viewport, "")
		
		return m, func() tea.Msg {
			callback(false)
			return nil
		}
	}
	return m, nil
}

// handleLoadingCancel handles canceling AI processing
func (m *ChatUI) handleLoadingCancel() (tea.Model, tea.Cmd) {
	m.loading = false
	
	// Cancel the AI context if possible
	if m.termChat.cancel != nil {
		m.termChat.cancel()
	}
	// Create new context for future operations
	m.termChat.ctx, m.termChat.cancel = context.WithCancel(context.Background())
	
	// Close any open AI response box cleanly
	if len(m.viewport) > 0 {
		lastLine := m.viewport[len(m.viewport)-1]
		if !strings.HasSuffix(lastLine, "┘") && (strings.Contains(lastLine, "🤖 AI") || strings.HasPrefix(lastLine, "│")) {
			m.viewport = append(m.viewport, m.createErrorMessage("Response cancelled (ESC pressed)"))
			m.viewport = append(m.viewport, m.createBoxFooter())
		}
	}
	
	m.viewport = append(m.viewport, "")
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("⚙️ System", time.Now().Format("15:04:05")))
	m.viewport = append(m.viewport, m.createErrorMessage("AI processing cancelled (ESC pressed)"))
	m.viewport = append(m.viewport, m.createInfoMessage("Ready for new input"))
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	
	return m, nil
}

// handleEnterKey handles the enter key press
func (m *ChatUI) handleEnterKey() (tea.Model, tea.Cmd) {
	if m.confirmationMode && m.pendingFunctionCall != nil {
		// Handle confirmation response
		choice := strings.TrimSpace(m.input)
		m.input = ""
		m.cursor = 0
		return m, m.handleConfirmationChoice(choice)
	}
	
	if strings.TrimSpace(m.input) != "" {
		// Check for slash commands first
		if strings.HasPrefix(strings.TrimSpace(m.input), "/") {
			cmd := m.handleSlashCommand(strings.TrimSpace(m.input))
			m.input = ""
			m.cursor = 0
			return m, cmd
		}
		
		// Process regular input
		cmd := m.processInputCommand(m.input)
		m.input = ""
		m.cursor = 0
		return m, cmd
	}
	return m, nil
}

// handleModeToggle handles mode switching (YOLO, AUTO_EDIT, DEFAULT)
func (m *ChatUI) handleModeToggle(targetMode ApprovalMode) (tea.Model, tea.Cmd) {
	if m.termChat.approvalMode == targetMode {
		m.termChat.approvalMode = DEFAULT
		m.viewport = append(m.viewport, "")
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("⚙️ System", time.Now().Format("15:04:05")))
		m.viewport = append(m.viewport, m.createSuccessMessage("Switched to MANUAL mode"))
		m.viewport = append(m.viewport, m.createInfoMessage("Will ask for confirmation before executing actions"))
		m.viewport = append(m.viewport, m.createBoxFooter())
		m.viewport = append(m.viewport, "")
	} else {
		m.termChat.approvalMode = targetMode
		m.viewport = append(m.viewport, "")
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("⚙️ System", time.Now().Format("15:04:05")))
		
		var modeMsg, descMsg string
		switch targetMode {
		case YOLO:
			modeMsg = "Switched to YOLO mode 🔥"
			descMsg = "Will auto-approve all function calls"
		case AUTO_EDIT:
			modeMsg = "Switched to AUTO-EDIT mode ⚡"
			descMsg = "Will auto-approve safe operations"
		}
		
		m.viewport = append(m.viewport, m.createSuccessMessage(modeMsg))
		m.viewport = append(m.viewport, m.createInfoMessage(descMsg))
		m.viewport = append(m.viewport, m.createBoxFooter())
		m.viewport = append(m.viewport, "")
	}
	return m, nil
}

// handleVerboseToggle handles toggling verbose/compact output
func (m *ChatUI) handleVerboseToggle() (tea.Model, tea.Cmd) {
	m.termChat.verboseMode = !m.termChat.verboseMode
	m.viewport = append(m.viewport, "")
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("⚙️ System", time.Now().Format("15:04:05")))
	
	if m.termChat.verboseMode {
		m.viewport = append(m.viewport, m.createSuccessMessage("Switched to VERBOSE output mode 📋"))
		m.viewport = append(m.viewport, m.createInfoMessage("Will show detailed function results"))
	} else {
		m.viewport = append(m.viewport, m.createSuccessMessage("Switched to COMPACT output mode 📦"))
		m.viewport = append(m.viewport, m.createInfoMessage("Will show brief function summaries"))
	}
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	return m, nil
}

// processInputCommand handles user input as a Bubbletea Command
func (m *ChatUI) processInputCommand(input string) tea.Cmd {
	return func() tea.Msg {
		return userInputMsg{input: input, userDisplay: ""}
	}
}

// handleConfirmationChoice processes the user's confirmation choice
func (m *ChatUI) handleConfirmationChoice(choice string) tea.Cmd {
	if m.pendingFunctionCall == nil {
		return nil
	}
	
	// Exit confirmation mode
	m.confirmationMode = false
	
	// Process the choice with enhanced feedback
	var approved bool
	var feedbackMsg string
	
	switch choice {
	case "y", "yes", "":
		approved = true
		feedbackMsg = "Function approved ✅"
	case "Y":
		approved = true
		m.termChat.approvalMode = YOLO
		feedbackMsg = "Function approved and upgraded to YOLO mode 🔥"
	case "n", "no":
		approved = false
		feedbackMsg = "Function cancelled ❌"
	case "a", "always":
		approved = true
		m.termChat.approvalMode = AUTO_EDIT
		feedbackMsg = "Function approved and upgraded to AUTO mode ⚡"
	case "yolo", "YOLO":
		approved = true
		m.termChat.approvalMode = YOLO
		feedbackMsg = "Function approved and upgraded to YOLO mode 🔥"
	default:
		approved = false
		feedbackMsg = "Invalid choice - Function cancelled ❌"
	}
	
	// Add enhanced confirmation result to viewport
	m.viewport = append(m.viewport, m.createInfoMessage(feedbackMsg))
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	
	// Call the callback and clear the pending function
	callback := m.pendingFunctionCall.Callback
	m.pendingFunctionCall = nil
	
	// Execute the callback in a goroutine to avoid blocking
	return tea.Batch(
		func() tea.Msg {
			callback(approved)
			return nil
		},
		func() tea.Msg {
			// If approved, start the spinner again for AI continuation
			if approved {
				return startSpinnerMsg{}
			}
			return nil
		},
	)
}