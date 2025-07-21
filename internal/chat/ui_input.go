package chat

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// handleKeyPress processes keyboard input
func (m *ChatUI) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	keyStr := msg.String()
	
	// Voice key detection (Ctrl+T only)
	if m.termChat.voiceManager != nil && m.termChat.voiceManager.config.Enabled && keyStr == "ctrl+t" {
		// Check if already recording
		if m.termChat.voiceManager.isRecording {
			m.viewport = append(m.viewport, "")
			m.viewport = append(m.viewport, m.createEnhancedBoxHeader("Voice Recording", time.Now().Format("15:04:05")))
			m.viewport = append(m.viewport, m.createInfoMessage("🛑 Stopping recording early..."))
			m.viewport = append(m.viewport, m.createBoxFooter())
		} else {
			m.viewport = append(m.viewport, "")
			m.viewport = append(m.viewport, m.createEnhancedBoxHeader("Voice Recording", time.Now().Format("15:04:05")))
			m.viewport = append(m.viewport, m.createSuccessMessage("🎤 Recording... (auto-stops when you stop talking)"))
			m.viewport = append(m.viewport, m.createInfoMessage("🗣️  Speak now! Press Ctrl+T again to stop early"))
			m.viewport = append(m.viewport, m.createBoxFooter())
		}
		
		// Trigger manual recording (or stop if already recording)
		if err := m.termChat.voiceManager.TriggerManualRecording(); err != nil {
			m.viewport = append(m.viewport, "")
			m.viewport = append(m.viewport, m.createEnhancedBoxHeader("Voice Error", time.Now().Format("15:04:05")))
			m.viewport = append(m.viewport, m.createErrorMessage("Voice recording failed: "+err.Error()))
			m.viewport = append(m.viewport, m.createBoxFooter())
		}
		return m, nil
	}
	
	switch keyStr {
	case "ctrl+c":
		return m, tea.Quit
	
	case "esc":
		return m.handleEscapeKey()

	case "enter":
		return m.handleEnterKey()
		
	case "shift+enter":
		return m.handleShiftEnterKey()

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

	case "ctrl+v": // Paste from clipboard
		return m.handlePaste()

	case "alt+v", "ctrl+shift+v": // Toggle voice mode (moved from ctrl+v)
		return m.handleVoiceToggle()

	case "ctrl+n": // Skip to next TTS response
		return m.handleTTSSkip()

	case "ctrl+x": // Interrupt TTS (stop current and clear queue)
		return m.handleTTSInterrupt()

	case "ctrl+q": // Show TTS queue status
		return m.handleTTSQueueStatus()

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

// handlePaste handles pasting from clipboard
func (m *ChatUI) handlePaste() (tea.Model, tea.Cmd) {
	clipboardContent, err := m.getClipboardContent()
	
	if err != nil {
		// Show detailed error message with troubleshooting info
		m.viewport = append(m.viewport, "")
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("Paste Error", time.Now().Format("15:04:05")))
		m.viewport = append(m.viewport, m.createErrorMessage(fmt.Sprintf("Clipboard error: %v", err)))
		m.viewport = append(m.viewport, m.createInfoMessage(fmt.Sprintf("OS: %s", runtime.GOOS)))
		
		// Add OS-specific troubleshooting
		switch runtime.GOOS {
		case "linux":
			m.viewport = append(m.viewport, m.createInfoMessage("Install: sudo apt install xclip (or xsel)"))
		case "darwin":
			m.viewport = append(m.viewport, m.createInfoMessage("Try copying text again with ⌘+C"))
		case "windows":
			m.viewport = append(m.viewport, m.createInfoMessage("PowerShell clipboard access required"))
		}
		
		m.viewport = append(m.viewport, m.createBoxFooter())
		return m, nil
	}
	
	if clipboardContent == "" {
		m.viewport = append(m.viewport, "")
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("Paste", time.Now().Format("15:04:05")))
		m.viewport = append(m.viewport, m.createInfoMessage("Clipboard is empty"))
		m.viewport = append(m.viewport, m.createBoxFooter())
		return m, nil
	}
	
	// Clean up the content (remove trailing newlines)
	clipboardContent = strings.TrimRight(clipboardContent, "\n\r")
	
	// Insert clipboard content at cursor position
	if clipboardContent != "" {
		m.input = m.input[:m.cursor] + clipboardContent + m.input[m.cursor:]
		m.cursor += len(clipboardContent)
		
		// Show success feedback
		lines := strings.Split(clipboardContent, "\n")
		if len(lines) > 1 {
			m.viewport = append(m.viewport, "")
			m.viewport = append(m.viewport, m.createEnhancedBoxHeader("Paste", time.Now().Format("15:04:05")))
			m.viewport = append(m.viewport, m.createSuccessMessage(fmt.Sprintf("✅ Pasted %d lines", len(lines))))
			m.viewport = append(m.viewport, m.createBoxFooter())
		} else {
			// For single line, just show brief success
			m.viewport = append(m.viewport, "")
			m.viewport = append(m.viewport, m.createEnhancedBoxHeader("Paste", time.Now().Format("15:04:05")))
			preview := clipboardContent
			if len(preview) > 50 {
				preview = preview[:47] + "..."
			}
			m.viewport = append(m.viewport, m.createSuccessMessage(fmt.Sprintf("✅ Pasted: %s", preview)))
			m.viewport = append(m.viewport, m.createBoxFooter())
		}
	}
	
	return m, nil
}

