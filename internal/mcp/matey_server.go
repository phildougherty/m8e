package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/phildougherty/m8e/internal/compose"
	"github.com/phildougherty/m8e/internal/crd"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"k8s.io/apimachinery/pkg/types"
	corev1 "k8s.io/api/core/v1"
)

// MateyMCPServer provides MCP tools for interacting with Matey and the cluster
type MateyMCPServer struct {
	mateyBinary string
	configFile  string
	namespace   string
	k8sClient   client.Client
	composer    *compose.K8sComposer
	config      *rest.Config
}

// NewMateyMCPServer creates a new Matey MCP server
func NewMateyMCPServer(mateyBinary, configFile, namespace string) *MateyMCPServer {
	server := &MateyMCPServer{
		mateyBinary: mateyBinary,
		configFile:  configFile,
		namespace:   namespace,
	}
	
	// Initialize k8s client and composer
	server.initializeK8sComponents()
	
	return server
}

// initializeK8sComponents initializes the Kubernetes client and composer
func (m *MateyMCPServer) initializeK8sComponents() {
	// Create composer
	composer, err := compose.NewK8sComposer(m.configFile, m.namespace)
	if err != nil {
		// Log error but don't fail - fallback to binary execution
		fmt.Printf("Warning: Failed to initialize composer, falling back to binary execution: %v\n", err)
		return
	}
	m.composer = composer
	
	// Create k8s client
	config, err := createK8sConfig()
	if err != nil {
		fmt.Printf("Warning: Failed to create k8s config: %v\n", err)
		return
	}
	m.config = config
	
	scheme := runtime.NewScheme()
	if err := crd.AddToScheme(scheme); err != nil {
		fmt.Printf("Warning: Failed to add CRD scheme: %v\n", err)
		return
	}
	
	client, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		fmt.Printf("Warning: Failed to create k8s client: %v\n", err)
		return
	}
	m.k8sClient = client
}

// createK8sConfig creates a Kubernetes config from in-cluster or kubeconfig
func createK8sConfig() (*rest.Config, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}
	
	// Fall back to kubeconfig
	return clientcmd.BuildConfigFromFlags("", filepath.Join(os.Getenv("HOME"), ".kube", "config"))
}

