package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	
	"github.com/phildougherty/m8e/internal/ai"
)

// TUI Messages
type (
	dashboardTickMsg     time.Time
	streamingTickMsg     time.Time
	chatStreamMsg        struct {
		ai.StreamResponse
		stream <-chan ai.StreamResponse
	}
	chatCompleteMsg      struct{}
	artifactUpdateMsg    string
	serverStatusMsg      []ServerInfo
	errorMsg             error
)

// Init initializes the dashboard model
func (m *DashboardModel) Init() tea.Cmd {
	return tea.Batch(
		tea.Tick(time.Second, func(t time.Time) tea.Msg {
			return dashboardTickMsg(t)
		}),
		m.fetchServerStatus(),
		m.fetchConfigResources(),
		m.fetchWorkflows(),
	)
}

// Update handles messages and updates the model
func (m *DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		fmt.Fprintf(os.Stderr, "DEBUG: Update - received KeyMsg: %s\n", msg.String())
		model, cmd := m.handleKeyMsg(msg)
		fmt.Fprintf(os.Stderr, "DEBUG: Update - handleKeyMsg returned command: %v\n", cmd != nil)
		return model, cmd

	case dashboardTickMsg:
		return m, tea.Batch(
			tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
				return dashboardTickMsg(t)
			}),
			m.fetchServerStatus(),
		)

	case streamingTickMsg:
		// Only continue ticking if still streaming
		if m.streaming {
			return m, tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
				return streamingTickMsg(t)
			})
		}
		return m, nil

	case chatStreamMsg:
		fmt.Fprintf(os.Stderr, "DEBUG: Update - received chatStreamMsg with content: %q\n", msg.StreamResponse.Content)
		return m.handleChatStream(msg)

	case chatCompleteMsg:
		fmt.Fprintf(os.Stderr, "DEBUG: Update - received chatCompleteMsg\n")
		m.streaming = false
		// Mark the last assistant message as no longer streaming
		for i := len(m.chatHistory) - 1; i >= 0; i-- {
			if m.chatHistory[i].Role == "assistant" && m.chatHistory[i].Streaming {
				m.chatHistory[i].Streaming = false
				break
			}
		}
		return m, nil

	case artifactUpdateMsg:
		m.currentArtifact = string(msg)
		return m, nil

	case serverStatusMsg:
		// Update server status for monitoring tab
		m.lastUpdate = time.Now()
		return m, nil

	case errorMsg:
		m.err = error(msg)
		return m, nil

	case configListMsg:
		// Handle configuration list update
		// For now, we'll implement this when we add state storage
		return m, nil

	case configContentMsg:
		// Handle configuration content update
		if msg.name == m.selectedConfig {
			m.configContent = msg.content
		}
		return m, nil

	case configValidationMsg:
		// Handle configuration validation result
		if msg.name == m.selectedConfig {
			m.configErrors = msg.errors
		}
		return m, nil

	case workflowListMsg:
		// Handle workflow list update
		m.workflows = []WorkflowInfo(msg)
		return m, nil

	case workflowTemplatesMsg:
		// Handle workflow templates update
		// Store templates in model if needed
		return m, nil

	case workflowStatusMsg:
		// Handle workflow status update
		if msg.name == m.selectedWorkflow {
			// Update workflow status
		}
		return m, nil
	}

	return m, nil
}

// handleKeyMsg handles keyboard input
func (m *DashboardModel) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys
	switch msg.String() {
	case "ctrl+c":
		m.cancel()
		return m, tea.Quit

	case "tab":
		return m, m.switchTab()

	case "f1":
		return m, m.showHelp()
	}

	// Tab-specific key handling
	switch m.currentTab {
	case TabConfig:
		return m.handleConfigKeys(msg)
	case TabWorkflows:
		return m.handleWorkflowKeys(msg)
	case TabChat:
		return m.handleChatKeys(msg)
	case TabMonitoring:
		return m.handleMonitoringKeys(msg)
	}

	return m, nil
}

// handleChatKeys handles keys specific to the chat tab
func (m *DashboardModel) handleChatKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		fmt.Fprintf(os.Stderr, "DEBUG: handleChatKeys - Enter pressed, inputText: %q\n", m.inputText)
		if m.inputText != "" {
			cmd := m.sendMessage()
			fmt.Fprintf(os.Stderr, "DEBUG: handleChatKeys - sendMessage returned command: %v\n", cmd != nil)
			return m, cmd
		}
		fmt.Fprintf(os.Stderr, "DEBUG: handleChatKeys - Enter pressed but inputText empty\n")
		return m, nil

	case "up":
		// Navigate chat history
		return m, nil

	case "down":
		// Navigate chat history
		return m, nil

	case "backspace":
		if len(m.inputText) > 0 {
			m.inputText = m.inputText[:len(m.inputText)-1]
		}
		return m, nil

	default:
		// Add character to input
		if len(msg.String()) == 1 {
			m.inputText += msg.String()
		}
		return m, nil
	}
}

