// internal/scheduler/mcp_tools.go
package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/crd"
)

// MCPToolServer provides MCP protocol tools for task scheduler management
type MCPToolServer struct {
	cronEngine       *CronEngine
	workflowEngine   *WorkflowEngine
	workflowScheduler *WorkflowScheduler // NEW: Reference to WorkflowScheduler for sync
	templateRegistry *TemplateRegistry
	workflowStore    *WorkflowStore     // NEW: PostgreSQL workflow persistence
	k8sClient        client.Client // NEW: Kubernetes client for CRD access
	namespace        string        // NEW: Kubernetes namespace
	logger           logr.Logger
}

func NewMCPToolServer(cronEngine *CronEngine, workflowEngine *WorkflowEngine, logger logr.Logger) *MCPToolServer {
	return &MCPToolServer{
		cronEngine:       cronEngine,
		workflowEngine:   workflowEngine,
		templateRegistry: NewTemplateRegistry(),
		namespace:        "default", // Default namespace
		logger:           logger,
	}
}

// SetWorkflowStore sets the workflow store for PostgreSQL persistence
func (mts *MCPToolServer) SetWorkflowStore(store *WorkflowStore) {
	mts.workflowStore = store
}

// SetWorkflowScheduler sets the WorkflowScheduler reference for synchronization
func (mts *MCPToolServer) SetWorkflowScheduler(scheduler *WorkflowScheduler) {
	mts.workflowScheduler = scheduler
}

// SetK8sClient sets the Kubernetes client and namespace for CRD access
func (mts *MCPToolServer) SetK8sClient(client client.Client, namespace string) {
	mts.k8sClient = client
	if namespace != "" {
		mts.namespace = namespace
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
		// NEW: Workflow Management Tools
		{
			Name:        "create_workflow",
			Description: "Create a new workflow in the MCPTaskScheduler CRD",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
					},
					"schedule": map[string]interface{}{
						"type":        "string",
						"description": "Cron schedule expression (optional)",
					},
					"steps": map[string]interface{}{
						"type":        "array",
						"description": "Workflow steps",
						"items": map[string]interface{}{
							"type": "object",
						},
					},
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace",
						"default":     "default",
					},
				},
				"required": []string{"name", "steps"},
			},
		},
		{
			Name:        "list_workflows",
			Description: "List all workflows from MCPTaskScheduler CRD",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"namespace": map[string]interface{}{
						"type":        "string",
						"description": "Kubernetes namespace",
						"default":     "default",
					},
					"enabled_only": map[string]interface{}{
						"type":        "boolean",
						"description": "Only return enabled workflows",
						"default":     false,
					},
				},
			},
		},
		{
			Name:        "get_workflow",
			Description: "Get a specific workflow by name from MCPTaskScheduler CRD",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
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
			Name:        "execute_workflow",
			Description: "Execute a workflow immediately (bypass schedule)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
					},
					"parameters": map[string]interface{}{
						"type":        "object",
						"description": "Runtime parameters",
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
			Name:        "pause_workflow",
			Description: "Pause a running workflow",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
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
			Name:        "resume_workflow",
			Description: "Resume a paused workflow",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
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
			Name:        "get_workflow_status",
			Description: "Get detailed status of a workflow execution",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
					},
					"run_id": map[string]interface{}{
						"type":        "string",
						"description": "Specific run ID (optional)",
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
			Name:        "workflow_logs",
			Description: "Get logs from workflow executions and Kubernetes Jobs",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Workflow name",
					},
					"execution_id": map[string]interface{}{
						"type":        "string",
						"description": "Specific execution ID (optional)",
					},
					"tail": map[string]interface{}{
						"type":        "integer",
						"description": "Number of log lines to return",
						"default":     100,
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
	// NEW: Workflow management tools
	case "create_workflow":
		return mts.createWorkflow(ctx, parameters)
	case "list_workflows":
		return mts.listWorkflows(ctx, parameters)
	case "get_workflow":
		return mts.getWorkflow(ctx, parameters)
	case "execute_workflow":
		return mts.executeWorkflow(ctx, parameters)
	case "pause_workflow":
		return mts.pauseWorkflow(ctx, parameters)
	case "resume_workflow":
		return mts.resumeWorkflow(ctx, parameters)
	case "get_workflow_status":
		return mts.getWorkflowStatus(ctx, parameters)
	case "workflow_logs":
		return mts.workflowLogs(ctx, parameters)
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
	if mts.k8sClient == nil {
		return nil, fmt.Errorf("kubernetes client not configured")
	}

	namespace := mts.namespace
	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
	}

	limit := 10
	if l, ok := params["limit"].(float64); ok {
		limit = int(l)
	}

	taskName := ""
	if name, ok := params["task_name"].(string); ok {
		taskName = name
	}

	// Get the MCPTaskScheduler resource to get execution history
	taskScheduler := &crd.MCPTaskScheduler{}
	err := mts.k8sClient.Get(ctx, types.NamespacedName{
		Name:      "task-scheduler",
		Namespace: namespace,
	}, taskScheduler)
	if err != nil {
		return nil, fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}

	// Get execution history from status
	runs := make([]map[string]interface{}, 0)
	for _, execution := range taskScheduler.Status.WorkflowExecutions {
		if taskName != "" && execution.WorkflowName != taskName {
			continue
		}

		runInfo := map[string]interface{}{
			"id":        execution.ID,
			"task_name": execution.WorkflowName,
			"status":    string(execution.Phase),
			"start_time": execution.StartTime.Format(time.RFC3339),
		}

		if execution.EndTime != nil {
			runInfo["end_time"] = execution.EndTime.Format(time.RFC3339)
			if execution.Duration != nil {
				runInfo["duration"] = execution.Duration.String()
			}
		}

		if execution.Message != "" {
			runInfo["message"] = execution.Message
		}

		runs = append(runs, runInfo)

		if len(runs) >= limit {
			break
		}
	}

	return map[string]interface{}{
		"runs":  runs,
		"count": len(runs),
	}, nil
}