// Tool represents an MCP tool
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// ToolResult represents the result of a tool execution
type ToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Content represents content in a tool result
type Content struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// GetTools returns the available MCP tools
func (m *MateyMCPServer) GetTools() []Tool {
	return []Tool{
		{
			Name:        "matey_ps",
			Description: "Get status of all MCP servers in the cluster",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"watch": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to watch for live updates",
						"default":     false,
					},
					"filter": map[string]interface{}{
						"type":        "string",
						"description": "Filter by status, namespace, or labels",
					},
				},
			},
		},
		{
			Name:        "matey_up",
			Description: "Start all or specific MCP services",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"services": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Specific services to start (empty for all)",
					},
				},
			},
		},
		{
			Name:        "matey_down",
			Description: "Stop all or specific MCP services",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"services": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Specific services to stop (empty for all)",
					},
				},
			},
		},
		{
			Name:        "matey_logs",
			Description: "Get logs from MCP servers",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"server": map[string]interface{}{
						"type":        "string",
						"description": "Server name to get logs from",
					},
					"follow": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether to follow logs",
						"default":     false,
					},
					"tail": map[string]interface{}{
						"type":        "integer",
						"description": "Number of lines to tail",
						"default":     100,
					},
				},
			},
		},
		{
			Name:        "matey_inspect",
			Description: "Get detailed information about MCP resources",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"resource_type": map[string]interface{}{
						"type":        "string",
						"description": "Resource type (server, memory, task-scheduler, toolbox, workflow)",
					},
					"resource_name": map[string]interface{}{
						"type":        "string",
						"description": "Specific resource name",
					},
					"output_format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (table, json, yaml)",
						"default":     "table",
					},
				},
			},
		},
		{
			Name:        "apply_config",
			Description: "Apply a YAML configuration to the cluster",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"config_yaml": map[string]interface{}{
						"type":        "string",
						"description": "YAML configuration to apply",
					},
					"config_type": map[string]interface{}{
						"type":        "string",
						"description": "Type of config (matey, workflow, toolbox, etc.)",
					},
				},
				"required": []string{"config_yaml"},
			},
		},
		{
			Name:        "get_cluster_state",
			Description: "Get current state of the cluster and MCP resources",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"include_pods": map[string]interface{}{
						"type":        "boolean",
						"description": "Include pod information",
						"default":     true,
					},
					"include_logs": map[string]interface{}{
						"type":        "boolean",
						"description": "Include recent logs",
						"default":     false,
					},
				},
			},
		},
		{
			Name:        "create_workflow",
			Description: "Create a workflow from provided configuration",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "Workflow description",
					},
					"schedule": map[string]interface{}{
						"type":        "string",
						"description": "Cron schedule for workflow",
					},
					"steps": map[string]interface{}{
						"type":        "array",
						"description": "Workflow steps",
						"items": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"name":       map[string]interface{}{"type": "string"},
								"tool":       map[string]interface{}{"type": "string"},
								"parameters": map[string]interface{}{"type": "object"},
							},
							"required": []string{"name", "tool"},
						},
					},
				},
				"required": []string{"name", "steps"},
			},
		},
		// Workflow management tools
		{
			Name:        "list_workflows",
			Description: "List all workflows in the cluster",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"all_namespaces": map[string]interface{}{
						"type":        "boolean",
						"description": "List workflows from all namespaces",
						"default":     false,
					},
					"output_format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (table, json, yaml)",
						"default":     "table",
					},
				},
			},
		},
		{
			Name:        "get_workflow",
			Description: "Get details of a specific workflow",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
					},
					"output_format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (table, json, yaml)",
						"default":     "table",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "delete_workflow",
			Description: "Delete a workflow",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "execute_workflow",
			Description: "Manually execute a workflow",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "workflow_logs",
			Description: "Get workflow execution logs",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
					},
					"step": map[string]interface{}{
						"type":        "string",
						"description": "Get logs for specific step",
					},
					"follow": map[string]interface{}{
						"type":        "boolean",
						"description": "Follow log output",
						"default":     false,
					},
					"tail": map[string]interface{}{
						"type":        "integer",
						"description": "Number of lines to show from the end",
						"default":     100,
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "pause_workflow",
			Description: "Pause a workflow",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "resume_workflow",
			Description: "Resume a paused workflow",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "workflow_templates",
			Description: "List available workflow templates",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"category": map[string]interface{}{
						"type":        "string",
						"description": "Filter by category",
					},
				},
			},
		},
		// Service management tools
		{
			Name:        "start_service",
			Description: "Start specific MCP services",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"services": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Service names to start",
					},
				},
				"required": []string{"services"},
			},
		},
		{
			Name:        "stop_service",
			Description: "Stop specific MCP services",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"services": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Service names to stop",
					},
				},
				"required": []string{"services"},
			},
		},
		{
			Name:        "reload_proxy",
			Description: "Reload MCP proxy configuration to discover new servers",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		// Memory service management
		{
			Name:        "memory_status",
			Description: "Get memory service status",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "memory_start",
			Description: "Start memory service",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "memory_stop",
			Description: "Stop memory service",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		// Task scheduler management
		{
			Name:        "task_scheduler_status",
			Description: "Get task scheduler status",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "task_scheduler_start",
			Description: "Start task scheduler service",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "task_scheduler_stop",
			Description: "Stop task scheduler service",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		// Toolbox management
		{
			Name:        "list_toolboxes",
			Description: "List all toolboxes in the cluster",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"output_format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (table, json, yaml)",
						"default":     "table",
					},
				},
			},
		},
		{
			Name:        "get_toolbox",
			Description: "Get details of a specific toolbox",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Toolbox name",
					},
					"output_format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (table, json, yaml)",
						"default":     "table",
					},
				},
				"required": []string{"name"},
			},
		},
		// Configuration management
		{
			Name:        "validate_config",
			Description: "Validate the matey configuration file",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"config_file": map[string]interface{}{
						"type":        "string",
						"description": "Path to config file to validate",
					},
				},
			},
		},
		{
			Name:        "create_config",
			Description: "Create client configuration for MCP servers",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"output_file": map[string]interface{}{
						"type":        "string",
						"description": "Output file path",
					},
				},
			},
		},
		{
			Name:        "install_matey",
			Description: "Install Matey CRDs and required Kubernetes resources",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

// ExecuteTool executes a specific MCP tool
func (m *MateyMCPServer) ExecuteTool(ctx context.Context, name string, arguments map[string]interface{}) (*ToolResult, error) {
	switch name {
	// Core server management
	case "matey_ps":
		return m.mateyPS(ctx, arguments)
	case "matey_up":
		return m.mateyUp(ctx, arguments)
	case "matey_down":
		return m.mateyDown(ctx, arguments)
	case "matey_logs":
		return m.mateyLogs(ctx, arguments)
	case "matey_inspect":
		return m.mateyInspect(ctx, arguments)
	case "apply_config":
		return m.applyConfig(ctx, arguments)
	case "get_cluster_state":
		return m.getClusterState(ctx, arguments)
	
	// Workflow management
	case "create_workflow":
		return m.createWorkflow(ctx, arguments)
	case "list_workflows":
		return m.listWorkflows(ctx, arguments)
	case "get_workflow":
		return m.getWorkflow(ctx, arguments)
	case "delete_workflow":
		return m.deleteWorkflow(ctx, arguments)
	case "execute_workflow":
		return m.executeWorkflow(ctx, arguments)
	case "workflow_logs":
		return m.workflowLogs(ctx, arguments)
	case "pause_workflow":
		return m.pauseWorkflow(ctx, arguments)
	case "resume_workflow":
		return m.resumeWorkflow(ctx, arguments)
	case "workflow_templates":
		return m.workflowTemplates(ctx, arguments)
	
	// Service management
	case "start_service":
		return m.startService(ctx, arguments)
	case "stop_service":
		return m.stopService(ctx, arguments)
	case "reload_proxy":
		return m.reloadProxy(ctx, arguments)
	
	// Memory service management
	case "memory_status":
		return m.memoryStatus(ctx, arguments)
	case "memory_start":
		return m.memoryStart(ctx, arguments)
	case "memory_stop":
		return m.memoryStop(ctx, arguments)
	
	// Task scheduler management
	case "task_scheduler_status":
		return m.taskSchedulerStatus(ctx, arguments)
	case "task_scheduler_start":
		return m.taskSchedulerStart(ctx, arguments)
	case "task_scheduler_stop":
		return m.taskSchedulerStop(ctx, arguments)
	
	// Toolbox management
	case "list_toolboxes":
		return m.listToolboxes(ctx, arguments)
	case "get_toolbox":
		return m.getToolbox(ctx, arguments)
	
	// Configuration management
	case "validate_config":
		return m.validateConfig(ctx, arguments)
	case "create_config":
		return m.createConfig(ctx, arguments)
	case "install_matey":
		return m.installMatey(ctx, arguments)
	
	default:
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Unknown tool: %s", name)}},
			IsError: true,
		}, fmt.Errorf("unknown tool: %s", name)
	}
}

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
	cmdArgs := []string{"logs"}
	
	if server, ok := args["server"].(string); ok && server != "" {
		cmdArgs = append(cmdArgs, server)
	}
	
	if follow, ok := args["follow"].(bool); ok && follow {
		cmdArgs = append(cmdArgs, "-f")
	}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error running matey logs: %v\nOutput: %s", err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// mateyInspect executes 'matey inspect' command
