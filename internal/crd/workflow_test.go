package crd

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestWorkflow_GroupVersionKind(t *testing.T) {
	workflow := &Workflow{}
	gvk := workflow.GroupVersionKind()
	
	assert.Equal(t, WorkflowGroup, gvk.Group)
	assert.Equal(t, WorkflowVersion, gvk.Version)
	assert.Equal(t, "Workflow", gvk.Kind)
}

func TestWorkflowList_GroupVersionKind(t *testing.T) {
	workflowList := &WorkflowList{}
	gvk := workflowList.GroupVersionKind()
	
	assert.Equal(t, WorkflowGroup, gvk.Group)
	assert.Equal(t, WorkflowVersion, gvk.Version)
	assert.Equal(t, "WorkflowList", gvk.Kind)
}

func TestWorkflow_JSON_Serialization(t *testing.T) {
	now := metav1.Now()
	workflow := &Workflow{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Workflow",
			APIVersion: WorkflowGroup + "/" + WorkflowVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-workflow",
			Namespace: "default",
		},
		Spec: WorkflowSpec{
			Schedule: "0 0 * * *",
			Timezone: "UTC",
			Enabled:  true,
			Suspend:  false,
			Steps: []WorkflowStep{
				{
					Name:       "step1",
					Tool:       "code-analyzer",
					Parameters: map[string]interface{}{
						"language": "go",
						"path":     "/src",
					},
					Condition: "{{ .PreviousStep.Success }}",
					Timeout:   &metav1.Duration{Duration: 5 * time.Minute},
					RetryPolicy: &RetryPolicy{
						MaxRetries:    3,
						RetryDelay:    metav1.Duration{Duration: 30 * time.Second},
						BackoffPolicy: BackoffPolicyExponential,
					},
					DependsOn:       []string{},
					ContinueOnError: false,
					RunPolicy:       StepRunPolicyAlways,
				},
				{
					Name:       "step2",
					Tool:       "test-runner",
					Parameters: map[string]interface{}{
						"testPath": "/tests",
					},
					DependsOn: []string{"step1"},
					RunPolicy: StepRunPolicyOnSuccess,
				},
			},
			RetryPolicy: &RetryPolicy{
				MaxRetries:    2,
				RetryDelay:    metav1.Duration{Duration: 1 * time.Minute},
				BackoffPolicy: BackoffPolicyLinear,
			},
			Timeout:                    &metav1.Duration{Duration: 30 * time.Minute},
			ConcurrencyPolicy:          ConcurrencyPolicyForbid,
			SuccessfulJobsHistoryLimit: &[]int32{10}[0],
			FailedJobsHistoryLimit:     &[]int32{5}[0],
		},
		Status: WorkflowStatus{
			Phase:            WorkflowPhaseRunning,
			LastScheduleTime: &now,
			NextScheduleTime: &metav1.Time{Time: now.Add(24 * time.Hour)},
			Conditions: []WorkflowCondition{
				{
					Type:               WorkflowConditionScheduled,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: now,
					Reason:             "ScheduleCreated",
					Message:            "Workflow scheduled successfully",
				},
			},
			ActiveJobs: []JobReference{
				{
					Name:       "test-workflow-job-1",
					Namespace:  "default",
					UID:        "12345",
					APIVersion: "batch/v1",
					Kind:       "Job",
					StartTime:  &now,
				},
			},
			ExecutionHistory: []WorkflowExecution{
				{
					ID:        "exec-1",
					StartTime: now,
					Phase:     WorkflowPhaseRunning,
					Message:   "Execution started",
					StepResults: map[string]StepResult{
						"step1": {
							Phase:    StepPhaseSucceeded,
							Output:   "Analysis complete",
							Duration: 2 * time.Minute,
							Attempts: 1,
						},
					},
				},
			},
			StepStatuses: map[string]StepStatus{
				"step1": {
					Phase:      StepPhaseSucceeded,
					StartTime:  &now,
					EndTime:    &metav1.Time{Time: now.Add(2 * time.Minute)},
					Message:    "Step completed successfully",
					RetryCount: 0,
				},
			},
			ObservedGeneration: 1,
			Message:            "Workflow is running",
		},
	}

	// Test serialization
	data, err := json.Marshal(workflow)
	require.NoError(t, err)
	assert.Contains(t, string(data), "test-workflow")
	assert.Contains(t, string(data), "0 0 * * *")
	assert.Contains(t, string(data), "code-analyzer")
	assert.Contains(t, string(data), "test-runner")

	// Test deserialization
	var restored Workflow
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)
	
	assert.Equal(t, workflow.Name, restored.Name)
	assert.Equal(t, workflow.Spec.Schedule, restored.Spec.Schedule)
	assert.Equal(t, workflow.Spec.Enabled, restored.Spec.Enabled)
	assert.Equal(t, len(workflow.Spec.Steps), len(restored.Spec.Steps))
	assert.Equal(t, workflow.Status.Phase, restored.Status.Phase)
	assert.Equal(t, len(workflow.Status.Conditions), len(restored.Status.Conditions))
}

