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
	fmt.Printf("DEBUG: Initializing K8s components for MCP server...\n")
	fmt.Printf("DEBUG: Config file: %s, Namespace: %s\n", m.configFile, m.namespace)
	
	// Create composer
	fmt.Printf("DEBUG: Creating K8s composer...\n")
	composer, err := compose.NewK8sComposer(m.configFile, m.namespace)
	if err != nil {
		// Log error but don't fail - fallback to binary execution
		fmt.Printf("Warning: Failed to initialize composer, falling back to binary execution: %v\n", err)
		return
	}
	m.composer = composer
	fmt.Printf("DEBUG: Composer created successfully\n")
	
	// Create k8s client
	fmt.Printf("DEBUG: Creating K8s config...\n")
	config, err := createK8sConfig()
	if err != nil {
		fmt.Printf("Warning: Failed to create k8s config: %v\n", err)
		return
	}
	m.config = config
	fmt.Printf("DEBUG: K8s config created successfully\n")
	
	fmt.Printf("DEBUG: Adding CRD scheme...\n")
	scheme := runtime.NewScheme()
	if err := crd.AddToScheme(scheme); err != nil {
		fmt.Printf("Warning: Failed to add CRD scheme: %v\n", err)
		return
	}
	fmt.Printf("DEBUG: CRD scheme added successfully\n")
	
	fmt.Printf("DEBUG: Creating K8s client...\n")
	client, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		fmt.Printf("Warning: Failed to create k8s client: %v\n", err)
		return
	}
	m.k8sClient = client
	fmt.Printf("DEBUG: K8s client created successfully\n")
	fmt.Printf("DEBUG: K8s components initialized successfully - useK8sClient() will return: %v\n", m.useK8sClient())
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
		// Task scheduler workflow management
		{
			Name:        "create_workflow",
			Description: "Create a workflow in the task scheduler",
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
						"description": "Cron schedule expression",
					},
					"timezone": map[string]interface{}{
						"type":        "string",
						"description": "Timezone for schedule (e.g., America/New_York)",
						"default":     "UTC",
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
								"dependsOn":  map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
								"condition":  map[string]interface{}{"type": "string"},
								"continueOnError": map[string]interface{}{"type": "boolean"},
								"timeout":    map[string]interface{}{"type": "string"},
							},
							"required": []string{"name", "tool"},
						},
					},
					"enabled": map[string]interface{}{
						"type":        "boolean",
						"description": "Enable/disable workflow",
						"default":     true,
					},
					"concurrencyPolicy": map[string]interface{}{
						"type":        "string",
						"description": "Concurrent execution policy (Allow, Forbid, Replace)",
						"enum":        []string{"Allow", "Forbid", "Replace"},
						"default":     "Forbid",
					},
					"timeout": map[string]interface{}{
						"type":        "string",
						"description": "Maximum workflow execution timeout (e.g., '1h', '30m')",
					},
					"retryPolicy": map[string]interface{}{
						"type":        "object",
						"description": "Workflow retry policy",
						"properties": map[string]interface{}{
							"maxRetries":      map[string]interface{}{"type": "integer"},
							"retryDelay":      map[string]interface{}{"type": "string"},
							"backoffStrategy": map[string]interface{}{"type": "string", "enum": []string{"Linear", "Exponential", "Fixed"}},
						},
					},
				},
				"required": []string{"name", "steps"},
			},
		},
		{
			Name:        "list_workflows",
			Description: "List all workflows in the task scheduler",
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
					"tags": map[string]interface{}{
						"type":        "array",
						"description": "Filter by tags",
						"items":       map[string]interface{}{"type": "string"},
					},
					"enabled_only": map[string]interface{}{
						"type":        "boolean",
						"description": "Show only enabled workflows",
						"default":     false,
					},
				},
			},
		},
		{
			Name:        "get_workflow",
			Description: "Get details of a specific workflow in the task scheduler",
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
					"show_executions": map[string]interface{}{
						"type":        "boolean",
						"description": "Include execution history",
						"default":     true,
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "delete_workflow",
			Description: "Delete a workflow from the task scheduler",
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
			Description: "Manually execute a workflow in the task scheduler",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
					},
					"parameters": map[string]interface{}{
						"type":        "object",
						"description": "Override parameters for this execution",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "workflow_logs",
			Description: "Get workflow execution logs from the task scheduler",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
					},
					"execution_id": map[string]interface{}{
						"type":        "string",
						"description": "Specific execution ID",
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
			Description: "Pause a workflow in the task scheduler",
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
			Description: "Resume a paused workflow in the task scheduler",
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
			Description: "List available workflow templates in the task scheduler",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"category": map[string]interface{}{
						"type":        "string",
						"description": "Filter by category",
					},
					"output_format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (table, json, yaml)",
						"default":     "table",
					},
				},
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
	
	// Workflow management (unified under Task Scheduler)
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

