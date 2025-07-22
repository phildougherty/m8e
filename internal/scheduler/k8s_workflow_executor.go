// internal/scheduler/k8s_workflow_executor.go
package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/task_scheduler"
)

// K8sWorkflowExecutor executes workflows as Kubernetes Jobs
type K8sWorkflowExecutor struct {
	jobManager *task_scheduler.K8sJobManager
	logger     logr.Logger
	namespace  string
	config     *config.ComposeConfig
}

// NewK8sWorkflowExecutor creates a new Kubernetes workflow executor
func NewK8sWorkflowExecutor(namespace string, config *config.ComposeConfig, logger logr.Logger) (*K8sWorkflowExecutor, error) {
	if namespace == "" {
		namespace = "default"
	}

	jobManager, err := task_scheduler.NewK8sJobManager(namespace, config, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create job manager: %w", err)
	}

	return &K8sWorkflowExecutor{
		jobManager: jobManager,
		logger:     logger,
		namespace:  namespace,
		config:     config,
	}, nil
}

// ExecuteWorkflowAsJob executes a workflow definition as a Kubernetes Job
func (kwe *K8sWorkflowExecutor) ExecuteWorkflowAsJob(ctx context.Context, workflowDef *crd.WorkflowDefinition, workflowName, executionID string) (*task_scheduler.TaskStatus, error) {
	kwe.logger.Info("Executing workflow as Kubernetes Job", 
		"workflow", workflowName, 
		"executionID", executionID,
		"steps", len(workflowDef.Steps))

	// Create task request for the workflow
	taskRequest := kwe.createWorkflowTaskRequest(workflowDef, workflowName, executionID)

	// Submit the task as a Kubernetes Job
	taskStatus, err := kwe.jobManager.SubmitTask(ctx, taskRequest)
	if err != nil {
		kwe.logger.Error(err, "Failed to submit workflow as Kubernetes Job", 
			"workflow", workflowName, "executionID", executionID)
		return nil, fmt.Errorf("failed to submit workflow job: %w", err)
	}

	kwe.logger.Info("Workflow submitted as Kubernetes Job", 
		"workflow", workflowName, 
		"executionID", executionID,
		"jobName", taskStatus.JobName)

	return taskStatus, nil
}

// createWorkflowTaskRequest converts a workflow definition to a TaskRequest
func (kwe *K8sWorkflowExecutor) createWorkflowTaskRequest(workflowDef *crd.WorkflowDefinition, workflowName, executionID string) *task_scheduler.TaskRequest {
	// For now, create a simple workflow execution script that runs all steps
	// In production, this could be enhanced to run steps separately or use init containers
	
	// Determine appropriate container image and commands based on workflow steps
	image, command, args := kwe.buildWorkflowExecution(workflowDef)

	// Set environment variables for workflow execution
	env := map[string]string{
		"WORKFLOW_NAME":         workflowName,
		"WORKFLOW_EXECUTION_ID": executionID,
		"KUBERNETES_NAMESPACE":  kwe.namespace,
	}

	// Set timeout from workflow definition
	timeout := 30 * time.Minute // Default timeout
	if workflowDef.Timeout != "" {
		if parsedTimeout, err := time.ParseDuration(workflowDef.Timeout); err == nil {
			timeout = parsedTimeout
		}
	}

	// Set retry policy
	retryConfig := task_scheduler.TaskRetryConfig{
		MaxRetries:      3,
		RetryDelay:      30 * time.Second,
		BackoffStrategy: "exponential",
	}
	
	if workflowDef.RetryPolicy != nil {
		retryConfig.MaxRetries = int(workflowDef.RetryPolicy.MaxRetries)
		if workflowDef.RetryPolicy.RetryDelay != "" {
			if delay, err := time.ParseDuration(workflowDef.RetryPolicy.RetryDelay); err == nil {
				retryConfig.RetryDelay = delay
			}
		}
		retryConfig.BackoffStrategy = string(workflowDef.RetryPolicy.BackoffStrategy)
	}

	// Set resource requirements - WorkflowDefinition doesn't have Resources field
	// These would come from the MCPTaskScheduler spec or be configured elsewhere
	resources := task_scheduler.TaskResourceConfig{
		CPU:    "200m",
		Memory: "256Mi",
	}

	return &task_scheduler.TaskRequest{
		ID:          fmt.Sprintf("%s-%s", workflowName, executionID),
		Name:        fmt.Sprintf("workflow-%s", workflowName),
		Description: fmt.Sprintf("Workflow execution for %s (ID: %s)", workflowName, executionID),
		Command:     command,
		Args:        args,
		Image:       image,
		Env:         env,
		Timeout:     timeout,
		Retry:       retryConfig,
		Resources:   resources,
		Metadata: map[string]interface{}{
			"workflowName":    workflowName,
			"executionID":     executionID,
			"workflowType":    "scheduled",
			"stepCount":       len(workflowDef.Steps),
			"schedule":        workflowDef.Schedule,
			"timezone":        workflowDef.Timezone,
			"submittedAt":     time.Now().Format(time.RFC3339),
		},
	}
}

