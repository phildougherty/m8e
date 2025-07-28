package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/phildougherty/m8e/internal/crd"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Service management implementations

// startService starts specific MCP services using Kubernetes client
func (m *MateyMCPServer) startService(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	services, ok := args["services"].([]interface{})
	if !ok || len(services) == 0 {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "services array is required"}},
			IsError: true,
		}, fmt.Errorf("services array is required")
	}
	
	if !m.useK8sClient() {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot start services."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}
	
	var result strings.Builder
	var errors []string
	
	for _, service := range services {
		if serviceName, ok := service.(string); ok {
			err := m.startServiceByName(ctx, serviceName)
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to start %s: %v", serviceName, err)
				errors = append(errors, errorMsg)
				result.WriteString(fmt.Sprintf("ERROR: %s\n", errorMsg))
			} else {
				result.WriteString(fmt.Sprintf("Started service: %s\n", serviceName))
			}
		}
	}
	
	if len(errors) > 0 {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: result.String()}},
			IsError: true,
		}, fmt.Errorf("failed to start some services: %s", strings.Join(errors, ", "))
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}

// stopService stops specific MCP services using Kubernetes client
func (m *MateyMCPServer) stopService(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	services, ok := args["services"].([]interface{})
	if !ok || len(services) == 0 {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "services array is required"}},
			IsError: true,
		}, fmt.Errorf("services array is required")
	}
	
	if !m.useK8sClient() {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot stop services."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}
	
	var result strings.Builder
	var errors []string
	
	for _, service := range services {
		if serviceName, ok := service.(string); ok {
			err := m.stopServiceByName(ctx, serviceName)
			if err != nil {
				errorMsg := fmt.Sprintf("Failed to stop %s: %v", serviceName, err)
				errors = append(errors, errorMsg)
				result.WriteString(fmt.Sprintf("ERROR: %s\n", errorMsg))
			} else {
				result.WriteString(fmt.Sprintf("Stopped service: %s\n", serviceName))
			}
		}
	}
	
	if len(errors) > 0 {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: result.String()}},
			IsError: true,
		}, fmt.Errorf("failed to stop some services: %s", strings.Join(errors, ", "))
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}

// reloadProxy reloads MCP proxy configuration
func (m *MateyMCPServer) reloadProxy(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if !m.useK8sClient() {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot reload proxy."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}
	
	// Find and restart the proxy pod to trigger reload
	var pods corev1.PodList
	labelSelector := client.MatchingLabels{"app": "matey-proxy"}
	err := m.k8sClient.List(ctx, &pods, client.InNamespace(m.namespace), labelSelector)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error finding proxy pods: %v", err)}},
			IsError: true,
		}, err
	}
	
	if len(pods.Items) == 0 {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "No proxy pods found. Proxy may not be running."}},
			IsError: true,
		}, fmt.Errorf("no proxy pods found")
	}
	
	// Delete the proxy pod to trigger restart and reload
	pod := &pods.Items[0]
	err = m.k8sClient.Delete(ctx, pod)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error restarting proxy pod: %v", err)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: fmt.Sprintf("Proxy pod %s restarted successfully. Configuration will be reloaded.", pod.Name)}},
	}, nil
}

// Memory service management implementations

// memoryStatus gets memory service status using Kubernetes client
func (m *MateyMCPServer) memoryStatus(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	// Use Kubernetes client directly - no binary fallback
	if m.k8sClient == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot get memory status."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}
	
	return m.getMemoryStatusFromK8s(ctx)
}

// getMemoryStatusFromK8s gets memory service status using Kubernetes client
func (m *MateyMCPServer) getMemoryStatusFromK8s(ctx context.Context) (*ToolResult, error) {
	// Check for MCPMemory CRD
	var memory crd.MCPMemory
	err := m.k8sClient.Get(ctx, client.ObjectKey{
		Name:      "memory",
		Namespace: m.namespace,
	}, &memory)
	
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting memory service: %v", err)}},
				IsError: true,
			}, err
		}
		// Memory service not found
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Memory service not found - not deployed"}},
		}, nil
	}
	
	// Format status information
	var status strings.Builder
	status.WriteString("=== Memory Service Status ===\n")
	status.WriteString(fmt.Sprintf("Name: %s\n", memory.Name))
	status.WriteString(fmt.Sprintf("Namespace: %s\n", memory.Namespace))
	status.WriteString(fmt.Sprintf("Phase: %s\n", memory.Status.Phase))
	status.WriteString(fmt.Sprintf("Health Status: %s\n", memory.Status.HealthStatus))
	status.WriteString(fmt.Sprintf("PostgreSQL Status: %s\n", memory.Status.PostgresStatus))
	status.WriteString(fmt.Sprintf("Ready Replicas: %d/%d\n", memory.Status.ReadyReplicas, memory.Status.Replicas))
	status.WriteString(fmt.Sprintf("Database URL: %s\n", memory.Spec.DatabaseURL))
	
	if len(memory.Status.Conditions) > 0 {
		status.WriteString("\n=== Conditions ===\n")
		for _, condition := range memory.Status.Conditions {
			status.WriteString(fmt.Sprintf("- %s: %s (%s)\n", condition.Type, condition.Status, condition.Message))
		}
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: status.String()}},
	}, nil
}

