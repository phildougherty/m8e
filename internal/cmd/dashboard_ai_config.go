package cmd

import (
	"fmt"
	"strings"
	
	tea "github.com/charmbracelet/bubbletea"
)

// AI Configuration Assistant functionality

// ConfigAwarePrompt represents an AI prompt with configuration context
type ConfigAwarePrompt struct {
	UserMessage      string                 `json:"userMessage"`
	ConfigContext    []ConfigResource       `json:"configContext"`
	WorkflowContext  []WorkflowInfo         `json:"workflowContext"`
	SystemContext    map[string]interface{} `json:"systemContext"`
}

// enhancePromptWithConfigContext enhances user prompts with configuration context
func (m *DashboardModel) enhancePromptWithConfigContext(userMessage string) string {
	var contextParts []string
	
	// Add system context
	contextParts = append(contextParts, "You are Matey, an AI assistant for Kubernetes-native MCP (Model Context Protocol) server orchestration.")
	contextParts = append(contextParts, "You help users configure and manage MCP servers, workflows, and related resources using Kubernetes CRDs.")
	contextParts = append(contextParts, "")
	
	// Add current configuration context
	if len(m.getConfigResources()) > 0 {
		contextParts = append(contextParts, "Current Configurations:")
		for _, config := range m.getConfigResources() {
			status := "valid"
			if !config.Valid {
				status = fmt.Sprintf("invalid (%d errors)", len(config.Errors))
			}
			contextParts = append(contextParts, fmt.Sprintf("- %s/%s (%s) - %s", config.Kind, config.Name, config.Namespace, status))
		}
		contextParts = append(contextParts, "")
	}
	
	// Add current workflow context
	if len(m.workflows) > 0 {
		contextParts = append(contextParts, "Current Workflows:")
		for _, workflow := range m.workflows {
			enabled := "enabled"
			if !workflow.Enabled {
				enabled = "disabled"
			}
			contextParts = append(contextParts, fmt.Sprintf("- %s (%s) - %s, %d steps", workflow.Name, workflow.Phase, enabled, workflow.StepCount))
		}
		contextParts = append(contextParts, "")
	}
	
	// Add available templates
	contextParts = append(contextParts, "Available Configuration Templates:")
	configTemplates := []string{
		"MCPServer - MCP Server configuration",
		"MCPMemory - Memory service configuration", 
		"MCPTaskScheduler - Task scheduler configuration",
		"MCPProxy - Proxy configuration",
		"Workflow - Workflow configuration",
		"MCPToolbox - Toolbox configuration",
	}
	for _, template := range configTemplates {
		contextParts = append(contextParts, fmt.Sprintf("- %s", template))
	}
	contextParts = append(contextParts, "")
	
	contextParts = append(contextParts, "Available Workflow Templates:")
	workflowTemplates := []string{
		"health-monitoring - Monitor health metrics and send alerts",
		"data-backup - Automated data backup with cloud upload",
		"report-generation - Generate and distribute reports",
		"system-maintenance - System cleanup and maintenance",
		"code-quality-checks - Code linting, testing, and security",
		"database-maintenance - Database backup and optimization",
	}
	for _, template := range workflowTemplates {
		contextParts = append(contextParts, fmt.Sprintf("- %s", template))
	}
	contextParts = append(contextParts, "")
	
	// Add guidelines
	contextParts = append(contextParts, "Guidelines:")
	contextParts = append(contextParts, "- Always provide complete, valid YAML configurations")
	contextParts = append(contextParts, "- Use the apiVersion 'mcp.matey.ai/v1' for all MCP resources")
	contextParts = append(contextParts, "- Include proper metadata with name and namespace")
	contextParts = append(contextParts, "- Validate configurations against Kubernetes best practices")
	contextParts = append(contextParts, "- Suggest appropriate resource limits and security settings")
	contextParts = append(contextParts, "- Explain the purpose and benefits of configurations")
	contextParts = append(contextParts, "")
	
	// Combine context with user message
	fullPrompt := strings.Join(contextParts, "\n") + "User Request: " + userMessage
	
	return fullPrompt
}