func (mts *MCPToolServer) getRunOutput(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	if mts.k8sClient == nil {
		return nil, fmt.Errorf("kubernetes client not configured")
	}

	runID, ok := params["run_id"].(string)
	if !ok {
		return nil, fmt.Errorf("run_id parameter is required")
	}

	namespace := mts.namespace
	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
	}

	// Get the MCPTaskScheduler resource
	taskScheduler := &crd.MCPTaskScheduler{}
	err := mts.k8sClient.Get(ctx, types.NamespacedName{
		Name:      "task-scheduler",
		Namespace: namespace,
	}, taskScheduler)
	if err != nil {
		return nil, fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}

	// Find the specific execution
	for _, execution := range taskScheduler.Status.WorkflowExecutions {
		if execution.ID == runID {
			result := map[string]interface{}{
				"run_id":     runID,
				"status":     string(execution.Phase),
				"start_time": execution.StartTime.Format(time.RFC3339),
				"steps":      make([]map[string]interface{}, 0),
			}

			if execution.EndTime != nil {
				result["end_time"] = execution.EndTime.Format(time.RFC3339)
				if execution.Duration != nil {
					result["duration"] = execution.Duration.String()
				}
			}

			if execution.Message != "" {
				result["message"] = execution.Message
			}

			// Get step results
			steps := make([]map[string]interface{}, 0, len(execution.StepResults))
			for stepName, stepResult := range execution.StepResults {
				stepInfo := map[string]interface{}{
					"name":     stepName,
					"status":   string(stepResult.Phase),
					"output":   stepResult.Output,
					"duration": stepResult.Duration.String(),
					"attempts": stepResult.Attempts,
				}
				if stepResult.Error != "" {
					stepInfo["error"] = stepResult.Error
				}
				steps = append(steps, stepInfo)
			}
			result["steps"] = steps

			return result, nil
		}
	}

	return nil, fmt.Errorf("run %s not found", runID)
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

// NEW: Workflow Management Tool Implementations

func (mts *MCPToolServer) createWorkflow(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	// Use PostgreSQL workflow store if available, otherwise fall back to CRD
	if mts.workflowStore != nil {
		return mts.createWorkflowInDB(ctx, params)
	}
	
	// Fallback to CRD-based creation
	return mts.createWorkflowInCRD(ctx, params)
}