func (m *MateyMCPServer) mateyInspect(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	cmdArgs := []string{"inspect"}
	
	if resourceType, ok := args["resource_type"].(string); ok && resourceType != "" {
		cmdArgs = append(cmdArgs, resourceType)
	}
	
	if resourceName, ok := args["resource_name"].(string); ok && resourceName != "" {
		cmdArgs = append(cmdArgs, resourceName)
	}
	
	if outputFormat, ok := args["output_format"].(string); ok && outputFormat != "" {
		cmdArgs = append(cmdArgs, "-o", outputFormat)
	}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error running matey inspect: %v\nOutput: %s", err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
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
		// Fall back to binary execution
		psOutput, err := m.runMateyCommand(ctx, "ps", "-o", "json")
		if err == nil {
			result.WriteString("=== MCP Servers ===\n")
			result.WriteString(psOutput)
			result.WriteString("\n\n")
		}
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
			logsOutput, err := m.runMateyCommand(ctx, "logs", "--tail", "50")
			if err == nil {
				result.WriteString("=== Recent Logs ===\n")
				result.WriteString(logsOutput)
				result.WriteString("\n\n")
			}
		}
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: result.String()}},
	}, nil
}

// createWorkflow creates a workflow from provided configuration
func (m *MateyMCPServer) createWorkflow(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	steps, ok := args["steps"].([]interface{})
	if !ok || len(steps) == 0 {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "steps are required"}},
			IsError: true,
		}, fmt.Errorf("steps are required")
	}
	
	// Process steps to ensure correct structure for CRD
	processedSteps := make([]map[string]interface{}, 0, len(steps))
	for i, step := range steps {
		stepMap, ok := step.(map[string]interface{})
		if !ok {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("step %d is not a valid object", i)}},
				IsError: true,
			}, fmt.Errorf("step %d is not a valid object", i)
		}
		
		// Validate required fields
		stepName, nameOk := stepMap["name"].(string)
		stepTool, toolOk := stepMap["tool"].(string)
		if !nameOk || !toolOk {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("step %d is missing required 'name' or 'tool' field", i)}},
				IsError: true,
			}, fmt.Errorf("step %d is missing required 'name' or 'tool' field", i)
		}
		
		// Create properly structured step
		processedStep := map[string]interface{}{
			"name": stepName,
			"tool": stepTool,
		}
		
		// Add parameters if they exist
		if parameters, ok := stepMap["parameters"].(map[string]interface{}); ok {
			processedStep["parameters"] = parameters
		}
		
		// Add other optional fields
		if condition, ok := stepMap["condition"].(string); ok && condition != "" {
			processedStep["condition"] = condition
		}
		
		if continueOnError, ok := stepMap["continueOnError"].(bool); ok {
			processedStep["continueOnError"] = continueOnError
		}
		
		if dependsOn, ok := stepMap["dependsOn"].([]interface{}); ok && len(dependsOn) > 0 {
			processedStep["dependsOn"] = dependsOn
		}
		
		processedSteps = append(processedSteps, processedStep)
	}
	
	// Create workflow YAML structure matching the CRD
	workflow := map[string]interface{}{
		"apiVersion": "mcp.matey.ai/v1",
		"kind":       "Workflow",
		"metadata": map[string]interface{}{
			"name": name,
		},
		"spec": map[string]interface{}{
			"enabled":  true,
			"steps":    processedSteps,
			"schedule": "0 0 * * *", // Default to daily at midnight
		},
	}
	
	// Add description as annotation if provided
	if description, ok := args["description"].(string); ok && description != "" {
		metadata := workflow["metadata"].(map[string]interface{})
		annotations := make(map[string]interface{})
		annotations["description"] = description
		metadata["annotations"] = annotations
	}
	
	// Override default schedule if provided
	if schedule, ok := args["schedule"].(string); ok && schedule != "" {
		workflow["spec"].(map[string]interface{})["schedule"] = schedule
	}
	
	// Convert to JSON (kubectl accepts both JSON and YAML)
	jsonBytes, err := json.MarshalIndent(workflow, "", "  ")
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error creating workflow JSON: %v", err)}},
			IsError: true,
		}, err
	}
	
	// Apply the workflow
	return m.applyConfig(ctx, map[string]interface{}{
		"config_yaml": string(jsonBytes),
		"config_type": "workflow",
	})
}