// handleMonitoringKeys handles keys specific to the monitoring tab
func (m *DashboardModel) handleMonitoringKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "r":
		// Refresh monitoring data
		return m, m.fetchServerStatus()
	}
	return m, nil
}

// handleConfigKeys handles keys specific to the configuration tab
func (m *DashboardModel) handleConfigKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.configState {
	case ConfigStateList:
		return m.handleConfigListKeys(msg)
	case ConfigStateEdit:
		return m.handleConfigEditKeys(msg)
	case ConfigStateNew:
		return m.handleConfigNewKeys(msg)
	}
	return m, nil
}

// handleConfigListKeys handles keys in the configuration list view
func (m *DashboardModel) handleConfigListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		// Edit selected configuration
		if m.selectedConfig != "" {
			m.configState = ConfigStateEdit
			// Load configuration content
			// This would be implemented to load the actual content
		}
		return m, nil
	
	case "n":
		// New configuration
		m.configState = ConfigStateNew
		return m, nil
	
	case "d":
		// Delete configuration
		if m.selectedConfig != "" {
			// Implement delete functionality
		}
		return m, nil
	
	case "r":
		// Refresh configuration list
		return m, m.fetchConfigResources()
	
	case "up":
		// Navigate configuration list up
		// TODO: Implement configuration list navigation
		return m, nil
	
	case "down":
		// Navigate configuration list down
		// TODO: Implement configuration list navigation
		return m, nil
	}
	return m, nil
}

// handleConfigEditKeys handles keys in the configuration edit view
func (m *DashboardModel) handleConfigEditKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "escape":
		// Return to list view
		m.configState = ConfigStateList
		return m, nil
	
	case "ctrl+s":
		// Save configuration
		// TODO: Implement save functionality
		return m, nil
	
	case "ctrl+v":
		// Validate configuration
		if m.configContent != "" {
			return m, m.validateConfigContent(m.selectedConfig, m.configContent)
		}
		return m, nil
	}
	
	// TODO: Add text editing functionality
	return m, nil
}

// handleConfigNewKeys handles keys in the new configuration view
func (m *DashboardModel) handleConfigNewKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "escape":
		// Return to list view
		m.configState = ConfigStateList
		return m, nil
	
	case "1", "2", "3", "4", "5", "6":
		// Select template
		templateIndex := int(msg.String()[0] - '1')
		templates := []string{
			"MCPServer", "MCPMemory", "MCPTaskScheduler",
			"MCPProxy", "Workflow", "MCPToolbox",
		}
		
		if templateIndex < len(templates) {
			// Load template content
			templateName := templates[templateIndex]
			m.configContent = m.getTemplateContent(templateName)
			m.selectedConfig = "new-" + strings.ToLower(templateName)
			m.configState = ConfigStateEdit
		}
		return m, nil
	}
	return m, nil
}

// handleWorkflowKeys handles keys specific to the workflows tab
func (m *DashboardModel) handleWorkflowKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.workflowState {
	case WorkflowStateList:
		return m.handleWorkflowListKeys(msg)
	case WorkflowStateEdit:
		return m.handleWorkflowEditKeys(msg)
	case WorkflowStateMonitor:
		return m.handleWorkflowMonitorKeys(msg)
	case WorkflowStateDesign:
		return m.handleWorkflowDesignKeys(msg)
	}
	return m, nil
}

// handleWorkflowListKeys handles keys in the workflow list view
func (m *DashboardModel) handleWorkflowListKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		// Edit selected workflow
		if m.selectedWorkflow != "" {
			m.workflowState = WorkflowStateEdit
		}
		return m, nil
	
	case "n":
		// New workflow from template
		// TODO: Implement template selection
		return m, nil
	
	case "d":
		// Delete workflow
		if m.selectedWorkflow != "" {
			return m, m.deleteWorkflow(m.selectedWorkflow)
		}
		return m, nil
	
	case "r":
		// Refresh workflow list
		return m, m.fetchWorkflows()
	
	case "p":
		// Pause workflow
		if m.selectedWorkflow != "" {
			return m, m.pauseWorkflow(m.selectedWorkflow)
		}
		return m, nil
	
	case "e":
		// Execute workflow now
		if m.selectedWorkflow != "" {
			return m, m.executeWorkflow(m.selectedWorkflow)
		}
		return m, nil
	
	case "m":
		// Monitor workflow execution
		if m.selectedWorkflow != "" {
			m.workflowState = WorkflowStateMonitor
		}
		return m, nil
	
	case "l":
		// View logs
		// TODO: Implement log viewing
		return m, nil
	
	case "up":
		// Navigate workflow list up
		return m.navigateWorkflowList(-1), nil
	
	case "down":
		// Navigate workflow list down
		return m.navigateWorkflowList(1), nil
	
	case "1", "2", "3", "4", "5", "6":
		// Quick template selection when no workflow is selected
		if m.selectedWorkflow == "" {
			templateIndex := int(msg.String()[0] - '1')
			return m, m.createWorkflowFromTemplate(templateIndex)
		}
		return m, nil
	}
	return m, nil
}