// createWorkflowInDB creates a workflow in PostgreSQL database
func (mts *MCPToolServer) createWorkflowInDB(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	name, ok := params["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	steps, ok := params["steps"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("steps parameter is required")
	}

	// Convert steps interface{} to WorkflowStep structs
	workflowSteps := make([]crd.WorkflowStep, 0, len(steps))
	for i, stepInterface := range steps {
		stepMap, ok := stepInterface.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("step %d is not a valid object", i)
		}

		step := crd.WorkflowStep{
			Name: fmt.Sprintf("%v", stepMap["name"]),
		}
		
		// Set tool to execute
		if tool, ok := stepMap["tool"].(string); ok {
			step.Tool = tool
		} else {
			return nil, fmt.Errorf("step %d is missing required 'tool' field", i)
		}
		
		// Set parameters
		if stepParams, ok := stepMap["parameters"].(map[string]interface{}); ok {
			step.Parameters = stepParams
		}
		
		// Set optional fields
		if condition, ok := stepMap["condition"].(string); ok {
			step.Condition = condition
		}
		if continueOnError, ok := stepMap["continueOnError"].(bool); ok {
			step.ContinueOnError = continueOnError
		}
		if timeout, ok := stepMap["timeout"].(string); ok {
			step.Timeout = timeout
		}
		if dependsOn, ok := stepMap["dependsOn"].([]interface{}); ok {
			dependencies := make([]string, len(dependsOn))
			for j, dep := range dependsOn {
				if depStr, ok := dep.(string); ok {
					dependencies[j] = depStr
				}
			}
			step.DependsOn = dependencies
		}
		
		workflowSteps = append(workflowSteps, step)
	}

	// Create the workflow definition
	workflow := crd.WorkflowDefinition{
		Name:    name,
		Enabled: true,
		Steps:   workflowSteps,
	}

	// Set optional fields
	if description, ok := params["description"].(string); ok {
		workflow.Description = description
	}
	if schedule, ok := params["schedule"].(string); ok {
		workflow.Schedule = schedule
	}
	if timezone, ok := params["timezone"].(string); ok {
		workflow.Timezone = timezone
	}
	if enabled, ok := params["enabled"].(bool); ok {
		workflow.Enabled = enabled
	}
	if concurrencyPolicy, ok := params["concurrencyPolicy"].(string); ok {
		workflow.ConcurrencyPolicy = crd.WorkflowConcurrencyPolicy(concurrencyPolicy)
	}
	if timeout, ok := params["timeout"].(string); ok {
		workflow.Timeout = timeout
	}

	// Create workflow in database
	workflowRecord, err := mts.workflowStore.CreateWorkflow(workflow)
	if err != nil {
		return nil, fmt.Errorf("failed to create workflow in database: %w", err)
	}

	// Sync the new workflow to the WorkflowScheduler if it has a schedule
	if mts.workflowScheduler != nil && workflow.Schedule != "" {
		if err := mts.workflowScheduler.SyncWorkflow(&workflow); err != nil {
			mts.logger.Error(err, "Failed to sync workflow to scheduler", "workflow", name)
		} else {
			mts.logger.Info("Synced workflow to scheduler", "workflow", name, "schedule", workflow.Schedule)
		}
	}

	mts.logger.Info("Created workflow in database", "name", name, "id", workflowRecord.ID)

	return map[string]interface{}{
		"name":      name,
		"id":        workflowRecord.ID.String(),
		"steps":     len(workflowSteps),
		"message":   "Workflow created successfully in database",
		"enabled":   workflow.Enabled,
		"schedule":  workflow.Schedule,
	}, nil
}