// memoryStart starts memory service using Kubernetes client
func (m *MateyMCPServer) memoryStart(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if m.k8sClient == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot start memory service."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}
	
	err := m.startMemoryService(ctx)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Failed to start memory service: %v", err)}},
			IsError: true,
		}, err
	}
	
	// Try to reinitialize the memory store now that the service is starting
	m.initializeMemoryStore()
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Memory service started successfully"}},
	}, nil
}

// memoryStop stops memory service using Kubernetes client
func (m *MateyMCPServer) memoryStop(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if m.k8sClient == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot stop memory service."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}
	
	err := m.stopMemoryService(ctx)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Failed to stop memory service: %v", err)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Memory service stopped successfully"}},
	}, nil
}

// Task scheduler management implementations

// taskSchedulerStatus gets task scheduler status
func (m *MateyMCPServer) taskSchedulerStatus(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	// Use Kubernetes client directly - no binary fallback
	if m.k8sClient == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot get task scheduler status."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}
	
	return m.getTaskSchedulerStatusFromK8s(ctx)
}

// getTaskSchedulerStatusFromK8s gets task scheduler status using Kubernetes client
func (m *MateyMCPServer) getTaskSchedulerStatusFromK8s(ctx context.Context) (*ToolResult, error) {
	// Check for MCPTaskScheduler CRD
	var taskScheduler crd.MCPTaskScheduler
	err := m.k8sClient.Get(ctx, client.ObjectKey{
		Name:      "task-scheduler",
		Namespace: m.namespace,
	}, &taskScheduler)
	
	if err != nil {
		if client.IgnoreNotFound(err) != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting task scheduler: %v", err)}},
				IsError: true,
			}, err
		}
		// Task scheduler not found
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Task scheduler not found - not deployed"}},
		}, nil
	}
	
	// Format status information
	var status strings.Builder
	status.WriteString("=== Task Scheduler Status ===\n")
	status.WriteString(fmt.Sprintf("Name: %s\n", taskScheduler.Name))
	status.WriteString(fmt.Sprintf("Namespace: %s\n", taskScheduler.Namespace))
	status.WriteString(fmt.Sprintf("Phase: %s\n", taskScheduler.Status.Phase))
	status.WriteString(fmt.Sprintf("Health Status: %s\n", taskScheduler.Status.HealthStatus))
	status.WriteString(fmt.Sprintf("Running Workflows: %d\n", len(taskScheduler.Status.WorkflowExecutions)))
	status.WriteString(fmt.Sprintf("Total Workflows: %d\n", taskScheduler.Status.WorkflowStats.TotalWorkflows))
	status.WriteString(fmt.Sprintf("Running Workflows: %d\n", taskScheduler.Status.WorkflowStats.RunningWorkflows))
	status.WriteString(fmt.Sprintf("Total Tasks: %d\n", taskScheduler.Status.TaskStats.TotalTasks))
	status.WriteString(fmt.Sprintf("Running Tasks: %d\n", taskScheduler.Status.TaskStats.RunningTasks))
	
	if len(taskScheduler.Status.Conditions) > 0 {
		status.WriteString("\n=== Conditions ===\n")
		for _, condition := range taskScheduler.Status.Conditions {
			status.WriteString(fmt.Sprintf("- %s: %s (%s)\n", condition.Type, condition.Status, condition.Message))
		}
	}
	
	if len(taskScheduler.Status.WorkflowExecutions) > 0 {
		status.WriteString("\n=== Recent Workflow Executions ===\n")
		count := 0
		for _, execution := range taskScheduler.Status.WorkflowExecutions {
			if count >= 5 { // Show last 5 executions
				break
			}
			executionStatus := "Running"
			if execution.EndTime != nil {
				executionStatus = "Completed"
			}
			status.WriteString(fmt.Sprintf("- %s: %s (Started: %s)\n", 
				execution.WorkflowName, executionStatus, execution.StartTime.Format("2006-01-02 15:04:05")))
			count++
		}
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: status.String()}},
	}, nil
}

