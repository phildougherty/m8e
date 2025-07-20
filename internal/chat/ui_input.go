package chat

import (
	"context"
	"fmt"
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

	case "up":
		// Scroll up through viewport history
		return m.handleScrollUp()

	case "down":
		// Scroll down through viewport history  
		return m.handleScrollDown()

	case "pgup", "shift+up":
		// Page up - scroll up by larger amount
		return m.handlePageUp()

	case "pgdown", "shift+down":
		// Page down - scroll down by larger amount
		return m.handlePageDown()

	case "home":
		// Go to top of viewport
		return m.handleScrollToTop()

	case "end":
		// Go to bottom of viewport
		return m.handleScrollToBottom()

	case "ctrl+l": // Clear screen
		m.viewport = []string{}
		return m, nil

	case "ctrl+y": // Toggle YOLO/DEFAULT mode
		return m.handleModeToggle(YOLO)

	case "tab": // Toggle AUTO_EDIT/DEFAULT mode
		return m.handleModeToggle(AUTO_EDIT)

	case "ctrl+r": // Toggle verbose/compact output
		return m.handleVerboseToggle()

	case "ctrl+v": // Toggle voice mode
		return m.handleVoiceToggle()

	case "ctrl+space": // PTT (Push-to-Talk) mode - better key binding
		if m.termChat.voiceManager != nil && m.termChat.voiceManager.config.Enabled {
			return m.handlePushToTalk()
		}
		return m, nil

	case "ctrl+m": // Manual voice trigger (alternative to wake word)
		if m.termChat.voiceManager != nil && m.termChat.voiceManager.config.Enabled {
			return m.handleManualVoiceTrigger()
		}
		return m, nil

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
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("System", time.Now().Format("15:04:05")))
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
	m.currentSpinnerQuote = "" // Clear quote for next loading session
	
	// Cancel the AI context if possible
	if m.termChat.cancel != nil {
		m.termChat.cancel()
	}
	// Create new context for future operations
	m.termChat.ctx, m.termChat.cancel = context.WithCancel(context.Background())
	
	// Close any open AI response box cleanly
	if len(m.viewport) > 0 {
		lastLine := m.viewport[len(m.viewport)-1]
		if !strings.HasSuffix(lastLine, "┘") && (strings.Contains(lastLine, "AI") || strings.HasPrefix(lastLine, "│")) {
			m.viewport = append(m.viewport, m.createErrorMessage("Response cancelled (ESC pressed)"))
			m.viewport = append(m.viewport, m.createBoxFooter())
		}
	}
	
	m.viewport = append(m.viewport, "")
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("System", time.Now().Format("15:04:05")))
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
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("System", time.Now().Format("15:04:05")))
		m.viewport = append(m.viewport, m.createSuccessMessage("Switched to MANUAL mode"))
		m.viewport = append(m.viewport, m.createInfoMessage("Will ask for confirmation before executing actions"))
		m.viewport = append(m.viewport, m.createBoxFooter())
		m.viewport = append(m.viewport, "")
	} else {
		m.termChat.approvalMode = targetMode
		m.viewport = append(m.viewport, "")
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("System", time.Now().Format("15:04:05")))
		
		var modeMsg, descMsg string
		switch targetMode {
		case YOLO:
			modeMsg = "Switched to YOLO mode "
			descMsg = "Will auto-approve all function calls"
		case AUTO_EDIT:
			modeMsg = "Switched to AUTO-EDIT mode "
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
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("System", time.Now().Format("15:04:05")))
	
	if m.termChat.verboseMode {
		m.viewport = append(m.viewport, m.createSuccessMessage("Switched to VERBOSE output mode"))
		m.viewport = append(m.viewport, m.createInfoMessage("Will show detailed function results"))
	} else {
		m.viewport = append(m.viewport, m.createSuccessMessage("Switched to COMPACT output mode"))
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
		feedbackMsg = "Function approved and upgraded to YOLO mode "
	case "n", "no":
		approved = false
		feedbackMsg = "Function cancelled ❌"
	case "a", "always":
		approved = true
		m.termChat.approvalMode = AUTO_EDIT
		feedbackMsg = "Function approved and upgraded to AUTO mode "
	case "yolo", "YOLO":
		approved = true
		m.termChat.approvalMode = YOLO
		feedbackMsg = "Function approved and upgraded to YOLO mode "
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

// Scroll handling methods

// handleScrollUp scrolls up by one line
func (m *ChatUI) handleScrollUp() (tea.Model, tea.Cmd) {
	if m.viewportOffset < len(m.viewport)-1 {
		m.viewportOffset++
	}
	return m, nil
}

// handleScrollDown scrolls down by one line
func (m *ChatUI) handleScrollDown() (tea.Model, tea.Cmd) {
	if m.viewportOffset > 0 {
		m.viewportOffset--
	}
	return m, nil
}

// handlePageUp scrolls up by page (10 lines)
func (m *ChatUI) handlePageUp() (tea.Model, tea.Cmd) {
	pageSize := 10
	m.viewportOffset += pageSize
	if m.viewportOffset > len(m.viewport)-1 {
		m.viewportOffset = len(m.viewport) - 1
	}
	return m, nil
}

// handlePageDown scrolls down by page (10 lines)
func (m *ChatUI) handlePageDown() (tea.Model, tea.Cmd) {
	pageSize := 10
	m.viewportOffset -= pageSize
	if m.viewportOffset < 0 {
		m.viewportOffset = 0
	}
	return m, nil
}

// handleScrollToTop scrolls to the very top
func (m *ChatUI) handleScrollToTop() (tea.Model, tea.Cmd) {
	if len(m.viewport) > 0 {
		m.viewportOffset = len(m.viewport) - 1
	}
	return m, nil
}

// handleScrollToBottom scrolls to the very bottom (live view)
func (m *ChatUI) handleScrollToBottom() (tea.Model, tea.Cmd) {
	m.viewportOffset = 0
	return m, nil
}

// handleVoiceToggle handles toggling voice mode on/off
func (m *ChatUI) handleVoiceToggle() (tea.Model, tea.Cmd) {
	if m.termChat.voiceManager == nil {
		m.viewport = append(m.viewport, "")
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("System", time.Now().Format("15:04:05")))
		m.viewport = append(m.viewport, m.createErrorMessage("Voice system not initialized"))
		m.viewport = append(m.viewport, m.createInfoMessage("Set VOICE_ENABLED=true to enable voice features"))
		m.viewport = append(m.viewport, m.createBoxFooter())
		m.viewport = append(m.viewport, "")
		return m, nil
	}

	if !m.termChat.voiceManager.config.Enabled {
		// Enable voice mode
		m.termChat.voiceManager.config.Enabled = true
		if err := m.termChat.voiceManager.Start(); err != nil {
			m.viewport = append(m.viewport, "")
			m.viewport = append(m.viewport, m.createEnhancedBoxHeader("System", time.Now().Format("15:04:05")))
			m.viewport = append(m.viewport, m.createErrorMessage("Failed to start voice system: "+err.Error()))
			m.viewport = append(m.viewport, m.createBoxFooter())
			m.viewport = append(m.viewport, "")
			return m, nil
		}
		
		m.viewport = append(m.viewport, "")
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("System", time.Now().Format("15:04:05")))
		m.viewport = append(m.viewport, m.createSuccessMessage("Voice mode ENABLED 🎤"))
		m.viewport = append(m.viewport, m.createInfoMessage("Wake word: '"+m.termChat.voiceManager.config.WakeWord+"'"))
		m.viewport = append(m.viewport, m.createInfoMessage("Use Ctrl+Space for Push-to-Talk, Ctrl+M for manual trigger"))
		m.viewport = append(m.viewport, m.createBoxFooter())
		m.viewport = append(m.viewport, "")
	} else {
		// Disable voice mode
		m.termChat.voiceManager.Stop()
		m.termChat.voiceManager.config.Enabled = false
		
		m.viewport = append(m.viewport, "")
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("System", time.Now().Format("15:04:05")))
		m.viewport = append(m.viewport, m.createInfoMessage("Voice mode DISABLED"))
		m.viewport = append(m.viewport, m.createBoxFooter())
		m.viewport = append(m.viewport, "")
	}
	
	return m, nil
}

