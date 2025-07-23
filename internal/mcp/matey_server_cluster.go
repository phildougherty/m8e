package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/phildougherty/m8e/internal/compose"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// mateyPS executes 'matey ps' command using compose library
func (m *MateyMCPServer) mateyPS(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	// Use compose library directly instead of subprocess
	status, err := compose.Status(m.configFile)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting service status: %v", err)}},
			IsError: true,
		}, err
	}
	
	// Format output similar to matey ps
	var output strings.Builder
	output.WriteString(fmt.Sprintf("%-20s %-15s %-10s\n", "SERVICE", "STATUS", "TYPE"))
	output.WriteString(strings.Repeat("-", 50))
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
func (m *MateyMCPServer) mateyUp(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
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
	err := compose.Up(m.configFile, serviceNames)
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
func (m *MateyMCPServer) mateyDown(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
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
	err := compose.Down(m.configFile, serviceNames)
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
func (m *MateyMCPServer) mateyLogs(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	// Try using compose.Logs directly
	if m.useK8sClient() {
		var serverNames []string
		
		if server, ok := args["server"].(string); ok && server != "" {
			serverNames = []string{server}
		}
		
		follow := false
		if f, ok := args["follow"].(bool); ok {
			follow = f
		}
		
		// Capture logs output using compose.Logs
		err := compose.Logs(m.configFile, serverNames, follow)
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
	return m.mateyLogsWithBinary(ctx, args)
}

func (m *MateyMCPServer) mateyLogsWithBinary(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if !m.useK8sClient() {
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
		logs, err := m.getPodLogsForServer(ctx, serverName, tailLines)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting logs for server %s: %v", serverName, err)}},
				IsError: true,
			}, err
		}
		result.WriteString(logs)
	} else {
		// Get logs for all MCP servers
		logs, err := m.getAllMCPServerLogs(ctx, tailLines)
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
func (m *MateyMCPServer) mateyInspect(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if !m.useK8sClient() {
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
		return m.inspectMCPServers(ctx, resourceName, outputFormat)
	case "memory":
		return m.inspectMCPMemory(ctx, resourceName, outputFormat)
	case "task-scheduler", "taskscheduler":
		return m.inspectMCPTaskScheduler(ctx, resourceName, outputFormat)
	case "proxy":
		return m.inspectMCPProxy(ctx, resourceName, outputFormat)
	case "toolbox":
		return m.inspectMCPToolbox(ctx, resourceName, outputFormat)
	case "all", "":
		return m.inspectAllResources(ctx, outputFormat)
	default:
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Unknown resource type: %s. Supported types: server, memory, task-scheduler, proxy, toolbox, all", resourceType)}},
			IsError: true,
		}, fmt.Errorf("unknown resource type: %s", resourceType)
	}
}

// applyConfig applies a YAML configuration to the cluster
func (m *MateyMCPServer) applyConfig(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	configYAML, ok := args["config_yaml"].(string)
	if !ok || configYAML == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "config_yaml is required"}},
			IsError: true,
		}, fmt.Errorf("config_yaml is required")
	}
	
	// Write config to temporary file
	tempFile := filepath.Join(os.TempDir(), fmt.Sprintf("matey-config-%d.yaml", time.Now().Unix()))
	err := os.WriteFile(tempFile, []byte(configYAML), 0644)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error writing config file: %v", err)}},
			IsError: true,
		}, err
	}
	defer os.Remove(tempFile)
	
	// Apply using kubectl
	cmdArgs := []string{"apply", "-f", tempFile}
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	cmd := exec.CommandContext(ctx, "kubectl", cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error applying config: %v\nOutput: %s", err, string(output))}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: string(output)}},
	}, nil
}

// getClusterState gets current state of the cluster
func (m *MateyMCPServer) getClusterState(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	var result strings.Builder
	
	// Get MCP servers using compose directly
	if m.useK8sClient() {
		status, err := compose.Status(m.configFile)
		if err == nil {
			result.WriteString("=== MCP Servers ===\n")
			result.WriteString(fmt.Sprintf("%-20s %-15s %-10s\n", "SERVICE", "STATUS", "TYPE"))
			result.WriteString(strings.Repeat("-", 50))
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
	
	if includePods && m.k8sClient != nil {
		var pods corev1.PodList
		listOpts := &client.ListOptions{}
		if m.namespace != "" {
			listOpts.Namespace = m.namespace
		}
		
		err := m.k8sClient.List(ctx, &pods, listOpts)
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
		if m.useK8sClient() {
			err := compose.Logs(m.configFile, []string{}, false)
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

// Helper functions for cluster operations