func TestWorkflowSpec_Structure(t *testing.T) {
	tests := []struct {
		name string
		spec WorkflowSpec
	}{
		{
			name: "minimal workflow spec",
			spec: WorkflowSpec{
				Schedule: "0 0 * * *",
				Enabled:  true,
				Steps: []WorkflowStep{
					{
						Name: "simple-step",
						Tool: "simple-tool",
					},
				},
			},
		},
		{
			name: "complex workflow spec",
			spec: WorkflowSpec{
				Schedule:  "*/5 * * * *",
				Timezone:  "America/New_York",
				Enabled:   true,
				Suspend:   false,
				Steps: []WorkflowStep{
					{
						Name: "build-step",
						Tool: "build-tool",
						Parameters: map[string]interface{}{
							"buildTarget": "production",
							"optimize":    true,
						},
						Condition: "{{ .Environment == 'production' }}",
						Timeout:   &metav1.Duration{Duration: 10 * time.Minute},
						RetryPolicy: &RetryPolicy{
							MaxRetries:    5,
							RetryDelay:    metav1.Duration{Duration: 1 * time.Minute},
							BackoffPolicy: BackoffPolicyExponential,
							MaxRetryDelay: &metav1.Duration{Duration: 10 * time.Minute},
						},
						DependsOn:       []string{},
						ContinueOnError: false,
						RunPolicy:       StepRunPolicyAlways,
					},
					{
						Name: "test-step",
						Tool: "test-tool",
						Parameters: map[string]interface{}{
							"testSuite": "integration",
						},
						DependsOn:       []string{"build-step"},
						ContinueOnError: true,
						RunPolicy:       StepRunPolicyOnSuccess,
					},
				},
				RetryPolicy: &RetryPolicy{
					MaxRetries:    3,
					RetryDelay:    metav1.Duration{Duration: 2 * time.Minute},
					BackoffPolicy: BackoffPolicyLinear,
				},
				Timeout:                    &metav1.Duration{Duration: 1 * time.Hour},
				ConcurrencyPolicy:          ConcurrencyPolicyReplace,
				SuccessfulJobsHistoryLimit: &[]int32{20}[0],
				FailedJobsHistoryLimit:     &[]int32{10}[0],
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, tt.spec.Schedule)
			assert.True(t, tt.spec.Enabled)
			assert.NotEmpty(t, tt.spec.Steps)
			
			for _, step := range tt.spec.Steps {
				assert.NotEmpty(t, step.Name)
				assert.NotEmpty(t, step.Tool)
			}
		})
	}
}

