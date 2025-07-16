package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Configuration management messages
type (
	configListMsg      []ConfigResource
	configContentMsg   struct {
		name    string
		content string
	}
	configValidationMsg struct {
		name   string
		valid  bool
		errors []string
	}
)

// fetchConfigResources fetches all MCP configuration resources from Kubernetes
func (m *DashboardModel) fetchConfigResources() tea.Cmd {
	return func() tea.Msg {
		var resources []ConfigResource
		
		// Define the CRDs we want to fetch
		crdTypes := []struct {
			group    string
			version  string
			resource string
			kind     string
		}{
			{"mcp.matey.ai", "v1", "mcpservers", "MCPServer"},
			{"mcp.matey.ai", "v1", "mcpmemories", "MCPMemory"},
			{"mcp.matey.ai", "v1", "mcptaskschedulers", "MCPTaskScheduler"},
			{"mcp.matey.ai", "v1", "mcpproxies", "MCPProxy"},
			{"mcp.matey.ai", "v1", "workflows", "Workflow"},
			{"mcp.matey.ai", "v1", "mcptoolboxes", "MCPToolbox"},
		}

		// Create dynamic client for CRDs
		dynamicClient, err := createDynamicClient()
		if err != nil {
			return errorMsg(fmt.Errorf("failed to create dynamic client: %w", err))
		}

		for _, crdType := range crdTypes {
			gvr := schema.GroupVersionResource{
				Group:    crdType.group,
				Version:  crdType.version,
				Resource: crdType.resource,
			}

			list, err := dynamicClient.Resource(gvr).Namespace("").List(context.Background(), metav1.ListOptions{})
			if err != nil {
				// Skip CRDs that don't exist or can't be accessed
				continue
			}

			for _, item := range list.Items {
				content, err := yaml.Marshal(item.Object)
				if err != nil {
					continue
				}

				resource := ConfigResource{
					Name:      item.GetName(),
					Namespace: item.GetNamespace(),
					Kind:      crdType.kind,
					Content:   string(content),
					Modified:  item.GetCreationTimestamp().Time,
					Valid:     true, // We'll validate separately
				}

				resources = append(resources, resource)
			}
		}

		// Sort by modified time (most recent first)
		sort.Slice(resources, func(i, j int) bool {
			return resources[i].Modified.After(resources[j].Modified)
		})

		return configListMsg(resources)
	}
}

// validateConfigContent validates YAML configuration content
func (m *DashboardModel) validateConfigContent(name, content string) tea.Cmd {
	return func() tea.Msg {
		errors := validateYAMLConfig(content)
		return configValidationMsg{
			name:   name,
			valid:  len(errors) == 0,
			errors: errors,
		}
	}
}

// validateYAMLConfig validates YAML configuration using the existing validation system
func validateYAMLConfig(content string) []string {
	validator := NewConfigValidator()
	result := validator.ValidateYAML(content)
	
	var errors []string
	for _, err := range result.Errors {
		errors = append(errors, fmt.Sprintf("%s: %s", err.Path, err.Message))
	}
	
	return errors
}

// Enhanced renderConfigView with proper configuration management
func (m *DashboardModel) renderConfigView() string {
	var content strings.Builder

	// Header
	content.WriteString(m.renderTabs())
	content.WriteString("\n\n")

	// Calculate layout dimensions
	contentHeight := m.height - 6 // Reserve space for tabs and input
	leftWidth := m.width/3 - 1
	rightWidth := m.width*2/3 - 1

	switch m.configState {
	case ConfigStateList:
		content.WriteString(m.renderConfigListView(leftWidth, rightWidth, contentHeight))
	case ConfigStateEdit:
		content.WriteString(m.renderConfigEditView(leftWidth, rightWidth, contentHeight))
	case ConfigStateNew:
		content.WriteString(m.renderConfigNewView(leftWidth, rightWidth, contentHeight))
	}

	return content.String()
}

