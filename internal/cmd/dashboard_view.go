package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View renders the dashboard TUI
func (m *DashboardModel) View() string {
	if m.err != nil {
		return DashboardStyles.ErrorMessage.Render(fmt.Sprintf("Error: %v\n\nPress Ctrl+C to exit", m.err))
	}

	if m.width < 80 || m.height < 20 {
		return DashboardStyles.ErrorMessage.Render("Terminal too small. Please resize to at least 80x20.")
	}

	// Calculate layout dimensions
	contentHeight := m.height - 4 // Reserve space for input and tabs
	chatWidth := m.width/2 - 1
	artifactsWidth := m.width/2 - 1

	// Render based on current tab
	switch m.currentTab {
	case TabChat:
		return m.renderChatView(chatWidth, artifactsWidth, contentHeight)
	case TabMonitoring:
		return m.renderMonitoringView()
	case TabConfig:
		return m.renderConfigView()
	case TabWorkflows:
		return m.renderWorkflowsView()
	default:
		return m.renderChatView(chatWidth, artifactsWidth, contentHeight)
	}
}

// renderChatView renders the main chat interface
func (m *DashboardModel) renderChatView(chatWidth, artifactsWidth, contentHeight int) string {
	var b strings.Builder

	// Header with tabs
	b.WriteString(m.renderTabs())
	b.WriteString("\n")

	// Main content area
	chatPane := m.renderChatPane(chatWidth, contentHeight-2)
	artifactsPane := m.renderArtifactsPane(artifactsWidth, contentHeight-2)

	contentRow := lipgloss.JoinHorizontal(lipgloss.Top, chatPane, artifactsPane)
	b.WriteString(contentRow)
	b.WriteString("\n")

	// Input area
	b.WriteString(m.renderInputArea())

	return b.String()
}

// renderTabs renders the tab navigation
func (m *DashboardModel) renderTabs() string {
	var tabs []string

	// Define tab names and icons
	tabInfo := []struct {
		tab   Tab
		name  string
		icon  string
	}{
		{TabChat, "Chat", "💬"},
		{TabMonitoring, "Monitoring", "📊"},
		{TabConfig, "Config", "⚙️"},
		{TabWorkflows, "Workflows", "🔄"},
	}

	for _, info := range tabInfo {
		tabText := fmt.Sprintf("%s %s", info.icon, info.name)
		
		if info.tab == m.currentTab {
			tabs = append(tabs, DashboardStyles.TabActive.Render(tabText))
		} else {
			tabs = append(tabs, DashboardStyles.TabInactive.Render(tabText))
		}
	}

	// Add provider info
	provider, err := m.aiManager.GetCurrentProvider()
	if err == nil {
		providerName := provider.Name()
		currentModel := provider.DefaultModel()
		providerBadge := DashboardStyles.ProviderBadge.Render(fmt.Sprintf("🤖 %s/%s", providerName, currentModel))
		tabs = append(tabs, providerBadge)
	}

	// Add status badge
	statusBadge := DashboardStyles.StatusBadge.Render("🟢 Online")
	tabs = append(tabs, statusBadge)

	return strings.Join(tabs, " ")
}

// renderChatPane renders the chat history pane
func (m *DashboardModel) renderChatPane(width, height int) string {
	var chatContent strings.Builder

	// Title
	chatContent.WriteString(DashboardStyles.UserMessage.Render("Chat History"))
	chatContent.WriteString("\n\n")

	// Chat messages
	visibleMessages := m.getVisibleMessages(height - 4)
	for _, msg := range visibleMessages {
		chatContent.WriteString(m.renderChatMessage(msg))
		chatContent.WriteString("\n")
	}

	// Handle streaming indicator with animation
	if m.streaming {
		dots := m.getAnimatedDots()
		chatContent.WriteString(DashboardStyles.SystemMessage.Render(fmt.Sprintf("🤖 AI is responding%s", dots)))
	}

	// Wrap in styled pane
	content := DashboardStyles.ChatPane.
		Width(width).
		Height(height).
		Render(chatContent.String())

	return content
}

// renderArtifactsPane renders the artifacts pane
func (m *DashboardModel) renderArtifactsPane(width, height int) string {
	var artifactsContent strings.Builder

	// Title
	artifactsContent.WriteString(DashboardStyles.AIMessage.Render("Artifacts"))
	artifactsContent.WriteString("\n\n")

	// Artifacts content
	if m.currentArtifact != "" {
		artifactsContent.WriteString(m.renderArtifact(m.currentArtifact, m.artifactType))
	} else {
		artifactsContent.WriteString(DashboardStyles.SystemMessage.Render("Artifacts will appear here as the AI generates configurations, diagrams, and code."))
	}

	// Wrap in styled pane
	content := DashboardStyles.ArtifactsPane.
		Width(width).
		Height(height).
		Render(artifactsContent.String())

	return content
}