func TestWorkflowStatus_Structure(t *testing.T) {
	now := metav1.Now()
	status := &WorkflowStatus{
		Phase:            WorkflowPhaseRunning,
		LastScheduleTime: &now,
		NextScheduleTime: &metav1.Time{Time: now.Add(time.Hour)},
		LastExecutionTime: &now,
		LastSuccessTime:   &metav1.Time{Time: now.Add(-time.Hour)},
		Conditions: []WorkflowCondition{
			{
				Type:               WorkflowConditionScheduled,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: now,
				Reason:             "CronScheduleCreated",
				Message:            "Cron schedule created successfully",
			},
			{
				Type:               WorkflowConditionExecuting,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: now,
				Reason:             "JobStarted",
				Message:            "Workflow execution job started",
			},
		},
		ActiveJobs: []JobReference{
			{
				Name:       "workflow-job-1",
				Namespace:  "default",
				UID:        "abc123",
				APIVersion: "batch/v1",
				Kind:       "Job",
				StartTime:  &now,
			},
		},
		ExecutionHistory: []WorkflowExecution{
			{
				ID:        "exec-1",
				StartTime: now,
				EndTime:   &metav1.Time{Time: now.Add(5 * time.Minute)},
				Phase:     WorkflowPhaseSucceeded,
				Message:   "Execution completed successfully",
				Duration:  &metav1.Duration{Duration: 5 * time.Minute},
				StepResults: map[string]StepResult{
					"step1": {
						Phase:    StepPhaseSucceeded,
						Output:   "Success",
						Duration: 2 * time.Minute,
						Attempts: 1,
					},
				},
			},
		},
		StepStatuses: map[string]StepStatus{
			"step1": {
				Phase:      StepPhaseSucceeded,
				StartTime:  &now,
				EndTime:    &metav1.Time{Time: now.Add(2 * time.Minute)},
				Message:    "Step completed",
				RetryCount: 0,
			},
		},
		ObservedGeneration: 1,
		Message:            "Workflow is running normally",
	}

	// Test structure
	assert.Equal(t, WorkflowPhaseRunning, status.Phase)
	assert.NotNil(t, status.LastScheduleTime)
	assert.NotNil(t, status.NextScheduleTime)
	assert.Len(t, status.Conditions, 2)
	assert.Equal(t, WorkflowConditionScheduled, status.Conditions[0].Type)
	assert.Equal(t, WorkflowConditionExecuting, status.Conditions[1].Type)
	assert.Len(t, status.ActiveJobs, 1)
	assert.Equal(t, "workflow-job-1", status.ActiveJobs[0].Name)
	assert.Len(t, status.ExecutionHistory, 1)
	assert.Equal(t, "exec-1", status.ExecutionHistory[0].ID)
	assert.Len(t, status.StepStatuses, 1)
	assert.Equal(t, StepPhaseSucceeded, status.StepStatuses["step1"].Phase)
	assert.Equal(t, int64(1), status.ObservedGeneration)
}

func TestWorkflowCondition_Structure(t *testing.T) {
	tests := []struct {
		name      string
		condition WorkflowCondition
	}{
		{
			name: "scheduled condition",
			condition: WorkflowCondition{
				Type:               WorkflowConditionScheduled,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "ScheduleCreated",
				Message:            "Cron schedule created",
			},
		},
		{
			name: "failed condition",
			condition: WorkflowCondition{
				Type:               WorkflowConditionFailed,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "ExecutionFailed",
				Message:            "Workflow execution failed",
			},
		},
		{
			name: "progressing condition",
			condition: WorkflowCondition{
				Type:               WorkflowConditionProgressing,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
				Reason:             "StepsExecuting",
				Message:            "Workflow steps are executing",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, tt.condition.Type)
			assert.NotEmpty(t, tt.condition.Status)
			assert.NotEmpty(t, tt.condition.Reason)
			assert.NotEmpty(t, tt.condition.Message)
		})
	}
}

func TestWorkflowStep_Structure(t *testing.T) {
	tests := []struct {
		name string
		step WorkflowStep
	}{
		{
			name: "minimal step",
			step: WorkflowStep{
				Name: "simple-step",
				Tool: "simple-tool",
			},
		},
		{
			name: "full step configuration",
			step: WorkflowStep{
				Name: "complex-step",
				Tool: "complex-tool",
				Parameters: map[string]interface{}{
					"param1": "value1",
					"param2": 42,
					"param3": true,
				},
				Condition: "{{ .PreviousStep.Success }}",
				Timeout:   &metav1.Duration{Duration: 10 * time.Minute},
				RetryPolicy: &RetryPolicy{
					MaxRetries:    3,
					RetryDelay:    metav1.Duration{Duration: 30 * time.Second},
					BackoffPolicy: BackoffPolicyExponential,
					MaxRetryDelay: &metav1.Duration{Duration: 5 * time.Minute},
				},
				DependsOn:       []string{"step1", "step2"},
				ContinueOnError: true,
				RunPolicy:       StepRunPolicyOnCondition,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, tt.step.Name)
			assert.NotEmpty(t, tt.step.Tool)
			
			if tt.step.Parameters != nil {
				assert.NotEmpty(t, tt.step.Parameters)
			}
			
			if tt.step.RetryPolicy != nil {
				assert.NotZero(t, tt.step.RetryPolicy.MaxRetries)
				assert.NotZero(t, tt.step.RetryPolicy.RetryDelay.Duration)
			}
		})
	}
}

