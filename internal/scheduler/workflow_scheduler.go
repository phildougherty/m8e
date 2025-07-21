// internal/scheduler/workflow_scheduler.go
package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/crd"
)

// WorkflowScheduler integrates CronEngine with WorkflowEngine for scheduled workflow execution
type WorkflowScheduler struct {
	cronEngine     *CronEngine
	workflowEngine *WorkflowEngine
	k8sClient      client.Client
	namespace      string
	logger         logr.Logger
}

// NewWorkflowScheduler creates a new workflow scheduler
func NewWorkflowScheduler(cronEngine *CronEngine, workflowEngine *WorkflowEngine, k8sClient client.Client, namespace string, logger logr.Logger) *WorkflowScheduler {
	return &WorkflowScheduler{
		cronEngine:     cronEngine,
		workflowEngine: workflowEngine,
		k8sClient:      k8sClient,
		namespace:      namespace,
		logger:         logger,
	}
}

// Start starts the workflow scheduler
func (ws *WorkflowScheduler) Start() error {
	ws.logger.Info("Starting workflow scheduler")
	
	// Start the cron engine
	ws.cronEngine.Start()
	
	// Load and schedule workflows from MCPTaskScheduler CRD
	return ws.syncWorkflowsFromCRD()
}

// Stop stops the workflow scheduler
func (ws *WorkflowScheduler) Stop() {
	ws.logger.Info("Stopping workflow scheduler")
	ws.cronEngine.Stop()
}

// syncWorkflowsFromCRD loads workflows from the MCPTaskScheduler CRD and schedules them
func (ws *WorkflowScheduler) syncWorkflowsFromCRD() error {
	if ws.k8sClient == nil {
		ws.logger.Info("Kubernetes client not configured, skipping CRD sync")
		return nil
	}

	ctx := context.Background()
	taskScheduler := &crd.MCPTaskScheduler{}
	err := ws.k8sClient.Get(ctx, types.NamespacedName{
		Name:      "task-scheduler",
		Namespace: ws.namespace,
	}, taskScheduler)
	if err != nil {
		return fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}

	// Schedule each workflow that has a schedule
	for _, workflow := range taskScheduler.Spec.Workflows {
		if workflow.Schedule != "" && workflow.Enabled {
			err := ws.scheduleWorkflow(&workflow)
			if err != nil {
				ws.logger.Error(err, "Failed to schedule workflow", "workflow", workflow.Name)
				continue
			}
		}
	}

	ws.logger.Info("Synchronized workflows from CRD", "count", len(taskScheduler.Spec.Workflows))
	return nil
}

// scheduleWorkflow schedules a single workflow using CronEngine
func (ws *WorkflowScheduler) scheduleWorkflow(workflow *crd.WorkflowDefinition) error {
	jobID := fmt.Sprintf("%s/%s", ws.namespace, workflow.Name)
	
	// Create job handler that executes the workflow
	handler := func(ctx context.Context, jobID string) error {
		ws.logger.Info("Executing scheduled workflow", "workflow", workflow.Name, "jobID", jobID)
		
		// Execute the workflow using WorkflowEngine
		execution, err := ws.workflowEngine.ExecuteWorkflow(ctx, workflow, workflow.Name, ws.namespace)
		if err != nil {
			ws.logger.Error(err, "Workflow execution failed", "workflow", workflow.Name)
			return err
		}

		// Update the MCPTaskScheduler status with execution results
		return ws.updateWorkflowExecutionStatus(ctx, execution)
	}

	// Parse timezone if specified
	var timezone *time.Location
	if workflow.Timezone != "" {
		tz, err := time.LoadLocation(workflow.Timezone)
		if err != nil {
			ws.logger.Error(err, "Invalid timezone, using UTC", "timezone", workflow.Timezone)
			timezone = time.UTC
		} else {
			timezone = tz
		}
	} else {
		timezone = time.UTC
	}

	// Create JobSpec for CronEngine
	jobSpec := &JobSpec{
		ID:         jobID,
		Name:       workflow.Name,
		Schedule:   workflow.Schedule,
		Timezone:   timezone,
		Enabled:    workflow.Enabled,
		Handler:    handler,
		MaxRetries: 3, // Default retry count
		RetryDelay: 30 * time.Second,
	}

	// Set retry configuration if specified
	if workflow.RetryPolicy != nil {
		jobSpec.MaxRetries = int(workflow.RetryPolicy.MaxRetries)
		if workflow.RetryPolicy.RetryDelay != "" {
			if delay, err := time.ParseDuration(workflow.RetryPolicy.RetryDelay); err == nil {
				jobSpec.RetryDelay = delay
			}
		}
	}

	// Add job to CronEngine
	return ws.cronEngine.AddJob(jobSpec)
}

