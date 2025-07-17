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
)

// MateyMCPServer provides MCP tools for interacting with Matey and the cluster
type MateyMCPServer struct {
	mateyBinary string
	configFile  string
	namespace   string
}

// NewMateyMCPServer creates a new Matey MCP server
func NewMateyMCPServer(mateyBinary, configFile, namespace string) *MateyMCPServer {
	return &MateyMCPServer{
		mateyBinary: mateyBinary,
		configFile:  configFile,
		namespace:   namespace,
	}
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
	}
}

// ExecuteTool executes a specific MCP tool
func (m *MateyMCPServer) ExecuteTool(ctx context.Context, name string, arguments map[string]interface{}) (*ToolResult, error) {
	switch name {
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
	case "create_workflow":
		return m.createWorkflow(ctx, arguments)
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
	
	// Get MCP servers
	psOutput, err := m.runMateyCommand(ctx, "ps", "-o", "json")
	if err == nil {
		result.WriteString("=== MCP Servers ===\n")
		result.WriteString(psOutput)
		result.WriteString("\n\n")
	}
	
	// Get pods if requested
	if includePods, ok := args["include_pods"].(bool); ok && includePods {
		cmd := exec.CommandContext(ctx, "kubectl", "get", "pods", "-o", "json")
		if m.namespace != "" {
			cmd.Args = append(cmd.Args, "-n", m.namespace)
		}
		
		if output, err := cmd.CombinedOutput(); err == nil {
			result.WriteString("=== Pods ===\n")
			result.WriteString(string(output))
			result.WriteString("\n\n")
		}
	}
	
	// Get recent logs if requested
	if includeLogs, ok := args["include_logs"].(bool); ok && includeLogs {
		logsOutput, err := m.runMateyCommand(ctx, "logs", "--tail", "50")
		if err == nil {
			result.WriteString("=== Recent Logs ===\n")
			result.WriteString(logsOutput)
			result.WriteString("\n\n")
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

// runMateyCommand runs a matey command and returns the output
func (m *MateyMCPServer) runMateyCommand(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, m.mateyBinary, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}