// buildWorkflowExecution creates the appropriate container image and command for executing the workflow
func (kwe *K8sWorkflowExecutor) buildWorkflowExecution(workflowDef *crd.WorkflowDefinition) (string, []string, []string) {
	// Use the standard workflow runner image for all workflows
	// This image contains all common tools: bash, curl, wget, jq, git, python3, nodejs, etc.
	
	if len(workflowDef.Steps) == 0 {
		return "mcp.robotrad.io/workflow-runner:latest", []string{}, []string{"echo 'No steps defined'"}
	}
	
	// Build the complete workflow script
	scriptCommands := kwe.buildShellScript(workflowDef)
	
	// The workflow-runner entrypoint expects the script as the first argument
	return "mcp.robotrad.io/workflow-runner:latest", []string{}, []string{scriptCommands}
}

// buildShellScript creates a shell script that executes all workflow steps
func (kwe *K8sWorkflowExecutor) buildShellScript(workflowDef *crd.WorkflowDefinition) string {
	script := "#!/bin/bash\n"
	script += "set -e\n" // Exit on error
	script += fmt.Sprintf("echo 'Starting workflow: %s'\n", workflowDef.Name)
	
	for i, step := range workflowDef.Steps {
		script += fmt.Sprintf("echo 'Step %d: %s'\n", i+1, step.Name)
		
		switch step.Tool {
		case "echo":
			message := "Hello World"
			if msg, ok := step.Parameters["message"].(string); ok {
				message = msg
			}
			script += fmt.Sprintf("echo '%s'\n", message)
			
		case "date":
			format := ""
			if fmt, ok := step.Parameters["format"].(string); ok {
				format = fmt
			}
			if format != "" {
				script += fmt.Sprintf("date '%s'\n", format)
			} else {
				script += "date\n"
			}
			
		case "sleep":
			duration := "1"
			if dur, ok := step.Parameters["duration"].(string); ok {
				duration = dur
			}
			script += fmt.Sprintf("sleep %s\n", duration)
			
		case "curl":
			url := ""
			method := "GET"
			headers := ""
			data := ""
			
			if u, ok := step.Parameters["url"].(string); ok {
				url = u
			}
			if m, ok := step.Parameters["method"].(string); ok {
				method = m
			}
			if h, ok := step.Parameters["headers"].(string); ok {
				headers = h
			}
			if d, ok := step.Parameters["data"].(string); ok {
				data = d
			}
			
			if url != "" {
				curlCmd := "curl -s"
				if method != "GET" {
					curlCmd += fmt.Sprintf(" -X %s", method)
				}
				if headers != "" {
					curlCmd += fmt.Sprintf(" -H '%s'", headers)
				}
				if data != "" {
					curlCmd += fmt.Sprintf(" -d '%s'", data)
				}
				curlCmd += fmt.Sprintf(" '%s'\n", url)
				script += curlCmd
			}
			
		case "wget":
			url := ""
			output := ""
			if u, ok := step.Parameters["url"].(string); ok {
				url = u
			}
			if o, ok := step.Parameters["output"].(string); ok {
				output = o
			}
			if url != "" {
				wgetCmd := "wget -q"
				if output != "" {
					wgetCmd += fmt.Sprintf(" -O '%s'", output)
				}
				wgetCmd += fmt.Sprintf(" '%s'\n", url)
				script += wgetCmd
			}
			
		case "python3", "python":
			code := ""
			file := ""
			if c, ok := step.Parameters["code"].(string); ok {
				code = c
			}
			if f, ok := step.Parameters["file"].(string); ok {
				file = f
			}
			
			if code != "" {
				script += fmt.Sprintf("python3 -c '%s'\n", code)
			} else if file != "" {
				script += fmt.Sprintf("python3 '%s'\n", file)
			}
			
		case "node", "nodejs":
			code := ""
			file := ""
			if c, ok := step.Parameters["code"].(string); ok {
				code = c
			}
			if f, ok := step.Parameters["file"].(string); ok {
				file = f
			}
			
			if code != "" {
				script += fmt.Sprintf("node -e '%s'\n", code)
			} else if file != "" {
				script += fmt.Sprintf("node '%s'\n", file)
			}
			
		case "jq":
			filter := "."
			input := ""
			if f, ok := step.Parameters["filter"].(string); ok {
				filter = f
			}
			if i, ok := step.Parameters["input"].(string); ok {
				input = i
			}
			
			if input != "" {
				script += fmt.Sprintf("echo '%s' | jq '%s'\n", input, filter)
			} else {
				script += fmt.Sprintf("jq '%s'\n", filter)
			}
			
		case "git":
			command := ""
			if cmd, ok := step.Parameters["command"].(string); ok {
				command = cmd
			}
			if command != "" {
				script += fmt.Sprintf("git %s\n", command)
			}
			
		case "bash", "sh":
			command := ""
			if cmd, ok := step.Parameters["command"].(string); ok {
				command = cmd
			}
			if command != "" {
				script += fmt.Sprintf("%s\n", command)
			}
			
		default:
			// For unknown tools, try to execute them as commands
			command := step.Tool
			if cmd, ok := step.Parameters["command"].(string); ok {
				command = cmd
			} else if args, ok := step.Parameters["args"].(string); ok {
				command = fmt.Sprintf("%s %s", step.Tool, args)
			}
			script += fmt.Sprintf("%s || echo 'Command failed: %s'\n", command, command)
		}
		
		script += fmt.Sprintf("echo 'Step %d completed'\n", i+1)
	}
	
	script += fmt.Sprintf("echo 'Workflow %s completed successfully'\n", workflowDef.Name)
	return script
}