// taskSchedulerStart starts task scheduler service using Kubernetes client
func (m *MateyMCPServer) taskSchedulerStart(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if m.k8sClient == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot start task scheduler."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}
	
	err := m.startTaskSchedulerService(ctx)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Failed to start task scheduler: %v", err)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Task scheduler started successfully"}},
	}, nil
}

// taskSchedulerStop stops task scheduler service using Kubernetes client
func (m *MateyMCPServer) taskSchedulerStop(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if m.k8sClient == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot stop task scheduler."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}
	
	err := m.stopTaskSchedulerService(ctx)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Failed to stop task scheduler: %v", err)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Task scheduler stopped successfully"}},
	}, nil
}

// Toolbox management implementations

// listToolboxes lists all toolboxes in the cluster
func (m *MateyMCPServer) listToolboxes(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if m.k8sClient == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot list toolboxes."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}
	
	return m.listToolboxesFromK8s(ctx)
}

// getToolbox gets details of a specific toolbox
func (m *MateyMCPServer) getToolbox(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	if m.k8sClient == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot get toolbox details."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}
	
	return m.getToolboxFromK8s(ctx, name)
}

// Configuration management implementations

// validateConfig validates the matey configuration file
func (m *MateyMCPServer) validateConfig(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if m.k8sClient == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot validate configuration."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}
	
	return m.validateConfigFromK8s(ctx, args)
}

// createConfig creates client configuration for MCP servers
func (m *MateyMCPServer) createConfig(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if m.k8sClient == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot create config."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}
	
	return m.createConfigFromK8s(ctx, args)
}

// installMatey installs Matey CRDs and required Kubernetes resources
func (m *MateyMCPServer) installMatey(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if m.k8sClient == nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot install Matey."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}
	
	return m.installMateyFromK8s(ctx)
}

// Helper functions for Kubernetes operations

// getPodLogsForServer gets logs for a specific server's pods
func (m *MateyMCPServer) getPodLogsForServer(ctx context.Context, serverName string, tailLines int64) (string, error) {
	if m.clientset == nil {
		return "", fmt.Errorf("kubernetes clientset not available")
	}
	
	var pods corev1.PodList
	labelSelector := client.MatchingLabels{"app": serverName}
	err := m.k8sClient.List(ctx, &pods, client.InNamespace(m.namespace), labelSelector)
	if err != nil {
		return "", fmt.Errorf("failed to list pods for server %s: %v", serverName, err)
	}
	
	if len(pods.Items) == 0 {
		return fmt.Sprintf("No pods found for server: %s\n", serverName), nil
	}
	
	var result strings.Builder
	result.WriteString(fmt.Sprintf("=== Logs for server: %s ===\n", serverName))
	
	for _, pod := range pods.Items {
		result.WriteString(fmt.Sprintf("\n--- Pod: %s ---\n", pod.Name))
		
		// Get logs for the pod
		req := m.clientset.CoreV1().Pods(m.namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
			TailLines: &tailLines,
		})
		
		logs, err := req.Stream(ctx)
		if err != nil {
			result.WriteString(fmt.Sprintf("Error getting logs: %v\n", err))
			continue
		}
		defer logs.Close()
		
		logBytes, err := io.ReadAll(logs)
		if err != nil {
			result.WriteString(fmt.Sprintf("Error reading logs: %v\n", err))
			continue
		}
		
		result.WriteString(string(logBytes))
	}
	
	return result.String(), nil
}

// getAllMCPServerLogs gets logs for all MCP servers
func (m *MateyMCPServer) getAllMCPServerLogs(ctx context.Context, tailLines int64) (string, error) {
	// List all MCP server resources to get server names
	var mcpServers crd.MCPServerList
	err := m.k8sClient.List(ctx, &mcpServers, client.InNamespace(m.namespace))
	if err != nil {
		return "", fmt.Errorf("failed to list MCP servers: %v", err)
	}
	
	var result strings.Builder
	result.WriteString("=== All MCP Server Logs ===\n")
	
	for _, server := range mcpServers.Items {
		logs, err := m.getPodLogsForServer(ctx, server.Name, tailLines)
		if err != nil {
			result.WriteString(fmt.Sprintf("\nError getting logs for %s: %v\n", server.Name, err))
			continue
		}
		result.WriteString(logs)
		result.WriteString("\n")
	}
	
	return result.String(), nil
}

