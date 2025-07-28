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


	case voiceTranscriptMsg:
		return m.handleVoiceTranscriptMessage(msg)

	case voiceTTSReadyMsg:
		return m.handleVoiceTTSReadyMessage(msg)
	}

	return m, nil
}

// handleAIStreamMessage handles streaming AI content
func (m *ChatUI) handleAIStreamMessage(msg aiStreamMsg) (tea.Model, tea.Cmd) {
	if msg.content == "" {
		// Start new AI response - track starting position
		m.loading = true
		m.appendToViewport("")
		// Store the index where AI response content starts
		m.aiResponseStartIdx = len(m.viewport)
	} else {
		// Handle streaming content - add raw content for immediate display
		lines := strings.Split(msg.content, "\n")
		for i, line := range lines {
			if i == 0 && len(m.viewport) > m.aiResponseStartIdx {
				// First line - append to current line (only if we're not at the start)
				lastIndex := len(m.viewport) - 1
				m.viewport[lastIndex] += line
			} else {
				// Subsequent lines or first line at start - just add them
				m.appendToViewport(line)
			}
		}
	}
	return m, nil
}

// handleAIResponseMessage handles complete AI responses
func (m *ChatUI) handleAIResponseMessage(msg aiResponseMsg) (tea.Model, tea.Cmd) {
	// Stop loading spinner
	m.loading = false
	m.currentSpinnerQuote = "" // Clear quote for next loading session
	
	// Replace the streamed raw content with properly rendered markdown
	if msg.content != "" && m.aiResponseStartIdx >= 0 && m.aiResponseStartIdx < len(m.viewport) {
		// Preserve function call lines that were added during streaming
		var preservedLines []string
		for i := m.aiResponseStartIdx; i < len(m.viewport); i++ {
			line := m.viewport[i]
			// Preserve function call display lines (contain -> or +)
			if strings.Contains(line, "->") || strings.Contains(line, "+") || strings.Contains(line, "x") {
				preservedLines = append(preservedLines, line)
			}
		}
		
		// Remove all content from the start index onwards
		m.viewport = m.viewport[:m.aiResponseStartIdx]
		
		// Add preserved function call lines first
		for _, line := range preservedLines {
			m.appendToViewport(line)
		}
		
		// Add the properly rendered markdown content
		rendered := m.termChat.renderMarkdown(msg.content)
		lines := strings.Split(rendered, "\n")
		for _, line := range lines {
			// Skip function call XML - handled by our new system
			if strings.Contains(line, "<function_calls>") || strings.Contains(line, "</function_calls>") ||
			   strings.Contains(line, "<invoke") || strings.Contains(line, "</invoke>") ||
			   strings.Contains(line, "<parameter") {
				continue
			}
			m.appendToViewport(line)
		}
		
		// Reset the start index
		m.aiResponseStartIdx = -1
		
		// Convert AI response to speech if voice is enabled
		if m.termChat.voiceManager != nil && m.termChat.voiceManager.config.Enabled {
			go func() {
				if err := m.termChat.voiceManager.TextToSpeech(msg.content); err != nil {
					// Don't block UI for TTS errors, just log them
					// Could send error message to UI if desired
				}
			}()
		}
	}
	
	// Note: Spacing now handled only when there's actual next content
	
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
	m.currentSpinnerQuote = "" // Clear quote for next loading session
	
	// Add enhanced confirmation prompt to viewport
	m.viewport = append(m.viewport, "")
	m.viewport = append(m.viewport, m.createEnhancedFunctionConfirmationBox(msg.functionName, msg.arguments))
	return m, nil
}

// handleUserInputMessage handles user input
func (m *ChatUI) handleUserInputMessage(msg userInputMsg) (tea.Model, tea.Cmd) {
	// Add user message to viewport with enhanced formatting
	timestamp := time.Now().Format("15:04:05")
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("You", timestamp))
	// Add user message with indentation
	lines := strings.Split(msg.input, "\n")
	for _, line := range lines {
		m.viewport = append(m.viewport, "│ "+line)
	}
	m.viewport = append(m.viewport, m.createBoxFooter())
	// Removed automatic empty line after user input
	
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
	return "│ " + style.Render("╭─ Function Call ─╮")
}

func (m *ChatUI) createEnhancedFunctionCallEnd() string {
	style := lipgloss.NewStyle().Foreground(LightGreen).Bold(true)
	return "│ " + style.Render("+ Call Complete")
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
	return "│ " + style.Render(fmt.Sprintf("  Invoking: %s", funcName))
}

func (m *ChatUI) createEnhancedFunctionInvokeEnd() string {
	style := lipgloss.NewStyle().Foreground(LightGreen)
	return "│ " + style.Render("  + Invocation complete")
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
	return "│ " + style.Render(fmt.Sprintf("    Parameter: %s", paramName))
}