func TestRetryPolicy_Structure(t *testing.T) {
	tests := []struct {
		name   string
		policy RetryPolicy
	}{
		{
			name: "linear backoff policy",
			policy: RetryPolicy{
				MaxRetries:    3,
				RetryDelay:    metav1.Duration{Duration: 1 * time.Minute},
				BackoffPolicy: BackoffPolicyLinear,
			},
		},
		{
			name: "exponential backoff policy",
			policy: RetryPolicy{
				MaxRetries:    5,
				RetryDelay:    metav1.Duration{Duration: 30 * time.Second},
				BackoffPolicy: BackoffPolicyExponential,
				MaxRetryDelay: &metav1.Duration{Duration: 10 * time.Minute},
			},
		},
		{
			name: "fixed backoff policy",
			policy: RetryPolicy{
				MaxRetries:    2,
				RetryDelay:    metav1.Duration{Duration: 2 * time.Minute},
				BackoffPolicy: BackoffPolicyFixed,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotZero(t, tt.policy.MaxRetries)
			assert.NotZero(t, tt.policy.RetryDelay.Duration)
			assert.NotEmpty(t, tt.policy.BackoffPolicy)
		})
	}
}

func TestWorkflow_DeepCopy(t *testing.T) {
	original := &Workflow{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Workflow",
			APIVersion: WorkflowGroup + "/" + WorkflowVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-workflow",
			Namespace: "default",
		},
		Spec: WorkflowSpec{
			Schedule: "0 0 * * *",
			Enabled:  true,
			Steps: []WorkflowStep{
				{
					Name: "step1",
					Tool: "tool1",
					Parameters: map[string]interface{}{
						"key": "value",
					},
				},
			},
		},
		Status: WorkflowStatus{
			Phase: WorkflowPhaseRunning,
			Conditions: []WorkflowCondition{
				{
					Type:   WorkflowConditionScheduled,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}

	// Test deep copy
	copied := original.DeepCopy()
	
	// Verify they are different objects
	assert.NotSame(t, original, copied)
	assert.NotSame(t, &original.Spec, &copied.Spec)
	assert.NotSame(t, &original.Status, &copied.Status)
	
	// Verify content is the same
	assert.Equal(t, original.Name, copied.Name)
	assert.Equal(t, original.Spec.Schedule, copied.Spec.Schedule)
	assert.Equal(t, original.Spec.Enabled, copied.Spec.Enabled)
	assert.Equal(t, len(original.Spec.Steps), len(copied.Spec.Steps))
	assert.Equal(t, original.Status.Phase, copied.Status.Phase)
	
	// Verify modifying copy doesn't affect original
	copied.Spec.Enabled = false
	assert.NotEqual(t, original.Spec.Enabled, copied.Spec.Enabled)
	
	// Verify deep copy of slices
	copied.Spec.Steps[0].Name = "modified"
	assert.NotEqual(t, original.Spec.Steps[0].Name, copied.Spec.Steps[0].Name)
}

func TestWorkflowList_DeepCopy(t *testing.T) {
	original := &WorkflowList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "WorkflowList",
			APIVersion: WorkflowGroup + "/" + WorkflowVersion,
		},
		ListMeta: metav1.ListMeta{
			ResourceVersion: "123",
		},
		Items: []Workflow{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "workflow1",
					Namespace: "default",
				},
				Spec: WorkflowSpec{
					Schedule: "0 0 * * *",
					Enabled:  true,
					Steps: []WorkflowStep{
						{
							Name: "step1",
							Tool: "tool1",
						},
					},
				},
			},
		},
	}

	// Test deep copy
	copied := original.DeepCopy()
	
	// Verify they are different objects
	assert.NotSame(t, original, copied)
	assert.NotSame(t, &original.Items[0], &copied.Items[0])
	
	// Verify content is the same
	assert.Equal(t, original.ResourceVersion, copied.ResourceVersion)
	assert.Equal(t, len(original.Items), len(copied.Items))
	assert.Equal(t, original.Items[0].Name, copied.Items[0].Name)
	
	// Verify modifying copy doesn't affect original
	copied.Items[0].Name = "modified"
	assert.NotEqual(t, original.Items[0].Name, copied.Items[0].Name)
}

func TestWorkflow_RuntimeObject(t *testing.T) {
	workflow := &Workflow{}
	
	// Test that it implements runtime.Object
	var obj runtime.Object = workflow
	assert.NotNil(t, obj)
	
	// Test GetObjectKind
	gvk := obj.GetObjectKind().GroupVersionKind()
	assert.Equal(t, "", gvk.Group) // Empty until set
	assert.Equal(t, "", gvk.Version)
	assert.Equal(t, "", gvk.Kind)
	
	// Set and test
	obj.GetObjectKind().SetGroupVersionKind(schema.GroupVersionKind{
		Group:   WorkflowGroup,
		Version: WorkflowVersion,
		Kind:    "Workflow",
	})
	
	gvk = obj.GetObjectKind().GroupVersionKind()
	assert.Equal(t, WorkflowGroup, gvk.Group)
	assert.Equal(t, WorkflowVersion, gvk.Version)
	assert.Equal(t, "Workflow", gvk.Kind)
}