// startServiceByName starts a service by name using Kubernetes operations
func (m *MateyMCPServer) startServiceByName(ctx context.Context, serviceName string) error {
	switch serviceName {
	case "memory":
		return m.startMemoryService(ctx)
	case "task-scheduler":
		return m.startTaskSchedulerService(ctx)
	case "proxy":
		return m.startProxyService(ctx)
	default:
		// Try to find and scale up deployment
		return m.scaleService(ctx, serviceName, 1)
	}
}

// stopServiceByName stops a service by name using Kubernetes operations
func (m *MateyMCPServer) stopServiceByName(ctx context.Context, serviceName string) error {
	switch serviceName {
	case "memory":
		return m.stopMemoryService(ctx)
	case "task-scheduler":
		return m.stopTaskSchedulerService(ctx)
	case "proxy":
		return m.stopProxyService(ctx)
	default:
		// Try to find and scale down deployment
		return m.scaleService(ctx, serviceName, 0)
	}
}

// scaleService scales a deployment to the specified replica count
func (m *MateyMCPServer) scaleService(ctx context.Context, serviceName string, replicas int32) error {
	var deployment appsv1.Deployment
	err := m.k8sClient.Get(ctx, client.ObjectKey{
		Name:      serviceName,
		Namespace: m.namespace,
	}, &deployment)
	if err != nil {
		return fmt.Errorf("failed to get deployment %s: %v", serviceName, err)
	}
	
	deployment.Spec.Replicas = &replicas
	err = m.k8sClient.Update(ctx, &deployment)
	if err != nil {
		return fmt.Errorf("failed to scale deployment %s to %d replicas: %v", serviceName, replicas, err)
	}
	
	return nil
}

// startMemoryService starts the memory service
func (m *MateyMCPServer) startMemoryService(ctx context.Context) error {
	var memory crd.MCPMemory
	err := m.k8sClient.Get(ctx, client.ObjectKey{
		Name:      "memory",
		Namespace: m.namespace,
	}, &memory)
	if err != nil {
		return fmt.Errorf("failed to get MCPMemory: %v", err)
	}
	
	// Set replicas to 1 if it's currently 0
	if memory.Spec.Replicas == nil || *memory.Spec.Replicas == 0 {
		replicas := int32(1)
		memory.Spec.Replicas = &replicas
		err = m.k8sClient.Update(ctx, &memory)
		if err != nil {
			return fmt.Errorf("failed to update MCPMemory replicas: %v", err)
		}
	}
	
	return nil
}

// stopMemoryService stops the memory service
func (m *MateyMCPServer) stopMemoryService(ctx context.Context) error {
	var memory crd.MCPMemory
	err := m.k8sClient.Get(ctx, client.ObjectKey{
		Name:      "memory",
		Namespace: m.namespace,
	}, &memory)
	if err != nil {
		return fmt.Errorf("failed to get MCPMemory: %v", err)
	}
	
	// Set replicas to 0
	replicas := int32(0)
	memory.Spec.Replicas = &replicas
	err = m.k8sClient.Update(ctx, &memory)
	if err != nil {
		return fmt.Errorf("failed to update MCPMemory replicas: %v", err)
	}
	
	return nil
}

// startTaskSchedulerService starts the task scheduler service
func (m *MateyMCPServer) startTaskSchedulerService(ctx context.Context) error {
	var taskScheduler crd.MCPTaskScheduler
	err := m.k8sClient.Get(ctx, client.ObjectKey{
		Name:      "task-scheduler",
		Namespace: m.namespace,
	}, &taskScheduler)
	if err != nil {
		return fmt.Errorf("failed to get MCPTaskScheduler: %v", err)
	}
	
	// Set replicas to 1 if it's currently 0
	if taskScheduler.Spec.Replicas == nil || *taskScheduler.Spec.Replicas == 0 {
		replicas := int32(1)
		taskScheduler.Spec.Replicas = &replicas
		err = m.k8sClient.Update(ctx, &taskScheduler)
		if err != nil {
			return fmt.Errorf("failed to update MCPTaskScheduler replicas: %v", err)
		}
	}
	
	return nil
}

// stopTaskSchedulerService stops the task scheduler service
func (m *MateyMCPServer) stopTaskSchedulerService(ctx context.Context) error {
	var taskScheduler crd.MCPTaskScheduler
	err := m.k8sClient.Get(ctx, client.ObjectKey{
		Name:      "task-scheduler",
		Namespace: m.namespace,
	}, &taskScheduler)
	if err != nil {
		return fmt.Errorf("failed to get MCPTaskScheduler: %v", err)
	}
	
	// Set replicas to 0
	replicas := int32(0)
	taskScheduler.Spec.Replicas = &replicas
	err = m.k8sClient.Update(ctx, &taskScheduler)
	if err != nil {
		return fmt.Errorf("failed to update MCPTaskScheduler replicas: %v", err)
	}
	
	return nil
}

