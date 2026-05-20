package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/phildougherty/m8e/internal/compose"
	"github.com/phildougherty/m8e/internal/crd"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// clusterTools owns the cluster lifecycle and introspection tools: matey_ps,
// matey_up, matey_down, matey_logs, matey_inspect and get_cluster_state, plus
// the pod-log and resource-inspection helpers they delegate to.
type clusterTools struct {
	deps clusterDeps
}

func newClusterTools(deps clusterDeps) *clusterTools {
	return &clusterTools{deps: deps}
}

// mateyPS executes 'matey ps' command using compose library
func (c *clusterTools) mateyPS(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	// Use compose library directly instead of subprocess
	status, err := compose.Status(c.deps.configFile)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting service status: %v", err)}},
			IsError: true,
		}, err
	}

	// Format output with service count summary
	var output strings.Builder
	serviceCount := len(status.Services)
	runningCount := 0
	for _, svc := range status.Services {
		if strings.ToLower(svc.Status) == "running" || strings.ToLower(svc.Status) == "up" {
			runningCount++
		}
	}
	output.WriteString(fmt.Sprintf("MCP Services (%d total, %d running)\n", serviceCount, runningCount))
	output.WriteString(strings.Repeat("=", 40))
	output.WriteString("\n")

	for name, svc := range status.Services {
		// Apply filter if specified
		if filter, ok := args["filter"].(string); ok && filter != "" {
			if !strings.Contains(name, filter) && !strings.Contains(svc.Status, filter) && !strings.Contains(svc.Type, filter) {
				continue
			}
		}
		output.WriteString(fmt.Sprintf("%-20s %-15s %-10s\n", name, svc.Status, svc.Type))
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: output.String()}},
	}, nil
}

// mateyUp executes 'matey up' command using compose library
func (c *clusterTools) mateyUp(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	// Extract service names from arguments
	var serviceNames []string
	if services, ok := args["services"].([]interface{}); ok && len(services) > 0 {
		for _, service := range services {
			if s, ok := service.(string); ok {
				serviceNames = append(serviceNames, s)
			}
		}
	}

	// Use compose library directly instead of subprocess
	err := compose.Up(c.deps.configFile, serviceNames)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error starting services: %v", err)}},
			IsError: true,
		}, err
	}

	// Return success message
	var output strings.Builder
	if len(serviceNames) > 0 {
		output.WriteString(fmt.Sprintf("Successfully started services: %s\n", strings.Join(serviceNames, ", ")))
	} else {
		output.WriteString("Successfully started all enabled services\n")
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: output.String()}},
	}, nil
}

// mateyDown executes 'matey down' command using compose library
func (c *clusterTools) mateyDown(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	// Extract service names from arguments
	var serviceNames []string
	if services, ok := args["services"].([]interface{}); ok && len(services) > 0 {
		for _, service := range services {
			if s, ok := service.(string); ok {
				serviceNames = append(serviceNames, s)
			}
		}
	}

	// Use compose library directly instead of subprocess
	err := compose.Down(c.deps.configFile, serviceNames)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error stopping services: %v", err)}},
			IsError: true,
		}, err
	}

	// Return success message
	var output strings.Builder
	if len(serviceNames) > 0 {
		output.WriteString(fmt.Sprintf("Successfully stopped services: %s\n", strings.Join(serviceNames, ", ")))
	} else {
		output.WriteString("Successfully stopped all services\n")
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: output.String()}},
	}, nil
}

// mateyLogs executes 'matey logs' command
func (c *clusterTools) mateyLogs(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	// Try using compose.Logs directly
	if c.deps.useK8sClient() {
		var serverNames []string

		if server, ok := args["server"].(string); ok && server != "" {
			serverNames = []string{server}
		}

		follow := false
		if f, ok := args["follow"].(bool); ok {
			follow = f
		}

		// Capture logs output using compose.Logs
		err := compose.Logs(c.deps.configFile, serverNames, follow)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting logs: %v", err)}},
				IsError: true,
			}, err
		}

		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Logs command executed successfully"}},
		}, nil
	}

	// Fall back to binary execution
	return c.mateyLogsWithBinary(ctx, args)
}

func (c *clusterTools) mateyLogsWithBinary(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if !c.deps.useK8sClient() {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot retrieve logs without binary."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}

	// Get server name from args
	serverName, _ := args["server"].(string)
	tailLines := int64(100) // default
	if tail, ok := args["tail"].(float64); ok {
		tailLines = int64(tail)
	}

	// Use Kubernetes client to get pod logs
	var result strings.Builder

	if serverName != "" {
		// Get logs for specific server
		logs, err := c.getPodLogsForServer(ctx, serverName, tailLines)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting logs for server %s: %v", serverName, err)}},
				IsError: true,
			}, err
		}
		result.WriteString(logs)
	} else {
		// Get logs for all MCP servers
		logs, err := c.getAllMCPServerLogs(ctx, tailLines)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting all server logs: %v", err)}},
				IsError: true,
			}, err
		}
		result.WriteString(logs)
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}