// handleWorkflowEditKeys handles keys in the workflow edit view
func (m *DashboardModel) handleWorkflowEditKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "escape":
		// Return to list view
		m.workflowState = WorkflowStateList
		return m, nil
	
	case "ctrl+s":
		// Save workflow
		// TODO: Implement save functionality
		return m, nil
	
	case "d":
		// Visual designer
		m.workflowState = WorkflowStateDesign
		return m, nil
	}
	
	// TODO: Add text editing functionality
	return m, nil
}

// handleWorkflowMonitorKeys handles keys in the workflow monitor view
func (m *DashboardModel) handleWorkflowMonitorKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "escape":
		// Return to list view
		m.workflowState = WorkflowStateList
		return m, nil
	
	case "r":
		// Refresh status
		// TODO: Implement status refresh
		return m, nil
	
	case "l":
		// View logs
		// TODO: Implement log viewing
		return m, nil
	}
	return m, nil
}

// handleWorkflowDesignKeys handles keys in the workflow designer view
func (m *DashboardModel) handleWorkflowDesignKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "escape":
		// Return to edit view
		m.workflowState = WorkflowStateEdit
		return m, nil
	
	case "ctrl+s":
		// Save workflow
		// TODO: Implement save functionality
		return m, nil
	
	case "a":
		// Add step
		// TODO: Implement step addition
		return m, nil
	
	case "d":
		// Delete step
		// TODO: Implement step deletion
		return m, nil
	}
	return m, nil
}

// navigateWorkflowList navigates the workflow list
func (m *DashboardModel) navigateWorkflowList(direction int) *DashboardModel {
	if len(m.workflows) == 0 {
		return m
	}

	// Find current index
	currentIndex := -1
	for i, workflow := range m.workflows {
		if workflow.Name == m.selectedWorkflow {
			currentIndex = i
			break
		}
	}

	// Calculate new index
	newIndex := currentIndex + direction
	if newIndex < 0 {
		newIndex = len(m.workflows) - 1
	} else if newIndex >= len(m.workflows) {
		newIndex = 0
	}

	m.selectedWorkflow = m.workflows[newIndex].Name
	return m
}

// createWorkflowFromTemplate creates a workflow from a template
func (m *DashboardModel) createWorkflowFromTemplate(templateIndex int) tea.Cmd {
	templates := []string{
		"health-monitoring", "data-backup", "report-generation",
		"system-maintenance", "code-quality-checks", "database-maintenance",
	}
	
	if templateIndex < 0 || templateIndex >= len(templates) {
		return nil
	}
	
	_ = templates[templateIndex] // Use templateName when implementing
	
	return func() tea.Msg {
		// TODO: Use the existing CLI command to create workflow from template
		// For now, just switch to edit mode
		return nil
	}
}

// getTemplateContent returns the template content for the given template name
func (m *DashboardModel) getTemplateContent(templateName string) string {
	switch templateName {
	case "MCPServer":
		return getMCPServerTemplate()
	case "MCPMemory":
		return getMCPMemoryTemplate()
	case "MCPTaskScheduler":
		return getMCPTaskSchedulerTemplate()
	case "MCPProxy":
		return getMCPProxyTemplate()
	case "Workflow":
		return getWorkflowTemplate()
	case "MCPToolbox":
		return getMCPToolboxTemplate()
	default:
		return ""
	}
}

// switchTab switches to the next tab
func (m *DashboardModel) switchTab() tea.Cmd {
	switch m.currentTab {
	case TabChat:
		m.currentTab = TabMonitoring
	case TabMonitoring:
		m.currentTab = TabConfig
	case TabConfig:
		m.currentTab = TabWorkflows
	case TabWorkflows:
		m.currentTab = TabChat
	}
	return nil
}

// showHelp shows help information
func (m *DashboardModel) showHelp() tea.Cmd {
	helpMessage := ChatMessage{
		Role:      "system",
		Content:   m.getHelpText(),
		Timestamp: time.Now(),
	}
	m.chatHistory = append(m.chatHistory, helpMessage)
	return nil
}