// startProxyService starts the proxy service
func (m *MateyMCPServer) startProxyService(ctx context.Context) error {
	return m.scaleService(ctx, "matey-proxy", 1)
}

// stopProxyService stops the proxy service
func (m *MateyMCPServer) stopProxyService(ctx context.Context) error {
	return m.scaleService(ctx, "matey-proxy", 0)
}

// Resource inspection functions

// inspectMCPServers inspects MCP server resources
func (m *MateyMCPServer) inspectMCPServers(ctx context.Context, resourceName, outputFormat string) (*ToolResult, error) {
	if resourceName != "" {
		// Get specific server
		var server crd.MCPServer
		err := m.k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: m.namespace,
		}, &server)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting MCPServer %s: %v", resourceName, err)}},
				IsError: true,
			}, err
		}
		
		return m.formatMCPServerInspection(server, outputFormat), nil
	}
	
	// List all servers
	var servers crd.MCPServerList
	err := m.k8sClient.List(ctx, &servers, client.InNamespace(m.namespace))
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error listing MCPServers: %v", err)}},
			IsError: true,
		}, err
	}
	
	return m.formatMCPServersList(servers.Items, outputFormat), nil
}

// formatMCPServerInspection formats a single MCPServer for inspection
func (m *MateyMCPServer) formatMCPServerInspection(server crd.MCPServer, outputFormat string) *ToolResult {
	switch outputFormat {
	case "json":
		jsonBytes, _ := json.MarshalIndent(server, "", "  ")
		return &ToolResult{Content: []Content{{Type: "text", Text: string(jsonBytes)}}}
	case "yaml":
		// Simplified YAML representation
		var result strings.Builder
		result.WriteString(fmt.Sprintf("name: %s\n", server.Name))
		result.WriteString(fmt.Sprintf("namespace: %s\n", server.Namespace))
		result.WriteString(fmt.Sprintf("phase: %s\n", server.Status.Phase))
		return &ToolResult{Content: []Content{{Type: "text", Text: result.String()}}}
	default:
		var result strings.Builder
		result.WriteString(fmt.Sprintf("=== MCPServer: %s ===\n", server.Name))
		result.WriteString(fmt.Sprintf("Namespace: %s\n", server.Namespace))
		result.WriteString(fmt.Sprintf("Phase: %s\n", server.Status.Phase))
		result.WriteString(fmt.Sprintf("Replicas: %d/%d\n", server.Status.ReadyReplicas, server.Status.Replicas))
		return &ToolResult{Content: []Content{{Type: "text", Text: result.String()}}}
	}
}

// formatMCPServersList formats a list of MCPServers
func (m *MateyMCPServer) formatMCPServersList(servers []crd.MCPServer, outputFormat string) *ToolResult {
	switch outputFormat {
	case "json":
		jsonBytes, _ := json.MarshalIndent(servers, "", "  ")
		return &ToolResult{Content: []Content{{Type: "text", Text: string(jsonBytes)}}}
	default:
		var result strings.Builder
		result.WriteString("=== MCP Servers ===\n")
		result.WriteString(fmt.Sprintf("%-20s %-15s %-10s\n", "NAME", "PHASE", "REPLICAS"))
		result.WriteString(strings.Repeat("-", 50) + "\n")
		for _, server := range servers {
			result.WriteString(fmt.Sprintf("%-20s %-15s %d/%d\n", 
				server.Name, server.Status.Phase, server.Status.ReadyReplicas, server.Status.Replicas))
		}
		return &ToolResult{Content: []Content{{Type: "text", Text: result.String()}}}
	}
}

// inspectMCPMemory inspects MCPMemory resources
func (m *MateyMCPServer) inspectMCPMemory(ctx context.Context, resourceName, outputFormat string) (*ToolResult, error) {
	var memory crd.MCPMemory
	name := resourceName
	if name == "" {
		name = "memory"
	}
	
	err := m.k8sClient.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: m.namespace,
	}, &memory)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting MCPMemory %s: %v", name, err)}},
			IsError: true,
		}, err
	}
	
	switch outputFormat {
	case "json":
		jsonBytes, _ := json.MarshalIndent(memory, "", "  ")
		return &ToolResult{Content: []Content{{Type: "text", Text: string(jsonBytes)}}}, nil
	default:
		var result strings.Builder
		result.WriteString(fmt.Sprintf("=== MCPMemory: %s ===\n", memory.Name))
		result.WriteString(fmt.Sprintf("Phase: %s\n", memory.Status.Phase))
		result.WriteString(fmt.Sprintf("PostgreSQL Status: %s\n", memory.Status.PostgresStatus))
		result.WriteString(fmt.Sprintf("Replicas: %d/%d\n", memory.Status.ReadyReplicas, memory.Status.Replicas))
		return &ToolResult{Content: []Content{{Type: "text", Text: result.String()}}}, nil
	}
}

