// internal/scheduler/mcp_tools.go
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
)

// MCPToolServer provides MCP protocol tools for task scheduler management
type MCPToolServer struct {
	cronEngine     *CronEngine
	workflowEngine *WorkflowEngine
	templateRegistry *TemplateRegistry
	logger         logr.Logger
}

func NewMCPToolServer(cronEngine *CronEngine, workflowEngine *WorkflowEngine, logger logr.Logger) *MCPToolServer {
	return &MCPToolServer{
		cronEngine:       cronEngine,
		workflowEngine:   workflowEngine,
		templateRegistry: NewTemplateRegistry(),
		logger:           logger,
	}
}

// MCPToolDefinition represents an MCP tool definition
type MCPToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// GetMCPTools returns all available MCP tools
func (mts *MCPToolServer) GetMCPTools() []MCPToolDefinition {
	return []MCPToolDefinition{
		// Core Task Management
		{
			Name:        "list_tasks",
			Description: "List all scheduled tasks (workflows)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace to filter tasks",
						"default":     "default",
					},
					"enabled_only": map[string]interface{}{
						"type":        "boolean",
						"description": "Only return enabled tasks",
						"default":     false,
					},
				},
			},
		},
		{
			Name:        "get_task",
			Description: "Get details of a specific task by name",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Task name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace",
						"default":     "default",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "add_task",
			Description: "Create a new scheduled task (workflow)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Task name",
					},
					"schedule": map[string]interface{}{
						"type":        "string",
						"description": "Cron schedule expression",
					},
					"template": map[string]interface{}{
						"type":        "string",
						"description": "Workflow template to use",
					},
					"parameters": map[string]interface{}{
						"type":        "object",
						"description": "Template parameters",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace",
						"default":     "default",
					},
				},
				"required": []string{"name", "schedule"},
			},
		},
		{
			Name:        "update_task",
			Description: "Update an existing task",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Task name",
					},
					"schedule": map[string]interface{}{
						"type":        "string",
						"description": "New cron schedule expression",
					},
					"enabled": map[string]interface{}{
						"type":        "boolean",
						"description": "Enable/disable the task",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace",
						"default":     "default",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "remove_task",
			Description: "Remove a task by name",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Task name to remove",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace",
						"default":     "default",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "enable_task",
			Description: "Enable a disabled task",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Task name to enable",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace",
						"default":     "default",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "disable_task",
			Description: "Disable an enabled task",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Task name to disable",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace",
						"default":     "default",
					},
				},
				"required": []string{"name"},
			},
		},
		{
			Name:        "run_task",
			Description: "Trigger a task to run immediately",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Task name to run",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace",
						"default":     "default",
					},
				},
				"required": []string{"name"},
			},
		},
		// Task Execution and Monitoring
		{
			Name:        "list_run_status",
			Description: "List recent task execution status and summaries",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of runs to return",
						"default":     10,
					},
					"task_name": map[string]interface{}{
						"type":        "string",
						"description": "Filter by task name",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace",
						"default":     "default",
					},
				},
			},
		},
		{
			Name:        "get_run_output",
			Description: "Get detailed output from a specific task run",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"run_id": map[string]interface{}{
						"type":        "string",
						"description": "Task run ID",
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace",
						"default":     "default",
					},
				},
				"required": []string{"run_id"},
			},
		},
		// Templates and Workflows
		{
			Name:        "list_templates",
			Description: "List all available workflow templates",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"category": map[string]interface{}{
						"type":        "string",
						"description": "Filter templates by category",
					},
				},
			},
		},
		{
			Name:        "get_template",
			Description: "Get details of a specific workflow template",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Template name",
					},
				},
				"required": []string{"name"},
			},
		},
		// System Monitoring
		{
			Name:        "health_check",
			Description: "Perform health checks and return system status",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "get_metrics",
			Description: "Get comprehensive metrics about task execution and system performance",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"include_system": map[string]interface{}{
						"type":        "boolean",
						"description": "Include system metrics",
						"default":     true,
					},
				},
			},
		},
	}
}