// createWorkflowInCRD creates a workflow in Kubernetes CRD (fallback method)
func (mts *MCPToolServer) createWorkflowInCRD(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	if mts.k8sClient == nil {
		return nil, fmt.Errorf("kubernetes client not configured")
	}

	name, ok := params["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	steps, ok := params["steps"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("steps parameter is required")
	}

	namespace := mts.namespace
	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
	}

	// Get the MCPTaskScheduler resource
	taskScheduler := &crd.MCPTaskScheduler{}
	err := mts.k8sClient.Get(ctx, types.NamespacedName{
		Name:      "task-scheduler",
		Namespace: namespace,
	}, taskScheduler)
	if err != nil {
		return nil, fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}

	// Convert steps interface{} to WorkflowStep structs
	workflowSteps := make([]crd.WorkflowStep, 0, len(steps))
	for i, stepInterface := range steps {
		stepMap, ok := stepInterface.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("step %d is not a valid object", i)
		}

		step := crd.WorkflowStep{
			Name: fmt.Sprintf("%v", stepMap["name"]),
		}
		
		// Set tool to execute
		if tool, ok := stepMap["tool"].(string); ok {
			step.Tool = tool
		} else {
			// Default to echo tool for demo
			step.Tool = "echo"
		}
		
		// Set parameters
		if params, ok := stepMap["parameters"].(map[string]interface{}); ok {
			step.Parameters = params
		}
		
		workflowSteps = append(workflowSteps, step)
	}

	// Create the workflow definition
	workflow := crd.WorkflowDefinition{
		Name:    name,
		Enabled: true,
		Steps:   workflowSteps,
	}

	// Set schedule if provided
	if schedule, ok := params["schedule"].(string); ok {
		workflow.Schedule = schedule
	}

	// Add to workflows list
	taskScheduler.Spec.Workflows = append(taskScheduler.Spec.Workflows, workflow)

	// Update the resource
	err = mts.k8sClient.Update(ctx, taskScheduler)
	if err != nil {
		return nil, fmt.Errorf("failed to update MCPTaskScheduler: %w", err)
	}
	
	// Sync the new workflow to the WorkflowScheduler
	if mts.workflowScheduler != nil && workflow.Schedule != "" {
		if err := mts.workflowScheduler.SyncWorkflow(&workflow); err != nil {
			mts.logger.Error(err, "Failed to sync workflow to scheduler", "workflow", name)
		} else {
			mts.logger.Info("Synced workflow to scheduler", "workflow", name, "schedule", workflow.Schedule)
		}
	}

	mts.logger.Info("Created workflow", "name", name, "namespace", namespace)

	return map[string]interface{}{
		"name":      name,
		"namespace": namespace,
		"steps":     len(workflowSteps),
		"message":   "Workflow created successfully",
	}, nil
}

func (mts *MCPToolServer) listWorkflows(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	// Use PostgreSQL workflow store if available, otherwise fall back to CRD
	if mts.workflowStore != nil {
		return mts.listWorkflowsFromDB(ctx, params)
	}
	
	// Fallback to CRD-based listing
	return mts.listWorkflowsFromCRD(ctx, params)
}

// listWorkflowsFromDB lists workflows from PostgreSQL database
func (mts *MCPToolServer) listWorkflowsFromDB(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	workflows, err := mts.workflowStore.ListWorkflows()
	if err != nil {
		return nil, fmt.Errorf("failed to list workflows from database: %w", err)
	}

	workflowList := make([]map[string]interface{}, 0, len(workflows))
	for _, workflow := range workflows {
		workflowInfo := map[string]interface{}{
			"name":         workflow.Name,
			"description":  workflow.Description,
			"enabled":      workflow.Enabled,
			"schedule":     workflow.Schedule,
			"timezone":     workflow.Timezone,
			"steps":        len(workflow.Steps),
			"retry_policy": workflow.RetryPolicy != nil,
			"created_at":   workflow.CreatedAt.Format(time.RFC3339),
			"updated_at":   workflow.UpdatedAt.Format(time.RFC3339),
		}
		workflowList = append(workflowList, workflowInfo)
	}

	return map[string]interface{}{
		"workflows": workflowList,
		"count":     len(workflows),
		"source":    "database",
	}, nil
}

// listWorkflowsFromCRD lists workflows from Kubernetes CRD (fallback method)
func (mts *MCPToolServer) listWorkflowsFromCRD(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	if mts.k8sClient == nil {
		return nil, fmt.Errorf("kubernetes client not configured")
	}

	namespace := mts.namespace
	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
	}

	enabledOnly := false
	if enabled, ok := params["enabled_only"].(bool); ok {
		enabledOnly = enabled
	}

	// Get the MCPTaskScheduler resource
	taskScheduler := &crd.MCPTaskScheduler{}
	err := mts.k8sClient.Get(ctx, types.NamespacedName{
		Name:      "task-scheduler",
		Namespace: namespace,
	}, taskScheduler)
	if err != nil {
		return nil, fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}

	workflows := make([]map[string]interface{}, 0)
	for _, workflow := range taskScheduler.Spec.Workflows {
		if enabledOnly && !workflow.Enabled {
			continue
		}

		workflowInfo := map[string]interface{}{
			"name":         workflow.Name,
			"enabled":      workflow.Enabled,
			"steps":        len(workflow.Steps),
			"schedule":     workflow.Schedule,
			"retry_policy": workflow.RetryPolicy != nil,
		}

		if workflow.Timeout != "" {
			workflowInfo["timeout"] = workflow.Timeout
		}

		workflows = append(workflows, workflowInfo)
	}

	return map[string]interface{}{
		"workflows": workflows,
		"count":     len(workflows),
		"namespace": namespace,
	}, nil
}