// mateyInspect executes resource inspection using Kubernetes client
func (c *clusterTools) mateyInspect(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if !c.deps.useK8sClient() {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Kubernetes client not available. Cannot inspect resources."}},
			IsError: true,
		}, fmt.Errorf("kubernetes client not available")
	}

	resourceType, _ := args["resource_type"].(string)
	resourceName, _ := args["resource_name"].(string)
	outputFormat, _ := args["output_format"].(string)
	if outputFormat == "" {
		outputFormat = "table"
	}

	switch resourceType {
	case "server", "servers":
		return c.inspectMCPServers(ctx, resourceName, outputFormat)
	case "memory":
		return c.inspectMCPMemory(ctx, resourceName, outputFormat)
	case "task-scheduler", "taskscheduler":
		return c.inspectMCPTaskScheduler(ctx, resourceName, outputFormat)
	case "proxy":
		return c.inspectMCPProxy(ctx, resourceName, outputFormat)
	case "all", "":
		return c.inspectAllResources(ctx, outputFormat)
	default:
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Unknown resource type: %s. Supported types: server, memory, task-scheduler, proxy, all", resourceType)}},
			IsError: true,
		}, fmt.Errorf("unknown resource type: %s", resourceType)
	}
}

// getClusterState gets current state of the cluster
func (c *clusterTools) getClusterState(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	var result strings.Builder

	// Get MCP servers using compose directly
	if c.deps.useK8sClient() {
		status, err := compose.Status(c.deps.configFile)
		if err == nil {
			serviceCount := len(status.Services)
			runningCount := 0
			for _, svc := range status.Services {
				if strings.ToLower(svc.Status) == "running" || strings.ToLower(svc.Status) == "up" {
					runningCount++
				}
			}
			result.WriteString(fmt.Sprintf("=== MCP Servers (%d total, %d running) ===\n", serviceCount, runningCount))
			result.WriteString(strings.Repeat("=", 50))
			result.WriteString("\n")

			for name, svc := range status.Services {
				result.WriteString(fmt.Sprintf("%-20s %-15s %-10s\n", name, svc.Status, svc.Type))
			}
			result.WriteString("\n\n")
		}
	} else {
		// Kubernetes client not available - provide informational message
		result.WriteString("=== MCP Servers ===\n")
		result.WriteString("Kubernetes client not available. Unable to retrieve MCP server status.\n\n")
	}

	// Get pods if requested (default to true)
	includePods := true
	if val, ok := args["include_pods"].(bool); ok {
		includePods = val
	}

	if includePods && c.deps.k8sClient != nil {
		var pods corev1.PodList
		listOpts := &client.ListOptions{}
		if c.deps.namespace != "" {
			listOpts.Namespace = c.deps.namespace
		}

		err := c.deps.k8sClient.List(ctx, &pods, listOpts)
		if err == nil {
			result.WriteString("=== Pods ===\n")
			result.WriteString(fmt.Sprintf("%-40s %-15s %-10s %-15s\n", "NAME", "STATUS", "READY", "RESTARTS"))
			result.WriteString(strings.Repeat("-", 85))
			result.WriteString("\n")

			for _, pod := range pods.Items {
				ready := "0/0"
				if len(pod.Status.ContainerStatuses) > 0 {
					readyCount := 0
					for _, status := range pod.Status.ContainerStatuses {
						if status.Ready {
							readyCount++
						}
					}
					ready = fmt.Sprintf("%d/%d", readyCount, len(pod.Status.ContainerStatuses))
				}

				restarts := int32(0)
				if len(pod.Status.ContainerStatuses) > 0 {
					for _, status := range pod.Status.ContainerStatuses {
						restarts += status.RestartCount
					}
				}

				result.WriteString(fmt.Sprintf("%-40s %-15s %-10s %-15d\n",
					pod.Name, pod.Status.Phase, ready, restarts))
			}
			result.WriteString("\n\n")
		}
	}

	// Get recent logs if requested
	if includeLogs, ok := args["include_logs"].(bool); ok && includeLogs {
		if c.deps.useK8sClient() {
			err := compose.Logs(c.deps.configFile, []string{}, false)
			if err == nil {
				result.WriteString("=== Recent Logs ===\n")
				result.WriteString("Logs command executed successfully\n\n")
			}
		} else {
			result.WriteString("=== Recent Logs ===\n")
			result.WriteString("Kubernetes client not available for log retrieval.\n\n")
		}
	}

	return &ToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}