// runMateyCommand runs a matey command and returns the output (fallback for unsupported operations)
func (m *MateyMCPServer) runMateyCommand(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, m.mateyBinary, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// useK8sClient returns true if we can use k8s client for operations
func (m *MateyMCPServer) useK8sClient() bool {
	return m.k8sClient != nil && m.composer != nil
}

// Workflow management implementations

// listWorkflows lists all workflows in the cluster
func (m *MateyMCPServer) listWorkflows(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	// Try direct k8s client first
	if m.k8sClient != nil {
		var workflows crd.WorkflowList
		namespace := m.namespace
		if allNamespaces, ok := args["all_namespaces"].(bool); ok && allNamespaces {
			namespace = ""
		}
		
		listOpts := &client.ListOptions{}
		if namespace != "" {
			listOpts.Namespace = namespace
		}
		
		err := m.k8sClient.List(ctx, &workflows, listOpts)
		if err != nil {
			// Fall back to binary execution
			return m.listWorkflowsWithBinary(ctx, args)
		}
		
		// Format output
		outputFormat := "table"
		if format, ok := args["output_format"].(string); ok && format != "" {
			outputFormat = format
		}
		
		return m.formatWorkflowList(workflows.Items, outputFormat)
	}
	
	// Fall back to binary execution
	return m.listWorkflowsWithBinary(ctx, args)
}

// listWorkflowsWithBinary falls back to binary execution
func (m *MateyMCPServer) listWorkflowsWithBinary(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	cmdArgs := []string{"workflow", "list"}
	
	if allNamespaces, ok := args["all_namespaces"].(bool); ok && allNamespaces {
		cmdArgs = append(cmdArgs, "--all-namespaces")
	}
	
	if outputFormat, ok := args["output_format"].(string); ok && outputFormat != "" {
		cmdArgs = append(cmdArgs, "-o", outputFormat)
	}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error listing workflows: %v\nOutput: %s", err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// formatWorkflowList formats workflow list output
func (m *MateyMCPServer) formatWorkflowList(workflows []crd.Workflow, outputFormat string) (*ToolResult, error) {
	switch outputFormat {
	case "json":
		jsonBytes, err := json.MarshalIndent(workflows, "", "  ")
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error formatting workflows as JSON: %v", err)}},
				IsError: true,
			}, err
		}
		return &ToolResult{
			Content: []Content{{Type: "text", Text: string(jsonBytes)}},
		}, nil
	case "yaml":
		// For YAML output, we'll use a simple format
		var yamlOutput strings.Builder
		for _, workflow := range workflows {
			yamlOutput.WriteString(fmt.Sprintf("- name: %s\n", workflow.Name))
			yamlOutput.WriteString(fmt.Sprintf("  namespace: %s\n", workflow.Namespace))
			yamlOutput.WriteString(fmt.Sprintf("  enabled: %v\n", workflow.Spec.Enabled))
			if workflow.Spec.Schedule != "" {
				yamlOutput.WriteString(fmt.Sprintf("  schedule: %s\n", workflow.Spec.Schedule))
			}
			yamlOutput.WriteString("\n")
		}
		return &ToolResult{
			Content: []Content{{Type: "text", Text: yamlOutput.String()}},
		}, nil
	default: // table format
		var output strings.Builder
		output.WriteString("NAME                 NAMESPACE       ENABLED    SCHEDULE        LAST RUN             PHASE     \n")
		output.WriteString("----                 ---------       -------    --------        --------             -----     \n")
		
		for _, workflow := range workflows {
			name := workflow.Name
			if len(name) > 20 {
				name = name[:17] + "..."
			}
			
			namespace := workflow.Namespace
			if len(namespace) > 15 {
				namespace = namespace[:12] + "..."
			}
			
			enabled := fmt.Sprintf("%v", workflow.Spec.Enabled)
			schedule := workflow.Spec.Schedule
			if schedule == "" {
				schedule = "Manual"
			}
			if len(schedule) > 15 {
				schedule = schedule[:12] + "..."
			}
			
			lastRun := "Never"
			if workflow.Status.LastExecutionTime != nil {
				lastRun = workflow.Status.LastExecutionTime.Format("2006-01-02 15:04")
			}
			
			phase := "Unknown"
			if workflow.Status.Phase != "" {
				phase = string(workflow.Status.Phase)
			}
			
			output.WriteString(fmt.Sprintf("%-20s %-15s %-10s %-15s %-20s %-10s\n",
				name, namespace, enabled, schedule, lastRun, phase))
		}
		
		return &ToolResult{
			Content: []Content{{Type: "text", Text: output.String()}},
		}, nil
	}
}