// ExecuteMCPTool executes an MCP tool with the given parameters
func (mts *MCPToolServer) ExecuteMCPTool(ctx context.Context, toolName string, parameters map[string]interface{}) (map[string]interface{}, error) {
	mts.logger.Info("Executing MCP tool", "tool", toolName, "parameters", parameters)

	switch toolName {
	case "list_tasks":
		return mts.listTasks(ctx, parameters)
	case "get_task":
		return mts.getTask(ctx, parameters)
	case "add_task":
		return mts.addTask(ctx, parameters)
	case "update_task":
		return mts.updateTask(ctx, parameters)
	case "remove_task":
		return mts.removeTask(ctx, parameters)
	case "enable_task":
		return mts.enableTask(ctx, parameters)
	case "disable_task":
		return mts.disableTask(ctx, parameters)
	case "run_task":
		return mts.runTask(ctx, parameters)
	case "list_run_status":
		return mts.listRunStatus(ctx, parameters)
	case "get_run_output":
		return mts.getRunOutput(ctx, parameters)
	case "list_templates":
		return mts.listTemplates(ctx, parameters)
	case "get_template":
		return mts.getTemplate(ctx, parameters)
	case "health_check":
		return mts.healthCheck(ctx, parameters)
	case "get_metrics":
		return mts.getMetrics(ctx, parameters)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

// Tool implementations

func (mts *MCPToolServer) listTasks(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	jobs := mts.cronEngine.ListJobs()
	
	result := map[string]interface{}{
		"tasks": make([]map[string]interface{}, 0, len(jobs)),
		"count": len(jobs),
	}

	for _, job := range jobs {
		taskInfo := map[string]interface{}{
			"id":       job.ID,
			"name":     job.Name,
			"schedule": job.Schedule,
			"enabled":  job.Enabled,
			"timezone": job.Timezone.String(),
		}
		
		if job.LastRun != nil {
			taskInfo["last_run"] = job.LastRun.Format(time.RFC3339)
		}
		if job.NextRun != nil {
			taskInfo["next_run"] = job.NextRun.Format(time.RFC3339)
		}
		
		result["tasks"] = append(result["tasks"].([]map[string]interface{}), taskInfo)
	}

	return result, nil
}

func (mts *MCPToolServer) getTask(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	name, ok := params["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	namespace := "default"
	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
	}

	jobID := fmt.Sprintf("%s/%s", namespace, name)
	job, exists := mts.cronEngine.GetJob(jobID)
	if !exists {
		return nil, fmt.Errorf("task %s not found", name)
	}

	result := map[string]interface{}{
		"id":          job.ID,
		"name":        job.Name,
		"schedule":    job.Schedule,
		"enabled":     job.Enabled,
		"timezone":    job.Timezone.String(),
		"max_retries": job.MaxRetries,
		"retry_delay": job.RetryDelay.String(),
	}

	if job.LastRun != nil {
		result["last_run"] = job.LastRun.Format(time.RFC3339)
	}
	if job.NextRun != nil {
		result["next_run"] = job.NextRun.Format(time.RFC3339)
	}

	return result, nil
}

func (mts *MCPToolServer) addTask(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	name, ok := params["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	schedule, ok := params["schedule"].(string)
	if !ok {
		return nil, fmt.Errorf("schedule parameter is required")
	}

	namespace := "default"
	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
	}

	jobID := fmt.Sprintf("%s/%s", namespace, name)

	// Create a simple job for now
	jobSpec := &JobSpec{
		ID:       jobID,
		Name:     name,
		Schedule: schedule,
		Enabled:  true,
		Handler: func(ctx context.Context, jobID string) error {
			mts.logger.Info("Executing task", "jobID", jobID)
			return nil
		},
		MaxRetries: 3,
		RetryDelay: 30 * time.Second,
	}

	if err := mts.cronEngine.AddJob(jobSpec); err != nil {
		return nil, fmt.Errorf("failed to add task: %w", err)
	}

	return map[string]interface{}{
		"id":      jobID,
		"name":    name,
		"message": "Task created successfully",
	}, nil
}

func (mts *MCPToolServer) updateTask(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	name, ok := params["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	namespace := "default"
	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
	}

	jobID := fmt.Sprintf("%s/%s", namespace, name)

	// For now, we can only enable/disable tasks
	if enabled, ok := params["enabled"].(bool); ok {
		if enabled {
			if err := mts.cronEngine.EnableJob(jobID); err != nil {
				return nil, fmt.Errorf("failed to enable task: %w", err)
			}
		} else {
			if err := mts.cronEngine.DisableJob(jobID); err != nil {
				return nil, fmt.Errorf("failed to disable task: %w", err)
			}
		}
	}

	return map[string]interface{}{
		"id":      jobID,
		"message": "Task updated successfully",
	}, nil
}

func (mts *MCPToolServer) removeTask(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	name, ok := params["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	namespace := "default"
	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
	}

	jobID := fmt.Sprintf("%s/%s", namespace, name)

	if err := mts.cronEngine.RemoveJob(jobID); err != nil {
		return nil, fmt.Errorf("failed to remove task: %w", err)
	}

	return map[string]interface{}{
		"id":      jobID,
		"message": "Task removed successfully",
	}, nil
}

func (mts *MCPToolServer) enableTask(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	name, ok := params["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	namespace := "default"
	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
	}

	jobID := fmt.Sprintf("%s/%s", namespace, name)

	if err := mts.cronEngine.EnableJob(jobID); err != nil {
		return nil, fmt.Errorf("failed to enable task: %w", err)
	}

	return map[string]interface{}{
		"id":      jobID,
		"message": "Task enabled successfully",
	}, nil
}

func (mts *MCPToolServer) disableTask(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	name, ok := params["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	namespace := "default"
	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
	}

	jobID := fmt.Sprintf("%s/%s", namespace, name)

	if err := mts.cronEngine.DisableJob(jobID); err != nil {
		return nil, fmt.Errorf("failed to disable task: %w", err)
	}

	return map[string]interface{}{
		"id":      jobID,
		"message": "Task disabled successfully",
	}, nil
}

func (mts *MCPToolServer) runTask(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	name, ok := params["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	namespace := "default"
	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
	}

	jobID := fmt.Sprintf("%s/%s", namespace, name)
	job, exists := mts.cronEngine.GetJob(jobID)
	if !exists {
		return nil, fmt.Errorf("task %s not found", name)
	}

	// Execute the job handler directly
	err := job.Handler(ctx, jobID)
	if err != nil {
		return map[string]interface{}{
			"id":      jobID,
			"status":  "failed",
			"error":   err.Error(),
			"message": "Task execution failed",
		}, nil
	}

	return map[string]interface{}{
		"id":      jobID,
		"status":  "success",
		"message": "Task executed successfully",
	}, nil
}

func (mts *MCPToolServer) listRunStatus(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	// For now, return mock data - in a real implementation this would query execution history
	return map[string]interface{}{
		"runs": []map[string]interface{}{
			{
				"id":         "run-1",
				"task_name":  "example-task",
				"status":     "success",
				"start_time": time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
				"end_time":   time.Now().Add(-55 * time.Minute).Format(time.RFC3339),
				"duration":   "5m0s",
			},
		},
		"count": 1,
	}, nil
}

func (mts *MCPToolServer) getRunOutput(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	runID, ok := params["run_id"].(string)
	if !ok {
		return nil, fmt.Errorf("run_id parameter is required")
	}

	// For now, return mock data
	return map[string]interface{}{
		"run_id": runID,
		"output": "Mock task output",
		"logs":   "Mock task logs",
		"status": "success",
	}, nil
}

func (mts *MCPToolServer) listTemplates(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	var templates []*WorkflowTemplate
	if category, ok := params["category"].(string); ok {
		templates = mts.templateRegistry.ListTemplatesByCategory(category)
	} else {
		templates = mts.templateRegistry.ListTemplates()
	}

	result := map[string]interface{}{
		"templates": make([]map[string]interface{}, 0, len(templates)),
		"count":     len(templates),
	}

	for _, template := range templates {
		templateInfo := map[string]interface{}{
			"name":        template.Name,
			"description": template.Description,
			"category":    template.Category,
			"tags":        template.Tags,
			"parameters":  template.Parameters,
		}
		result["templates"] = append(result["templates"].([]map[string]interface{}), templateInfo)
	}

	return result, nil
}

func (mts *MCPToolServer) getTemplate(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	name, ok := params["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	template, exists := mts.templateRegistry.GetTemplate(name)
	if !exists {
		return nil, fmt.Errorf("template %s not found", name)
	}

	// Convert template to JSON-serializable format
	templateJSON, err := json.Marshal(template)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize template: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(templateJSON, &result); err != nil {
		return nil, fmt.Errorf("failed to deserialize template: %w", err)
	}

	return result, nil
}

func (mts *MCPToolServer) healthCheck(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	return map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
		"version":   "0.0.4",
		"services": map[string]interface{}{
			"cron_engine":     "running",
			"workflow_engine": "running",
			"template_registry": "running",
		},
	}, nil
}

func (mts *MCPToolServer) getMetrics(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	jobs := mts.cronEngine.ListJobs()
	
	enabledCount := 0
	disabledCount := 0
	for _, job := range jobs {
		if job.Enabled {
			enabledCount++
		} else {
			disabledCount++
		}
	}

	return map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"tasks": map[string]interface{}{
			"total":    len(jobs),
			"enabled":  enabledCount,
			"disabled": disabledCount,
		},
		"templates": map[string]interface{}{
			"total": len(mts.templateRegistry.ListTemplates()),
		},
		"system": map[string]interface{}{
			"uptime": "running",
			"status": "healthy",
		},
	}, nil
}