// Workflow management implementations using MCPTaskScheduler

// createWorkflow creates a workflow in the MCPTaskScheduler
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
	
	// Process steps for MCPTaskScheduler WorkflowDefinition
	workflowSteps := make([]map[string]interface{}, 0, len(steps))
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
		
		// Create WorkflowStep structure
		workflowStep := map[string]interface{}{
			"name": stepName,
			"tool": stepTool,
		}
		
		// Add optional fields
		if parameters, ok := stepMap["parameters"].(map[string]interface{}); ok {
			workflowStep["parameters"] = parameters
		}
		if condition, ok := stepMap["condition"].(string); ok && condition != "" {
			workflowStep["condition"] = condition
		}
		if continueOnError, ok := stepMap["continueOnError"].(bool); ok {
			workflowStep["continueOnError"] = continueOnError
		}
		if dependsOn, ok := stepMap["dependsOn"].([]interface{}); ok && len(dependsOn) > 0 {
			workflowStep["dependsOn"] = dependsOn
		}
		if timeout, ok := stepMap["timeout"].(string); ok && timeout != "" {
			workflowStep["timeout"] = timeout
		}
		
		workflowSteps = append(workflowSteps, workflowStep)
	}
	
	// Build workflow definition for MCPTaskScheduler
	workflowDef := map[string]interface{}{
		"name":    name,
		"steps":   workflowSteps,
		"enabled": true,
	}
	
	// Add optional fields
	if description, ok := args["description"].(string); ok && description != "" {
		workflowDef["description"] = description
	}
	if schedule, ok := args["schedule"].(string); ok && schedule != "" {
		workflowDef["schedule"] = schedule
	}
	if timezone, ok := args["timezone"].(string); ok && timezone != "" {
		workflowDef["timezone"] = timezone
	}
	if enabled, ok := args["enabled"].(bool); ok {
		workflowDef["enabled"] = enabled
	}
	if concurrencyPolicy, ok := args["concurrencyPolicy"].(string); ok && concurrencyPolicy != "" {
		workflowDef["concurrencyPolicy"] = concurrencyPolicy
	}
	if timeout, ok := args["timeout"].(string); ok && timeout != "" {
		workflowDef["timeout"] = timeout
	}
	if retryPolicy, ok := args["retryPolicy"].(map[string]interface{}); ok {
		workflowDef["retryPolicy"] = retryPolicy
	}
	
	// Try to get existing MCPTaskScheduler and add workflow to it
	fmt.Printf("DEBUG: Checking if K8s client is available for workflow creation\n")
	fmt.Printf("DEBUG: useK8sClient() returns: %v\n", m.useK8sClient())
	fmt.Printf("DEBUG: k8sClient != nil: %v, composer != nil: %v\n", m.k8sClient != nil, m.composer != nil)
	
	if m.useK8sClient() {
		fmt.Printf("DEBUG: Using K8s client for workflow creation\n")
		// Use direct k8s client approach
		result, err := m.addWorkflowToTaskScheduler(ctx, workflowDef)
		if err != nil {
			fmt.Printf("DEBUG: K8s workflow creation failed: %v\n", err)
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Kubernetes workflow creation failed: %v", err)}},
				IsError: true,
			}, err
		}
		fmt.Printf("DEBUG: K8s workflow creation succeeded\n")
		return result, nil
	}
	
	// Fall back to binary execution
	fmt.Printf("DEBUG: Falling back to binary execution for workflow creation\n")
	return m.createWorkflowWithBinary(ctx, args)
}