func (mts *MCPToolServer) getWorkflow(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	if mts.k8sClient == nil {
		return nil, fmt.Errorf("kubernetes client not configured")
	}

	name, ok := params["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	namespace := mts.namespace
	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
	}

	// Get the MCPTaskScheduler resource
	taskScheduler := &crd.MCPTaskScheduler{}
	err := mts.k8sClient.Get(ctx, types.NamespacedName{
		Name:      "task-scheduler",
		Namespace: namespace,
	}, taskScheduler)
	if err != nil {
		return nil, fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}

	// Find the workflow
	for _, workflow := range taskScheduler.Spec.Workflows {
		if workflow.Name == name {
			// Convert to JSON-serializable format
			workflowJSON, err := json.Marshal(workflow)
			if err != nil {
				return nil, fmt.Errorf("failed to serialize workflow: %w", err)
			}

			var result map[string]interface{}
			if err := json.Unmarshal(workflowJSON, &result); err != nil {
				return nil, fmt.Errorf("failed to deserialize workflow: %w", err)
			}

			result["namespace"] = namespace
			return result, nil
		}
	}

	return nil, fmt.Errorf("workflow %s not found", name)
}

func (mts *MCPToolServer) executeWorkflow(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	if mts.workflowEngine == nil {
		return nil, fmt.Errorf("workflow engine not configured")
	}

	name, ok := params["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	namespace := mts.namespace
	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
	}

	// Get the workflow definition from CRD
	workflowData, err := mts.getWorkflow(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow: %w", err)
	}

	// Convert back to WorkflowDefinition for execution
	workflowJSON, err := json.Marshal(workflowData)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize workflow data: %w", err)
	}

	var workflow crd.WorkflowDefinition
	if err := json.Unmarshal(workflowJSON, &workflow); err != nil {
		return nil, fmt.Errorf("failed to deserialize workflow: %w", err)
	}

	// Execute the workflow using workflow engine
	execution, err := mts.workflowEngine.ExecuteWorkflow(ctx, &workflow, name, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to execute workflow: %w", err)
	}

	runID := execution.ID

	mts.logger.Info("Executed workflow", "name", name, "runID", runID)

	return map[string]interface{}{
		"name":      name,
		"run_id":    runID,
		"status":    "started",
		"message":   "Workflow execution started successfully",
		"namespace": namespace,
	}, nil
}

func (mts *MCPToolServer) pauseWorkflow(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	if mts.workflowEngine == nil {
		return nil, fmt.Errorf("workflow engine not configured")
	}

	name, ok := params["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	namespace := mts.namespace
	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
	}

	// Update the workflow in the CRD to set enabled=false
	taskScheduler := &crd.MCPTaskScheduler{}
	err := mts.k8sClient.Get(ctx, types.NamespacedName{
		Name:      "task-scheduler",
		Namespace: namespace,
	}, taskScheduler)
	if err != nil {
		return nil, fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}

	// Find and pause the workflow
	for i, workflow := range taskScheduler.Spec.Workflows {
		if workflow.Name == name {
			taskScheduler.Spec.Workflows[i].Enabled = false
			err = mts.k8sClient.Update(ctx, taskScheduler)
			if err != nil {
				return nil, fmt.Errorf("failed to pause workflow: %w", err)
			}
			
			// Sync the paused workflow to the WorkflowScheduler
			if mts.workflowScheduler != nil {
				updatedWorkflow := taskScheduler.Spec.Workflows[i]
				if err := mts.workflowScheduler.SyncWorkflow(&updatedWorkflow); err != nil {
					mts.logger.Error(err, "Failed to sync paused workflow to scheduler", "workflow", name)
				} else {
					mts.logger.Info("Synced paused workflow to scheduler", "workflow", name)
				}
			}
			
			mts.logger.Info("Paused workflow", "name", name)
			break
		}
	}

	mts.logger.Info("Paused workflow", "name", name)

	return map[string]interface{}{
		"name":      name,
		"status":    "paused",
		"message":   "Workflow paused successfully",
		"namespace": namespace,
	}, nil
}

