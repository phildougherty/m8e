package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	
	"github.com/phildougherty/m8e/internal/scheduler"
)

// Workflow management messages
type (
	workflowListMsg      []WorkflowInfo
	workflowTemplatesMsg []*scheduler.WorkflowTemplate
	workflowStatusMsg    struct {
		name   string
		status WorkflowExecutionStatus
	}
)

// WorkflowExecutionStatus represents the execution status of a workflow
type WorkflowExecutionStatus struct {
	Phase          string                     `json:"phase"`
	LastExecution  *time.Time                 `json:"lastExecution,omitempty"`
	NextExecution  *time.Time                 `json:"nextExecution,omitempty"`
	StepStatuses   map[string]WorkflowStepStatus `json:"stepStatuses,omitempty"`
	Message        string                     `json:"message,omitempty"`
}

// WorkflowStepStatus represents the status of a workflow step
type WorkflowStepStatus struct {
	Phase     string     `json:"phase"`
	StartTime *time.Time `json:"startTime,omitempty"`
	EndTime   *time.Time `json:"endTime,omitempty"`
	Message   string     `json:"message,omitempty"`
	Output    string     `json:"output,omitempty"`
}

// fetchWorkflows fetches all workflows from Kubernetes
func (m *DashboardModel) fetchWorkflows() tea.Cmd {
	return func() tea.Msg {
		var workflows []WorkflowInfo
		
		// Create dynamic client for workflows
		dynamicClient, err := createDynamicClient()
		if err != nil {
			return errorMsg(fmt.Errorf("failed to create dynamic client: %w", err))
		}

		gvr := schema.GroupVersionResource{
			Group:    "mcp.matey.ai",
			Version:  "v1",
			Resource: "workflows",
		}

		list, err := dynamicClient.Resource(gvr).Namespace("").List(context.Background(), metav1.ListOptions{})
		if err != nil {
			// Return empty list if CRD doesn't exist or can't be accessed
			return workflowListMsg(workflows)
		}

		for _, item := range list.Items {
			workflow := WorkflowInfo{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
				Phase:     "Unknown",
			}

			// Extract spec information
			if spec, found, err := unstructured.NestedMap(item.Object, "spec"); err == nil && found {
				if schedule, found, err := unstructured.NestedString(spec, "schedule"); err == nil && found {
					workflow.Schedule = schedule
				}
				if enabled, found, err := unstructured.NestedBool(spec, "enabled"); err == nil && found {
					workflow.Enabled = enabled
				}
				if steps, found, err := unstructured.NestedSlice(spec, "steps"); err == nil && found {
					workflow.StepCount = len(steps)
				}
			}

			// Extract status information
			if status, found, err := unstructured.NestedMap(item.Object, "status"); err == nil && found {
				if phase, found, err := unstructured.NestedString(status, "phase"); err == nil && found {
					workflow.Phase = phase
				}
				if lastRun, found, err := unstructured.NestedString(status, "lastExecutionTime"); err == nil && found {
					if t, err := time.Parse(time.RFC3339, lastRun); err == nil {
						workflow.LastRun = &t
					}
				}
				if nextRun, found, err := unstructured.NestedString(status, "nextScheduleTime"); err == nil && found {
					if t, err := time.Parse(time.RFC3339, nextRun); err == nil {
						workflow.NextRun = &t
					}
				}
			}

			workflows = append(workflows, workflow)
		}

		// Sort by name
		sort.Slice(workflows, func(i, j int) bool {
			return workflows[i].Name < workflows[j].Name
		})

		return workflowListMsg(workflows)
	}
}

// fetchWorkflowTemplates fetches available workflow templates
func (m *DashboardModel) fetchWorkflowTemplates() tea.Cmd {
	return func() tea.Msg {
		templateRegistry := scheduler.NewTemplateRegistry()
		templates := templateRegistry.ListTemplates()
		return workflowTemplatesMsg(templates)
	}
}

// Enhanced renderWorkflowsView with proper workflow management
func (m *DashboardModel) renderWorkflowsView() string {
	var content strings.Builder

	// Header
	content.WriteString(m.renderTabs())
	content.WriteString("\n\n")

	// Calculate layout dimensions
	contentHeight := m.height - 6 // Reserve space for tabs and input
	leftWidth := m.width/3 - 1
	rightWidth := m.width*2/3 - 1

	switch m.workflowState {
	case WorkflowStateList:
		content.WriteString(m.renderWorkflowListView(leftWidth, rightWidth, contentHeight))
	case WorkflowStateEdit:
		content.WriteString(m.renderWorkflowEditView(leftWidth, rightWidth, contentHeight))
	case WorkflowStateMonitor:
		content.WriteString(m.renderWorkflowMonitorView(leftWidth, rightWidth, contentHeight))
	case WorkflowStateDesign:
		content.WriteString(m.renderWorkflowDesignView(leftWidth, rightWidth, contentHeight))
	}

	return content.String()
}