// addWorkflowToTaskScheduler adds a workflow to an existing MCPTaskScheduler
func (m *MateyMCPServer) addWorkflowToTaskScheduler(ctx context.Context, workflowDef map[string]interface{}) (*ToolResult, error) {
	// First, get the current MCPTaskScheduler to retrieve existing workflows
	var taskScheduler crd.MCPTaskScheduler
	taskSchedulerName := "task-scheduler"
	err := m.k8sClient.Get(ctx, client.ObjectKey{
		Name:      taskSchedulerName,
		Namespace: m.namespace,
	}, &taskScheduler)
	
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting task scheduler '%s': %v", taskSchedulerName, err)}},
			IsError: true,
		}, err
	}
	
	// Convert workflow definition to proper WorkflowDefinition struct
	workflowName, _ := workflowDef["name"].(string)
	
	// Check if workflow with same name already exists
	for _, existingWorkflow := range taskScheduler.Spec.Workflows {
		if existingWorkflow.Name == workflowName {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Workflow '%s' already exists. Use update operation to modify it.", workflowName)}},
				IsError: true,
			}, fmt.Errorf("workflow '%s' already exists", workflowName)
		}
	}
	
	// Convert map to WorkflowDefinition struct
	newWorkflow := crd.WorkflowDefinition{
		Name:    workflowName,
		Enabled: true,
	}
	
	// Set optional fields
	if description, ok := workflowDef["description"].(string); ok {
		newWorkflow.Description = description
	}
	if schedule, ok := workflowDef["schedule"].(string); ok {
		newWorkflow.Schedule = schedule
	}
	if timezone, ok := workflowDef["timezone"].(string); ok {
		newWorkflow.Timezone = timezone
	}
	if enabled, ok := workflowDef["enabled"].(bool); ok {
		newWorkflow.Enabled = enabled
	}
	if concurrencyPolicy, ok := workflowDef["concurrencyPolicy"].(string); ok {
		newWorkflow.ConcurrencyPolicy = crd.WorkflowConcurrencyPolicy(concurrencyPolicy)
	}
	if timeout, ok := workflowDef["timeout"].(string); ok {
		newWorkflow.Timeout = timeout
	}
	
	// Convert steps
	if stepsInterface, ok := workflowDef["steps"].([]map[string]interface{}); ok {
		for _, stepMap := range stepsInterface {
			step := crd.WorkflowStep{
				Name: stepMap["name"].(string),
				Tool: stepMap["tool"].(string),
			}
			
			if parameters, ok := stepMap["parameters"].(map[string]interface{}); ok {
				step.Parameters = parameters
			}
			if condition, ok := stepMap["condition"].(string); ok {
				step.Condition = condition
			}
			if continueOnError, ok := stepMap["continueOnError"].(bool); ok {
				step.ContinueOnError = continueOnError
			}
			if dependsOnInterface, ok := stepMap["dependsOn"].([]interface{}); ok {
				dependsOn := make([]string, len(dependsOnInterface))
				for i, dep := range dependsOnInterface {
					if depStr, ok := dep.(string); ok {
						dependsOn[i] = depStr
					}
				}
				step.DependsOn = dependsOn
			}
			if timeout, ok := stepMap["timeout"].(string); ok {
				step.Timeout = timeout
			}
			
			newWorkflow.Steps = append(newWorkflow.Steps, step)
		}
	}
	
	// Handle retry policy
	if retryPolicyMap, ok := workflowDef["retryPolicy"].(map[string]interface{}); ok {
		retryPolicy := &crd.WorkflowRetryPolicy{}
		if maxRetries, ok := retryPolicyMap["maxRetries"].(float64); ok {
			maxRetriesInt := int32(maxRetries)
			retryPolicy.MaxRetries = maxRetriesInt
		}
		if retryDelay, ok := retryPolicyMap["retryDelay"].(string); ok {
			retryPolicy.RetryDelay = retryDelay
		}
		if backoffStrategy, ok := retryPolicyMap["backoffStrategy"].(string); ok {
			retryPolicy.BackoffStrategy = crd.WorkflowBackoffStrategy(backoffStrategy)
		}
		if maxRetryDelay, ok := retryPolicyMap["maxRetryDelay"].(string); ok {
			retryPolicy.MaxRetryDelay = maxRetryDelay
		}
		if backoffMultiplier, ok := retryPolicyMap["backoffMultiplier"].(float64); ok {
			retryPolicy.BackoffMultiplier = backoffMultiplier
		}
		newWorkflow.RetryPolicy = retryPolicy
	}
	
	// Add the new workflow to the existing list
	taskScheduler.Spec.Workflows = append(taskScheduler.Spec.Workflows, newWorkflow)
	
	// Update the MCPTaskScheduler
	err = m.k8sClient.Update(ctx, &taskScheduler)
	if err != nil {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Error updating task scheduler with new workflow: %v", err)}},
			IsError: true,
		}, err
	}
	
	return &ToolResult{
		Content: []Content{{Type: "text", Text: fmt.Sprintf("Successfully created workflow '%s' in task scheduler. Total workflows: %d", workflowName, len(taskScheduler.Spec.Workflows))}},
	}, nil
}

