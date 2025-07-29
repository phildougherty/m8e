// internal/scheduler/workflow_scheduler.go
package scheduler

import (
	"context"
	"fmt"
	"sync"
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
	workflowStore   *WorkflowStore        // NEW: PostgreSQL workflow persistence
	watchContext    context.Context       // NEW: Context for watch operations
	watchCancel     context.CancelFunc    // NEW: Cancel function for watch
	watchMutex      sync.RWMutex          // NEW: Mutex for watch operations
	enableDeletionSync bool               // NEW: Whether to delete workflows from PostgreSQL when removed from CRD
	started         bool                  // NEW: Track if scheduler has been started
	startMutex      sync.Mutex            // NEW: Mutex for start/stop operations
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

// SetWorkflowStore sets the workflow store for PostgreSQL persistence and CRD sync
func (ws *WorkflowScheduler) SetWorkflowStore(store *WorkflowStore) {
	ws.workflowStore = store
}

// EnableDeletionSync enables deletion sync with safety checks to prevent accidental mass deletion
func (ws *WorkflowScheduler) EnableDeletionSync() {
	ws.enableDeletionSync = true
	ws.logger.Info("Deletion sync enabled with safety checks")
}

// Start starts the workflow scheduler
func (ws *WorkflowScheduler) Start() error {
	ws.startMutex.Lock()
	defer ws.startMutex.Unlock()
	
	// Check if already started - make this method idempotent
	if ws.started {
		ws.logger.V(1).Info("Workflow scheduler already started, skipping")
		return nil
	}
	
	ws.logger.Info("Starting workflow scheduler")
	
	// Start the cron engine
	ws.cronEngine.Start()
	
	// Create watch context
	ws.watchContext, ws.watchCancel = context.WithCancel(context.Background())
	
	// First sync PostgreSQL workflows to CRD on startup (if workflow store is available)
	if ws.workflowStore != nil {
		if err := ws.syncWorkflowsFromPostgreSQLToCRD(); err != nil {
			ws.logger.Error(err, "Failed to sync workflows from PostgreSQL to CRD on startup")
			// Continue - this is not a fatal error
		}
	}
	
	// Load and schedule workflows from MCPTaskScheduler CRD
	if err := ws.syncWorkflowsFromCRD(); err != nil {
		return err
	}
	
	// Start watching for CRD changes if k8s client is available
	if ws.k8sClient != nil {
		go ws.watchCRDChanges()
	}
	
	// Mark as started
	ws.started = true
	
	return nil
}

// Stop stops the workflow scheduler
func (ws *WorkflowScheduler) Stop() {
	ws.startMutex.Lock()
	defer ws.startMutex.Unlock()
	
	if !ws.started {
		ws.logger.V(1).Info("Workflow scheduler already stopped, skipping")
		return
	}
	
	ws.logger.Info("Stopping workflow scheduler")
	
	// Cancel the watch context to stop watching CRD changes
	if ws.watchCancel != nil {
		ws.watchCancel()
	}
	
	ws.cronEngine.Stop()
	
	// Mark as stopped
	ws.started = false
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

	// Sync CRD workflows to PostgreSQL if workflow store is available
	if ws.workflowStore != nil {
		ws.logger.Info("Syncing CRD workflows to PostgreSQL", "count", len(taskScheduler.Spec.Workflows))
		if err := ws.syncWorkflowsToPostgreSQL(taskScheduler.Spec.Workflows); err != nil {
			ws.logger.Error(err, "Failed to sync workflows to PostgreSQL")
		}
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
			if err := ws.updateWorkflowExecutionStatus(ctx, execution); err != nil {
				ws.logger.Error(err, "Failed to update workflow execution status", "workflow", workflowName, "executionID", executionID)
			}
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
				
				if err := ws.updateWorkflowExecutionStatus(ctx, execution); err != nil {
					ws.logger.Error(err, "Failed to update workflow execution status", "workflow", workflowName, "executionID", executionID)
				}
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
				
				if err := ws.updateWorkflowExecutionStatus(ctx, execution); err != nil {
					ws.logger.Error(err, "Failed to update workflow execution status", "workflow", workflowName, "executionID", executionID)
				}
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
	
	// Check if job already exists and if it needs updating
	existingJob, exists := ws.cronEngine.GetJob(jobID)
	needsUpdate := false
	
	if exists {
		// Compare existing job with new workflow definition to see if anything changed
		if existingJob.Schedule != workflow.Schedule ||
		   existingJob.Enabled != workflow.Enabled ||
		   existingJob.Name != workflow.Name {
			needsUpdate = true
			ws.logger.Info("Workflow definition changed, updating job", 
				"jobID", jobID,
				"oldSchedule", existingJob.Schedule, 
				"newSchedule", workflow.Schedule,
				"oldEnabled", existingJob.Enabled,
				"newEnabled", workflow.Enabled)
		} else {
			// Job exists and hasn't changed - no action needed
			ws.logger.V(1).Info("Job already exists with same configuration, skipping sync", "jobID", jobID)
			return nil
		}
	}
	
	// Remove existing job only if it needs updating
	if exists && needsUpdate {
		if err := ws.cronEngine.RemoveJob(jobID); err != nil {
			ws.logger.Error(err, "Failed to remove existing job for update", "jobID", jobID)
		}
	}

	// Schedule the workflow if it has a schedule and is enabled
	if workflow.Schedule != "" && workflow.Enabled {
		return ws.scheduleWorkflow(workflow)
	} else if exists {
		// Workflow is disabled or has no schedule, remove it if it exists
		if err := ws.cronEngine.RemoveJob(jobID); err != nil {
			ws.logger.Error(err, "Failed to remove disabled job", "jobID", jobID)
		}
		ws.logger.Info("Workflow disabled or no schedule, removed job", "workflow", workflow.Name)
	}

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

// watchCRDChanges periodically checks for changes to the MCPTaskScheduler CRD and syncs workflows to PostgreSQL
func (ws *WorkflowScheduler) watchCRDChanges() {
	ws.logger.Info("Starting CRD polling for workflow synchronization (checking every 30 seconds)")
	
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	var lastResourceVersion string
	
	for {
		select {
		case <-ws.watchContext.Done():
			ws.logger.Info("CRD polling stopped")
			return
		case <-ticker.C:
			if err := ws.checkCRDChanges(&lastResourceVersion); err != nil {
				ws.logger.Error(err, "Failed to check CRD changes")
			}
		}
	}
}

// checkCRDChanges checks if the MCPTaskScheduler CRD has changed and syncs if necessary
func (ws *WorkflowScheduler) checkCRDChanges(lastResourceVersion *string) error {
	ws.watchMutex.Lock()
	defer ws.watchMutex.Unlock()
	
	// Get the current MCPTaskScheduler
	taskScheduler := &crd.MCPTaskScheduler{}
	err := ws.k8sClient.Get(ws.watchContext, types.NamespacedName{
		Name:      "task-scheduler",
		Namespace: ws.namespace,
	}, taskScheduler)
	
	if err != nil {
		return fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}
	
	// Check if the resource version has changed
	currentResourceVersion := taskScheduler.ResourceVersion
	if *lastResourceVersion != "" && *lastResourceVersion == currentResourceVersion {
		// No changes, skip sync
		return nil
	}
	
	// Resource version changed, sync workflows to PostgreSQL
	if ws.workflowStore != nil {
		ws.logger.Info("MCPTaskScheduler CRD changed, syncing workflows to PostgreSQL", 
			"resourceVersion", currentResourceVersion, "workflowCount", len(taskScheduler.Spec.Workflows))
		
		if err := ws.syncWorkflowsToPostgreSQL(taskScheduler.Spec.Workflows); err != nil {
			return fmt.Errorf("failed to sync workflows to PostgreSQL: %w", err)
		}
	}
	
	// Update the last known resource version
	*lastResourceVersion = currentResourceVersion
	return nil
}

// syncWorkflowsToPostgreSQL syncs a list of workflows to PostgreSQL (handles additions, updates, and deletions)
func (ws *WorkflowScheduler) syncWorkflowsToPostgreSQL(workflows []crd.WorkflowDefinition) error {
	// Step 1: Get all workflows currently in PostgreSQL
	existingWorkflows, err := ws.workflowStore.ListWorkflows()
	if err != nil {
		return fmt.Errorf("failed to list existing workflows from PostgreSQL: %w", err)
	}
	
	// Step 2: Create a map of CRD workflow names for efficient lookup
	crdWorkflowNames := make(map[string]bool)
	for _, workflow := range workflows {
		crdWorkflowNames[workflow.Name] = true
	}
	
	// Step 3: Sync additions/updates from CRD to PostgreSQL
	syncedCount := 0
	for _, workflow := range workflows {
		if err := ws.syncWorkflowToPostgreSQL(workflow); err != nil {
			ws.logger.Error(err, "Failed to sync workflow to PostgreSQL", "workflow", workflow.Name)
			continue
		}
		syncedCount++
	}
	
	// Step 4: Handle deletions (now safe with soft deletion)
	deletedCount := 0
	if ws.enableDeletionSync {
		for _, existingWorkflow := range existingWorkflows {
			if !crdWorkflowNames[existingWorkflow.Name] {
				if err := ws.workflowStore.DeleteWorkflow(existingWorkflow.Name); err != nil {
					ws.logger.Error(err, "Failed to soft delete workflow from PostgreSQL", "workflow", existingWorkflow.Name)
					continue
				}
				ws.logger.Info("Soft deleted workflow from PostgreSQL (no longer in CRD)", "workflow", existingWorkflow.Name)
				deletedCount++
			}
		}
	} else {
		ws.logger.V(1).Info("Deletion sync disabled, skipping workflow cleanup")
	}
	
	
	ws.logger.Info("Completed workflow sync", 
		"synced", syncedCount, "total", len(workflows), 
		"deleted", deletedCount, "existing", len(existingWorkflows))
	return nil
}

// syncWorkflowToPostgreSQL syncs a single CRD workflow to PostgreSQL
func (ws *WorkflowScheduler) syncWorkflowToPostgreSQL(workflow crd.WorkflowDefinition) error {
	// Check if workflow already exists in PostgreSQL
	existingWorkflow, err := ws.workflowStore.GetWorkflow(workflow.Name)
	if err == nil && existingWorkflow != nil {
		// Workflow already exists, skip syncing
		ws.logger.V(1).Info("Workflow already exists in PostgreSQL, skipping sync", "workflow", workflow.Name)
		return nil
	}
	
	// Create workflow in PostgreSQL
	_, err = ws.workflowStore.CreateWorkflow(workflow)
	if err != nil {
		return fmt.Errorf("failed to create workflow in PostgreSQL: %w", err)
	}
	
	ws.logger.Info("Successfully synced workflow to PostgreSQL", "workflow", workflow.Name)
	return nil
}

// syncWorkflowsFromPostgreSQLToCRD syncs active workflows from PostgreSQL to the CRD on startup
func (ws *WorkflowScheduler) syncWorkflowsFromPostgreSQLToCRD() error {
	if ws.k8sClient == nil || ws.workflowStore == nil {
		return nil
	}

	// Get all active workflows from PostgreSQL
	pgWorkflows, err := ws.workflowStore.ListWorkflows()
	if err != nil {
		return fmt.Errorf("failed to list workflows from PostgreSQL: %w", err)
	}

	if len(pgWorkflows) == 0 {
		ws.logger.Info("No active workflows found in PostgreSQL to sync to CRD")
		return nil
	}

	// Get current CRD
	ctx := context.Background()
	taskScheduler := &crd.MCPTaskScheduler{}
	err = ws.k8sClient.Get(ctx, types.NamespacedName{
		Name:      "task-scheduler",
		Namespace: ws.namespace,
	}, taskScheduler)
	if err != nil {
		return fmt.Errorf("failed to get MCPTaskScheduler for PostgreSQL sync: %w", err)
	}

	// Convert PostgreSQL workflows to CRD format
	crdWorkflows := make([]crd.WorkflowDefinition, 0, len(pgWorkflows))
	for _, pgWorkflow := range pgWorkflows {
		crdWorkflow := pgWorkflow.ToCRDWorkflowDefinition()
		crdWorkflows = append(crdWorkflows, crdWorkflow)
	}

	// Update CRD with PostgreSQL workflows
	taskScheduler.Spec.Workflows = crdWorkflows
	
	err = ws.k8sClient.Update(ctx, taskScheduler)
	if err != nil {
		return fmt.Errorf("failed to update MCPTaskScheduler with PostgreSQL workflows: %w", err)
	}

	ws.logger.Info("Successfully synced workflows from PostgreSQL to CRD", 
		"syncedCount", len(crdWorkflows), 
		"workflows", func() []string {
			names := make([]string, len(crdWorkflows))
			for i, w := range crdWorkflows {
				names[i] = w.Name
			}
			return names
		}())
	
	return nil
}

// ExecuteWorkflowManually executes a workflow manually with a specific execution ID
func (ws *WorkflowScheduler) ExecuteWorkflowManually(ctx context.Context, workflowDef *crd.WorkflowDefinition, executionID string) error {
	if workflowDef == nil {
		return fmt.Errorf("workflow definition is nil")
	}
	
	ws.logger.Info("Executing workflow manually", 
		"workflow", workflowDef.Name, 
		"executionID", executionID)
	
	// Check if manual execution is allowed (allow by default for now)
	// In the future, we can implement proper ManualExecution flag checking
	manualExecutionAllowed := true
	if !manualExecutionAllowed {
		return fmt.Errorf("manual execution is disabled for workflow '%s'", workflowDef.Name)
	}
	
	// Use K8s executor if available, otherwise fall back to in-process execution
	if ws.k8sExecutor != nil {
		ws.logger.Info("Executing workflow as Kubernetes Job", 
			"workflow", workflowDef.Name, 
			"executionID", executionID)
		
		// Execute workflow as a Kubernetes Job
		_, err := ws.k8sExecutor.ExecuteWorkflowAsJob(ctx, workflowDef, workflowDef.Name, executionID)
		if err != nil {
			return fmt.Errorf("failed to execute workflow '%s' as K8s Job: %w", workflowDef.Name, err)
		}
		
		ws.logger.Info("Successfully submitted workflow for execution as K8s Job", 
			"workflow", workflowDef.Name, 
			"executionID", executionID)
		
		return nil
	}
	
	// Fall back to in-process execution via workflow engine
	ws.logger.Info("Executing workflow in-process via workflow engine", 
		"workflow", workflowDef.Name, 
		"executionID", executionID)
	
	execution, err := ws.workflowEngine.ExecuteWorkflow(ctx, workflowDef, workflowDef.Name, ws.namespace)
	if err != nil {
		return fmt.Errorf("failed to execute workflow '%s' in-process: %w", workflowDef.Name, err)
	}
	
	ws.logger.Info("Successfully executed workflow in-process", 
		"workflow", workflowDef.Name, 
		"executionID", executionID,
		"actualExecutionID", execution.ID,
		"status", execution.Status)
	
	return nil
}