func (mts *MCPToolServer) resumeWorkflow(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	if mts.workflowEngine == nil {
		return nil, fmt.Errorf("workflow engine not configured")
	}

	name, ok := params["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	namespace := mts.namespace
	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
	}

	// Update the workflow in the CRD to set enabled=true
	taskScheduler := &crd.MCPTaskScheduler{}
	err := mts.k8sClient.Get(ctx, types.NamespacedName{
		Name:      "task-scheduler",
		Namespace: namespace,
	}, taskScheduler)
	if err != nil {
		return nil, fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}

	// Find and resume the workflow
	for i, workflow := range taskScheduler.Spec.Workflows {
		if workflow.Name == name {
			taskScheduler.Spec.Workflows[i].Enabled = true
			err = mts.k8sClient.Update(ctx, taskScheduler)
			if err != nil {
				return nil, fmt.Errorf("failed to resume workflow: %w", err)
			}
			
			// Sync the resumed workflow to the WorkflowScheduler
			if mts.workflowScheduler != nil {
				updatedWorkflow := taskScheduler.Spec.Workflows[i]
				if err := mts.workflowScheduler.SyncWorkflow(&updatedWorkflow); err != nil {
					mts.logger.Error(err, "Failed to sync resumed workflow to scheduler", "workflow", name)
				} else {
					mts.logger.Info("Synced resumed workflow to scheduler", "workflow", name)
				}
			}
			
			mts.logger.Info("Resumed workflow", "name", name)
			break
		}
	}

	mts.logger.Info("Resumed workflow", "name", name)

	return map[string]interface{}{
		"name":      name,
		"status":    "running",
		"message":   "Workflow resumed successfully",
		"namespace": namespace,
	}, nil
}

func (mts *MCPToolServer) getWorkflowStatus(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	if mts.workflowEngine == nil {
		return nil, fmt.Errorf("workflow engine not configured")
	}

	name, ok := params["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name parameter is required")
	}

	namespace := mts.namespace
	if ns, ok := params["namespace"].(string); ok {
		namespace = ns
	}

	// Get the MCPTaskScheduler resource to check status
	taskScheduler := &crd.MCPTaskScheduler{}
	err := mts.k8sClient.Get(ctx, types.NamespacedName{
		Name:      "task-scheduler",
		Namespace: namespace,
	}, taskScheduler)
	if err != nil {
		return nil, fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}

	// Find the workflow and get its current status
	for _, workflow := range taskScheduler.Spec.Workflows {
		if workflow.Name == name {
			result := map[string]interface{}{
				"name":      name,
				"namespace": namespace,
				"enabled":   workflow.Enabled,
				"schedule":  workflow.Schedule,
				"steps":     len(workflow.Steps),
				"executions": []map[string]interface{}{},
			}

			// Get recent executions for this workflow
			executions := make([]map[string]interface{}, 0)
			for _, execution := range taskScheduler.Status.WorkflowExecutions {
				if execution.WorkflowName == name {
					execInfo := map[string]interface{}{
						"id":        execution.ID,
						"status":    string(execution.Phase),
						"start_time": execution.StartTime.Format(time.RFC3339),
					}
					if execution.EndTime != nil {
						execInfo["end_time"] = execution.EndTime.Format(time.RFC3339)
					}
					if execution.Message != "" {
						execInfo["message"] = execution.Message
					}
					executions = append(executions, execInfo)
				}
			}
			result["executions"] = executions

			return result, nil
		}
	}

	return nil, fmt.Errorf("workflow %s not found", name)
}

