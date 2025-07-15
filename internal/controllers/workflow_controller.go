// internal/controllers/workflow_controller.go
package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/scheduler"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	WorkflowFinalizer = "workflow.mcp.matey.ai/finalizer"
	WorkflowJobLabel  = "workflow.mcp.matey.ai/name"
)

type WorkflowReconciler struct {
	client.Client
	Scheme     *runtime.Scheme
	Logger     logr.Logger
	CronEngine *scheduler.CronEngine
}

func (r *WorkflowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Logger.WithValues("workflow", req.NamespacedName)

	// Fetch the Workflow instance
	var workflow crd.Workflow
	if err := r.Get(ctx, req.NamespacedName, &workflow); err != nil {
		if client.IgnoreNotFound(err) == nil {
			log.Info("Workflow resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Failed to get Workflow")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if workflow.DeletionTimestamp != nil {
		return r.reconcileDelete(ctx, &workflow)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&workflow, WorkflowFinalizer) {
		controllerutil.AddFinalizer(&workflow, WorkflowFinalizer)
		if err := r.Update(ctx, &workflow); err != nil {
			log.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Reconcile the workflow
	return r.reconcileNormal(ctx, &workflow)
}

func (r *WorkflowReconciler) reconcileNormal(ctx context.Context, workflow *crd.Workflow) (ctrl.Result, error) {
	log := r.Logger.WithValues("workflow", workflow.Name, "namespace", workflow.Namespace)

	// Validate workflow specification
	if err := r.validateWorkflow(workflow); err != nil {
		log.Error(err, "Workflow validation failed")
		r.updateWorkflowCondition(workflow, crd.WorkflowConditionValidated, metav1.ConditionFalse, "ValidationFailed", err.Error())
		if updateErr := r.Status().Update(ctx, workflow); updateErr != nil {
			log.Error(updateErr, "Failed to update workflow status")
		}
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// Mark workflow as validated
	r.updateWorkflowCondition(workflow, crd.WorkflowConditionValidated, metav1.ConditionTrue, "ValidationSucceeded", "Workflow specification is valid")

	// Update observed generation
	workflow.Status.ObservedGeneration = workflow.Generation

	// Handle workflow scheduling
	if workflow.Spec.Enabled && !workflow.Spec.Suspend {
		if err := r.scheduleWorkflow(ctx, workflow); err != nil {
			log.Error(err, "Failed to schedule workflow")
			r.updateWorkflowCondition(workflow, crd.WorkflowConditionScheduled, metav1.ConditionFalse, "SchedulingFailed", err.Error())
			workflow.Status.Phase = crd.WorkflowPhaseFailed
		} else {
			r.updateWorkflowCondition(workflow, crd.WorkflowConditionScheduled, metav1.ConditionTrue, "SchedulingSucceeded", "Workflow scheduled successfully")
			if workflow.Status.Phase == "" || workflow.Status.Phase == crd.WorkflowPhasePending {
				workflow.Status.Phase = crd.WorkflowPhaseRunning
			}
		}
	} else {
		// Remove from scheduler if disabled or suspended
		if err := r.unscheduleWorkflow(workflow); err != nil {
			log.Error(err, "Failed to unschedule workflow")
		}
		if workflow.Spec.Suspend {
			workflow.Status.Phase = crd.WorkflowPhaseSuspended
			r.updateWorkflowCondition(workflow, crd.WorkflowConditionSuspended, metav1.ConditionTrue, "WorkflowSuspended", "Workflow execution suspended")
		}
	}

	// Clean up old jobs based on history limits
	if err := r.cleanupOldJobs(ctx, workflow); err != nil {
		log.Error(err, "Failed to cleanup old jobs")
	}

	// Update status with next schedule time
	if workflow.Spec.Enabled && !workflow.Spec.Suspend {
		r.updateNextScheduleTime(workflow)
	}

	// Update the workflow status
	if err := r.Status().Update(ctx, workflow); err != nil {
		log.Error(err, "Failed to update workflow status")
		return ctrl.Result{}, err
	}

	// Requeue to check for schedule updates
	return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
}

func (r *WorkflowReconciler) reconcileDelete(ctx context.Context, workflow *crd.Workflow) (ctrl.Result, error) {
	log := r.Logger.WithValues("workflow", workflow.Name, "namespace", workflow.Namespace)

	// Remove from cron scheduler
	if err := r.unscheduleWorkflow(workflow); err != nil {
		log.Error(err, "Failed to unschedule workflow during deletion")
	}

	// Remove the finalizer to allow deletion
	controllerutil.RemoveFinalizer(workflow, WorkflowFinalizer)
	if err := r.Update(ctx, workflow); err != nil {
		log.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	log.Info("Workflow deletion completed")
	return ctrl.Result{}, nil
}

func (r *WorkflowReconciler) validateWorkflow(workflow *crd.Workflow) error {
	// Validate cron expression
	if err := scheduler.ParseCronExpression(workflow.Spec.Schedule); err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", workflow.Spec.Schedule, err)
	}

	// Validate steps
	if len(workflow.Spec.Steps) == 0 {
		return fmt.Errorf("workflow must have at least one step")
	}

	stepNames := make(map[string]bool)
	for i, step := range workflow.Spec.Steps {
		if step.Name == "" {
			return fmt.Errorf("step %d: name cannot be empty", i)
		}
		if stepNames[step.Name] {
			return fmt.Errorf("step %d: duplicate step name %q", i, step.Name)
		}
		stepNames[step.Name] = true

		if step.Tool == "" {
			return fmt.Errorf("step %q: tool cannot be empty", step.Name)
		}

		// Validate dependencies
		for _, dep := range step.DependsOn {
			if !stepNames[dep] {
				return fmt.Errorf("step %q: dependency %q not found", step.Name, dep)
			}
		}
	}

	return nil
}

func (r *WorkflowReconciler) scheduleWorkflow(ctx context.Context, workflow *crd.Workflow) error {
	jobID := fmt.Sprintf("%s/%s", workflow.Namespace, workflow.Name)

	// Parse timezone
	var timezone *time.Location
	if workflow.Spec.Timezone != "" {
		tz, err := time.LoadLocation(workflow.Spec.Timezone)
		if err != nil {
			return fmt.Errorf("invalid timezone %q: %w", workflow.Spec.Timezone, err)
		}
		timezone = tz
	}

	// Create job spec
	jobSpec := &scheduler.JobSpec{
		ID:         jobID,
		Name:       workflow.Name,
		Schedule:   workflow.Spec.Schedule,
		Timezone:   timezone,
		Enabled:    true,
		Handler:    r.createWorkflowHandler(workflow),
		MaxRetries: 3,
		RetryDelay: 30 * time.Second,
	}

	// Configure retry policy from workflow spec
	if workflow.Spec.RetryPolicy != nil {
		jobSpec.MaxRetries = int(workflow.Spec.RetryPolicy.MaxRetries)
		jobSpec.RetryDelay = workflow.Spec.RetryPolicy.RetryDelay.Duration
	}

	// Remove existing job if present
	if existingJob, exists := r.CronEngine.GetJob(jobID); exists {
		if err := r.CronEngine.RemoveJob(jobID); err != nil {
			return fmt.Errorf("failed to remove existing job: %w", err)
		}
		r.Logger.Info("Removed existing cron job", "workflow", workflow.Name, "jobID", existingJob.ID)
	}

	// Add job to cron engine
	if err := r.CronEngine.AddJob(jobSpec); err != nil {
		return fmt.Errorf("failed to add job to cron engine: %w", err)
	}

	r.Logger.Info("Scheduled workflow", "workflow", workflow.Name, "schedule", workflow.Spec.Schedule)
	return nil
}

func (r *WorkflowReconciler) unscheduleWorkflow(workflow *crd.Workflow) error {
	jobID := fmt.Sprintf("%s/%s", workflow.Namespace, workflow.Name)

	if _, exists := r.CronEngine.GetJob(jobID); exists {
		if err := r.CronEngine.RemoveJob(jobID); err != nil {
			return fmt.Errorf("failed to remove job from cron engine: %w", err)
		}
		r.Logger.Info("Unscheduled workflow", "workflow", workflow.Name)
	}

	return nil
}

func (r *WorkflowReconciler) createWorkflowHandler(workflow *crd.Workflow) scheduler.JobHandler {
	return func(ctx context.Context, jobID string) error {
		log := r.Logger.WithValues("workflow", workflow.Name, "jobID", jobID)
		log.Info("Executing workflow")

		// Create a Kubernetes Job to execute the workflow
		job := r.createWorkflowJob(workflow)

		if err := r.Create(ctx, job); err != nil {
			log.Error(err, "Failed to create workflow job")
			return fmt.Errorf("failed to create workflow job: %w", err)
		}

		log.Info("Created workflow job", "jobName", job.Name)

		// Update workflow status with active job
		workflow.Status.LastExecutionTime = &metav1.Time{Time: time.Now()}
		jobRef := crd.JobReference{
			Name:       job.Name,
			Namespace:  job.Namespace,
			UID:        string(job.UID),
			APIVersion: job.APIVersion,
			Kind:       job.Kind,
			StartTime:  &metav1.Time{Time: time.Now()},
		}
		workflow.Status.ActiveJobs = append(workflow.Status.ActiveJobs, jobRef)

		return nil
	}
}

func (r *WorkflowReconciler) createWorkflowJob(workflow *crd.Workflow) *batchv1.Job {
	jobName := fmt.Sprintf("workflow-%s-%d", workflow.Name, time.Now().Unix())

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: workflow.Namespace,
			Labels: map[string]string{
				WorkflowJobLabel: workflow.Name,
				"app":            "matey-workflow",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         crd.WorkflowGVK.GroupVersion().String(),
					Kind:               crd.WorkflowGVK.Kind,
					Name:               workflow.Name,
					UID:                workflow.UID,
					Controller:         &[]bool{true}[0],
					BlockOwnerDeletion: &[]bool{true}[0],
				},
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "workflow-executor",
							Image: "matey:latest", // This should be configurable
							Command: []string{
								"/usr/local/bin/matey",
								"workflow",
								"execute",
								"--workflow-name", workflow.Name,
								"--workflow-namespace", workflow.Namespace,
							},
							Env: []corev1.EnvVar{
								{
									Name:  "WORKFLOW_NAME",
									Value: workflow.Name,
								},
								{
									Name:  "WORKFLOW_NAMESPACE",
									Value: workflow.Namespace,
								},
							},
						},
					},
				},
			},
		},
	}

	// Set timeout if specified
	if workflow.Spec.Timeout != nil {
		activeDeadlineSeconds := int64(workflow.Spec.Timeout.Duration.Seconds())
		job.Spec.ActiveDeadlineSeconds = &activeDeadlineSeconds
	}

	return job
}

