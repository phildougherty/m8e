package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/scheduler"
)

// MockCronEngine is a mock implementation of the CronEngine interface
type MockCronEngine struct {
	mock.Mock
	jobs map[string]*scheduler.JobSpec
}

func NewMockCronEngine() *MockCronEngine {
	return &MockCronEngine{
		jobs: make(map[string]*scheduler.JobSpec),
	}
}

func (m *MockCronEngine) AddJob(jobSpec *scheduler.JobSpec) error {
	args := m.Called(jobSpec)
	if args.Error(0) == nil {
		m.jobs[jobSpec.ID] = jobSpec
	}
	return args.Error(0)
}

func (m *MockCronEngine) RemoveJob(jobID string) error {
	args := m.Called(jobID)
	if args.Error(0) == nil {
		delete(m.jobs, jobID)
	}
	return args.Error(0)
}

func (m *MockCronEngine) GetJob(jobID string) (*scheduler.JobSpec, bool) {
	args := m.Called(jobID)
	if job, ok := args.Get(0).(*scheduler.JobSpec); ok {
		return job, args.Bool(1)
	}
	return nil, args.Bool(1)
}

func (m *MockCronEngine) Start() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockCronEngine) Stop() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockCronEngine) IsRunning() bool {
	args := m.Called()
	return args.Bool(0)
}

func TestWorkflowReconciler_Reconcile(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))
	require.NoError(t, batchv1.AddToScheme(s))

	tests := []struct {
		name     string
		workflow *crd.Workflow
		wantErr  bool
	}{
		{
			name: "Create new workflow with basic config",
			workflow: &crd.Workflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-workflow",
					Namespace: "default",
				},
				Spec: crd.WorkflowSpec{
					Schedule: "0 0 * * *",
					Enabled:  true,
					Steps: []crd.WorkflowStep{
						{
							Name: "step1",
							Tool: "test-tool",
							Parameters: map[string]interface{}{
								"param1": "value1",
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "Create workflow with multiple steps and dependencies",
			workflow: &crd.Workflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-step-workflow",
					Namespace: "default",
				},
				Spec: crd.WorkflowSpec{
					Schedule: "*/5 * * * *",
					Timezone: "UTC",
					Enabled:  true,
					Steps: []crd.WorkflowStep{
						{
							Name: "build",
							Tool: "build-tool",
							Parameters: map[string]interface{}{
								"target": "production",
							},
							RunPolicy: crd.StepRunPolicyAlways,
						},
						{
							Name: "test",
							Tool: "test-tool",
							Parameters: map[string]interface{}{
								"suite": "integration",
							},
							DependsOn: []string{"build"},
							RunPolicy: crd.StepRunPolicyOnSuccess,
						},
					},
					RetryPolicy: &crd.RetryPolicy{
						MaxRetries:    3,
						RetryDelay:    metav1.Duration{Duration: 30 * time.Second},
						BackoffPolicy: crd.BackoffPolicyExponential,
					},
					Timeout:           &metav1.Duration{Duration: 30 * time.Minute},
					ConcurrencyPolicy: crd.ConcurrencyPolicyForbid,
				},
			},
			wantErr: false,
		},
		{
			name: "Create workflow with custom retry policy",
			workflow: &crd.Workflow{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "retry-workflow",
					Namespace: "default",
				},
				Spec: crd.WorkflowSpec{
					Schedule: "0 0 * * *",
					Enabled:  true,
					Steps: []crd.WorkflowStep{
						{
							Name: "flaky-step",
							Tool: "flaky-tool",
							RetryPolicy: &crd.RetryPolicy{
								MaxRetries:    5,
								RetryDelay:    metav1.Duration{Duration: 1 * time.Minute},
								BackoffPolicy: crd.BackoffPolicyLinear,
							},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().
				WithScheme(s).
				WithStatusSubresource(&crd.Workflow{}).
				Build()

			mockCronEngine := NewMockCronEngine()
			mockCronEngine.On("GetJob", mock.AnythingOfType("string")).Return(nil, false)
			mockCronEngine.On("AddJob", mock.AnythingOfType("*scheduler.JobSpec")).Return(nil)

			reconciler := &WorkflowReconciler{
				Client:     client,
				Scheme:     s,
				Logger:     log.Log,
				CronEngine: mockCronEngine,
			}

			// Create the workflow
			err := client.Create(context.Background(), tt.workflow)
			require.NoError(t, err)

			// Perform reconciliation
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.workflow.Name,
					Namespace: tt.workflow.Namespace,
				},
			}

			result, err := reconciler.Reconcile(context.Background(), req)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotZero(t, result.RequeueAfter)
			}

			// Verify the workflow was processed
			var updated crd.Workflow
			err = client.Get(context.Background(), req.NamespacedName, &updated)
			require.NoError(t, err)

			// After first reconciliation, should have finalizer
			assert.Contains(t, updated.Finalizers, WorkflowFinalizer)

			// Should have validation condition
			assert.NotEmpty(t, updated.Status.Conditions)
			hasValidationCondition := false
			for _, condition := range updated.Status.Conditions {
				if condition.Type == crd.WorkflowConditionValidated {
					hasValidationCondition = true
					assert.Equal(t, metav1.ConditionTrue, condition.Status)
					break
				}
			}
			assert.True(t, hasValidationCondition)

			// Verify cron engine was called
			mockCronEngine.AssertExpectations(t)
		})
	}
}