// handleConfigurationCommand handles AI commands specific to configuration
func (m *DashboardModel) handleConfigurationCommand(command string, args []string) (string, bool) {
	switch command {
	case "/generate-config":
		if len(args) < 1 {
			return "Usage: /generate-config <type> [name] [parameters...]\nExample: /generate-config mcpserver my-server port=8080", true
		}
		return m.generateConfigWithAI(args[0], args[1:])
	
	case "/validate-config":
		if m.selectedConfig == "" {
			return "No configuration selected. Please select a configuration first.", true
		}
		return m.validateConfigWithAI(m.selectedConfig, m.configContent)
	
	case "/suggest-workflow":
		if len(args) < 1 {
			return "Usage: /suggest-workflow <purpose>\nExample: /suggest-workflow monitor database health", true
		}
		return m.suggestWorkflowWithAI(strings.Join(args, " "))
	
	case "/optimize-config":
		if m.selectedConfig == "" {
			return "No configuration selected. Please select a configuration first.", true
		}
		return m.optimizeConfigWithAI(m.selectedConfig, m.configContent)
	
	case "/explain-config":
		if m.selectedConfig == "" {
			return "No configuration selected. Please select a configuration first.", true
		}
		return m.explainConfigWithAI(m.selectedConfig, m.configContent)
	
	default:
		return "", false
	}
}

// generateConfigWithAI generates configuration using AI
func (m *DashboardModel) generateConfigWithAI(configType string, args []string) (string, bool) {
	// Parse arguments into parameters
	params := make(map[string]string)
	var name string
	
	for i, arg := range args {
		if strings.Contains(arg, "=") {
			parts := strings.SplitN(arg, "=", 2)
			params[parts[0]] = parts[1]
		} else if i == 0 {
			name = arg
		}
	}
	
	if name == "" {
		name = "example-" + strings.ToLower(configType)
	}
	
	prompt := fmt.Sprintf("Generate a complete %s configuration named '%s'", configType, name)
	if len(params) > 0 {
		prompt += " with the following parameters:"
		for key, value := range params {
			prompt += fmt.Sprintf(" %s=%s", key, value)
		}
	}
	prompt += ". Include all required fields and follow best practices."
	
	// Send to AI and mark as artifact (we return the command separately)
	m.sendAIMessage(prompt, "config")
	
	return fmt.Sprintf("Generating %s configuration...", configType), true
}

// validateConfigWithAI validates configuration using AI
func (m *DashboardModel) validateConfigWithAI(name, content string) (string, bool) {
	prompt := fmt.Sprintf("Please validate this %s configuration and suggest improvements:\n\n%s", name, content)
	
	m.sendAIMessage(prompt, "validation")
	
	return "Validating configuration with AI...", true
}

// suggestWorkflowWithAI suggests workflows using AI
func (m *DashboardModel) suggestWorkflowWithAI(purpose string) (string, bool) {
	prompt := fmt.Sprintf("Suggest and create a workflow for: %s. Include appropriate steps, schedule, and configuration.", purpose)
	
	m.sendAIMessage(prompt, "workflow")
	
	return "Suggesting workflow configuration...", true
}

// optimizeConfigWithAI optimizes configuration using AI
func (m *DashboardModel) optimizeConfigWithAI(name, content string) (string, bool) {
	prompt := fmt.Sprintf("Optimize this %s configuration for performance, security, and best practices:\n\n%s", name, content)
	
	m.sendAIMessage(prompt, "optimization")
	
	return "Optimizing configuration with AI...", true
}

// explainConfigWithAI explains configuration using AI
func (m *DashboardModel) explainConfigWithAI(name, content string) (string, bool) {
	prompt := fmt.Sprintf("Explain this %s configuration in detail, including its purpose, key settings, and how it works:\n\n%s", name, content)
	
	m.sendAIMessage(prompt, "explanation")
	
	return "Explaining configuration...", true
}