// renderWorkflowListView renders the workflow list view
func (m *DashboardModel) renderWorkflowListView(leftWidth, rightWidth, contentHeight int) string {
	var leftPane, rightPane strings.Builder

	// Left pane: Workflow list
	leftPane.WriteString(DashboardStyles.UserMessage.Render("🔄 Workflows"))
	leftPane.WriteString("\n\n")

	// Workflow list
	if len(m.workflows) == 0 {
		leftPane.WriteString(DashboardStyles.SystemMessage.Render("No workflows found.\nPress 'n' to create a new one."))
	} else {
		for _, workflow := range m.workflows {
			var style lipgloss.Style
			if workflow.Name == m.selectedWorkflow {
				style = DashboardStyles.ConfigSelected
			} else {
				switch workflow.Phase {
				case "Running":
					style = DashboardStyles.WorkflowActive
				case "Paused", "Suspended":
					style = DashboardStyles.WorkflowPaused
				default:
					style = DashboardStyles.WorkflowCard
				}
			}

			// Status icon
			status := "⚪"
			switch workflow.Phase {
			case "Running":
				status = "🟢"
			case "Paused", "Suspended":
				status = "🟡"
			case "Failed":
				status = "🔴"
			case "Completed":
				status = "✅"
			}

			workflowItem := fmt.Sprintf("%s %s", status, workflow.Name)
			if workflow.Schedule != "" {
				workflowItem += fmt.Sprintf(" (%s)", workflow.Schedule)
			}
			if !workflow.Enabled {
				workflowItem += " [disabled]"
			}

			leftPane.WriteString(style.Render(workflowItem))
			leftPane.WriteString("\n")
		}
	}

	// Right pane: Workflow details/templates
	rightPane.WriteString(DashboardStyles.UserMessage.Render("📋 Workflow Details"))
	rightPane.WriteString("\n\n")

	if m.selectedWorkflow != "" {
		selectedWorkflow := m.getSelectedWorkflow()
		if selectedWorkflow != nil {
			// Show workflow metadata
			rightPane.WriteString(fmt.Sprintf("Name: %s\n", selectedWorkflow.Name))
			rightPane.WriteString(fmt.Sprintf("Namespace: %s\n", selectedWorkflow.Namespace))
			rightPane.WriteString(fmt.Sprintf("Schedule: %s\n", selectedWorkflow.Schedule))
			rightPane.WriteString(fmt.Sprintf("Enabled: %v\n", selectedWorkflow.Enabled))
			rightPane.WriteString(fmt.Sprintf("Phase: %s\n", selectedWorkflow.Phase))
			rightPane.WriteString(fmt.Sprintf("Steps: %d\n", selectedWorkflow.StepCount))
			rightPane.WriteString("\n")

			// Show execution times
			if selectedWorkflow.LastRun != nil {
				rightPane.WriteString(fmt.Sprintf("Last Run: %s\n", selectedWorkflow.LastRun.Format(time.RFC3339)))
			}
			if selectedWorkflow.NextRun != nil {
				rightPane.WriteString(fmt.Sprintf("Next Run: %s\n", selectedWorkflow.NextRun.Format(time.RFC3339)))
			}
			rightPane.WriteString("\n")

			// Quick actions
			rightPane.WriteString("Quick Actions:\n")
			if selectedWorkflow.Phase == "Running" {
				rightPane.WriteString("• p: Pause workflow\n")
			} else {
				rightPane.WriteString("• r: Resume workflow\n")
			}
			rightPane.WriteString("• e: Execute now\n")
			rightPane.WriteString("• l: View logs\n")
			rightPane.WriteString("• m: Monitor execution\n")
		}
	} else {
		// Show workflow templates
		rightPane.WriteString("Available Templates:\n\n")
		
		templates := []struct {
			name        string
			category    string
			description string
		}{
			{"health-monitoring", "monitoring", "Monitor health metrics and send alerts"},
			{"data-backup", "backup", "Automated data backup with cloud upload"},
			{"report-generation", "reporting", "Generate and distribute reports"},
			{"system-maintenance", "maintenance", "System cleanup and maintenance"},
			{"code-quality-checks", "development", "Code linting, testing, and security"},
			{"database-maintenance", "database", "Database backup and optimization"},
		}

		for i, template := range templates {
			templateItem := fmt.Sprintf("%d. %s (%s)\n   %s", 
				i+1, template.name, template.category, template.description)
			rightPane.WriteString(DashboardStyles.ConfigList.Render(templateItem))
			rightPane.WriteString("\n\n")
		}
	}

	// Actions
	rightPane.WriteString("\nActions:")
	rightPane.WriteString("\n• Enter: Edit workflow")
	rightPane.WriteString("\n• n: New workflow from template")
	rightPane.WriteString("\n• d: Delete workflow")
	rightPane.WriteString("\n• r: Refresh list")

	// Combine panes
	leftPaneStyled := DashboardStyles.ConfigList.Width(leftWidth).Height(contentHeight).Render(leftPane.String())
	rightPaneStyled := DashboardStyles.ConfigEditor.Width(rightWidth).Height(contentHeight).Render(rightPane.String())

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPaneStyled, rightPaneStyled)
}