// updateWorkflowExecutionStatus updates the MCPTaskScheduler CRD with workflow execution results
func (ws *WorkflowScheduler) updateWorkflowExecutionStatus(ctx context.Context, execution *WorkflowExecution) error {
	if ws.k8sClient == nil {
		return nil // Can't update status without k8s client
	}

	// Get the current MCPTaskScheduler
	taskScheduler := &crd.MCPTaskScheduler{}
	err := ws.k8sClient.Get(ctx, types.NamespacedName{
		Name:      "task-scheduler",
		Namespace: ws.namespace,
	}, taskScheduler)
	if err != nil {
		return fmt.Errorf("failed to get MCPTaskScheduler for status update: %w", err)
	}

	// Convert WorkflowExecution to WorkflowExecution status
	statusExecution := crd.WorkflowExecution{
		ID:           execution.ID,
		WorkflowName: execution.WorkflowName,
		StartTime:    execution.StartTime,
		Phase:        crd.WorkflowPhase(execution.Status),
		StepResults:  make(map[string]crd.StepResult),
	}

	if execution.EndTime != nil {
		statusExecution.EndTime = execution.EndTime
		duration := execution.EndTime.Sub(execution.StartTime)
		statusExecution.Duration = &duration
	}

	if execution.Error != "" {
		statusExecution.Message = execution.Error
	}

	// Convert step executions to step results
	for _, step := range execution.Steps {
		stepResult := crd.StepResult{
			Phase:    crd.StepPhase(step.Status),
			Output:   step.Output,
			Duration: step.Duration,
			Attempts: int32(step.Attempts),
		}
		if step.Error != "" {
			stepResult.Error = step.Error
		}
		statusExecution.StepResults[step.Name] = stepResult
	}

	// Update the status - limit to most recent 10 executions per workflow
	if taskScheduler.Status.WorkflowExecutions == nil {
		taskScheduler.Status.WorkflowExecutions = make([]crd.WorkflowExecution, 0)
	}

	// Add new execution
	taskScheduler.Status.WorkflowExecutions = append(taskScheduler.Status.WorkflowExecutions, statusExecution)

	// Keep only the most recent 10 executions per workflow
	executionsByWorkflow := make(map[string][]crd.WorkflowExecution)
	for _, exec := range taskScheduler.Status.WorkflowExecutions {
		executionsByWorkflow[exec.WorkflowName] = append(executionsByWorkflow[exec.WorkflowName], exec)
	}

	// Trim each workflow's execution history to 10 most recent
	var allExecutions []crd.WorkflowExecution
	for workflowName, executions := range executionsByWorkflow {
		if len(executions) > 10 {
			// Sort by start time (most recent first) and take first 10
			// For simplicity, just take the last 10 (they're likely already in order)
			executions = executions[len(executions)-10:]
		}
		allExecutions = append(allExecutions, executions...)
		ws.logger.Info("Keeping execution history", "workflow", workflowName, "count", len(executions))
	}
	
	taskScheduler.Status.WorkflowExecutions = allExecutions

	// Update the status
	err = ws.k8sClient.Status().Update(ctx, taskScheduler)
	if err != nil {
		ws.logger.Error(err, "Failed to update workflow execution status", "workflow", execution.WorkflowName)
		return err
	}

	ws.logger.Info("Updated workflow execution status", "workflow", execution.WorkflowName, "executionID", execution.ID, "status", execution.Status)
	return nil
}

// SyncWorkflow synchronizes a single workflow schedule
func (ws *WorkflowScheduler) SyncWorkflow(workflow *crd.WorkflowDefinition) error {
	jobID := fmt.Sprintf("%s/%s", ws.namespace, workflow.Name)
	
	// Remove existing job if it exists
	if _, exists := ws.cronEngine.GetJob(jobID); exists {
		if err := ws.cronEngine.RemoveJob(jobID); err != nil {
			ws.logger.Error(err, "Failed to remove existing job", "jobID", jobID)
		}
	}

	// Schedule the workflow if it has a schedule and is enabled
	if workflow.Schedule != "" && workflow.Enabled {
		return ws.scheduleWorkflow(workflow)
	}

	ws.logger.Info("Workflow not scheduled (no schedule or disabled)", "workflow", workflow.Name)
	return nil
}

// RemoveWorkflow removes a workflow from scheduling
func (ws *WorkflowScheduler) RemoveWorkflow(workflowName string) error {
	jobID := fmt.Sprintf("%s/%s", ws.namespace, workflowName)
	return ws.cronEngine.RemoveJob(jobID)
}

// ListScheduledWorkflows returns all currently scheduled workflows
func (ws *WorkflowScheduler) ListScheduledWorkflows() []*JobSpec {
	return ws.cronEngine.ListJobs()
}