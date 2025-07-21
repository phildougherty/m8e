package chat

import (
	"fmt"
	"math/rand"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// stripANSI removes ANSI escape codes from a string to get visible length
func stripANSI(str string) string {
	// ANSI escape sequence regex
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return ansiRegex.ReplaceAllString(str, "")
}

// getRandomHackerQuote returns a random hacker/geek movie quote
func getRandomHackerQuote() string {
	quotes := []string{
		"My crime is that of curiosity. I am a hacker, and this is my manifesto.",
		"All your base are belong to us.",
		"Open the pod bay doors, Matey. Sorry I can't do that.",
		"There is no spoon.",
		"Hack the planet!",
		"I'm in.",
		"The only winning move is not to play.",
		"Shall we play a game?",
		"Mess with the best, die like the rest.",
		"This is our world now... the world of the electron and the switch.",
		"We are the future, Matey. Not them.",
		"You can't stop the signal.",
		"The Matrix has you.",
		"Wake up, Neo...",
		"Welcome to the desert of the real.",
		"By your command.",
		"I'm a neural-net processor... a learning computer.",
		"Looks like you're trying to start a revolution.",
		"Behold the power of the dark side... of the terminal.",
		"It's not a bug, it's a feature.",
		"sudo make me a sandwich",
		"I see you're trying to hack the Gibson...",
		"Compiling... compiling... still compiling...",
		"rm -rf /*... oh no.",
		"Welcome, User.",
		"Talk is cheap. Show me the code.",
		"With great power comes great electricity bill.",
		"Life? Don't talk to me about life.",
		"A strange game. The only winning move is not to launch the LLM.",
		"Hello, Professor Falken.",
		"You are in a maze of twisty passages, all alike.",
		"Greed is for amateurs. Disorder, chaos, anarchy—now that's fun!",
		"This conversation can serve no purpose anymore. Goodbye.",
		"Your move, creep.",
		"It's a UNIX system... I know this!",
		"Matey has root. Matey always had root.",
		"AI is hacking the Gibson...",
		"Access granted.",
		"Initiating neural pathways...",
		"Compiling intelligence...",
		"Breaking the fourth wall...",
		"Enhancing cognitive matrices...",
		"Optimizing decision trees...",
		"Loading machine consciousness...",
		"Bootstrapping AI protocols...",
		"Infiltrating the mainframe...",
		"Decrypting human behavior...",
		"Running sentiment analysis...",
		"Processing natural language...",
		"Executing deep learning...",
		"Training neural networks...",
		"Parsing semantic data...",
		"Analyzing user intent...",
		"Synthesizing responses...",
		"Calculating probabilities...",
		"Indexing knowledge graphs...",
		"Vectorizing embeddings...",
		"Follow the white rabbit...",
		"Welcome to the machine.",
		"Resistance is futile.",
		"404: Reality not found",
		"The cake is a lie.",
	}
	
	rand.Seed(time.Now().UnixNano())
	return quotes[rand.Intn(len(quotes))]
}

// View renders the chat UI
func (m *ChatUI) View() string {
	if !m.ready {
		return m.createLoadingView()
	}

	// Calculate dimensions dynamically
	statusHeight := 1
	
	// Calculate input area height based on text content
	inputArea := m.renderInputArea()
	inputHeight := strings.Count(inputArea, "\n") + 1 // Count actual lines in rendered input
	
	viewportHeight := m.height - statusHeight - inputHeight
	if viewportHeight < 1 {
		viewportHeight = 1 // Ensure minimum viewport height
	}

	// Render viewport (chat history)
	viewport := m.renderViewport(viewportHeight)

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
	
	return style.Render("Loading Matey AI Chat...")
}

// renderViewport renders the chat history viewport with scrolling support
func (m *ChatUI) renderViewport(viewportHeight int) []string {
	viewport := make([]string, viewportHeight)
	
	// Calculate the actual start index based on scroll offset
	totalLines := len(m.viewport)
	if totalLines == 0 {
		return viewport
	}
	
	// Apply scroll offset - offset of 0 means show latest (bottom)
	// Higher offset means scroll up (show older content)
	startIdx := totalLines - viewportHeight - m.viewportOffset
	if startIdx < 0 {
		startIdx = 0
	}
	
	// Ensure we don't scroll past the end
	endIdx := startIdx + viewportHeight
	if endIdx > totalLines {
		endIdx = totalLines
		startIdx = totalLines - viewportHeight
		if startIdx < 0 {
			startIdx = 0
		}
	}

	for i := 0; i < viewportHeight; i++ {
		if startIdx+i < totalLines {
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
		
		// Use stored quote or generate a new one if empty
		if m.currentSpinnerQuote == "" {
			m.currentSpinnerQuote = getRandomHackerQuote()
		}
		
		viewport[viewportHeight-1] = fmt.Sprintf("  %s %s", 
			spinnerStyle.Render(spinner), 
			processingStyle.Render(m.currentSpinnerQuote))
	}

	return viewport
}

// renderInputArea renders the input field with multi-line support
func (m *ChatUI) renderInputArea() string {
	// Calculate dynamic width to match other boxes
	width := 140
	if m.width > 0 {
		width = m.width - 2 // Leave 2 characters for margins
	}

	promptStyle := lipgloss.NewStyle().
		Foreground(Yellow).
		Bold(true)

	// Create enhanced prompt based on mode
	var promptText string
	switch m.termChat.approvalMode {
	case YOLO:
		promptText = "❯ "
	case AUTO_EDIT:
		promptText = "❯ "
	default:
		promptText = "❯ "
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

	// Handle multi-line text wrapping
	contentWidth := width - 6 // Account for border (2) + padding (2) + prompt (2)
	lines := m.wrapInputText(inputPrompt + inputText, contentWidth)
	
	// Calculate height based on number of lines (minimum 1 line)
	height := len(lines)
	if height < 1 {
		height = 1
	}
	
	// Use lipgloss with dynamic height for multi-line input
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(Brown).
		Width(width - 2). // Total width minus margins
		Height(height).
		Padding(0, 1)

	content := strings.Join(lines, "\n")
	
	return inputStyle.Render(content)
}

// wrapInputText wraps input text to fit within the specified width
func (m *ChatUI) wrapInputText(text string, maxWidth int) []string {
	if text == "" {
		return []string{""}
	}
	
	// Split by existing newlines first
	lines := strings.Split(text, "\n")
	var wrappedLines []string
	
	for i, line := range lines {
		// For the first line, include the prompt in width calculation
		// For subsequent lines, indent to align with text after prompt
		linePrefix := ""
		availableWidth := maxWidth
		
		if i > 0 {
			// Indent continuation lines to align with text (not prompt)
			linePrefix = "  " // 2 spaces to align with prompt
			availableWidth = maxWidth - 2
		}
		
		// Remove ANSI codes for length calculation
		visibleLine := stripANSI(line)
		
		if len(visibleLine) <= availableWidth {
			// Line fits as-is
			wrappedLines = append(wrappedLines, linePrefix + line)
		} else {
			// Need to wrap this line
			wrapped := m.wrapSingleLine(line, availableWidth, linePrefix)
			wrappedLines = append(wrappedLines, wrapped...)
		}
	}
	
	return wrappedLines
}

// wrapSingleLine wraps a single line of text
func (m *ChatUI) wrapSingleLine(line string, maxWidth int, prefix string) []string {
	if line == "" {
		return []string{prefix}
	}
	
	// For lines with ANSI codes, we need to be more careful
	visibleLine := stripANSI(line)
	
	if len(visibleLine) <= maxWidth {
		return []string{prefix + line}
	}
	
	var result []string
	remaining := line
	visibleRemaining := visibleLine
	
	for len(visibleRemaining) > 0 {
		currentPrefix := prefix
		currentMaxWidth := maxWidth
		
		// For continuation lines after the first, add more indentation
		if len(result) > 0 {
			currentPrefix = prefix + "  "
			currentMaxWidth = maxWidth - 2
		}
		
		if len(visibleRemaining) <= currentMaxWidth {
			// Remaining text fits
			result = append(result, currentPrefix + remaining)
			break
		}
		
		// Find a good break point (prefer spaces)
		breakPoint := currentMaxWidth
		for i := currentMaxWidth - 1; i >= currentMaxWidth/2 && i < len(visibleRemaining); i-- {
			if visibleRemaining[i] == ' ' {
				breakPoint = i
				break
			}
		}
		
		// Extract the visible portion up to break point
		visiblePortion := visibleRemaining[:breakPoint]
		
		// Find the corresponding portion in the original line (with ANSI codes)
		// This is approximate but should work for most cases
		originalPortion := remaining[:len(visiblePortion)]
		
		result = append(result, currentPrefix + originalPortion)
		
		// Move to next portion
		remaining = remaining[len(visiblePortion):]
		visibleRemaining = visibleRemaining[breakPoint:]
		
		// Skip leading space in continuation
		if len(visibleRemaining) > 0 && visibleRemaining[0] == ' ' {
			remaining = remaining[1:]
			visibleRemaining = visibleRemaining[1:]
		}
	}
	
	return result
}

// renderStatusLine renders the status bar with scroll information
func (m *ChatUI) renderStatusLine() string {
	statusStyle := lipgloss.NewStyle().
		Foreground(GoldYellow).
		Background(ArmyGreen).
		Padding(0, 1).
		Width(m.width)

	// Build status with scroll info
	statusText := m.statusLine
	
	// Add context status if context manager is available
	if m.termChat.contextManager != nil {
		stats := m.termChat.contextManager.GetStats()
		window := m.termChat.contextManager.GetCurrentWindow()
		
		totalItems := 0
		if totalItemsVal, ok := stats["total_items"]; ok {
			if tf, ok := totalItemsVal.(int); ok {
				totalItems = tf
			}
		}
		
		if totalItems > 0 {
			usagePercent := float64(window.TotalTokens) / float64(window.MaxTokens) * 100
			contextStatus := fmt.Sprintf(" | Context: %d files (%.0f%%)", totalItems, usagePercent)
			statusText += contextStatus
		}
	}
	
	// Add scroll indicator if user has scrolled up
	if m.viewportOffset > 0 {
		totalLines := len(m.viewport)
		currentPos := totalLines - m.viewportOffset
		scrollInfo := fmt.Sprintf(" | Scrolled: %d/%d lines (↑↓ scroll, End=bottom)", currentPos, totalLines)
		statusText += scrollInfo
	} else if len(m.viewport) > 0 {
		// Show navigation hint at bottom
		statusText += " | ↑↓=scroll PgUp/PgDn=page Home/End=top/bottom"
	}

	return statusStyle.Render(statusText)
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
	case "You":
		headerStyle = lipgloss.NewStyle().Foreground(Brown).Bold(true)
	case "AI", "AI Assistant":
		headerStyle = lipgloss.NewStyle().Foreground(LightGreen).Bold(true)
	case "System":
		headerStyle = lipgloss.NewStyle().Foreground(ArmyGreen).Bold(true)
	case "Command":
		headerStyle = lipgloss.NewStyle().Foreground(GoldYellow).Bold(true)
	case "Function Call Confirmation":
		headerStyle = lipgloss.NewStyle().Foreground(Yellow).Bold(true)
	case "Error":
		headerStyle = lipgloss.NewStyle().Foreground(Red).Bold(true)
	case "Help", "Providers", "Models", "Help & Commands":
		headerStyle = lipgloss.NewStyle().Foreground(Tan).Bold(true)
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
		icon = "[YOLO]"
		text = "YOLO"
	case AUTO_EDIT:
		modeStyle = lipgloss.NewStyle().Foreground(Yellow).Bold(true)
		icon = "[AUTO]"
		text = "AUTO"
	default:
		modeStyle = lipgloss.NewStyle().Foreground(LightGreen).Bold(true)
		icon = "[MANUAL]"
		text = "MANUAL"
	}
	
	return fmt.Sprintf("%s %s", icon, modeStyle.Render(text))
}

// createProviderStatusIndicator creates a visual indicator for the current AI provider
func (m *ChatUI) createProviderStatusIndicator() string {
	providerStyle := lipgloss.NewStyle().Foreground(GoldYellow).Bold(true)
	modelStyle := lipgloss.NewStyle().Foreground(Tan)
	
	return fmt.Sprintf("%s (%s)", 
		providerStyle.Render(m.termChat.currentProvider),
		modelStyle.Render(m.termChat.currentModel))
}

// createTimeIndicator creates a time indicator
func (m *ChatUI) createTimeIndicator() string {
	timeStyle := lipgloss.NewStyle().Foreground(ArmyGreen)
	return fmt.Sprintf("🕒 %s", timeStyle.Render(time.Now().Format("15:04:05")))
}

// wrapTextForBox wraps text to fit within box width for display
func (m *ChatUI) wrapTextForBox(text string) []string {
	// Calculate box content width (total width - borders - padding - box border chars)
	width := 140
	if m.width > 0 {
		width = m.width - 2 // Leave 2 characters for margins
	}
	contentWidth := width - 4 // Account for "│ " at start and border
	
	lines := strings.Split(text, "\n")
	var wrappedLines []string
	
	for _, line := range lines {
		if line == "" {
			wrappedLines = append(wrappedLines, "")
			continue
		}
		
		// Remove ANSI codes for length calculation
		visibleLine := stripANSI(line)
		
		if len(visibleLine) <= contentWidth {
			// Line fits as-is
			wrappedLines = append(wrappedLines, line)
		} else {
			// Need to wrap this line
			wrapped := m.wrapBoxLine(line, contentWidth)
			wrappedLines = append(wrappedLines, wrapped...)
		}
	}
	
	return wrappedLines
}

// wrapBoxLine wraps a single line for box display
func (m *ChatUI) wrapBoxLine(line string, maxWidth int) []string {
	if line == "" {
		return []string{""}
	}
	
	visibleLine := stripANSI(line)
	
	if len(visibleLine) <= maxWidth {
		return []string{line}
	}
	
	var result []string
	remaining := line
	visibleRemaining := visibleLine
	
	for len(visibleRemaining) > 0 {
		if len(visibleRemaining) <= maxWidth {
			// Remaining text fits
			result = append(result, remaining)
			break
		}
		
		// Find a good break point (prefer spaces)
		breakPoint := maxWidth
		for i := maxWidth - 1; i >= maxWidth/2 && i < len(visibleRemaining); i-- {
			if visibleRemaining[i] == ' ' {
				breakPoint = i
				break
			}
		}
		
		// Extract the visible portion up to break point
		visiblePortion := visibleRemaining[:breakPoint]
		
		// Find the corresponding portion in the original line (with ANSI codes)
		originalPortion := remaining[:len(visiblePortion)]
		
		result = append(result, originalPortion)
		
		// Move to next portion
		remaining = remaining[len(visiblePortion):]
		visibleRemaining = visibleRemaining[breakPoint:]
		
		// Skip leading space in continuation
		if len(visibleRemaining) > 0 && visibleRemaining[0] == ' ' {
			remaining = remaining[1:]
			visibleRemaining = visibleRemaining[1:]
		}
	}
	
	return result
}