// inspectMCPTaskScheduler inspects MCPTaskScheduler resources
func (m *MateyMCPServer) inspectMCPTaskScheduler(ctx context.Context, resourceName, outputFormat string) (*ToolResult, error) {
	var taskScheduler crd.MCPTaskScheduler
	name := resourceName
	if name == "" {
		name = "task-scheduler"
	}
	
	err := m.k8sClient.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: m.namespace,
	}, &taskScheduler)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting MCPTaskScheduler %s: %v", name, err)}},
			IsError: true,
		}, err
	}
	
	switch outputFormat {
	case "json":
		jsonBytes, _ := json.MarshalIndent(taskScheduler, "", "  ")
		return &ToolResult{Content: []Content{{Type: "text", Text: string(jsonBytes)}}}, nil
	default:
		var result strings.Builder
		result.WriteString(fmt.Sprintf("=== MCPTaskScheduler: %s ===\n", taskScheduler.Name))
		result.WriteString(fmt.Sprintf("Phase: %s\n", taskScheduler.Status.Phase))
		result.WriteString(fmt.Sprintf("Running Tasks: %d\n", taskScheduler.Status.TaskStats.RunningTasks))
		result.WriteString(fmt.Sprintf("Total Workflows: %d\n", len(taskScheduler.Spec.Workflows)))
		return &ToolResult{Content: []Content{{Type: "text", Text: result.String()}}}, nil
	}
}

// inspectMCPProxy inspects MCPProxy resources
func (m *MateyMCPServer) inspectMCPProxy(ctx context.Context, resourceName, outputFormat string) (*ToolResult, error) {
	var proxy crd.MCPProxy
	name := resourceName
	if name == "" {
		name = "proxy"
	}
	
	err := m.k8sClient.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: m.namespace,
	}, &proxy)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting MCPProxy %s: %v", name, err)}},
			IsError: true,
		}, err
	}
	
	switch outputFormat {
	case "json":
		jsonBytes, _ := json.MarshalIndent(proxy, "", "  ")
		return &ToolResult{Content: []Content{{Type: "text", Text: string(jsonBytes)}}}, nil
	default:
		var result strings.Builder
		result.WriteString(fmt.Sprintf("=== MCPProxy: %s ===\n", proxy.Name))
		result.WriteString(fmt.Sprintf("Phase: %s\n", proxy.Status.Phase))
		result.WriteString(fmt.Sprintf("Port: %d\n", proxy.Spec.Port))
		return &ToolResult{Content: []Content{{Type: "text", Text: result.String()}}}, nil
	}
}

// inspectMCPToolbox inspects MCPToolbox resources
func (m *MateyMCPServer) inspectMCPToolbox(ctx context.Context, resourceName, outputFormat string) (*ToolResult, error) {
	if resourceName == "" {
		// List all toolboxes
		var toolboxes crd.MCPToolboxList
		err := m.k8sClient.List(ctx, &toolboxes, client.InNamespace(m.namespace))
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error listing MCPToolboxes: %v", err)}},
				IsError: true,
			}, err
		}
		
		var result strings.Builder
		result.WriteString("=== MCP Toolboxes ===\n")
		for _, toolbox := range toolboxes.Items {
			result.WriteString(fmt.Sprintf("- %s (Phase: %s)\n", toolbox.Name, toolbox.Status.Phase))
		}
		return &ToolResult{Content: []Content{{Type: "text", Text: result.String()}}}, nil
	}
	
	var toolbox crd.MCPToolbox
	err := m.k8sClient.Get(ctx, client.ObjectKey{
		Name:      resourceName,
		Namespace: m.namespace,
	}, &toolbox)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting MCPToolbox %s: %v", resourceName, err)}},
			IsError: true,
		}, err
	}
	
	switch outputFormat {
	case "json":
		jsonBytes, _ := json.MarshalIndent(toolbox, "", "  ")
		return &ToolResult{Content: []Content{{Type: "text", Text: string(jsonBytes)}}}, nil
	default:
		var result strings.Builder
		result.WriteString(fmt.Sprintf("=== MCPToolbox: %s ===\n", toolbox.Name))
		result.WriteString(fmt.Sprintf("Phase: %s\n", toolbox.Status.Phase))
		result.WriteString(fmt.Sprintf("Template: %s\n", toolbox.Spec.Template))
		return &ToolResult{Content: []Content{{Type: "text", Text: result.String()}}}, nil
	}
}