// getPodLogsForServer gets logs for a specific server's pods
func (c *clusterTools) getPodLogsForServer(ctx context.Context, serverName string, tailLines int64) (string, error) {
	if c.deps.clientset == nil {
		return "", fmt.Errorf("kubernetes clientset not available")
	}

	var pods corev1.PodList
	labelSelector := client.MatchingLabels{"app": serverName}
	err := c.deps.k8sClient.List(ctx, &pods, client.InNamespace(c.deps.namespace), labelSelector)
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
		req := c.deps.clientset.CoreV1().Pods(c.deps.namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
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
func (c *clusterTools) getAllMCPServerLogs(ctx context.Context, tailLines int64) (string, error) {
	// List all MCP server resources to get server names
	var mcpServers crd.MCPServerList
	err := c.deps.k8sClient.List(ctx, &mcpServers, client.InNamespace(c.deps.namespace))
	if err != nil {
		return "", fmt.Errorf("failed to list MCP servers: %v", err)
	}

	var result strings.Builder
	result.WriteString("=== All MCP Server Logs ===\n")

	for _, server := range mcpServers.Items {
		logs, err := c.getPodLogsForServer(ctx, server.Name, tailLines)
		if err != nil {
			result.WriteString(fmt.Sprintf("\nError getting logs for %s: %v\n", server.Name, err))
			continue
		}
		result.WriteString(logs)
		result.WriteString("\n")
	}

	return result.String(), nil
}

// inspectMCPServers inspects MCP server resources
func (c *clusterTools) inspectMCPServers(ctx context.Context, resourceName, outputFormat string) (*ToolResult, error) {
	if resourceName != "" {
		// Get specific server
		var server crd.MCPServer
		err := c.deps.k8sClient.Get(ctx, client.ObjectKey{
			Name:      resourceName,
			Namespace: c.deps.namespace,
		}, &server)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting MCPServer %s: %v", resourceName, err)}},
				IsError: true,
			}, err
		}

		return c.formatMCPServerInspection(server, outputFormat), nil
	}

	// List all servers
	var servers crd.MCPServerList
	err := c.deps.k8sClient.List(ctx, &servers, client.InNamespace(c.deps.namespace))
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error listing MCPServers: %v", err)}},
			IsError: true,
		}, err
	}

	return c.formatMCPServersList(servers.Items, outputFormat), nil
}

// formatMCPServerInspection formats a single MCPServer for inspection
func (c *clusterTools) formatMCPServerInspection(server crd.MCPServer, outputFormat string) *ToolResult {
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
func (c *clusterTools) formatMCPServersList(servers []crd.MCPServer, outputFormat string) *ToolResult {
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
func (c *clusterTools) inspectMCPMemory(ctx context.Context, resourceName, outputFormat string) (*ToolResult, error) {
	var memory crd.MCPMemory
	name := resourceName
	if name == "" {
		name = "memory"
	}

	err := c.deps.k8sClient.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: c.deps.namespace,
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
func (c *clusterTools) inspectMCPTaskScheduler(ctx context.Context, resourceName, outputFormat string) (*ToolResult, error) {
	var taskScheduler crd.MCPTaskScheduler
	name := resourceName
	if name == "" {
		name = "task-scheduler"
	}

	err := c.deps.k8sClient.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: c.deps.namespace,
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
func (c *clusterTools) inspectMCPProxy(ctx context.Context, resourceName, outputFormat string) (*ToolResult, error) {
	var proxy crd.MCPProxy
	name := resourceName
	if name == "" {
		name = "proxy"
	}

	err := c.deps.k8sClient.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: c.deps.namespace,
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
func (c *clusterTools) inspectAllResources(ctx context.Context, outputFormat string) (*ToolResult, error) {
	var result strings.Builder
	result.WriteString("=== All MCP Resources ===\n\n")

	// MCP Servers
	serverResult, _ := c.inspectMCPServers(ctx, "", "table")
	result.WriteString(serverResult.Content[0].Text)
	result.WriteString("\n")

	// MCP Memory
	memoryResult, _ := c.inspectMCPMemory(ctx, "", "table")
	result.WriteString(memoryResult.Content[0].Text)
	result.WriteString("\n")

	// MCP Task Scheduler
	taskSchedulerResult, _ := c.inspectMCPTaskScheduler(ctx, "", "table")
	result.WriteString(taskSchedulerResult.Content[0].Text)
	result.WriteString("\n")

	// MCP Proxy
	proxyResult, _ := c.inspectMCPProxy(ctx, "", "table")
	result.WriteString(proxyResult.Content[0].Text)
	result.WriteString("\n")

	return &ToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}