// renderConfigListView renders the configuration list view
func (m *DashboardModel) renderConfigListView(leftWidth, rightWidth, contentHeight int) string {
	var leftPane, rightPane strings.Builder

	// Left pane: Configuration list
	leftPane.WriteString(DashboardStyles.UserMessage.Render("⚙️ Configurations"))
	leftPane.WriteString("\n\n")

	// Configuration list
	configs := m.getConfigResources() // This would fetch from state
	if len(configs) == 0 {
		leftPane.WriteString(DashboardStyles.SystemMessage.Render("No configurations found.\nPress 'n' to create a new one."))
	} else {
		for _, config := range configs {
			var style lipgloss.Style
			if config.Name == m.selectedConfig {
				style = DashboardStyles.ConfigSelected
			} else {
				style = DashboardStyles.ConfigList
			}

			status := "✅"
			if !config.Valid {
				status = "❌"
			}

			configItem := fmt.Sprintf("%s %s/%s (%s)", status, config.Kind, config.Name, config.Namespace)
			if len(config.Errors) > 0 {
				configItem += fmt.Sprintf(" - %d errors", len(config.Errors))
			}

			leftPane.WriteString(style.Render(configItem))
			leftPane.WriteString("\n")
		}
	}

	// Right pane: Configuration details/preview
	rightPane.WriteString(DashboardStyles.UserMessage.Render("📄 Configuration Details"))
	rightPane.WriteString("\n\n")

	if m.selectedConfig != "" {
		selectedResource := m.getSelectedConfigResource()
		if selectedResource != nil {
			// Show configuration metadata
			rightPane.WriteString(fmt.Sprintf("Name: %s\n", selectedResource.Name))
			rightPane.WriteString(fmt.Sprintf("Namespace: %s\n", selectedResource.Namespace))
			rightPane.WriteString(fmt.Sprintf("Kind: %s\n", selectedResource.Kind))
			rightPane.WriteString(fmt.Sprintf("Modified: %s\n", selectedResource.Modified.Format(time.RFC3339)))
			rightPane.WriteString("\n")

			// Show validation status
			if selectedResource.Valid {
				rightPane.WriteString(DashboardStyles.ConfigValid.Render("✅ Configuration is valid"))
			} else {
				rightPane.WriteString(DashboardStyles.ConfigInvalid.Render("❌ Configuration has errors:"))
				rightPane.WriteString("\n")
				for _, err := range selectedResource.Errors {
					rightPane.WriteString(DashboardStyles.ConfigInvalid.Render(fmt.Sprintf("  • %s", err)))
					rightPane.WriteString("\n")
				}
			}
			rightPane.WriteString("\n")

			// Show content preview (first 10 lines)
			lines := strings.Split(selectedResource.Content, "\n")
			previewLines := 10
			if len(lines) < previewLines {
				previewLines = len(lines)
			}

			rightPane.WriteString("Preview:\n")
			preview := strings.Join(lines[:previewLines], "\n")
			if len(lines) > previewLines {
				preview += "\n... (truncated)"
			}
			rightPane.WriteString(DashboardStyles.ConfigEditor.Render(preview))
		}
	} else {
		rightPane.WriteString(DashboardStyles.SystemMessage.Render("Select a configuration to view details"))
	}

	// Actions
	rightPane.WriteString("\n\nActions:")
	rightPane.WriteString("\n• Enter: Edit configuration")
	rightPane.WriteString("\n• n: New configuration")
	rightPane.WriteString("\n• d: Delete configuration")
	rightPane.WriteString("\n• r: Refresh list")

	// Combine panes
	leftPaneStyled := DashboardStyles.ConfigList.Width(leftWidth).Height(contentHeight).Render(leftPane.String())
	rightPaneStyled := DashboardStyles.ConfigEditor.Width(rightWidth).Height(contentHeight).Render(rightPane.String())

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPaneStyled, rightPaneStyled)
}

// renderConfigEditView renders the configuration edit view
func (m *DashboardModel) renderConfigEditView(leftWidth, rightWidth, contentHeight int) string {
	var content strings.Builder

	content.WriteString(DashboardStyles.UserMessage.Render(fmt.Sprintf("✏️ Editing: %s", m.selectedConfig)))
	content.WriteString("\n\n")

	// YAML editor (simplified - in a real implementation you'd want proper text editing)
	content.WriteString("YAML Editor:")
	content.WriteString("\n")
	content.WriteString(DashboardStyles.ConfigEditor.Width(m.width-4).Height(contentHeight-8).Render(m.configContent))

	// Validation status
	if len(m.configErrors) > 0 {
		content.WriteString("\n")
		content.WriteString(DashboardStyles.ConfigInvalid.Render("Validation Errors:"))
		content.WriteString("\n")
		for _, err := range m.configErrors {
			content.WriteString(DashboardStyles.ConfigInvalid.Render(fmt.Sprintf("• %s", err)))
			content.WriteString("\n")
		}
	} else {
		content.WriteString("\n")
		content.WriteString(DashboardStyles.ConfigValid.Render("✅ Configuration is valid"))
		content.WriteString("\n")
	}

	content.WriteString("\nActions:")
	content.WriteString("\n• Ctrl+S: Save configuration")
	content.WriteString("\n• Esc: Cancel and return to list")
	content.WriteString("\n• Ctrl+V: Validate configuration")

	return content.String()
}