// renderWorkflowEditView renders the workflow edit view
func (m *DashboardModel) renderWorkflowEditView(leftWidth, rightWidth, contentHeight int) string {
	var content strings.Builder

	content.WriteString(DashboardStyles.UserMessage.Render(fmt.Sprintf("✏️ Editing: %s", m.selectedWorkflow)))
	content.WriteString("\n\n")

	// Workflow editor (simplified - in a real implementation you'd want proper editing)
	content.WriteString("Workflow Editor:")
	content.WriteString("\n")
	content.WriteString(DashboardStyles.ConfigEditor.Width(m.width-4).Height(contentHeight-8).Render("Workflow editing interface coming soon..."))

	content.WriteString("\nActions:")
	content.WriteString("\n• Ctrl+S: Save workflow")
	content.WriteString("\n• Esc: Return to list")
	content.WriteString("\n• d: Visual designer")

	return content.String()
}

// renderWorkflowMonitorView renders the workflow monitoring view
func (m *DashboardModel) renderWorkflowMonitorView(leftWidth, rightWidth, contentHeight int) string {
	var content strings.Builder

	content.WriteString(DashboardStyles.UserMessage.Render(fmt.Sprintf("📊 Monitoring: %s", m.selectedWorkflow)))
	content.WriteString("\n\n")

	// Monitoring interface
	content.WriteString("Real-time Execution Monitoring:")
	content.WriteString("\n")
	content.WriteString(DashboardStyles.ConfigEditor.Width(m.width-4).Height(contentHeight-8).Render("Workflow monitoring interface coming soon..."))

	content.WriteString("\nActions:")
	content.WriteString("\n• Esc: Return to list")
	content.WriteString("\n• r: Refresh status")
	content.WriteString("\n• l: View logs")

	return content.String()
}

// renderWorkflowDesignView renders the visual workflow designer
func (m *DashboardModel) renderWorkflowDesignView(leftWidth, rightWidth, contentHeight int) string {
	var content strings.Builder

	content.WriteString(DashboardStyles.UserMessage.Render(fmt.Sprintf("🎨 Designer: %s", m.selectedWorkflow)))
	content.WriteString("\n\n")

	// Visual designer interface
	content.WriteString("Visual Workflow Designer:")
	content.WriteString("\n")
	content.WriteString(DashboardStyles.ConfigEditor.Width(m.width-4).Height(contentHeight-8).Render("Visual workflow designer coming soon..."))

	content.WriteString("\nActions:")
	content.WriteString("\n• Ctrl+S: Save workflow")
	content.WriteString("\n• Esc: Return to editor")
	content.WriteString("\n• a: Add step")
	content.WriteString("\n• d: Delete step")

	return content.String()
}

// Helper functions

func (m *DashboardModel) getSelectedWorkflow() *WorkflowInfo {
	for _, workflow := range m.workflows {
		if workflow.Name == m.selectedWorkflow {
			return &workflow
		}
	}
	return nil
}

// Workflow management commands

func (m *DashboardModel) pauseWorkflow(name string) tea.Cmd {
	return func() tea.Msg {
		// TODO: Implement pause workflow using the existing CLI command
		return nil
	}
}

func (m *DashboardModel) resumeWorkflow(name string) tea.Cmd {
	return func() tea.Msg {
		// TODO: Implement resume workflow using the existing CLI command
		return nil
	}
}

func (m *DashboardModel) executeWorkflow(name string) tea.Cmd {
	return func() tea.Msg {
		// TODO: Implement execute workflow using the existing CLI command
		return nil
	}
}

func (m *DashboardModel) deleteWorkflow(name string) tea.Cmd {
	return func() tea.Msg {
		// TODO: Implement delete workflow using the existing CLI command
		return nil
	}
}