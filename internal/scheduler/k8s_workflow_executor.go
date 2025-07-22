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
	// Use the matey binary to execute the workflow
	image := "mcp.robotrad.io/matey:latest"
	
	// Command to execute the workflow using matey's workflow engine
	command := []string{"./matey"}
	args := []string{
		"scheduler-execute-workflow",
		"--workflow-name", workflowName,
		"--execution-id", executionID,
		"--namespace", kwe.namespace,
	}

	// Set environment variables for workflow execution
	env := map[string]string{
		"WORKFLOW_NAME":         workflowName,
		"WORKFLOW_EXECUTION_ID": executionID,
		"KUBERNETES_NAMESPACE":  kwe.namespace,
		"LOG_LEVEL":            "info",
	}

	// Add proxy configuration if available
	if kwe.config != nil && kwe.config.Servers != nil {
		if proxyConfig, exists := kwe.config.Servers["matey-proxy"]; exists {
			env["MCP_PROXY_URL"] = fmt.Sprintf("http://matey-proxy.%s.svc.cluster.local:%d", kwe.namespace, proxyConfig.HttpPort)
			if proxyConfig.Authentication != nil {
				// Would need to extract API key from authentication config
				env["MCP_PROXY_API_KEY"] = "proxy-api-key" // Placeholder
			}
		}
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