// renderConfigNewView renders the new configuration view
func (m *DashboardModel) renderConfigNewView(leftWidth, rightWidth, contentHeight int) string {
	var content strings.Builder

	content.WriteString(DashboardStyles.UserMessage.Render("📄 New Configuration"))
	content.WriteString("\n\n")

	// Configuration templates
	content.WriteString("Select a template:")
	content.WriteString("\n\n")

	templates := []struct {
		name        string
		description string
		template    string
	}{
		{"MCPServer", "MCP Server configuration", getMCPServerTemplate()},
		{"MCPMemory", "Memory service configuration", getMCPMemoryTemplate()},
		{"MCPTaskScheduler", "Task scheduler configuration", getMCPTaskSchedulerTemplate()},
		{"MCPProxy", "Proxy configuration", getMCPProxyTemplate()},
		{"Workflow", "Workflow configuration", getWorkflowTemplate()},
		{"MCPToolbox", "Toolbox configuration", getMCPToolboxTemplate()},
	}

	for i, template := range templates {
		prefix := fmt.Sprintf("%d. ", i+1)
		content.WriteString(DashboardStyles.ConfigList.Render(prefix + template.name + " - " + template.description))
		content.WriteString("\n")
	}

	content.WriteString("\nActions:")
	content.WriteString("\n• 1-6: Select template")
	content.WriteString("\n• Esc: Return to list")

	return content.String()
}

// Helper functions

func (m *DashboardModel) getConfigResources() []ConfigResource {
	// This would return the actual config resources from the model state
	// For now, return empty slice - this will be populated when we fetch resources
	return []ConfigResource{}
}

func (m *DashboardModel) getSelectedConfigResource() *ConfigResource {
	configs := m.getConfigResources()
	for _, config := range configs {
		if config.Name == m.selectedConfig {
			return &config
		}
	}
	return nil
}

// Configuration templates
func getMCPServerTemplate() string {
	return `apiVersion: mcp.matey.ai/v1
kind: MCPServer
metadata:
  name: example-server
  namespace: default
spec:
  enabled: true
  transport:
    type: http
    config:
      host: "0.0.0.0"
      port: 8080
  command: ["python", "-m", "example_server"]
  environment:
    - name: LOG_LEVEL
      value: info
  resources:
    requests:
      memory: "128Mi"
      cpu: "100m"
    limits:
      memory: "256Mi"
      cpu: "200m"`
}

func getMCPMemoryTemplate() string {
	return `apiVersion: mcp.matey.ai/v1
kind: MCPMemory
metadata:
  name: example-memory
  namespace: default
spec:
  enabled: true
  storageType: redis
  config:
    host: redis-service
    port: 6379
    database: 0
  persistence:
    enabled: true
    storageClass: standard
    size: 1Gi`
}

func getMCPTaskSchedulerTemplate() string {
	return `apiVersion: mcp.matey.ai/v1
kind: MCPTaskScheduler
metadata:
  name: example-scheduler
  namespace: default
spec:
  enabled: true
  workers: 3
  queue:
    type: redis
    config:
      host: redis-service
      port: 6379
  resources:
    requests:
      memory: "256Mi"
      cpu: "200m"
    limits:
      memory: "512Mi"
      cpu: "500m"`
}

func getMCPProxyTemplate() string {
	return `apiVersion: mcp.matey.ai/v1
kind: MCPProxy
metadata:
  name: example-proxy
  namespace: default
spec:
  enabled: true
  config:
    host: "0.0.0.0"
    port: 8080
    upstreams:
      - name: server1
        url: http://server1:8080
        weight: 1
      - name: server2
        url: http://server2:8080
        weight: 1
  loadBalancer:
    strategy: round_robin`
}

func getWorkflowTemplate() string {
	return `apiVersion: mcp.matey.ai/v1
kind: Workflow
metadata:
  name: example-workflow
  namespace: default
spec:
  enabled: true
  schedule: "0 2 * * *"
  timezone: "UTC"
  steps:
    - name: step1
      tool: example_tool
      parameters:
        param1: value1
      retry:
        maxRetries: 3
        backoffStrategy: exponential`
}

func getMCPToolboxTemplate() string {
	return `apiVersion: mcp.matey.ai/v1
kind: MCPToolbox
metadata:
  name: example-toolbox
  namespace: default
spec:
  displayName: "Example Toolbox"
  description: "An example toolbox configuration"
  servers:
    - name: server1
      serverRef:
        name: example-server
        namespace: default
  security:
    podSecurityStandards: restricted
    imagePullPolicy: Always`
}

// createDynamicClient creates a dynamic Kubernetes client
func createDynamicClient() (dynamic.Interface, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
		}
	}

	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return dynamicClient, nil
}