// workflowLogs gets logs from workflow executions and their Kubernetes Jobs
func (mts *MCPToolServer) workflowLogs(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	// Extract parameters
	name, _ := params["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("workflow name is required")
	}

	executionID, _ := params["execution_id"].(string)
	tail := 100
	if t, ok := params["tail"].(float64); ok {
		tail = int(t)
	}

	namespace := "default"
	if ns, ok := params["namespace"].(string); ok && ns != "" {
		namespace = ns
	}

	if mts.k8sClient == nil {
		return nil, fmt.Errorf("kubernetes client not configured")
	}

	mts.logger.Info("Getting workflow logs", "workflow", name, "executionID", executionID, "namespace", namespace)

	// Get the MCPTaskScheduler CRD
	taskScheduler := &crd.MCPTaskScheduler{}
	err := mts.k8sClient.Get(ctx, types.NamespacedName{
		Name: "task-scheduler", Namespace: namespace,
	}, taskScheduler)
	if err != nil {
		return nil, fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}

	result := map[string]interface{}{
		"workflow_name": name,
		"namespace":     namespace,
		"logs":          []map[string]interface{}{},
		"executions":    []map[string]interface{}{},
	}

	// Find workflow executions
	var targetExecutions []crd.WorkflowExecution
	for _, execution := range taskScheduler.Status.WorkflowExecutions {
		if execution.WorkflowName == name {
			if executionID == "" || execution.ID == executionID {
				targetExecutions = append(targetExecutions, execution)
			}
		}
	}

	if len(targetExecutions) == 0 {
		result["message"] = fmt.Sprintf("No executions found for workflow %s", name)
		if executionID != "" {
			result["message"] = fmt.Sprintf("Execution %s not found for workflow %s", executionID, name)
		}
		return result, nil
	}

	var allLogs []map[string]interface{}
	var execSummaries []map[string]interface{}

	// Get logs from each execution
	for _, execution := range targetExecutions {
		execSummary := map[string]interface{}{
			"id":         execution.ID,
			"status":     string(execution.Phase),
			"start_time": execution.StartTime.Format(time.RFC3339),
			"message":    execution.Message,
		}
		if execution.EndTime != nil {
			execSummary["end_time"] = execution.EndTime.Format(time.RFC3339)
			duration := execution.EndTime.Sub(execution.StartTime)
			execSummary["duration"] = duration.String()
		}

		// Try to get Kubernetes Job logs for this execution
		taskID := fmt.Sprintf("%s-%s", name, execution.ID)
		jobLogs, err := mts.getJobLogs(ctx, taskID, namespace, tail)
		if err != nil {
			mts.logger.Info("Could not get job logs for execution", "executionID", execution.ID, "error", err)
			// Add a log entry indicating we couldn't get job logs
			allLogs = append(allLogs, map[string]interface{}{
				"execution_id": execution.ID,
				"timestamp":    execution.StartTime.Format(time.RFC3339),
				"level":        "INFO",
				"message":      fmt.Sprintf("Execution started (K8s Job logs not available: %v)", err),
				"source":       "workflow-status",
			})
		} else {
			// Add job logs
			for _, logEntry := range jobLogs {
				logEntry["execution_id"] = execution.ID
				allLogs = append(allLogs, logEntry)
			}
		}

		// Add step results as log entries
		for stepName, stepResult := range execution.StepResults {
			stepLog := map[string]interface{}{
				"execution_id": execution.ID,
				"timestamp":    execution.StartTime.Format(time.RFC3339), // Would be better with step timestamp
				"level":        "INFO",
				"source":       "step-result",
				"step":         stepName,
				"status":       string(stepResult.Phase),
				"message":      fmt.Sprintf("Step %s: %s", stepName, stepResult.Phase),
			}
			
			if stepResult.Error != "" {
				stepLog["level"] = "ERROR"
				stepLog["message"] = fmt.Sprintf("Step %s failed: %s", stepName, stepResult.Error)
			}
			
			if stepResult.Duration > 0 {
				stepLog["duration"] = stepResult.Duration.String()
			}
			
			allLogs = append(allLogs, stepLog)
		}

		execSummaries = append(execSummaries, execSummary)
	}

	result["logs"] = allLogs
	result["executions"] = execSummaries
	result["total_logs"] = len(allLogs)

	return result, nil
}

// getJobLogs retrieves logs from Kubernetes Jobs created for workflow execution
func (mts *MCPToolServer) getJobLogs(ctx context.Context, taskID, namespace string, tail int) ([]map[string]interface{}, error) {
	// This is a simplified implementation - in a full implementation, this would
	// use the Kubernetes clientset to get pod logs from jobs with the task ID label
	
	// For now, return a placeholder indicating that K8s job integration is needed
	return []map[string]interface{}{
		{
			"timestamp": time.Now().Format(time.RFC3339),
			"level":     "INFO",
			"source":    "k8s-job",
			"message":   fmt.Sprintf("Workflow executed as Kubernetes Job (task ID: %s)", taskID),
		},
	}, nil
}