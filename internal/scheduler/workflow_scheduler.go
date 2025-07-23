// internal/scheduler/workflow_scheduler.go
package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/task_scheduler"
)

// WorkflowScheduler integrates CronEngine with WorkflowEngine for scheduled workflow execution
type WorkflowScheduler struct {
	cronEngine      *CronEngine
	workflowEngine  *WorkflowEngine
	k8sExecutor     *K8sWorkflowExecutor  // NEW: K8s Job executor
	k8sClient       client.Client
	namespace       string
	logger          logr.Logger
	config          *config.ComposeConfig  // NEW: Configuration
}

// NewWorkflowScheduler creates a new workflow scheduler
func NewWorkflowScheduler(cronEngine *CronEngine, workflowEngine *WorkflowEngine, k8sClient client.Client, namespace string, config *config.ComposeConfig, logger logr.Logger) *WorkflowScheduler {
	// Create K8s workflow executor
	k8sExecutor, err := NewK8sWorkflowExecutor(namespace, config, logger)
	if err != nil {
		logger.Error(err, "Failed to create K8s workflow executor, workflows will run in-process")
		// Continue without K8s executor for backward compatibility
	}

	return &WorkflowScheduler{
		cronEngine:      cronEngine,
		workflowEngine:  workflowEngine,
		k8sExecutor:     k8sExecutor,
		k8sClient:       k8sClient,
		namespace:       namespace,
		config:          config,
		logger:          logger,
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
		
		// Generate unique execution ID
		executionID := fmt.Sprintf("%s-%d", workflow.Name, time.Now().Unix())
		
		// Use K8s Job executor if available, otherwise fall back to in-process execution
		if ws.k8sExecutor != nil {
			ws.logger.Info("Executing workflow as Kubernetes Job", "workflow", workflow.Name, "executionID", executionID)
			
			// Execute as Kubernetes Job
			taskStatus, err := ws.k8sExecutor.ExecuteWorkflowAsJob(ctx, workflow, workflow.Name, executionID)
			if err != nil {
				ws.logger.Error(err, "Failed to execute workflow as Kubernetes Job", "workflow", workflow.Name)
				return err
			}
			
			ws.logger.Info("Workflow submitted as Kubernetes Job", 
				"workflow", workflow.Name, 
				"executionID", executionID,
				"jobName", taskStatus.JobName,
				"taskID", taskStatus.ID)
			
			// Start a goroutine to monitor the Job and update status when complete
			go ws.monitorJobAndUpdateStatus(ctx, workflow, workflow.Name, executionID, taskStatus)
			
			return nil
		} else {
			ws.logger.Info("Executing workflow in-process (no K8s executor available)", "workflow", workflow.Name)
			
			// Fallback to in-process execution
			execution, err := ws.workflowEngine.ExecuteWorkflow(ctx, workflow, workflow.Name, ws.namespace)
			if err != nil {
				ws.logger.Error(err, "Workflow execution failed", "workflow", workflow.Name)
				return err
			}

			// Update the MCPTaskScheduler status with execution results
			return ws.updateWorkflowExecutionStatus(ctx, execution)
		}
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

// monitorJobAndUpdateStatus monitors a Kubernetes Job and updates the MCPTaskScheduler status when complete
func (ws *WorkflowScheduler) monitorJobAndUpdateStatus(ctx context.Context, workflow *crd.WorkflowDefinition, workflowName, executionID string, taskStatus *task_scheduler.TaskStatus) {
	// Create a WorkflowExecution for status tracking
	execution := &WorkflowExecution{
		ID:           executionID,
		WorkflowName: workflowName,
		StartTime:    time.Now(),
		Status:       WorkflowExecutionStatusRunning,
		Steps:        make([]StepExecution, len(workflow.Steps)),
	}
	
	// Initialize step executions
	for i, step := range workflow.Steps {
		execution.Steps[i] = StepExecution{
			Name:   step.Name,
			Status: StepExecutionStatusPending,
		}
	}
	
	ws.logger.Info("Monitoring Kubernetes Job for workflow execution", 
		"workflow", workflowName, "executionID", executionID, "jobName", taskStatus.JobName)
	
	// Monitor the job status in a loop
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	
	timeout := time.After(30 * time.Minute) // Default timeout
	if workflow.Timeout != "" {
		if duration, err := time.ParseDuration(workflow.Timeout); err == nil {
			timeout = time.After(duration)
		}
	}
	
	for {
		select {
		case <-ctx.Done():
			ws.logger.Info("Context cancelled, stopping job monitoring", "workflow", workflowName)
			return
			
		case <-timeout:
			ws.logger.Error(nil, "Job monitoring timed out", "workflow", workflowName, "executionID", executionID)
			execution.Status = WorkflowExecutionStatusFailed
			execution.Error = "Job execution timed out"
			endTime := time.Now()
			execution.EndTime = &endTime
			ws.updateWorkflowExecutionStatus(ctx, execution)
			return
			
		case <-ticker.C:
			// Check job status
			currentStatus, err := ws.k8sExecutor.GetWorkflowJobStatus(ctx, workflowName, executionID)
			if err != nil {
				ws.logger.Error(err, "Failed to get job status", "workflow", workflowName, "executionID", executionID)
				continue
			}
			
			// Map TaskStatus.Phase to workflow status
			var jobStatus string
			switch currentStatus.Phase {
			case "Succeeded":
				jobStatus = "completed"
			case "Failed":
				jobStatus = "failed"  
			case "Running":
				jobStatus = "running"
			default:
				jobStatus = "unknown"
			}
			
			switch jobStatus {
			case "completed":
				ws.logger.Info("Kubernetes Job completed successfully", 
					"workflow", workflowName, "executionID", executionID)
				
				execution.Status = WorkflowExecutionStatusSucceeded
				endTime := time.Now()
				execution.EndTime = &endTime
				
				// Mark all steps as completed (simplified for now)
				for i := range execution.Steps {
					execution.Steps[i].Status = StepExecutionStatusSucceeded
					execution.Steps[i].Duration = execution.EndTime.Sub(execution.StartTime)
				}
				
				// Get job logs if possible
				if logs, err := ws.k8sExecutor.GetWorkflowJobLogs(ctx, workflowName, executionID); err == nil && logs != "" {
					execution.Error = "" // Clear any previous error, use logs for output
				}
				
				ws.updateWorkflowExecutionStatus(ctx, execution)
				return
				
			case "failed":
				ws.logger.Error(nil, "Kubernetes Job failed", 
					"workflow", workflowName, "executionID", executionID)
				
				execution.Status = WorkflowExecutionStatusFailed
				endTime := time.Now()
				execution.EndTime = &endTime
				
				// Get job logs for error details
				if logs, err := ws.k8sExecutor.GetWorkflowJobLogs(ctx, workflowName, executionID); err == nil {
					execution.Error = logs
				} else {
					execution.Error = "Job execution failed"
				}
				
				ws.updateWorkflowExecutionStatus(ctx, execution)
				return
				
			case "running":
				// Job still running, continue monitoring
				continue
				
			default:
				ws.logger.Info("Kubernetes Job in unknown state", 
					"workflow", workflowName, "executionID", executionID, "phase", currentStatus.Phase)
				continue
			}
		}
	}
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