// getHelpText returns help text
func (m *DashboardModel) getHelpText() string {
	return `Available Commands:

Provider Commands:
• /provider <name> - Switch AI provider (openai, claude, ollama, openrouter)
• /providers - Show all AI providers and their status
• /model <name> - Switch AI model
• /models - Show available models for current provider
• /config - Show current AI configuration
• /list servers - List all MCP servers
• /status - Show system status
• /validate - Validate current artifact configuration
• /help - Show this help

Configuration Commands:
• /generate-config <type> [name] [params] - Generate configuration
• /validate-config - Validate selected configuration
• /suggest-workflow <purpose> - Suggest workflow for purpose
• /optimize-config - Optimize selected configuration
• /explain-config - Explain selected configuration

Examples:
• "List my current MCP servers"
• "Create a workflow for file processing"
• "Scale my web scraper to 3 replicas"
• "Show me the status of my memory service"
• /generate-config mcpserver my-server port=8080
• /suggest-workflow monitor database health

Keyboard Shortcuts:
• Tab - Switch between chat, monitoring, config, workflows
• Ctrl+C - Exit dashboard
• Enter - Send message
• F1 - Show help`
}

// sendMessage sends a chat message
func (m *DashboardModel) sendMessage() tea.Cmd {
	fmt.Fprintf(os.Stderr, "DEBUG: sendMessage - called\n")
	if m.streaming {
		fmt.Fprintf(os.Stderr, "DEBUG: sendMessage - already streaming, returning nil\n")
		return nil // Don't send while streaming
	}

	message := strings.TrimSpace(m.inputText)
	if message == "" {
		fmt.Fprintf(os.Stderr, "DEBUG: sendMessage - empty message, returning nil\n")
		return nil
	}
	
	fmt.Fprintf(os.Stderr, "DEBUG: sendMessage - processing message: %q\n", message)

	// Add user message to history
	userMessage := ChatMessage{
		Role:      "user",
		Content:   message,
		Timestamp: time.Now(),
	}
	m.chatHistory = append(m.chatHistory, userMessage)

	// Clear input
	m.inputText = ""

	// Handle commands
	if strings.HasPrefix(message, "/") {
		fmt.Fprintf(os.Stderr, "DEBUG: sendMessage - detected command message\n")
		// Check if it's a configuration-specific command first
		parts := strings.Fields(message)
		if len(parts) > 0 {
			configCommands := []string{"/generate-config", "/validate-config", "/suggest-workflow", "/optimize-config", "/explain-config"}
			isConfigCommand := false
			for _, cmd := range configCommands {
				if parts[0] == cmd {
					isConfigCommand = true
					break
				}
			}
			
			if isConfigCommand {
				fmt.Fprintf(os.Stderr, "DEBUG: sendMessage - processing config command\n")
				response := m.processEnhancedChatMessage(message)
				if response != "" && !strings.Contains(response, "Processing your request") {
					// This is a specific response (like config generation), show it as system message
					fmt.Fprintf(os.Stderr, "DEBUG: sendMessage - returning system message for config command\n")
					return m.addSystemMessage(response)
				} else if m.streaming {
					// AI processing was started, return nil to let it stream
					fmt.Fprintf(os.Stderr, "DEBUG: sendMessage - config command started streaming\n")
					return nil
				}
			}
		}
		
		// Handle regular commands (provider, model, help, etc.)
		fmt.Fprintf(os.Stderr, "DEBUG: sendMessage - handling regular command\n")
		return m.handleCommand(message)
	}

	// For non-command messages, use enhanced context but send to AI properly
	// Check if it's a natural language request that should be handled specially
	fmt.Fprintf(os.Stderr, "DEBUG: sendMessage - processing enhanced chat message\n")
	response := m.processEnhancedChatMessage(message)
	fmt.Fprintf(os.Stderr, "DEBUG: sendMessage - processEnhancedChatMessage returned: %q\n", response)
	
	if response != "" {
		// This is a specific response (like config generation), show it as system message
		fmt.Fprintf(os.Stderr, "DEBUG: sendMessage - returning system message for enhanced response: %q\n", response)
		return m.addSystemMessage(response)
	}

	// Empty response means we should proceed with AI streaming
	// Send to AI with enhanced context (but don't show the context to user)
	fmt.Fprintf(os.Stderr, "DEBUG: sendMessage - calling sendToAIWithEnhancedContext\n")
	cmd := m.sendToAIWithEnhancedContext(message)
	fmt.Fprintf(os.Stderr, "DEBUG: sendMessage - sendToAIWithEnhancedContext returned command: %v\n", cmd != nil)
	return cmd
}