// getClipboardContent gets clipboard content with improved cross-platform support
func (m *ChatUI) getClipboardContent() (string, error) {
	switch runtime.GOOS {
	case "darwin": // macOS
		return m.getMacClipboard()
	case "linux":
		return m.getLinuxClipboard()
	case "windows":
		return m.getWindowsClipboard()
	default:
		return "", fmt.Errorf("clipboard not supported on %s", runtime.GOOS)
	}
}

// getMacClipboard gets clipboard content on macOS
func (m *ChatUI) getMacClipboard() (string, error) {
	// Try pbpaste
	cmd := exec.Command("pbpaste")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pbpaste failed: %w", err)
	}
	return string(output), nil
}

// getLinuxClipboard gets clipboard content on Linux with multiple fallbacks
func (m *ChatUI) getLinuxClipboard() (string, error) {
	// Try multiple methods in order of preference
	methods := []struct {
		name string
		cmd  []string
	}{
		{"xclip", []string{"xclip", "-selection", "clipboard", "-o"}},
		{"xsel", []string{"xsel", "--clipboard", "--output"}},
		{"wl-paste", []string{"wl-paste"}}, // Wayland clipboard
	}
	
	var lastErr error
	for _, method := range methods {
		cmd := exec.Command(method.cmd[0], method.cmd[1:]...)
		output, err := cmd.Output()
		if err == nil {
			return string(output), nil
		}
		lastErr = fmt.Errorf("%s failed: %w", method.name, err)
	}
	
	return "", fmt.Errorf("all clipboard methods failed, last error: %v", lastErr)
}