func (r *WorkflowReconciler) cleanupOldJobs(ctx context.Context, workflow *crd.Workflow) error {
	// List all jobs owned by this workflow
	var jobList batchv1.JobList
	listOpts := []client.ListOption{
		client.InNamespace(workflow.Namespace),
		client.MatchingLabels{WorkflowJobLabel: workflow.Name},
	}

	if err := r.List(ctx, &jobList, listOpts...); err != nil {
		return fmt.Errorf("failed to list jobs: %w", err)
	}

	// Separate successful and failed jobs
	var successfulJobs, failedJobs []batchv1.Job
	for _, job := range jobList.Items {
		if job.Status.Succeeded > 0 {
			successfulJobs = append(successfulJobs, job)
		} else if job.Status.Failed > 0 {
			failedJobs = append(failedJobs, job)
		}
	}

	// Clean up old successful jobs
	successLimit := int32(3)
	if workflow.Spec.SuccessfulJobsHistoryLimit != nil {
		successLimit = *workflow.Spec.SuccessfulJobsHistoryLimit
	}
	if len(successfulJobs) > int(successLimit) {
		jobsToDelete := len(successfulJobs) - int(successLimit)
		for i := 0; i < jobsToDelete; i++ {
			if err := r.Delete(ctx, &successfulJobs[i]); err != nil {
				r.Logger.Error(err, "Failed to delete old successful job", "job", successfulJobs[i].Name)
			}
		}
	}

	// Clean up old failed jobs
	failedLimit := int32(1)
	if workflow.Spec.FailedJobsHistoryLimit != nil {
		failedLimit = *workflow.Spec.FailedJobsHistoryLimit
	}
	if len(failedJobs) > int(failedLimit) {
		jobsToDelete := len(failedJobs) - int(failedLimit)
		for i := 0; i < jobsToDelete; i++ {
			if err := r.Delete(ctx, &failedJobs[i]); err != nil {
				r.Logger.Error(err, "Failed to delete old failed job", "job", failedJobs[i].Name)
			}
		}
	}

	return nil
}

func (r *WorkflowReconciler) updateWorkflowCondition(workflow *crd.Workflow, conditionType crd.WorkflowConditionType, status metav1.ConditionStatus, reason, message string) {
	condition := crd.WorkflowCondition{
		Type:               conditionType,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	// Find existing condition
	for i, existing := range workflow.Status.Conditions {
		if existing.Type == conditionType {
			if existing.Status != status {
				workflow.Status.Conditions[i] = condition
			}
			return
		}
	}

	// Add new condition
	workflow.Status.Conditions = append(workflow.Status.Conditions, condition)
}

func (r *WorkflowReconciler) updateNextScheduleTime(workflow *crd.Workflow) {
	var timezone *time.Location
	if workflow.Spec.Timezone != "" {
		tz, err := time.LoadLocation(workflow.Spec.Timezone)
		if err != nil {
			timezone = time.UTC
		} else {
			timezone = tz
		}
	} else {
		timezone = time.UTC
	}

	if nextTime, err := scheduler.NextScheduledTime(workflow.Spec.Schedule, timezone); err == nil {
		workflow.Status.NextScheduleTime = &metav1.Time{Time: nextTime}
	}
}

func (r *WorkflowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&crd.Workflow{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}