package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/phildougherty/m8e/internal/crd"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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
		defer func() {
			if err := logs.Close(); err != nil {
				fmt.Printf("Warning: Failed to close logs stream: %v\n", err)
			}
		}()
		
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
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}