// renderInputArea renders the input area
func (m *DashboardModel) renderInputArea() string {
	prompt := "💬 Type your message... (Tab to switch tabs, F1 for help)"
	
	if m.streaming {
		prompt = "⏳ AI is responding..."
	}

	inputText := m.inputText
	if len(inputText) == 0 {
		inputText = DashboardStyles.SystemMessage.Render("Type your message...")
	}

	inputContent := fmt.Sprintf("%s\n> %s", 
		DashboardStyles.SystemMessage.Render(prompt),
		inputText,
	)

	return DashboardStyles.InputPane.
		Width(m.width - 2).
		Render(inputContent)
}

// renderChatMessage renders a single chat message
func (m *DashboardModel) renderChatMessage(msg ChatMessage) string {
	timestamp := msg.Timestamp.Format("15:04:05")
	
	var header string
	var content string
	var thinkSection string
	
	switch msg.Role {
	case "user":
		header = DashboardStyles.UserMessage.Render(fmt.Sprintf("👤 You [%s]", timestamp))
		content = msg.Content
	case "assistant":
		header = DashboardStyles.AIMessage.Render(fmt.Sprintf("🤖 AI [%s]", timestamp))
		content = msg.Content
		if msg.Streaming {
			content += DashboardStyles.SystemMessage.Render(" ▎")
		}
		
		// Add think content as collapsible section
		if msg.ThinkContent != "" {
			thinkIndicator := "🧠"
			if msg.ThinkExpanded {
				thinkIndicator = "🧠▼"
				thinkSection = fmt.Sprintf("\n%s\n%s", 
					DashboardStyles.SystemMessage.Render("💭 AI Reasoning:"),
					DashboardStyles.Muted.Render(msg.ThinkContent))
			} else {
				thinkIndicator = "🧠▶"
				thinkSection = fmt.Sprintf("\n%s", 
					DashboardStyles.Muted.Render("💭 Click to view AI reasoning"))
			}
			header = DashboardStyles.AIMessage.Render(fmt.Sprintf("🤖 AI %s [%s]", thinkIndicator, timestamp))
		}
	case "system":
		header = DashboardStyles.SystemMessage.Render(fmt.Sprintf("ℹ️ System [%s]", timestamp))
		content = msg.Content
	default:
		header = DashboardStyles.SystemMessage.Render(fmt.Sprintf("? Unknown [%s]", timestamp))
		content = msg.Content
	}

	if thinkSection != "" {
		return fmt.Sprintf("%s\n%s%s", header, content, thinkSection)
	}
	return fmt.Sprintf("%s\n%s", header, content)
}

// renderArtifact renders an artifact based on its type
func (m *DashboardModel) renderArtifact(content, artifactType string) string {
	switch artifactType {
	case "code":
		return DashboardStyles.CodeBlock.Render(content)
	case "config":
		return DashboardStyles.ConfigBlock.Render(content)
	case "diagram":
		return DashboardStyles.DiagramBlock.Render(content)
	default:
		return DashboardStyles.CodeBlock.Render(content)
	}
}

// getVisibleMessages returns messages that fit in the visible area
func (m *DashboardModel) getVisibleMessages(maxHeight int) []ChatMessage {
	if len(m.chatHistory) == 0 {
		return []ChatMessage{}
	}

	// Simple approach: show last N messages
	// In a real implementation, you'd calculate based on rendered height
	maxMessages := maxHeight / 4 // Rough estimate
	if maxMessages <= 0 {
		maxMessages = 1
	}

	start := len(m.chatHistory) - maxMessages
	if start < 0 {
		start = 0
	}

	return m.chatHistory[start:]
}

// renderMonitoringView renders the monitoring tab
func (m *DashboardModel) renderMonitoringView() string {
	var content strings.Builder

	// Header
	content.WriteString(m.renderTabs())
	content.WriteString("\n\n")

	// Monitoring content
	content.WriteString(DashboardStyles.UserMessage.Render("🔍 System Monitoring"))
	content.WriteString("\n\n")

	// Server status
	if status, err := m.composer.Status(); err == nil {
		content.WriteString(DashboardStyles.SystemMessage.Render("MCP Servers:"))
		content.WriteString("\n")
		
		for name, svcStatus := range status.Services {
			statusIcon := "🔴"
			if svcStatus.Status == "running" {
				statusIcon = "🟢"
			} else if svcStatus.Status == "pending" {
				statusIcon = "🟡"
			}
			
			content.WriteString(fmt.Sprintf("%s %s (%s) - %s\n", 
				statusIcon, name, svcStatus.Type, svcStatus.Status))
		}
	}

	// Provider status
	content.WriteString("\n")
	content.WriteString(DashboardStyles.SystemMessage.Render("AI Providers:"))
	content.WriteString("\n")
	
	providerStatus := m.aiManager.GetProviderStatus()
	for name, status := range providerStatus {
		statusIcon := "🔴"
		if status.Available {
			statusIcon = "🟢"
		}
		content.WriteString(fmt.Sprintf("%s %s\n", statusIcon, name))
	}

	// Last update
	content.WriteString("\n")
	content.WriteString(DashboardStyles.SystemMessage.Render(fmt.Sprintf("Last Update: %s", 
		m.lastUpdate.Format("15:04:05"))))

	return content.String()
}

// renderConfigView is now implemented in dashboard_config.go
// This is a placeholder to maintain compatibility

// renderWorkflowsView is now implemented in dashboard_workflows.go
// This is a placeholder to maintain compatibility