// createWorkflowWithBinary falls back to binary execution
func (m *MateyMCPServer) createWorkflowWithBinary(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	// For now, return a placeholder - would need actual workflow CLI commands
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Workflow creation via binary not yet implemented. Please ensure MCPTaskScheduler is deployed and try again."}},
		IsError: true,
	}, fmt.Errorf("workflow creation via binary not implemented")
}

// listWorkflows lists all workflows in the task scheduler
func (m *MateyMCPServer) listWorkflows(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	// Try direct k8s client first to get MCPTaskScheduler workflows
	if m.useK8sClient() {
		var taskSchedulers crd.MCPTaskSchedulerList
		namespace := m.namespace
		if allNamespaces, ok := args["all_namespaces"].(bool); ok && allNamespaces {
			namespace = ""
		}
		
		listOpts := &client.ListOptions{}
		if namespace != "" {
			listOpts.Namespace = namespace
		}
		
		err := m.k8sClient.List(ctx, &taskSchedulers, listOpts)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error listing task schedulers: %v", err)}},
				IsError: true,
			}, err
		}
		
		// Extract workflows from all task schedulers
		var allWorkflows []map[string]interface{}
		for _, ts := range taskSchedulers.Items {
			for _, workflow := range ts.Spec.Workflows {
				workflowInfo := map[string]interface{}{
					"name":        workflow.Name,
					"namespace":   ts.Namespace,
					"enabled":     workflow.Enabled,
					"schedule":    workflow.Schedule,
					"timezone":    workflow.Timezone,
					"description": workflow.Description,
					"stepCount":   len(workflow.Steps),
				}
				
				// Filter by enabled_only if specified
				if enabledOnly, ok := args["enabled_only"].(bool); ok && enabledOnly && !workflow.Enabled {
					continue
				}
				
				// Filter by tags if specified
				if filterTags, ok := args["tags"].([]interface{}); ok && len(filterTags) > 0 {
					hasTag := false
					for _, filterTag := range filterTags {
						if filterTagStr, ok := filterTag.(string); ok {
							for _, workflowTag := range workflow.Tags {
								if workflowTag == filterTagStr {
									hasTag = true
									break
								}
							}
							if hasTag {
								break
							}
						}
					}
					if !hasTag {
						continue
					}
				}
				
				allWorkflows = append(allWorkflows, workflowInfo)
			}
		}
		
		// Format output
		outputFormat := "table"
		if format, ok := args["output_format"].(string); ok && format != "" {
			outputFormat = format
		}
		
		return m.formatWorkflowsList(allWorkflows, outputFormat)
	}
	
	// Fall back to binary execution
	return m.listWorkflowsWithBinary(ctx, args)
}