// sendAIMessage sends a message to AI with proper artifact typing
func (m *DashboardModel) sendAIMessage(message, artifactType string) tea.Cmd {
	// Set artifact type for proper rendering
	m.artifactType = artifactType
	
	// Set streaming state to true since we're starting an AI request
	m.streaming = true
	
	// Use the enhanced context function to send to AI
	// This will handle the streaming and proper context
	return m.sendToAIWithEnhancedContext(message)
}

// Smart configuration suggestions based on context
func (m *DashboardModel) getSmartSuggestions() []string {
	var suggestions []string
	
	// Analyze current state and suggest improvements
	configs := m.getConfigResources()
	
	// Check for missing essential configurations
	hasServer := false
	hasMemory := false
	hasProxy := false
	
	for _, config := range configs {
		switch config.Kind {
		case "MCPServer":
			hasServer = true
		case "MCPMemory":
			hasMemory = true
		case "MCPProxy":
			hasProxy = true
		}
	}
	
	if !hasServer {
		suggestions = append(suggestions, "💡 Consider creating an MCPServer to handle MCP requests")
	}
	
	if hasServer && !hasMemory {
		suggestions = append(suggestions, "💡 Add MCPMemory for persistent storage and caching")
	}
	
	if hasServer && !hasProxy {
		suggestions = append(suggestions, "💡 Add MCPProxy for load balancing and high availability")
	}
	
	// Check for invalid configurations
	invalidCount := 0
	for _, config := range configs {
		if !config.Valid {
			invalidCount++
		}
	}
	
	if invalidCount > 0 {
		suggestions = append(suggestions, fmt.Sprintf("⚠️ Fix %d invalid configuration(s)", invalidCount))
	}
	
	// Check workflow suggestions
	if len(m.workflows) == 0 {
		suggestions = append(suggestions, "🔄 Create workflows to automate common tasks")
	}
	
	enabledWorkflows := 0
	for _, workflow := range m.workflows {
		if workflow.Enabled {
			enabledWorkflows++
		}
	}
	
	if len(m.workflows) > 0 && enabledWorkflows == 0 {
		suggestions = append(suggestions, "▶️ Enable workflows to start automation")
	}
	
	return suggestions
}

// Enhanced chat message processing with configuration awareness
func (m *DashboardModel) processEnhancedChatMessage(message string) string {
	// Check for configuration-related commands
	if strings.HasPrefix(message, "/") {
		parts := strings.Fields(message)
		command := parts[0]
		args := parts[1:]
		
		if response, handled := m.handleConfigurationCommand(command, args); handled {
			return response
		}
	}
	
	// Check for natural language configuration requests
	lowerMessage := strings.ToLower(message)
	
	// Detect configuration generation requests
	if strings.Contains(lowerMessage, "create") && (strings.Contains(lowerMessage, "config") || strings.Contains(lowerMessage, "server") || strings.Contains(lowerMessage, "workflow")) {
		if strings.Contains(lowerMessage, "server") {
			response, _ := m.generateConfigWithAI("MCPServer", []string{})
			return response
		} else if strings.Contains(lowerMessage, "workflow") {
			response, _ := m.suggestWorkflowWithAI(message)
			return response
		}
	}
	
	// Detect validation requests
	if strings.Contains(lowerMessage, "validate") || strings.Contains(lowerMessage, "check") {
		if m.selectedConfig != "" {
			response, _ := m.validateConfigWithAI(m.selectedConfig, m.configContent)
			return response
		}
	}
	
	// Detect optimization requests
	if strings.Contains(lowerMessage, "optimize") || strings.Contains(lowerMessage, "improve") {
		if m.selectedConfig != "" {
			response, _ := m.optimizeConfigWithAI(m.selectedConfig, m.configContent)
			return response
		}
	}
	
	// Use enhanced prompt for general messages - don't call sendAIMessage here
	// Instead, let the caller handle streaming
	return ""
}