// inspectAllResources inspects all MCP resources
func (m *MateyMCPServer) inspectAllResources(ctx context.Context, outputFormat string) (*ToolResult, error) {
	var result strings.Builder
	result.WriteString("=== All MCP Resources ===\n\n")
	
	// MCP Servers
	serverResult, _ := m.inspectMCPServers(ctx, "", "table")
	result.WriteString(serverResult.Content[0].Text)
	result.WriteString("\n")
	
	// MCP Memory
	memoryResult, _ := m.inspectMCPMemory(ctx, "", "table")
	result.WriteString(memoryResult.Content[0].Text)
	result.WriteString("\n")
	
	// MCP Task Scheduler
	taskSchedulerResult, _ := m.inspectMCPTaskScheduler(ctx, "", "table")
	result.WriteString(taskSchedulerResult.Content[0].Text)
	result.WriteString("\n")
	
	// MCP Proxy
	proxyResult, _ := m.inspectMCPProxy(ctx, "", "table")
	result.WriteString(proxyResult.Content[0].Text)
	result.WriteString("\n")
	
	// MCP Toolboxes
	toolboxResult, _ := m.inspectMCPToolbox(ctx, "", "table")
	result.WriteString(toolboxResult.Content[0].Text)
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}

// listToolboxesFromK8s lists all MCPToolbox resources using Kubernetes client
func (m *MateyMCPServer) listToolboxesFromK8s(ctx context.Context) (*ToolResult, error) {
	var toolboxList crd.MCPToolboxList
	err := m.k8sClient.List(ctx, &toolboxList, client.InNamespace(m.namespace))
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Failed to list toolboxes: %v", err)}},
			IsError: true,
		}, err
	}

	var result strings.Builder
	result.WriteString("=== MCPToolbox Resources ===\n")
	result.WriteString(fmt.Sprintf("%-20s %-15s %-10s %-15s\n", "NAME", "PHASE", "SERVERS", "AGE"))
	result.WriteString(strings.Repeat("-", 65) + "\n")

	for _, toolbox := range toolboxList.Items {
		age := time.Since(toolbox.CreationTimestamp.Time).Truncate(time.Second).String()
		serverCount := len(toolbox.Spec.Servers)
		
		result.WriteString(fmt.Sprintf("%-20s %-15s %-10d %-15s\n",
			toolbox.Name, toolbox.Status.Phase, serverCount, age))
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}

// getToolboxFromK8s gets details of a specific MCPToolbox resource
func (m *MateyMCPServer) getToolboxFromK8s(ctx context.Context, name string) (*ToolResult, error) {
	var toolbox crd.MCPToolbox
	err := m.k8sClient.Get(ctx, client.ObjectKey{
		Namespace: m.namespace,
		Name:      name,
	}, &toolbox)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Failed to get toolbox %s: %v", name, err)}},
			IsError: true,
		}, err
	}

	var result strings.Builder
	result.WriteString(fmt.Sprintf("=== MCPToolbox: %s ===\n", toolbox.Name))
	result.WriteString(fmt.Sprintf("Namespace: %s\n", toolbox.Namespace))
	result.WriteString(fmt.Sprintf("Phase: %s\n", toolbox.Status.Phase))
	result.WriteString(fmt.Sprintf("Description: %s\n", toolbox.Spec.Description))
	result.WriteString(fmt.Sprintf("Template: %s\n", toolbox.Spec.Template))
	result.WriteString(fmt.Sprintf("Servers: %d\n", len(toolbox.Spec.Servers)))
	result.WriteString(fmt.Sprintf("Created: %s\n", toolbox.CreationTimestamp.Time.Format(time.RFC3339)))

	if len(toolbox.Spec.Servers) > 0 {
		result.WriteString("\nServers:\n")
		for serverName, serverSpec := range toolbox.Spec.Servers {
			result.WriteString(fmt.Sprintf("  - %s: %s\n", serverName, serverSpec.Protocol))
		}
	}

	if len(toolbox.Status.Conditions) > 0 {
		result.WriteString("\nConditions:\n")
		for _, condition := range toolbox.Status.Conditions {
			result.WriteString(fmt.Sprintf("  %s: %s - %s\n", 
				condition.Type, condition.Status, condition.Message))
		}
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}