// GetWorkflowJobStatus gets the status of a workflow job
func (kwe *K8sWorkflowExecutor) GetWorkflowJobStatus(ctx context.Context, workflowName, executionID string) (*task_scheduler.TaskStatus, error) {
	taskID := fmt.Sprintf("%s-%s", workflowName, executionID)
	return kwe.jobManager.GetTaskStatus(ctx, taskID)
}

// GetWorkflowJobLogs gets logs from a workflow job
func (kwe *K8sWorkflowExecutor) GetWorkflowJobLogs(ctx context.Context, workflowName, executionID string) (string, error) {
	taskID := fmt.Sprintf("%s-%s", workflowName, executionID)
	return kwe.jobManager.GetTaskLogs(ctx, taskID)
}

// CancelWorkflowJob cancels a running workflow job
func (kwe *K8sWorkflowExecutor) CancelWorkflowJob(ctx context.Context, workflowName, executionID string) error {
	taskID := fmt.Sprintf("%s-%s", workflowName, executionID)
	return kwe.jobManager.CancelTask(ctx, taskID)
}

// ListWorkflowJobs lists all workflow jobs
func (kwe *K8sWorkflowExecutor) ListWorkflowJobs(ctx context.Context) ([]*task_scheduler.TaskStatus, error) {
	return kwe.jobManager.ListTasks(ctx, "workflow-scheduler")
}

// CleanupOldWorkflowJobs removes old completed workflow jobs
func (kwe *K8sWorkflowExecutor) CleanupOldWorkflowJobs(ctx context.Context, olderThan time.Duration) error {
	return kwe.jobManager.CleanupCompletedTasks(ctx, "workflow-scheduler", olderThan)
}