// getWorkflow gets details of a specific workflow
func (m *MateyMCPServer) getWorkflow(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "Error: name parameter is required for get_workflow. Usage: get_workflow({\"name\": \"workflow-name\"})"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	// Try direct k8s client first
	if m.k8sClient != nil {
		workflow := &crd.Workflow{}
		err := m.k8sClient.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: m.namespace,
		}, workflow)
		
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error finding workflow %s: %v", name, err)}},
				IsError: true,
			}, err
		}
		
		// Format output
		outputFormat := "table"
		if format, ok := args["output_format"].(string); ok && format != "" {
			outputFormat = format
		}
		
		return m.formatSingleWorkflow(*workflow, outputFormat)
	}
	
	// Fall back to binary execution
	return m.getWorkflowWithBinary(ctx, args)
}

func (m *MateyMCPServer) getWorkflowWithBinary(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	cmdArgs := []string{"workflow", "get", name}
	
	if outputFormat, ok := args["output_format"].(string); ok && outputFormat != "" {
		cmdArgs = append(cmdArgs, "-o", outputFormat)
	}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting workflow %s: %v\nOutput: %s", name, err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

func (m *MateyMCPServer) formatSingleWorkflow(workflow crd.Workflow, outputFormat string) (*ToolResult, error) {
	switch outputFormat {
	case "json":
		jsonBytes, err := json.MarshalIndent(workflow, "", "  ")
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error formatting workflow as JSON: %v", err)}},
				IsError: true,
			}, err
		}
		return &ToolResult{
			Content: []Content{{Type: "text", Text: string(jsonBytes)}},
		}, nil
	case "yaml":
		var yamlOutput strings.Builder
		yamlOutput.WriteString(fmt.Sprintf("name: %s\n", workflow.Name))
		yamlOutput.WriteString(fmt.Sprintf("namespace: %s\n", workflow.Namespace))
		yamlOutput.WriteString(fmt.Sprintf("enabled: %v\n", workflow.Spec.Enabled))
		if workflow.Spec.Schedule != "" {
			yamlOutput.WriteString(fmt.Sprintf("schedule: %s\n", workflow.Spec.Schedule))
		}
		yamlOutput.WriteString(fmt.Sprintf("phase: %s\n", workflow.Status.Phase))
		if workflow.Status.LastExecutionTime != nil {
			yamlOutput.WriteString(fmt.Sprintf("last_run: %s\n", workflow.Status.LastExecutionTime.Format("2006-01-02 15:04:05")))
		}
		return &ToolResult{
			Content: []Content{{Type: "text", Text: yamlOutput.String()}},
		}, nil
	default: // table format
		var output strings.Builder
		output.WriteString("=== Workflow Details ===\n")
		output.WriteString(fmt.Sprintf("Name: %s\n", workflow.Name))
		output.WriteString(fmt.Sprintf("Namespace: %s\n", workflow.Namespace))
		output.WriteString(fmt.Sprintf("Enabled: %v\n", workflow.Spec.Enabled))
		if workflow.Spec.Schedule != "" {
			output.WriteString(fmt.Sprintf("Schedule: %s\n", workflow.Spec.Schedule))
		}
		output.WriteString(fmt.Sprintf("Phase: %s\n", workflow.Status.Phase))
		if workflow.Status.LastExecutionTime != nil {
			output.WriteString(fmt.Sprintf("Last Run: %s\n", workflow.Status.LastExecutionTime.Format("2006-01-02 15:04:05")))
		}
		
		if len(workflow.Spec.Steps) > 0 {
			output.WriteString("\n=== Steps ===\n")
			for i, step := range workflow.Spec.Steps {
				output.WriteString(fmt.Sprintf("%d. %s (tool: %s)\n", i+1, step.Name, step.Tool))
			}
		}
		
		return &ToolResult{
			Content: []Content{{Type: "text", Text: output.String()}},
		}, nil
	}
}