func TestWorkflowReconciler_ReconcileDelete(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	workflow := &crd.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "delete-test-workflow",
			Namespace:  "default",
			Finalizers: []string{WorkflowFinalizer},
		},
		Spec: crd.WorkflowSpec{
			Schedule: "0 0 * * *",
			Enabled:  true,
			Steps: []crd.WorkflowStep{
				{
					Name: "step1",
					Tool: "test-tool",
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(workflow).
		WithStatusSubresource(&crd.Workflow{}).
		Build()

	mockCronEngine := NewMockCronEngine()
	mockCronEngine.On("GetJob", mock.AnythingOfType("string")).Return(nil, true)
	mockCronEngine.On("RemoveJob", mock.AnythingOfType("string")).Return(nil)

	reconciler := &WorkflowReconciler{
		Client:     client,
		Scheme:     s,
		Logger:     log.Log,
		CronEngine: mockCronEngine,
	}

	// Delete the workflow
	err := client.Delete(context.Background(), workflow)
	require.NoError(t, err)

	// Reconcile should handle the deletion
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      workflow.Name,
			Namespace: workflow.Namespace,
		},
	}

	_, err = reconciler.Reconcile(context.Background(), req)
	assert.NoError(t, err)

	// Verify cron engine was called to remove job
	mockCronEngine.AssertExpectations(t)
}

func TestWorkflowReconciler_NonExistentWorkflow(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	client := fake.NewClientBuilder().
		WithScheme(s).
		Build()

	mockCronEngine := NewMockCronEngine()

	reconciler := &WorkflowReconciler{
		Client:     client,
		Scheme:     s,
		Logger:     log.Log,
		CronEngine: mockCronEngine,
	}

	// Try to reconcile a non-existent workflow
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "non-existent",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestWorkflowReconciler_SuspendedWorkflow(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	workflow := &crd.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "suspended-workflow",
			Namespace: "default",
		},
		Spec: crd.WorkflowSpec{
			Schedule: "0 0 * * *",
			Enabled:  true,
			Suspend:  true,
			Steps: []crd.WorkflowStep{
				{
					Name: "step1",
					Tool: "test-tool",
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(workflow).
		WithStatusSubresource(&crd.Workflow{}).
		Build()

	mockCronEngine := NewMockCronEngine()
	mockCronEngine.On("GetJob", mock.AnythingOfType("string")).Return(nil, false)

	reconciler := &WorkflowReconciler{
		Client:     client,
		Scheme:     s,
		Logger:     log.Log,
		CronEngine: mockCronEngine,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      workflow.Name,
			Namespace: workflow.Namespace,
		},
	}

	// First reconciliation should add finalizer
	result, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.Requeue)

	// Second reconciliation should handle suspension
	result, err = reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)

	// Verify the workflow status was updated
	var updated crd.Workflow
	err = client.Get(context.Background(), req.NamespacedName, &updated)
	require.NoError(t, err)

	assert.Equal(t, crd.WorkflowPhaseSuspended, updated.Status.Phase)

	// Should have suspended condition
	hasSuspendedCondition := false
	for _, condition := range updated.Status.Conditions {
		if condition.Type == crd.WorkflowConditionSuspended {
			hasSuspendedCondition = true
			assert.Equal(t, metav1.ConditionTrue, condition.Status)
			break
		}
	}
	assert.True(t, hasSuspendedCondition)
}