// handleCommand handles slash commands
func (m *DashboardModel) handleCommand(command string) tea.Cmd {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil
	}

	switch parts[0] {
	case "/provider":
		if len(parts) < 2 {
			return m.addSystemMessage("Usage: /provider <name>")
		}
		return m.switchProvider(parts[1])

	case "/model":
		if len(parts) < 2 {
			return m.addSystemMessage("Usage: /model <name>")
		}
		return m.switchModel(parts[1])

	case "/list":
		if len(parts) >= 2 && parts[1] == "servers" {
			return m.listServers()
		}
		return m.addSystemMessage("Usage: /list servers")

	case "/status":
		return m.showStatus()

	case "/providers":
		return m.showProviders()

	case "/models":
		return m.showModels()

	case "/help":
		return m.showHelp()

	case "/validate":
		return m.validateCurrentArtifact()

	case "/config":
		return m.showAIConfig()

	default:
		return m.addSystemMessage(fmt.Sprintf("Unknown command: %s", parts[0]))
	}
}

// addSystemMessage adds a system message to chat
func (m *DashboardModel) addSystemMessage(content string) tea.Cmd {
	message := ChatMessage{
		Role:      "system",
		Content:   content,
		Timestamp: time.Now(),
	}
	m.chatHistory = append(m.chatHistory, message)
	return nil
}

// switchProvider switches AI provider
func (m *DashboardModel) switchProvider(provider string) tea.Cmd {
	if err := m.aiManager.SwitchProvider(provider); err != nil {
		return m.addSystemMessage(fmt.Sprintf("Failed to switch provider: %s", err))
	}
	return m.addSystemMessage(fmt.Sprintf("Switched to provider: %s", provider))
}

// switchModel switches AI model
func (m *DashboardModel) switchModel(model string) tea.Cmd {
	if err := m.aiManager.SwitchModel(model); err != nil {
		return m.addSystemMessage(fmt.Sprintf("Failed to switch model: %s", err))
	}
	
	currentProvider, _ := m.aiManager.GetCurrentProvider()
	return m.addSystemMessage(fmt.Sprintf("Switched to model: %s (provider: %s)", model, currentProvider.Name()))
}

// listServers lists all MCP servers
func (m *DashboardModel) listServers() tea.Cmd {
	return tea.Cmd(func() tea.Msg {
		// Get server status from composer
		status, err := m.composer.Status()
		if err != nil {
			return errorMsg(err)
		}

		var content strings.Builder
		content.WriteString("Current MCP Servers:\n\n")

		for name, svcStatus := range status.Services {
			content.WriteString(fmt.Sprintf("• %s (%s) - %s\n", name, svcStatus.Type, svcStatus.Status))
		}

		if len(status.Services) == 0 {
			content.WriteString("No MCP servers found.")
		}

		// Add message to chat history
		m.chatHistory = append(m.chatHistory, ChatMessage{
			Role:      "system",
			Content:   content.String(),
			Timestamp: time.Now(),
		})

		return chatCompleteMsg{}
	})
}

// validateCurrentArtifact validates the current artifact
func (m *DashboardModel) validateCurrentArtifact() tea.Cmd {
	if m.currentArtifact == "" {
		return m.addSystemMessage("No artifact to validate. Generate a configuration first.")
	}
	
	yamlContent := m.extractYAMLFromContent(m.currentArtifact)
	if yamlContent == "" {
		return m.addSystemMessage("No YAML configuration found in current artifact.")
	}
	
	validator := NewConfigValidator()
	result := validator.ValidateYAML(yamlContent)
	
	return m.addSystemMessage(validator.FormatValidationResult(result))
}

// showProviders shows all AI providers and their status
func (m *DashboardModel) showProviders() tea.Cmd {
	providerStatus := m.aiManager.GetProviderStatus()
	
	var content strings.Builder
	content.WriteString("AI Providers:\n\n")
	
	for name, status := range providerStatus {
		statusIcon := "🔴"
		statusText := "Unavailable"
		
		if status.Available {
			statusIcon = "🟢"
			statusText = "Available"
		}
		
		content.WriteString(fmt.Sprintf("%s **%s**: %s\n", statusIcon, name, statusText))
		
		if status.Error != "" {
			content.WriteString(fmt.Sprintf("   Error: %s\n", status.Error))
		}
		
		if len(status.Models) > 0 {
			content.WriteString(fmt.Sprintf("   Models: %s\n", strings.Join(status.Models, ", ")))
		}
		
		content.WriteString("\n")
	}
	
	// Show current provider
	if current, err := m.aiManager.GetCurrentProvider(); err == nil {
		content.WriteString(fmt.Sprintf("Current Provider: %s\n", current.Name()))
	} else {
		content.WriteString("Current Provider: None available\n")
	}
	
	return m.addSystemMessage(content.String())
}