// formatWorkflowsList formats workflow list output
func (m *MateyMCPServer) formatWorkflowsList(workflows []map[string]interface{}, outputFormat string) (*ToolResult, error) {
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
		var yamlOutput strings.Builder
		for _, workflow := range workflows {
			yamlOutput.WriteString(fmt.Sprintf("- name: %v\n", workflow["name"]))
			yamlOutput.WriteString(fmt.Sprintf("  namespace: %v\n", workflow["namespace"]))
			yamlOutput.WriteString(fmt.Sprintf("  enabled: %v\n", workflow["enabled"]))
			if schedule := workflow["schedule"]; schedule != nil && schedule != "" {
				yamlOutput.WriteString(fmt.Sprintf("  schedule: %v\n", schedule))
			}
			if description := workflow["description"]; description != nil && description != "" {
				yamlOutput.WriteString(fmt.Sprintf("  description: %v\n", description))
			}
			yamlOutput.WriteString(fmt.Sprintf("  steps: %v\n", workflow["stepCount"]))
			yamlOutput.WriteString("\n")
		}
		return &ToolResult{
			Content: []Content{{Type: "text", Text: yamlOutput.String()}},
		}, nil
	default: // table format
		var output strings.Builder
		output.WriteString("NAME                 NAMESPACE       ENABLED    SCHEDULE        STEPS    DESCRIPTION\n")
		output.WriteString("----                 ---------       -------    --------        -----    -----------\n")
		
		for _, workflow := range workflows {
			name := fmt.Sprintf("%v", workflow["name"])
			if len(name) > 20 {
				name = name[:17] + "..."
			}
			
			namespace := fmt.Sprintf("%v", workflow["namespace"])
			if len(namespace) > 15 {
				namespace = namespace[:12] + "..."
			}
			
			enabled := fmt.Sprintf("%v", workflow["enabled"])
			schedule := fmt.Sprintf("%v", workflow["schedule"])
			if schedule == "" || schedule == "<nil>" {
				schedule = "Manual"
			}
			if len(schedule) > 15 {
				schedule = schedule[:12] + "..."
			}
			
			stepCount := fmt.Sprintf("%v", workflow["stepCount"])
			
			description := fmt.Sprintf("%v", workflow["description"])
			if len(description) > 20 {
				description = description[:17] + "..."
			}
			if description == "<nil>" {
				description = "-"
			}
			
			output.WriteString(fmt.Sprintf("%-20s %-15s %-10s %-15s %-8s %-20s\n",
				name, namespace, enabled, schedule, stepCount, description))
		}
		
		return &ToolResult{
			Content: []Content{{Type: "text", Text: output.String()}},
		}, nil
	}
}

// listWorkflowsWithBinary falls back to binary execution
func (m *MateyMCPServer) listWorkflowsWithBinary(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Workflow listing via binary not yet implemented. Please ensure MCPTaskScheduler is deployed and try using the k8s client."}},
		IsError: true,
	}, fmt.Errorf("workflow listing via binary not implemented")
}

// getWorkflow gets details of a specific workflow
func (m *MateyMCPServer) getWorkflow(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	if m.useK8sClient() {
		var taskSchedulers crd.MCPTaskSchedulerList
		listOpts := &client.ListOptions{}
		if m.namespace != "" {
			listOpts.Namespace = m.namespace
		}
		
		err := m.k8sClient.List(ctx, &taskSchedulers, listOpts)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error listing task schedulers: %v", err)}},
				IsError: true,
			}, err
		}
		
		// Find the workflow in any task scheduler
		for _, ts := range taskSchedulers.Items {
			for _, workflow := range ts.Spec.Workflows {
				if workflow.Name == name {
					// Format output
					outputFormat := "table"
					if format, ok := args["output_format"].(string); ok && format != "" {
						outputFormat = format
					}
					
					return m.formatSingleWorkflowFromTaskScheduler(workflow, ts.Namespace, outputFormat)
				}
			}
		}
		
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Workflow '%s' not found", name)}},
			IsError: true,
		}, fmt.Errorf("workflow %s not found", name)
	}
	
	// Fall back to binary execution
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Workflow details via binary not yet implemented."}},
		IsError: true,
	}, fmt.Errorf("workflow details via binary not implemented")
}