// handlePushToTalk handles push-to-talk recording
func (m *ChatUI) handlePushToTalk() (tea.Model, tea.Cmd) {
	if m.termChat.voiceManager == nil || !m.termChat.voiceManager.config.Enabled {
		return m, nil
	}
	
	// Start recording for PTT
	m.viewport = append(m.viewport, "")
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("Voice", time.Now().Format("15:04:05")))
	m.viewport = append(m.viewport, m.createInfoMessage("🎤 Push-to-Talk recording... (Ctrl+Space again to stop)"))
	m.viewport = append(m.viewport, m.createBoxFooter())
	
	// Trigger manual voice recording
	go m.triggerVoiceRecording()
	
	return m, nil
}

// handleManualVoiceTrigger handles manual voice activation (bypass wake word)
func (m *ChatUI) handleManualVoiceTrigger() (tea.Model, tea.Cmd) {
	if m.termChat.voiceManager == nil || !m.termChat.voiceManager.config.Enabled {
		return m, nil
	}
	
	m.viewport = append(m.viewport, "")
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("Voice", time.Now().Format("15:04:05")))
	m.viewport = append(m.viewport, m.createInfoMessage("🎤 Voice activated manually - listening..."))
	m.viewport = append(m.viewport, m.createBoxFooter())
	
	// Trigger manual voice recording (bypass wake word detection)
	go m.triggerVoiceRecording()
	
	return m, nil
}

// triggerVoiceRecording manually triggers voice recording
func (m *ChatUI) triggerVoiceRecording() {
	if m.termChat.voiceManager != nil {
		// Use the proper manual recording method
		err := m.termChat.voiceManager.TriggerManualRecording()
		if err != nil {
			// Show error in UI
			if uiProgram != nil {
				uiProgram.Send(voiceTranscriptMsg{
					transcript: fmt.Sprintf("Voice recording error: %v", err),
				})
			}
		}
	}
}