// createEnhancedFunctionConfirmationBox creates a colorful function confirmation box
func (m *ChatUI) createEnhancedFunctionConfirmationBox(functionName, arguments string) string {
	// Calculate dynamic width to match other boxes
	width := 140
	if m.width > 0 {
		width = m.width - 2 // Leave 2 characters for margins
	}
	
	var result strings.Builder
	
	// Header with dynamic width - match other headers
	content := "Function Call Confirmation"
	dashSpace := width - 5 - len(content)
	if dashSpace < 0 {
		dashSpace = 0
	}
	header := "┌─ " + content + " " + strings.Repeat("─", dashSpace) + "┐"
	
	headerStyle := lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	result.WriteString(headerStyle.Render(header))
	result.WriteString("\n")
	
	// Function details with colorful formatting
	funcStyle := lipgloss.NewStyle().Foreground(GoldYellow).Bold(true)
	argStyle := lipgloss.NewStyle().Foreground(Tan)
	
	result.WriteString(fmt.Sprintf("│ %s %s", funcStyle.Render("Function:"), functionName))
	result.WriteString("\n")
	
	if arguments != "" && arguments != "{}" {
		result.WriteString(fmt.Sprintf("│ %s %s", argStyle.Render("Arguments:"), arguments))
		result.WriteString("\n")
	}
	
	result.WriteString("│")
	result.WriteString("\n")
	
	// Enhanced options with icons and colors
	optionStyle := lipgloss.NewStyle().Foreground(LightGreen).Bold(true)
	choiceStyle := lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	
	result.WriteString("│ " + optionStyle.Render("Options:"))
	result.WriteString("\n")
	result.WriteString("│   " + choiceStyle.Render("[y]") + " + Proceed once")
	result.WriteString("\n")
	result.WriteString("│   " + choiceStyle.Render("[n]") + " Cancel")
	result.WriteString("\n")
	result.WriteString("│   " + choiceStyle.Render("[a]") + " Auto-approve similar functions")
	result.WriteString("\n")
	result.WriteString("│   " + choiceStyle.Render("[Y]") + " YOLO mode (auto-approve everything)")
	result.WriteString("\n")
	result.WriteString("│")
	result.WriteString("\n")
	
	promptStyle := lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	result.WriteString("│ " + promptStyle.Render("Your choice [y/n/a/Y]: "))
	result.WriteString("\n")
	
	// Footer with dynamic width - match other footers
	footer := "└" + strings.Repeat("─", width-2) + "┘"
	footerStyle := lipgloss.NewStyle().Foreground(Brown)
	result.WriteString(footerStyle.Render(footer))
	
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

// appendToViewport adds content to viewport and maintains scroll position intelligently
func (m *ChatUI) appendToViewport(content string) {
	// If user is at the bottom (offset = 0), stay at bottom after adding content
	wasAtBottom := m.viewportOffset == 0
	
	// Add the content
	m.viewport = append(m.viewport, content)
	
	// If user was at bottom, keep them there
	// If they were scrolled up, maintain their relative position
	if !wasAtBottom {
		// User was scrolled up, so they probably want to stay where they are
		// No adjustment needed - they'll see the new content when they scroll down
	}
}

// handleVoiceWakeWordMessage handles wake word detection

// handleVoiceTranscriptMessage handles voice transcription results
func (m *ChatUI) handleVoiceTranscriptMessage(msg voiceTranscriptMsg) (tea.Model, tea.Cmd) {
	if msg.transcript == "" {
		m.viewport = append(m.viewport, "")
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("Voice", time.Now().Format("15:04:05")))
		m.viewport = append(m.viewport, m.createErrorMessage("No speech detected"))
		m.viewport = append(m.viewport, m.createBoxFooter())
		return m, nil
	}

	// Handle different message types
	if strings.HasPrefix(msg.transcript, "ERROR: ") {
		m.viewport = append(m.viewport, "")
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("Voice Error", time.Now().Format("15:04:05")))
		m.viewport = append(m.viewport, m.createErrorMessage(strings.TrimPrefix(msg.transcript, "ERROR: ")))
		m.viewport = append(m.viewport, m.createBoxFooter())
		return m, nil
	}

	if strings.HasPrefix(msg.transcript, "PROCESSING: ") {
		m.viewport = append(m.viewport, "")
		m.viewport = append(m.viewport, m.createEnhancedBoxHeader("Voice Processing", time.Now().Format("15:04:05")))
		m.viewport = append(m.viewport, m.createInfoMessage(strings.TrimPrefix(msg.transcript, "PROCESSING: ")))
		m.viewport = append(m.viewport, m.createBoxFooter())
		return m, nil
	}

	// Show successful transcription
	m.viewport = append(m.viewport, "")
	m.viewport = append(m.viewport, m.createEnhancedBoxHeader("Voice Complete", time.Now().Format("15:04:05")))
	m.viewport = append(m.viewport, m.createSuccessMessage("Transcribed: "+msg.transcript))
	m.viewport = append(m.viewport, m.createInfoMessage("Recording finished - processing as input"))
	m.viewport = append(m.viewport, m.createBoxFooter())

	// Process the voice transcript as user input
	return m, m.processInputCommand(msg.transcript)
}

// handleVoiceTTSReadyMessage handles TTS audio ready for playback
func (m *ChatUI) handleVoiceTTSReadyMessage(msg voiceTTSReadyMsg) (tea.Model, tea.Cmd) {
	if m.termChat.voiceManager != nil {
		go m.termChat.voiceManager.PlayAudio(msg.audioData)
	}
	return m, nil
}