func TestWorkflow_Constants(t *testing.T) {
	// Test workflow constants
	assert.Equal(t, "mcp.matey.ai", WorkflowGroup)
	assert.Equal(t, "v1", WorkflowVersion)
	assert.Equal(t, "Workflow", WorkflowKind)
	assert.Equal(t, "workflows", WorkflowPlural)
	
	// Test WorkflowGVK and WorkflowGVR
	assert.Equal(t, WorkflowGroup, WorkflowGVK.Group)
	assert.Equal(t, WorkflowVersion, WorkflowGVK.Version)
	assert.Equal(t, WorkflowKind, WorkflowGVK.Kind)
	
	assert.Equal(t, WorkflowGroup, WorkflowGVR.Group)
	assert.Equal(t, WorkflowVersion, WorkflowGVR.Version)
	assert.Equal(t, WorkflowPlural, WorkflowGVR.Resource)
}

func TestWorkflow_Enums(t *testing.T) {
	// Test BackoffPolicy constants
	assert.Equal(t, BackoffPolicy("Linear"), BackoffPolicyLinear)
	assert.Equal(t, BackoffPolicy("Exponential"), BackoffPolicyExponential)
	assert.Equal(t, BackoffPolicy("Fixed"), BackoffPolicyFixed)
	
	// Test ConcurrencyPolicy constants
	assert.Equal(t, ConcurrencyPolicy("Allow"), ConcurrencyPolicyAllow)
	assert.Equal(t, ConcurrencyPolicy("Forbid"), ConcurrencyPolicyForbid)
	assert.Equal(t, ConcurrencyPolicy("Replace"), ConcurrencyPolicyReplace)
	
	// Test StepRunPolicy constants
	assert.Equal(t, StepRunPolicy("Always"), StepRunPolicyAlways)
	assert.Equal(t, StepRunPolicy("OnSuccess"), StepRunPolicyOnSuccess)
	assert.Equal(t, StepRunPolicy("OnFailure"), StepRunPolicyOnFailure)
	assert.Equal(t, StepRunPolicy("OnCondition"), StepRunPolicyOnCondition)
	
	// Test WorkflowPhase constants
	assert.Equal(t, WorkflowPhase(""), WorkflowPhaseUnknown)
	assert.Equal(t, WorkflowPhase("Pending"), WorkflowPhasePending)
	assert.Equal(t, WorkflowPhase("Running"), WorkflowPhaseRunning)
	assert.Equal(t, WorkflowPhase("Succeeded"), WorkflowPhaseSucceeded)
	assert.Equal(t, WorkflowPhase("Failed"), WorkflowPhaseFailed)
	assert.Equal(t, WorkflowPhase("Suspended"), WorkflowPhaseSuspended)
	assert.Equal(t, WorkflowPhase("Completed"), WorkflowPhaseCompleted)
	
	// Test WorkflowConditionType constants
	assert.Equal(t, WorkflowConditionType("Scheduled"), WorkflowConditionScheduled)
	assert.Equal(t, WorkflowConditionType("Executing"), WorkflowConditionExecuting)
	assert.Equal(t, WorkflowConditionType("Succeeded"), WorkflowConditionSucceeded)
	assert.Equal(t, WorkflowConditionType("Failed"), WorkflowConditionFailed)
	assert.Equal(t, WorkflowConditionType("Suspended"), WorkflowConditionSuspended)
	assert.Equal(t, WorkflowConditionType("Validated"), WorkflowConditionValidated)
	assert.Equal(t, WorkflowConditionType("Progressing"), WorkflowConditionProgressing)
	
	// Test StepPhase constants
	assert.Equal(t, StepPhase("Pending"), StepPhasePending)
	assert.Equal(t, StepPhase("Running"), StepPhaseRunning)
	assert.Equal(t, StepPhase("Succeeded"), StepPhaseSucceeded)
	assert.Equal(t, StepPhase("Failed"), StepPhaseFailed)
	assert.Equal(t, StepPhase("Skipped"), StepPhaseSkipped)
	assert.Equal(t, StepPhase("Retrying"), StepPhaseRetrying)
}