// formatSingleWorkflowFromTaskScheduler formats a single workflow from MCPTaskScheduler
func (m *MateyMCPServer) formatSingleWorkflowFromTaskScheduler(workflow crd.WorkflowDefinition, namespace, outputFormat string) (*ToolResult, error) {
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
		yamlOutput.WriteString(fmt.Sprintf("namespace: %s\n", namespace))
		yamlOutput.WriteString(fmt.Sprintf("enabled: %v\n", workflow.Enabled))
		if workflow.Schedule != "" {
			yamlOutput.WriteString(fmt.Sprintf("schedule: %s\n", workflow.Schedule))
		}
		if workflow.Timezone != "" {
			yamlOutput.WriteString(fmt.Sprintf("timezone: %s\n", workflow.Timezone))
		}
		if workflow.Description != "" {
			yamlOutput.WriteString(fmt.Sprintf("description: %s\n", workflow.Description))
		}
		yamlOutput.WriteString(fmt.Sprintf("steps: %d\n", len(workflow.Steps)))
		return &ToolResult{
			Content: []Content{{Type: "text", Text: yamlOutput.String()}},
		}, nil
	default: // table format
		var output strings.Builder
		output.WriteString("=== Workflow Details ===\n")
		output.WriteString(fmt.Sprintf("Name: %s\n", workflow.Name))
		output.WriteString(fmt.Sprintf("Namespace: %s\n", namespace))
		output.WriteString(fmt.Sprintf("Enabled: %v\n", workflow.Enabled))
		if workflow.Schedule != "" {
			output.WriteString(fmt.Sprintf("Schedule: %s\n", workflow.Schedule))
		}
		if workflow.Timezone != "" {
			output.WriteString(fmt.Sprintf("Timezone: %s\n", workflow.Timezone))
		}
		if workflow.Description != "" {
			output.WriteString(fmt.Sprintf("Description: %s\n", workflow.Description))
		}
		if workflow.ConcurrencyPolicy != "" {
			output.WriteString(fmt.Sprintf("Concurrency Policy: %s\n", workflow.ConcurrencyPolicy))
		}
		if workflow.Timeout != "" {
			output.WriteString(fmt.Sprintf("Timeout: %s\n", workflow.Timeout))
		}
		
		if len(workflow.Steps) > 0 {
			output.WriteString("\n=== Steps ===\n")
			for i, step := range workflow.Steps {
				output.WriteString(fmt.Sprintf("%d. %s (tool: %s)\n", i+1, step.Name, step.Tool))
				if len(step.DependsOn) > 0 {
					output.WriteString(fmt.Sprintf("   Depends On: %v\n", step.DependsOn))
				}
				if step.Condition != "" {
					output.WriteString(fmt.Sprintf("   Condition: %s\n", step.Condition))
				}
				if step.Timeout != "" {
					output.WriteString(fmt.Sprintf("   Timeout: %s\n", step.Timeout))
				}
			}
		}
		
		return &ToolResult{
			Content: []Content{{Type: "text", Text: output.String()}},
		}, nil
	}
}