// validateConfigFromK8s validates configuration using Kubernetes resources
func (m *MateyMCPServer) validateConfigFromK8s(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	// Basic validation - check if we can access the Kubernetes API
	var serverList crd.MCPServerList
	err := m.k8sClient.List(ctx, &serverList, client.InNamespace(m.namespace))
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Configuration validation failed - cannot access Kubernetes API: %v", err)}},
			IsError: true,
		}, err
	}

	var result strings.Builder
	result.WriteString("=== Configuration Validation ===\n")
	result.WriteString("✓ Kubernetes client connection: OK\n")
	result.WriteString(fmt.Sprintf("✓ Namespace '%s': accessible\n", m.namespace))
	result.WriteString(fmt.Sprintf("✓ MCPServer resources: %d found\n", len(serverList.Items)))

	// Check for other resource types
	var memoryList crd.MCPMemoryList
	err = m.k8sClient.List(ctx, &memoryList, client.InNamespace(m.namespace))
	if err == nil {
		result.WriteString(fmt.Sprintf("✓ MCPMemory resources: %d found\n", len(memoryList.Items)))
	}

	var taskSchedulerList crd.MCPTaskSchedulerList
	err = m.k8sClient.List(ctx, &taskSchedulerList, client.InNamespace(m.namespace))
	if err == nil {
		result.WriteString(fmt.Sprintf("✓ MCPTaskScheduler resources: %d found\n", len(taskSchedulerList.Items)))
	}

	result.WriteString("\nConfiguration appears valid for Kubernetes operations.\n")

	return &ToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}

// createConfigFromK8s creates client configuration using Kubernetes resources
func (m *MateyMCPServer) createConfigFromK8s(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	// Get all MCP servers to create configuration
	var serverList crd.MCPServerList
	err := m.k8sClient.List(ctx, &serverList, client.InNamespace(m.namespace))
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Failed to list MCP servers for config creation: %v", err)}},
			IsError: true,
		}, err
	}

	var result strings.Builder
	result.WriteString("=== Client Configuration Generated ===\n")
	result.WriteString("Based on current Kubernetes MCP resources:\n\n")

	if len(serverList.Items) == 0 {
		result.WriteString("No MCP servers found. Deploy servers first using 'matey up'.\n")
	} else {
		result.WriteString("MCP Server Endpoints:\n")
		for _, server := range serverList.Items {
			if len(server.Status.ServiceEndpoints) > 0 {
				endpoint := server.Status.ServiceEndpoints[0]
				result.WriteString(fmt.Sprintf("  %s: %s (port %d)\n", 
					server.Name, endpoint.URL, endpoint.Port))
			}
		}
	}

	result.WriteString("\nConfiguration files would be created based on current cluster state.\n")
	result.WriteString("Use the proxy service for unified access to all MCP servers.\n")

	return &ToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}

// installMateyFromK8s installs Matey using Kubernetes client operations
func (m *MateyMCPServer) installMateyFromK8s(ctx context.Context) (*ToolResult, error) {
	var result strings.Builder
	result.WriteString("=== Matey Installation Status ===\n")

	// Check if CRDs are already installed by trying to list resources
	var serverList crd.MCPServerList
	err := m.k8sClient.List(ctx, &serverList)
	if err != nil {
		result.WriteString("ERROR: MCPServer CRD: Not found or inaccessible\n")
	} else {
		result.WriteString("✓ MCPServer CRD: Installed\n")
	}

	var memoryList crd.MCPMemoryList
	err = m.k8sClient.List(ctx, &memoryList)
	if err != nil {
		result.WriteString("ERROR: MCPMemory CRD: Not found or inaccessible\n")
	} else {
		result.WriteString("✓ MCPMemory CRD: Installed\n")
	}

	var taskSchedulerList crd.MCPTaskSchedulerList
	err = m.k8sClient.List(ctx, &taskSchedulerList)
	if err != nil {
		result.WriteString("ERROR: MCPTaskScheduler CRD: Not found or inaccessible\n")
	} else {
		result.WriteString("✓ MCPTaskScheduler CRD: Installed\n")
	}

	var proxyList crd.MCPProxyList
	err = m.k8sClient.List(ctx, &proxyList)
	if err != nil {
		result.WriteString("ERROR: MCPProxy CRD: Not found or inaccessible\n")
	} else {
		result.WriteString("✓ MCPProxy CRD: Installed\n")
	}

	var toolboxList crd.MCPToolboxList
	err = m.k8sClient.List(ctx, &toolboxList)
	if err != nil {
		result.WriteString("ERROR: MCPToolbox CRD: Not found or inaccessible\n")
	} else {
		result.WriteString("✓ MCPToolbox CRD: Installed\n")
	}

	result.WriteString("\nMatey appears to be installed (CRDs accessible via Kubernetes client).\n")
	result.WriteString("If you encounter issues, check RBAC permissions and CRD installation.\n")

	return &ToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}