// getWindowsClipboard gets clipboard content on Windows
func (m *ChatUI) getWindowsClipboard() (string, error) {
	cmd := exec.Command("powershell", "Get-Clipboard")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("powershell Get-Clipboard failed: %w", err)
	}
	return string(output), nil
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
	
	// Check for line continuation with backslash
	if strings.HasSuffix(strings.TrimRight(m.input, " \t"), "\\") {
		// Remove the trailing backslash and add a newline
		trimmed := strings.TrimRight(m.input, " \t")
		m.input = trimmed[:len(trimmed)-1] + "\n"
		m.cursor = len(m.input)
		return m, nil
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

// handleShiftEnterKey handles Shift+Enter for new lines
func (m *ChatUI) handleShiftEnterKey() (tea.Model, tea.Cmd) {
	// Add a newline at cursor position
	m.input = m.input[:m.cursor] + "\n" + m.input[m.cursor:]
	m.cursor++
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
		m.viewport = append(m.viewport, m.createInfoMessage("Press Ctrl+T to start voice recording"))
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

// TTS Control Handlers

// handleTTSSkip skips to the next TTS response in queue
func (m *ChatUI) handleTTSSkip() (tea.Model, tea.Cmd) {
	if m.termChat.voiceManager == nil || !m.termChat.voiceManager.config.Enabled {
		m.viewport = append(m.viewport, "")
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("TTS Control", time.Now().Format("15:04:05")))
		m.viewport = append(m.viewport, m.createErrorMessage("Voice system not enabled"))
		m.viewport = append(m.viewport, m.createBoxFooter())
		m.viewport = append(m.viewport, "")
		return m, nil
	}
	
	// Skip to next TTS
	m.termChat.voiceManager.SkipToNextTTS()
	
	queueLength, isPlaying := m.termChat.voiceManager.GetTTSQueueStatus()
	m.viewport = append(m.viewport, "")
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("TTS Control", time.Now().Format("15:04:05")))
	m.viewport = append(m.viewport, m.createSuccessMessage("⏭️  Skipped to next TTS response"))
	if queueLength > 0 {
		m.viewport = append(m.viewport, m.createInfoMessage(fmt.Sprintf("📋 %d responses remaining in queue", queueLength)))
	} else if !isPlaying {
		m.viewport = append(m.viewport, m.createInfoMessage("📋 Queue is empty"))
	}
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	return m, nil
}

// handleTTSInterrupt stops current TTS and clears the queue
func (m *ChatUI) handleTTSInterrupt() (tea.Model, tea.Cmd) {
	if m.termChat.voiceManager == nil || !m.termChat.voiceManager.config.Enabled {
		m.viewport = append(m.viewport, "")
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("TTS Control", time.Now().Format("15:04:05")))
		m.viewport = append(m.viewport, m.createErrorMessage("Voice system not enabled"))
		m.viewport = append(m.viewport, m.createBoxFooter())
		m.viewport = append(m.viewport, "")
		return m, nil
	}
	
	// Interrupt TTS
	m.termChat.voiceManager.InterruptTTS()
	
	m.viewport = append(m.viewport, "")
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("TTS Control", time.Now().Format("15:04:05")))
	m.viewport = append(m.viewport, m.createSuccessMessage("🛑 TTS interrupted and queue cleared"))
	m.viewport = append(m.viewport, m.createInfoMessage("🔇 All speech playback stopped"))
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	return m, nil
}

// handleTTSQueueStatus shows current TTS queue status
func (m *ChatUI) handleTTSQueueStatus() (tea.Model, tea.Cmd) {
	if m.termChat.voiceManager == nil || !m.termChat.voiceManager.config.Enabled {
		m.viewport = append(m.viewport, "")
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("TTS Status", time.Now().Format("15:04:05")))
		m.viewport = append(m.viewport, m.createErrorMessage("Voice system not enabled"))
		m.viewport = append(m.viewport, m.createBoxFooter())
		m.viewport = append(m.viewport, "")
		return m, nil
	}
	
	queueLength, isPlaying := m.termChat.voiceManager.GetTTSQueueStatus()
	
	m.viewport = append(m.viewport, "")
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("TTS Queue Status", time.Now().Format("15:04:05")))
	
	if isPlaying {
		m.viewport = append(m.viewport, m.createSuccessMessage("🔊 Currently playing TTS response"))
	} else {
		m.viewport = append(m.viewport, m.createInfoMessage("🔇 No TTS currently playing"))
	}
	
	if queueLength > 0 {
		m.viewport = append(m.viewport, m.createInfoMessage(fmt.Sprintf("📋 %d responses queued for playback", queueLength)))
	} else {
		m.viewport = append(m.viewport, m.createInfoMessage("📋 Queue is empty"))
	}
	
	m.viewport = append(m.viewport, m.createInfoMessage("⌨️  Controls: Ctrl+N (skip), Ctrl+X (interrupt), Ctrl+Q (status)"))
	m.viewport = append(m.viewport, m.createBoxFooter())
	m.viewport = append(m.viewport, "")
	return m, nil
}