// deleteWorkflow deletes a workflow from the task scheduler
func (m *MateyMCPServer) deleteWorkflow(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	name, ok := args["name"].(string)
	if !ok || name == "" {
		return &ToolResult{
			Content: []Content{{Type: "text", Text: "name is required"}},
			IsError: true,
		}, fmt.Errorf("name is required")
	}
	
	if m.useK8sClient() {
		// Get the current MCPTaskScheduler
		var taskScheduler crd.MCPTaskScheduler
		taskSchedulerName := "task-scheduler"
		err := m.k8sClient.Get(ctx, client.ObjectKey{
			Name:      taskSchedulerName,
			Namespace: m.namespace,
		}, &taskScheduler)
		
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting task scheduler '%s': %v", taskSchedulerName, err)}},
				IsError: true,
			}, err
		}
		
		// Find and remove the workflow
		workflowFound := false
		var updatedWorkflows []crd.WorkflowDefinition
		for _, workflow := range taskScheduler.Spec.Workflows {
			if workflow.Name != name {
				updatedWorkflows = append(updatedWorkflows, workflow)
			} else {
				workflowFound = true
			}
		}
		
		if !workflowFound {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Workflow '%s' not found", name)}},
				IsError: true,
			}, fmt.Errorf("workflow '%s' not found", name)
		}
		
		// Update the task scheduler
		taskScheduler.Spec.Workflows = updatedWorkflows
		err = m.k8sClient.Update(ctx, &taskScheduler)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error removing workflow from task scheduler: %v", err)}},
				IsError: true,
			}, err
		}
		
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Successfully deleted workflow '%s' from task scheduler. Remaining workflows: %d", name, len(updatedWorkflows))}},
		}, nil
	}
	
	// Fall back to binary execution
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Workflow deletion via binary not yet implemented."}},
		IsError: true,
	}, fmt.Errorf("workflow deletion via binary not implemented")
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
	
	// For now, return a placeholder - would need actual workflow execution API
	return &ToolResult{
		Content: []Content{{Type: "text", Text: fmt.Sprintf("Manual execution of workflow '%s' is not yet implemented. This requires connecting to the task scheduler's execution API.", name)}},
		IsError: true,
	}, fmt.Errorf("workflow execution not implemented")
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
	
	// For now, return a placeholder - would need actual workflow logs API
	return &ToolResult{
		Content: []Content{{Type: "text", Text: fmt.Sprintf("Getting logs for workflow '%s' is not yet implemented. This requires connecting to the task scheduler's logging API.", name)}},
		IsError: true,
	}, fmt.Errorf("workflow logs not implemented")
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
	
	return m.updateWorkflowEnabledStatus(ctx, name, false)
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
	
	return m.updateWorkflowEnabledStatus(ctx, name, true)
}

// updateWorkflowEnabledStatus updates the enabled status of a workflow
func (m *MateyMCPServer) updateWorkflowEnabledStatus(ctx context.Context, name string, enabled bool) (*ToolResult, error) {
	if m.useK8sClient() {
		// Get the current MCPTaskScheduler
		var taskScheduler crd.MCPTaskScheduler
		taskSchedulerName := "task-scheduler"
		err := m.k8sClient.Get(ctx, client.ObjectKey{
			Name:      taskSchedulerName,
			Namespace: m.namespace,
		}, &taskScheduler)
		
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error getting task scheduler '%s': %v", taskSchedulerName, err)}},
				IsError: true,
			}, err
		}
		
		// Find and update the workflow
		workflowFound := false
		for i, workflow := range taskScheduler.Spec.Workflows {
			if workflow.Name == name {
				taskScheduler.Spec.Workflows[i].Enabled = enabled
				workflowFound = true
				break
			}
		}
		
		if !workflowFound {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Workflow '%s' not found", name)}},
				IsError: true,
			}, fmt.Errorf("workflow '%s' not found", name)
		}
		
		// Update the task scheduler
		err = m.k8sClient.Update(ctx, &taskScheduler)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error updating workflow enabled status: %v", err)}},
				IsError: true,
			}, err
		}
		
		status := "paused"
		if enabled {
			status = "resumed"
		}
		
		return &ToolResult{
			Content: []Content{{Type: "text", Text: fmt.Sprintf("Successfully %s workflow '%s'", status, name)}},
		}, nil
	}
	
	// Fall back to binary execution
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Workflow status update via binary not yet implemented."}},
		IsError: true,
	}, fmt.Errorf("workflow status update via binary not implemented")
}