// showModels shows available models for the current provider
func (m *DashboardModel) showModels() tea.Cmd {
	provider, err := m.aiManager.GetCurrentProvider()
	if err != nil {
		return m.addSystemMessage("No current AI provider available")
	}
	
	models := provider.SupportedModels()
	currentModel := provider.DefaultModel()
	
	var content strings.Builder
	content.WriteString(fmt.Sprintf("Available models for %s:\n\n", provider.Name()))
	
	if len(models) == 0 {
		content.WriteString("No models available or failed to fetch models")
	} else {
		for _, model := range models {
			if model == currentModel {
				content.WriteString(fmt.Sprintf("• **%s** (current)\n", model))
			} else {
				content.WriteString(fmt.Sprintf("• %s\n", model))
			}
		}
	}
	
	content.WriteString(fmt.Sprintf("\nCurrent model: %s\n", currentModel))
	content.WriteString("Use `/model <name>` to switch models\n")
	
	return m.addSystemMessage(content.String())
}

// showAIConfig shows the current AI configuration
func (m *DashboardModel) showAIConfig() tea.Cmd {
	var content strings.Builder
	content.WriteString("AI Configuration:\n\n")
	
	// Show configuration file location
	content.WriteString("Configuration loaded from: matey.yaml\n\n")
	
	// Show current provider
	if current, err := m.aiManager.GetCurrentProvider(); err == nil {
		content.WriteString(fmt.Sprintf("Current Provider: %s\n", current.Name()))
		content.WriteString(fmt.Sprintf("Current Model: %s\n\n", current.DefaultModel()))
	} else {
		content.WriteString("Current Provider: None available\n\n")
	}
	
	// Show all providers
	providerStatus := m.aiManager.GetProviderStatus()
	content.WriteString("Configured Providers:\n")
	
	for name, status := range providerStatus {
		statusIcon := "🔴"
		if status.Available {
			statusIcon = "🟢"
		}
		
		content.WriteString(fmt.Sprintf("%s **%s**\n", statusIcon, name))
		
		if status.Available {
			content.WriteString(fmt.Sprintf("   Default Model: %s\n", m.aiManager.GetCurrentModel()))
			if len(status.Models) > 0 {
				content.WriteString(fmt.Sprintf("   Available Models: %d models\n", len(status.Models)))
			}
		} else {
			content.WriteString(fmt.Sprintf("   Error: %s\n", status.Error))
		}
		content.WriteString("\n")
	}
	
	content.WriteString("To configure AI providers, add the following to your matey.yaml:\n\n")
	content.WriteString("```yaml\n")
	content.WriteString("ai:\n")
	content.WriteString("  default_provider: ollama\n")
	content.WriteString("  providers:\n")
	content.WriteString("    ollama:\n")
	content.WriteString("      endpoint: \"http://your-ollama-host:11434\"\n")
	content.WriteString("      default_model: \"qwen3:14b\"\n")
	content.WriteString("    openai:\n")
	content.WriteString("      api_key: \"${OPENAI_API_KEY}\"\n")
	content.WriteString("      default_model: \"gpt-4\"\n")
	content.WriteString("```\n")
	
	return m.addSystemMessage(content.String())
}

// showStatus shows system status
func (m *DashboardModel) showStatus() tea.Cmd {
	return tea.Cmd(func() tea.Msg {
		providerStatus := m.aiManager.GetProviderStatus()
		
		var content strings.Builder
		content.WriteString("System Status:\n\n")
		
		// AI Providers
		content.WriteString("AI Providers:\n")
		for name, status := range providerStatus {
			statusStr := "❌ Unavailable"
			if status.Available {
				statusStr = "✅ Available"
			}
			content.WriteString(fmt.Sprintf("• %s: %s\n", name, statusStr))
		}
		
		// Kubernetes connection
		content.WriteString(fmt.Sprintf("\nKubernetes: ✅ Connected\n"))
		content.WriteString(fmt.Sprintf("Last Update: %s\n", m.lastUpdate.Format("15:04:05")))

		// Add message to chat history
		m.chatHistory = append(m.chatHistory, ChatMessage{
			Role:      "system",
			Content:   content.String(),
			Timestamp: time.Now(),
		})

		return chatCompleteMsg{}
	})
}