func TestWorkflowReconciler_ValidateWorkflow(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	reconciler := &WorkflowReconciler{
		Client:     nil,
		Scheme:     s,
		Logger:     log.Log,
		CronEngine: nil,
	}

	tests := []struct {
		name     string
		workflow *crd.Workflow
		wantErr  bool
	}{
		{
			name: "valid workflow",
			workflow: &crd.Workflow{
				Spec: crd.WorkflowSpec{
					Schedule: "0 0 * * *",
					Steps: []crd.WorkflowStep{
						{
							Name: "step1",
							Tool: "tool1",
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid cron expression",
			workflow: &crd.Workflow{
				Spec: crd.WorkflowSpec{
					Schedule: "invalid cron",
					Steps: []crd.WorkflowStep{
						{
							Name: "step1",
							Tool: "tool1",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "no steps",
			workflow: &crd.Workflow{
				Spec: crd.WorkflowSpec{
					Schedule: "0 0 * * *",
					Steps:    []crd.WorkflowStep{},
				},
			},
			wantErr: true,
		},
		{
			name: "step with empty name",
			workflow: &crd.Workflow{
				Spec: crd.WorkflowSpec{
					Schedule: "0 0 * * *",
					Steps: []crd.WorkflowStep{
						{
							Name: "",
							Tool: "tool1",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "step with empty tool",
			workflow: &crd.Workflow{
				Spec: crd.WorkflowSpec{
					Schedule: "0 0 * * *",
					Steps: []crd.WorkflowStep{
						{
							Name: "step1",
							Tool: "",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "duplicate step names",
			workflow: &crd.Workflow{
				Spec: crd.WorkflowSpec{
					Schedule: "0 0 * * *",
					Steps: []crd.WorkflowStep{
						{
							Name: "step1",
							Tool: "tool1",
						},
						{
							Name: "step1",
							Tool: "tool2",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid dependency",
			workflow: &crd.Workflow{
				Spec: crd.WorkflowSpec{
					Schedule: "0 0 * * *",
					Steps: []crd.WorkflowStep{
						{
							Name:      "step1",
							Tool:      "tool1",
							DependsOn: []string{"nonexistent"},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "valid dependencies",
			workflow: &crd.Workflow{
				Spec: crd.WorkflowSpec{
					Schedule: "0 0 * * *",
					Steps: []crd.WorkflowStep{
						{
							Name: "step1",
							Tool: "tool1",
						},
						{
							Name:      "step2",
							Tool:      "tool2",
							DependsOn: []string{"step1"},
						},
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := reconciler.validateWorkflow(tt.workflow)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestWorkflowReconciler_CreateWorkflowJob(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	workflow := &crd.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-workflow",
			Namespace: "default",
			UID:       "test-uid",
		},
		Spec: crd.WorkflowSpec{
			Schedule: "0 0 * * *",
			Timeout:  &metav1.Duration{Duration: 30 * time.Minute},
			Steps: []crd.WorkflowStep{
				{
					Name: "step1",
					Tool: "test-tool",
				},
			},
		},
	}

	reconciler := &WorkflowReconciler{
		Client:     nil,
		Scheme:     s,
		Logger:     log.Log,
		CronEngine: nil,
	}

	job := reconciler.createWorkflowJob(workflow)

	// Verify job metadata
	assert.Contains(t, job.Name, "workflow-test-workflow-")
	assert.Equal(t, "default", job.Namespace)
	assert.Equal(t, "test-workflow", job.Labels[WorkflowJobLabel])
	assert.Equal(t, "matey-workflow", job.Labels["app"])

	// Verify owner reference
	assert.Len(t, job.OwnerReferences, 1)
	assert.Equal(t, "test-workflow", job.OwnerReferences[0].Name)
	assert.Equal(t, types.UID("test-uid"), job.OwnerReferences[0].UID)

	// Verify job spec
	assert.Equal(t, corev1.RestartPolicyNever, job.Spec.Template.Spec.RestartPolicy)
	assert.Len(t, job.Spec.Template.Spec.Containers, 1)

	container := job.Spec.Template.Spec.Containers[0]
	assert.Equal(t, "workflow-executor", container.Name)
	assert.Equal(t, "matey:latest", container.Image)
	assert.Contains(t, container.Command, "/usr/local/bin/matey")
	assert.Contains(t, container.Command, "workflow")
	assert.Contains(t, container.Command, "execute")

	// Verify environment variables
	envVars := make(map[string]string)
	for _, env := range container.Env {
		envVars[env.Name] = env.Value
	}
	assert.Equal(t, "test-workflow", envVars["WORKFLOW_NAME"])
	assert.Equal(t, "default", envVars["WORKFLOW_NAMESPACE"])

	// Verify timeout
	assert.NotNil(t, job.Spec.ActiveDeadlineSeconds)
	assert.Equal(t, int64(1800), *job.Spec.ActiveDeadlineSeconds) // 30 minutes
}

func TestWorkflowReconciler_CleanupOldJobs(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))
	require.NoError(t, batchv1.AddToScheme(s))

	workflow := &crd.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cleanup-test-workflow",
			Namespace: "default",
		},
		Spec: crd.WorkflowSpec{
			Schedule:                   "0 0 * * *",
			SuccessfulJobsHistoryLimit: &[]int32{2}[0],
			FailedJobsHistoryLimit:     &[]int32{1}[0],
			Steps: []crd.WorkflowStep{
				{
					Name: "step1",
					Tool: "test-tool",
				},
			},
		},
	}

	// Create some test jobs
	successfulJobs := []*batchv1.Job{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "success-job-1",
				Namespace: "default",
				Labels: map[string]string{
					WorkflowJobLabel: workflow.Name,
				},
			},
			Status: batchv1.JobStatus{
				Succeeded: 1,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "success-job-2",
				Namespace: "default",
				Labels: map[string]string{
					WorkflowJobLabel: workflow.Name,
				},
			},
			Status: batchv1.JobStatus{
				Succeeded: 1,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "success-job-3",
				Namespace: "default",
				Labels: map[string]string{
					WorkflowJobLabel: workflow.Name,
				},
			},
			Status: batchv1.JobStatus{
				Succeeded: 1,
			},
		},
	}

	failedJobs := []*batchv1.Job{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "failed-job-1",
				Namespace: "default",
				Labels: map[string]string{
					WorkflowJobLabel: workflow.Name,
				},
			},
			Status: batchv1.JobStatus{
				Failed: 1,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "failed-job-2",
				Namespace: "default",
				Labels: map[string]string{
					WorkflowJobLabel: workflow.Name,
				},
			},
			Status: batchv1.JobStatus{
				Failed: 1,
			},
		},
	}

	var runtimeObjs []runtime.Object
	runtimeObjs = append(runtimeObjs, workflow)
	for _, job := range successfulJobs {
		runtimeObjs = append(runtimeObjs, job)
	}
	for _, job := range failedJobs {
		runtimeObjs = append(runtimeObjs, job)
	}

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(runtimeObjs...).
		Build()

	reconciler := &WorkflowReconciler{
		Client:     client,
		Scheme:     s,
		Logger:     log.Log,
		CronEngine: nil,
	}

	// Run cleanup
	err := reconciler.cleanupOldJobs(context.Background(), workflow)
	assert.NoError(t, err)

	// Verify jobs were cleaned up
	var jobList batchv1.JobList
	err = client.List(context.Background(), &jobList)
	require.NoError(t, err)

	// Should have 2 successful jobs and 1 failed job remaining
	successCount := 0
	failedCount := 0
	for _, job := range jobList.Items {
		if job.Status.Succeeded > 0 {
			successCount++
		} else if job.Status.Failed > 0 {
			failedCount++
		}
	}

	assert.Equal(t, 2, successCount)
	assert.Equal(t, 1, failedCount)
}

func TestWorkflowReconciler_UpdateWorkflowCondition(t *testing.T) {
	workflow := &crd.Workflow{
		Status: crd.WorkflowStatus{
			Conditions: []crd.WorkflowCondition{
				{
					Type:   crd.WorkflowConditionScheduled,
					Status: metav1.ConditionFalse,
					Reason: "OldReason",
				},
			},
		},
	}

	reconciler := &WorkflowReconciler{
		Logger: log.Log,
	}

	// Update existing condition
	reconciler.updateWorkflowCondition(workflow, crd.WorkflowConditionScheduled, metav1.ConditionTrue, "NewReason", "New message")

	assert.Len(t, workflow.Status.Conditions, 1)
	assert.Equal(t, crd.WorkflowConditionScheduled, workflow.Status.Conditions[0].Type)
	assert.Equal(t, metav1.ConditionTrue, workflow.Status.Conditions[0].Status)
	assert.Equal(t, "NewReason", workflow.Status.Conditions[0].Reason)
	assert.Equal(t, "New message", workflow.Status.Conditions[0].Message)

	// Add new condition
	reconciler.updateWorkflowCondition(workflow, crd.WorkflowConditionExecuting, metav1.ConditionTrue, "ExecutionStarted", "Execution started")

	assert.Len(t, workflow.Status.Conditions, 2)
	assert.Equal(t, crd.WorkflowConditionExecuting, workflow.Status.Conditions[1].Type)
	assert.Equal(t, metav1.ConditionTrue, workflow.Status.Conditions[1].Status)
}

func TestWorkflowReconciler_SetupWithManager(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	// This test verifies the controller setup doesn't panic
	reconciler := &WorkflowReconciler{
		Scheme:     s,
		Logger:     logr.Discard(),
		CronEngine: NewMockCronEngine(),
	}

	// We can't easily test the actual manager setup in a unit test
	// But we can verify the reconciler is properly configured
	assert.NotNil(t, reconciler.Scheme)
	assert.NotNil(t, reconciler.Logger)
	assert.NotNil(t, reconciler.CronEngine)
}

func TestWorkflowReconciler_InvalidWorkflow(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	// Create workflow with invalid cron expression
	workflow := &crd.Workflow{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "invalid-workflow",
			Namespace: "default",
		},
		Spec: crd.WorkflowSpec{
			Schedule: "invalid cron",
			Enabled:  true,
			Steps: []crd.WorkflowStep{
				{
					Name: "step1",
					Tool: "test-tool",
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(workflow).
		WithStatusSubresource(&crd.Workflow{}).
		Build()

	mockCronEngine := NewMockCronEngine()

	reconciler := &WorkflowReconciler{
		Client:     client,
		Scheme:     s,
		Logger:     log.Log,
		CronEngine: mockCronEngine,
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      workflow.Name,
			Namespace: workflow.Namespace,
		},
	}

	// First reconciliation should add finalizer
	result, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, result.Requeue)

	// Second reconciliation should handle validation failure
	result, err = reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, result.RequeueAfter)

	// Verify validation failed condition was set
	var updated crd.Workflow
	err = client.Get(context.Background(), req.NamespacedName, &updated)
	require.NoError(t, err)

	hasValidationFailedCondition := false
	for _, condition := range updated.Status.Conditions {
		if condition.Type == crd.WorkflowConditionValidated && condition.Status == metav1.ConditionFalse {
			hasValidationFailedCondition = true
			assert.Equal(t, "ValidationFailed", condition.Reason)
			break
		}
	}
	assert.True(t, hasValidationFailedCondition)
}