// workflowTemplates lists available workflow templates
func (m *MateyMCPServer) workflowTemplates(ctx context.Context, args map[string]interface{}) (*ToolResult, error) {
	if m.useK8sClient() {
		var taskSchedulers crd.MCPTaskSchedulerList
		listOpts := &client.ListOptions{}
		if m.namespace != "" {
			listOpts.Namespace = m.namespace
		}
		
		err := m.k8sClient.List(ctx, &taskSchedulers, listOpts)
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error listing task schedulers: %v", err)}},
				IsError: true,
			}, err
		}
		
		// Extract templates from all task schedulers
		var allTemplates []map[string]interface{}
		category := ""
		if cat, ok := args["category"].(string); ok {
			category = cat
		}
		
		for _, ts := range taskSchedulers.Items {
			for _, template := range ts.Spec.Templates {
				// Filter by category if specified
				if category != "" && template.Category != category {
					continue
				}
				
				templateInfo := map[string]interface{}{
					"name":        template.Name,
					"category":    template.Category,
					"description": template.Description,
					"version":     template.Version,
					"parameters":  len(template.Parameters),
					"steps":       len(template.Workflow.Steps),
				}
				allTemplates = append(allTemplates, templateInfo)
			}
		}
		
		// Format output
		outputFormat := "table"
		if format, ok := args["output_format"].(string); ok && format != "" {
			outputFormat = format
		}
		
		return m.formatTemplatesList(allTemplates, outputFormat)
	}
	
	// Fall back to placeholder
	return &ToolResult{
		Content: []Content{{Type: "text", Text: "Workflow templates listing not yet implemented via binary."}},
		IsError: true,
	}, fmt.Errorf("workflow templates not implemented")
}

// formatTemplatesList formats templates list output
func (m *MateyMCPServer) formatTemplatesList(templates []map[string]interface{}, outputFormat string) (*ToolResult, error) {
	switch outputFormat {
	case "json":
		jsonBytes, err := json.MarshalIndent(templates, "", "  ")
		if err != nil {
			return &ToolResult{
				Content: []Content{{Type: "text", Text: fmt.Sprintf("Error formatting templates as JSON: %v", err)}},
				IsError: true,
			}, err
		}
		return &ToolResult{
			Content: []Content{{Type: "text", Text: string(jsonBytes)}},
		}, nil
	case "yaml":
		var yamlOutput strings.Builder
		for _, template := range templates {
			yamlOutput.WriteString(fmt.Sprintf("- name: %v\n", template["name"]))
			yamlOutput.WriteString(fmt.Sprintf("  category: %v\n", template["category"]))
			yamlOutput.WriteString(fmt.Sprintf("  description: %v\n", template["description"]))
			yamlOutput.WriteString(fmt.Sprintf("  version: %v\n", template["version"]))
			yamlOutput.WriteString(fmt.Sprintf("  parameters: %v\n", template["parameters"]))
			yamlOutput.WriteString(fmt.Sprintf("  steps: %v\n", template["steps"]))
			yamlOutput.WriteString("\n")
		}
		return &ToolResult{
			Content: []Content{{Type: "text", Text: yamlOutput.String()}},
		}, nil
	default: // table format
		var output strings.Builder
		output.WriteString("NAME                 CATEGORY        VERSION    PARAMS   STEPS    DESCRIPTION\n")
		output.WriteString("----                 --------        -------    ------   -----    -----------\n")
		
		for _, template := range templates {
			name := fmt.Sprintf("%v", template["name"])
			if len(name) > 20 {
				name = name[:17] + "..."
			}
			
			category := fmt.Sprintf("%v", template["category"])
			if len(category) > 15 {
				category = category[:12] + "..."
			}
			
			version := fmt.Sprintf("%v", template["version"])
			if len(version) > 10 {
				version = version[:7] + "..."
			}
			
			params := fmt.Sprintf("%v", template["parameters"])
			steps := fmt.Sprintf("%v", template["steps"])
			
			description := fmt.Sprintf("%v", template["description"])
			if len(description) > 20 {
				description = description[:17] + "..."
			}
			
			output.WriteString(fmt.Sprintf("%-20s %-15s %-10s %-8s %-8s %-20s\n",
				name, category, version, params, steps, description))
		}
		
		return &ToolResult{
			Content: []Content{{Type: "text", Text: output.String()}},
		}, nil
	}
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