// deleteWorkflow deletes a workflow
func (m *MateyMCPServer) deleteWorkflow(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	// Try direct k8s client first
	if m.k8sClient != nil {
		workflow := &crd.Workflow{}
		err := m.k8sClient.Get(ctx, types.NamespacedName{
			Name:      name,
			Namespace: m.namespace,
		}, workflow)
		
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error finding workflow %s: %v", name, err)}},
				IsError: true,
			}, err
		}
		
		err = m.k8sClient.Delete(ctx, workflow)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error deleting workflow %s: %v", name, err)}},
				IsError: true,
			}, err
		}
		
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Workflow %s deleted successfully", name)}},
		}, nil
	}
	
	// Fall back to binary execution
	return m.deleteWorkflowWithBinary(ctx, args)
}

func (m *MateyMCPServer) deleteWorkflowWithBinary(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	cmdArgs := []string{"workflow", "delete", name}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error deleting workflow %s: %v\nOutput: %s", name, err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// executeWorkflow manually executes a workflow
func (m *MateyMCPServer) executeWorkflow(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	cmdArgs := []string{"workflow", "execute", name}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error executing workflow %s: %v\nOutput: %s", name, err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// workflowLogs gets workflow execution logs
func (m *MateyMCPServer) workflowLogs(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	cmdArgs := []string{"workflow", "logs", name}
	
	if step, ok := args["step"].(string); ok && step != "" {
		cmdArgs = append(cmdArgs, "--step", step)
	}
	
	if follow, ok := args["follow"].(bool); ok && follow {
		cmdArgs = append(cmdArgs, "-f")
	}
	
	if tail, ok := args["tail"].(float64); ok && tail > 0 {
		cmdArgs = append(cmdArgs, "--tail", fmt.Sprintf("%.0f", tail))
	}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting workflow logs for %s: %v\nOutput: %s", name, err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// pauseWorkflow pauses a workflow
func (m *MateyMCPServer) pauseWorkflow(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	cmdArgs := []string{"workflow", "pause", name}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error pausing workflow %s: %v\nOutput: %s", name, err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// resumeWorkflow resumes a paused workflow
func (m *MateyMCPServer) resumeWorkflow(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	cmdArgs := []string{"workflow", "resume", name}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error resuming workflow %s: %v\nOutput: %s", name, err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// workflowTemplates lists available workflow templates
func (m *MateyMCPServer) workflowTemplates(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	cmdArgs := []string{"workflow", "templates"}
	
	if category, ok := args["category"].(string); ok && category != "" {
		cmdArgs = append(cmdArgs, "--category", category)
	}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error listing workflow templates: %v\nOutput: %s", err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// Service management implementations

// startService starts specific MCP services
func (m *MateyMCPServer) startService(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	services, ok := args["services"].([]interface{})
	if !ok || len(services) == 0 {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "services array is required"}},
			IsError: true,
		}, fmt.Errorf("services array is required")
	}
	
	cmdArgs := []string{"start"}
	
	for _, service := range services {
		if s, ok := service.(string); ok {
			cmdArgs = append(cmdArgs, s)
		}
	}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error starting services: %v\nOutput: %s", err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// stopService stops specific MCP services
func (m *MateyMCPServer) stopService(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	services, ok := args["services"].([]interface{})
	if !ok || len(services) == 0 {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "services array is required"}},
			IsError: true,
		}, fmt.Errorf("services array is required")
	}
	
	cmdArgs := []string{"stop"}
	
	for _, service := range services {
		if s, ok := service.(string); ok {
			cmdArgs = append(cmdArgs, s)
		}
	}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error stopping services: %v\nOutput: %s", err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// reloadProxy reloads MCP proxy configuration
func (m *MateyMCPServer) reloadProxy(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	cmdArgs := []string{"reload"}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error reloading proxy: %v\nOutput: %s", err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// Memory service management implementations

// memoryStatus gets memory service status
func (m *MateyMCPServer) memoryStatus(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	cmdArgs := []string{"memory", "status"}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting memory status: %v\nOutput: %s", err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// memoryStart starts memory service
func (m *MateyMCPServer) memoryStart(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	cmdArgs := []string{"memory", "start"}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error starting memory service: %v\nOutput: %s", err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// memoryStop stops memory service
func (m *MateyMCPServer) memoryStop(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	cmdArgs := []string{"memory", "stop"}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error stopping memory service: %v\nOutput: %s", err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// Task scheduler management implementations

// taskSchedulerStatus gets task scheduler status
func (m *MateyMCPServer) taskSchedulerStatus(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	cmdArgs := []string{"task-scheduler", "status"}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting task scheduler status: %v\nOutput: %s", err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// taskSchedulerStart starts task scheduler service
func (m *MateyMCPServer) taskSchedulerStart(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	cmdArgs := []string{"task-scheduler", "start"}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error starting task scheduler: %v\nOutput: %s", err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// taskSchedulerStop stops task scheduler service
func (m *MateyMCPServer) taskSchedulerStop(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	cmdArgs := []string{"task-scheduler", "stop"}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error stopping task scheduler: %v\nOutput: %s", err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// Toolbox management implementations

// listToolboxes lists all toolboxes in the cluster
func (m *MateyMCPServer) listToolboxes(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	cmdArgs := []string{"toolbox", "list"}
	
	if outputFormat, ok := args["output_format"].(string); ok && outputFormat != "" {
		cmdArgs = append(cmdArgs, "-o", outputFormat)
	}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error listing toolboxes: %v\nOutput: %s", err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
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
	
	cmdArgs := []string{"toolbox", "get", name}
	
	if outputFormat, ok := args["output_format"].(string); ok && outputFormat != "" {
		cmdArgs = append(cmdArgs, "-o", outputFormat)
	}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting toolbox %s: %v\nOutput: %s", name, err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// Configuration management implementations

// validateConfig validates the matey configuration file
func (m *MateyMCPServer) validateConfig(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	cmdArgs := []string{"validate"}
	
	if configFile, ok := args["config_file"].(string); ok && configFile != "" {
		cmdArgs = append(cmdArgs, "-c", configFile)
	} else if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Configuration validation failed: %v\nOutput: %s", err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// createConfig creates client configuration for MCP servers
func (m *MateyMCPServer) createConfig(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	cmdArgs := []string{"create-config"}
	
	if outputFile, ok := args["output_file"].(string); ok && outputFile != "" {
		cmdArgs = append(cmdArgs, "-o", outputFile)
	}
	
	if m.configFile != "" {
		cmdArgs = append(cmdArgs, "-c", m.configFile)
	}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error creating config: %v\nOutput: %s", err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}

// installMatey installs Matey CRDs and required Kubernetes resources
func (m *MateyMCPServer) installMatey(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	cmdArgs := []string{"install"}
	
	if m.namespace != "" {
		cmdArgs = append(cmdArgs, "-n", m.namespace)
	}
	
	output, err := m.runMateyCommand(ctx, cmdArgs...)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error installing Matey: %v\nOutput: %s", err, output)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: output}},
	}, nil
}