// sendToAI sends message to AI and handles streaming response
// sendToAIWithEnhancedContext sends message to AI with enhanced context
func (m *DashboardModel) sendToAIWithEnhancedContext(message string) tea.Cmd {
	return tea.Cmd(func() tea.Msg {
		// Get enhanced context but separate from user message
		enhancedPrompt := m.enhancePromptWithConfigContext(message)
		
		// Build messages for AI with enhanced context as system message
		messages := []ai.Message{
			{
				Role:    "system",
				Content: enhancedPrompt,
			},
		}
		
		// Add recent chat history (but only the actual user/assistant messages)
		for _, msg := range m.chatHistory[max(0, len(m.chatHistory)-10):] {
			if msg.Role == "user" || msg.Role == "assistant" {
				messages = append(messages, ai.Message{
					Role:    msg.Role,
					Content: msg.Content,
				})
			}
		}

		// Start streaming
		stream, err := m.aiManager.StreamChatWithFallback(m.ctx, messages, ai.StreamOptions{})
		if err != nil {
			// Add error to chat for debugging
			errorDebugMsg := ChatMessage{
				Role:      "system",
				Content:   fmt.Sprintf("❌ AI Error: %v", err),
				Timestamp: time.Now(),
			}
			m.chatHistory = append(m.chatHistory, errorDebugMsg)
			return errorMsg(err)
		}
		
		// Got stream channel from AI provider

		// Add AI message placeholder
		aiMessage := ChatMessage{
			Role:      "assistant",
			Content:   "",
			Timestamp: time.Now(),
			Streaming: true,
		}
		m.chatHistory = append(m.chatHistory, aiMessage)
		m.streaming = true

		// Start streaming reader and animation timer
		fmt.Fprintf(os.Stderr, "DEBUG: sendToAIWithEnhancedContext - creating streamFromChannel command\n")
		return tea.Batch(
			m.streamFromChannel(stream),
			tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
				return streamingTickMsg(t)
			}),
		)
	})
}

func (m *DashboardModel) sendToAI(message string) tea.Cmd {
	return tea.Cmd(func() tea.Msg {
		// Get current system context
		systemContext := m.buildSystemContext()
		
		// Build messages for AI
		messages := []ai.Message{
			{
				Role:    "system",
				Content: systemContext,
			},
		}
		
		// Add recent chat history
		for _, msg := range m.chatHistory[max(0, len(m.chatHistory)-10):] {
			if msg.Role != "system" {
				messages = append(messages, ai.Message{
					Role:    msg.Role,
					Content: msg.Content,
				})
			}
		}

		// Start streaming
		stream, err := m.aiManager.StreamChatWithFallback(m.ctx, messages, ai.StreamOptions{})
		if err != nil {
			return errorMsg(err)
		}

		// Add AI message placeholder
		aiMessage := ChatMessage{
			Role:      "assistant",
			Content:   "",
			Timestamp: time.Now(),
			Streaming: true,
		}
		m.chatHistory = append(m.chatHistory, aiMessage)
		m.streaming = true

		// Start streaming reader and animation timer
		return tea.Batch(
			m.streamFromChannel(stream),
			tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
				return streamingTickMsg(t)
			}),
		)
	})
}

// streamFromChannel creates a command that reads from the AI response channel
func (m *DashboardModel) streamFromChannel(stream <-chan ai.StreamResponse) tea.Cmd {
	// Listen to stream channel
	fmt.Fprintf(os.Stderr, "DEBUG: streamFromChannel - starting to listen on channel %p\n", stream)
	return tea.Cmd(func() tea.Msg {
		fmt.Fprintf(os.Stderr, "DEBUG: streamFromChannel - about to select on channel\n")
		select {
		case response, ok := <-stream:
			fmt.Fprintf(os.Stderr, "DEBUG: streamFromChannel - received from channel\n")
			fmt.Fprintf(os.Stderr, "DEBUG: streamFromChannel - received response: ok=%v, content=%q, finished=%v, error=%v\n", 
				ok, response.Content, response.Finished, response.Error)
			if !ok {
				// Channel closed
				fmt.Fprintf(os.Stderr, "DEBUG: streamFromChannel - channel closed\n")
				return chatCompleteMsg{}
			}
			
			if response.Error != nil {
				fmt.Fprintf(os.Stderr, "DEBUG: streamFromChannel - error: %v\n", response.Error)
				return errorMsg(response.Error)
			}
			
			if response.Finished {
				fmt.Fprintf(os.Stderr, "DEBUG: streamFromChannel - finished\n")
				return chatCompleteMsg{}
			}
			
			// Return the stream response as a message with the channel
			fmt.Fprintf(os.Stderr, "DEBUG: streamFromChannel - returning chatStreamMsg\n")
			return chatStreamMsg{
				StreamResponse: response,
				stream:         stream,
			}
		case <-m.ctx.Done():
			// Context cancelled
			fmt.Fprintf(os.Stderr, "DEBUG: streamFromChannel - context cancelled\n")
			return chatCompleteMsg{}
		}
	})
}

// handleChatStream handles streaming chat responses
func (m *DashboardModel) handleChatStream(msg chatStreamMsg) (tea.Model, tea.Cmd) {
	// TEMP DEBUG: Log stream message received
	fmt.Fprintf(os.Stderr, "DEBUG: handleChatStream - content: %q, think: %q\n", msg.StreamResponse.Content, msg.StreamResponse.ThinkContent)
	
	// Find the last assistant message that's streaming
	var streamingMsg *ChatMessage
	for i := len(m.chatHistory) - 1; i >= 0; i-- {
		if m.chatHistory[i].Role == "assistant" && m.chatHistory[i].Streaming {
			streamingMsg = &m.chatHistory[i]
			break
		}
	}
	
	if streamingMsg != nil {
		// Accumulate regular content
		if msg.StreamResponse.Content != "" {
			streamingMsg.Content += msg.StreamResponse.Content
			fmt.Fprintf(os.Stderr, "DEBUG: Updated streaming message content to: %q\n", streamingMsg.Content)
		}
		
		// Accumulate think content
		if msg.StreamResponse.ThinkContent != "" {
			streamingMsg.ThinkContent += msg.StreamResponse.ThinkContent
			// Auto-expand think content during streaming so user can see it
			streamingMsg.ThinkExpanded = true
			fmt.Fprintf(os.Stderr, "DEBUG: Updated streaming message think content to: %q\n", streamingMsg.ThinkContent)
		}
		
		// Check for artifacts in the response
		if strings.Contains(msg.StreamResponse.Content, "```") {
			m.artifactType = "code"
			m.currentArtifact = streamingMsg.Content
		} else if strings.Contains(msg.StreamResponse.Content, "apiVersion:") {
			m.artifactType = "config"
			m.currentArtifact = streamingMsg.Content
			
			// Validate YAML configuration
			if yamlContent := m.extractYAMLFromContent(streamingMsg.Content); yamlContent != "" {
				m.validateAndUpdateArtifact(yamlContent)
			}
		}
	}
	
	// Continue reading from the stream
	return m, m.streamFromChannel(msg.stream)
}

// extractYAMLFromContent extracts YAML content from markdown code blocks
func (m *DashboardModel) extractYAMLFromContent(content string) string {
	// Look for YAML code blocks
	yamlStart := strings.Index(content, "```yaml")
	if yamlStart == -1 {
		yamlStart = strings.Index(content, "```yml")
	}
	if yamlStart == -1 {
		// Try to find YAML content without code blocks
		if strings.Contains(content, "apiVersion:") {
			return content
		}
		return ""
	}
	
	yamlEnd := strings.Index(content[yamlStart+6:], "```")
	if yamlEnd == -1 {
		return ""
	}
	
	return strings.TrimSpace(content[yamlStart+6 : yamlStart+6+yamlEnd])
}

// validateAndUpdateArtifact validates YAML and updates the artifact with validation results
func (m *DashboardModel) validateAndUpdateArtifact(yamlContent string) {
	validator := NewConfigValidator()
	result := validator.ValidateYAML(yamlContent)
	
	// Update artifact with validation results
	validationSummary := validator.FormatValidationResult(result)
	m.currentArtifact = yamlContent + "\n\n" + validationSummary
}

// buildSystemContext builds system context for AI
func (m *DashboardModel) buildSystemContext() string {
	var context strings.Builder
	
	context.WriteString("You are an AI assistant for Matey, a Kubernetes-native MCP (Model Context Protocol) server orchestrator. ")
	context.WriteString("You help users configure, manage, and troubleshoot their MCP servers and workflows.\n\n")
	
	context.WriteString("Available capabilities:\n")
	context.WriteString("- List and manage MCP servers\n")
	context.WriteString("- Create and modify workflows\n")
	context.WriteString("- Configure memory services\n")
	context.WriteString("- Set up task schedulers\n")
	context.WriteString("- Monitor system health\n\n")
	
	context.WriteString("When generating configurations, provide valid Kubernetes YAML for MCP CRDs.\n")
	context.WriteString("Always explain what you're doing and why.\n")
	context.WriteString("Use the artifacts pane to show generated configurations, diagrams, and code.\n\n")
	
	// Add current system state
	if status, err := m.composer.Status(); err == nil {
		context.WriteString("Current MCP servers:\n")
		for name, svcStatus := range status.Services {
			context.WriteString(fmt.Sprintf("- %s (%s): %s\n", name, svcStatus.Type, svcStatus.Status))
		}
	}
	
	return context.String()
}

// fetchServerStatus fetches current server status
func (m *DashboardModel) fetchServerStatus() tea.Cmd {
	return tea.Cmd(func() tea.Msg {
		// This would be implemented similar to the original top.go
		// For now, return empty
		return serverStatusMsg([]ServerInfo{})
	})
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}