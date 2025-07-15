package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/phildougherty/m8e/internal/crd"
)

// MockToolExecutor is a mock implementation of ToolExecutor
type MockToolExecutor struct {
	mock.Mock
}

func (m *MockToolExecutor) ExecuteStepWithContext(ctx context.Context, stepCtx *StepContext, tool string, parameters map[string]interface{}, timeout time.Duration) (*ToolExecutionResult, error) {
	args := m.Called(ctx, stepCtx, tool, parameters, timeout)
	return args.Get(0).(*ToolExecutionResult), args.Error(1)
}

func (m *MockToolExecutor) ExecuteTool(ctx context.Context, req *ToolExecutionRequest) (*ToolExecutionResult, error) {
	args := m.Called(ctx, req)
	return args.Get(0).(*ToolExecutionResult), args.Error(1)
}

func (m *MockToolExecutor) DiscoverTools(ctx context.Context) ([]string, error) {
	args := m.Called(ctx)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockToolExecutor) ValidateTool(ctx context.Context, toolName string) error {
	args := m.Called(ctx, toolName)
	return args.Error(0)
}

func (m *MockToolExecutor) HealthCheck(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func TestWorkflowEngine_ExecuteWorkflow(t *testing.T) {
	logger := logr.Discard()
	
	tests := []struct {
		name           string
		workflowSpec   *crd.WorkflowSpec
		mockSetup      func(*MockToolExecutor)
		expectedStatus WorkflowExecutionStatus
		expectedSteps  int
		wantErr        bool
	}{
		{
			name: "simple workflow with one step",
			workflowSpec: &crd.WorkflowSpec{
				Schedule: "0 0 * * *",
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
			mockSetup: func(m *MockToolExecutor) {
				m.On("ExecuteStepWithContext", mock.Anything, mock.Anything, "test-tool", mock.Anything, mock.Anything).Return(&ToolExecutionResult{
					Success: true,
					Output: map[string]interface{}{
						"result": "success",
					},
					Duration: 1 * time.Second,
				}, nil)
			},
			expectedStatus: WorkflowExecutionStatusSucceeded,
			expectedSteps:  1,
			wantErr:        false,
		},
		{
			name: "workflow with multiple steps and dependencies",
			workflowSpec: &crd.WorkflowSpec{
				Schedule: "0 0 * * *",
				Steps: []crd.WorkflowStep{
					{
						Name: "step1",
						Tool: "build-tool",
						Parameters: map[string]interface{}{
							"action": "build",
						},
					},
					{
						Name: "step2",
						Tool: "test-tool",
						Parameters: map[string]interface{}{
							"action": "test",
						},
						DependsOn: []string{"step1"},
					},
				},
			},
			mockSetup: func(m *MockToolExecutor) {
				m.On("ExecuteStepWithContext", mock.Anything, mock.Anything, "build-tool", mock.Anything, mock.Anything).Return(&ToolExecutionResult{
					Success: true,
					Output: map[string]interface{}{
						"buildResult": "success",
					},
					Duration: 2 * time.Second,
				}, nil)
				m.On("ExecuteStepWithContext", mock.Anything, mock.Anything, "test-tool", mock.Anything, mock.Anything).Return(&ToolExecutionResult{
					Success: true,
					Output: map[string]interface{}{
						"testResult": "passed",
					},
					Duration: 1 * time.Second,
				}, nil)
			},
			expectedStatus: WorkflowExecutionStatusSucceeded,
			expectedSteps:  2,
			wantErr:        false,
		},
		{
			name: "workflow with step failure",
			workflowSpec: &crd.WorkflowSpec{
				Schedule: "0 0 * * *",
				Steps: []crd.WorkflowStep{
					{
						Name: "failing-step",
						Tool: "failing-tool",
						Parameters: map[string]interface{}{
							"action": "fail",
						},
					},
				},
			},
			mockSetup: func(m *MockToolExecutor) {
				m.On("ExecuteStepWithContext", mock.Anything, mock.Anything, "failing-tool", mock.Anything, mock.Anything).Return(&ToolExecutionResult{
					Success: false,
					Error:   "Tool execution failed",
					Duration: 1 * time.Second,
				}, nil)
			},
			expectedStatus: WorkflowExecutionStatusFailed,
			expectedSteps:  1,
			wantErr:        false,
		},
		{
			name: "workflow with timeout",
			workflowSpec: &crd.WorkflowSpec{
				Schedule: "0 0 * * *",
				Timeout:  &metav1.Duration{Duration: 5 * time.Second},
				Steps: []crd.WorkflowStep{
					{
						Name: "timeout-step",
						Tool: "slow-tool",
						Parameters: map[string]interface{}{
							"action": "slow",
						},
					},
				},
			},
			mockSetup: func(m *MockToolExecutor) {
				m.On("ExecuteStepWithContext", mock.Anything, mock.Anything, "slow-tool", mock.Anything, mock.Anything).Return(&ToolExecutionResult{
					Success: true,
					Output: map[string]interface{}{
						"result": "completed",
					},
					Duration: 1 * time.Second,
				}, nil)
			},
			expectedStatus: WorkflowExecutionStatusSucceeded,
			expectedSteps:  1,
			wantErr:        false,
		},
		{
			name: "workflow with continue on error",
			workflowSpec: &crd.WorkflowSpec{
				Schedule: "0 0 * * *",
				Steps: []crd.WorkflowStep{
					{
						Name: "failing-step",
						Tool: "failing-tool",
						Parameters: map[string]interface{}{
							"action": "fail",
						},
						ContinueOnError: true,
					},
					{
						Name: "success-step",
						Tool: "success-tool",
						Parameters: map[string]interface{}{
							"action": "succeed",
						},
					},
				},
			},
			mockSetup: func(m *MockToolExecutor) {
				m.On("ExecuteStepWithContext", mock.Anything, mock.Anything, "failing-tool", mock.Anything, mock.Anything).Return(&ToolExecutionResult{
					Success: false,
					Error:   "Tool execution failed",
					Duration: 1 * time.Second,
				}, nil)
				m.On("ExecuteStepWithContext", mock.Anything, mock.Anything, "success-tool", mock.Anything, mock.Anything).Return(&ToolExecutionResult{
					Success: true,
					Output: map[string]interface{}{
						"result": "success",
					},
					Duration: 1 * time.Second,
				}, nil)
			},
			expectedStatus: WorkflowExecutionStatusSucceeded,
			expectedSteps:  2,
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockToolExecutor := &MockToolExecutor{}
			tt.mockSetup(mockToolExecutor)

			engine := NewWorkflowEngine(mockToolExecutor, logger)

			execution, err := engine.ExecuteWorkflow(context.Background(), tt.workflowSpec, "test-workflow", "default")

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			assert.Equal(t, tt.expectedStatus, execution.Status)
			assert.Equal(t, tt.expectedSteps, len(execution.Steps))
			assert.Equal(t, "test-workflow", execution.WorkflowName)
			assert.Equal(t, "default", execution.WorkflowNamespace)
			assert.NotEmpty(t, execution.ID)
			assert.NotZero(t, execution.StartTime)
			assert.NotNil(t, execution.EndTime)

			mockToolExecutor.AssertExpectations(t)
		})
	}
}

func TestWorkflowEngine_BuildDependencyGraph(t *testing.T) {
	logger := logr.Discard()
	engine := NewWorkflowEngine(nil, logger)

	tests := []struct {
		name          string
		steps         []crd.WorkflowStep
		expectedGraph [][]string
		wantErr       bool
	}{
		{
			name: "single step",
			steps: []crd.WorkflowStep{
				{Name: "step1", Tool: "tool1"},
			},
			expectedGraph: [][]string{
				{"step1"},
			},
			wantErr: false,
		},
		{
			name: "two independent steps",
			steps: []crd.WorkflowStep{
				{Name: "step1", Tool: "tool1"},
				{Name: "step2", Tool: "tool2"},
			},
			expectedGraph: [][]string{
				{"step1", "step2"},
			},
			wantErr: false,
		},
		{
			name: "two dependent steps",
			steps: []crd.WorkflowStep{
				{Name: "step1", Tool: "tool1"},
				{Name: "step2", Tool: "tool2", DependsOn: []string{"step1"}},
			},
			expectedGraph: [][]string{
				{"step1"},
				{"step2"},
			},
			wantErr: false,
		},
		{
			name: "complex dependency graph",
			steps: []crd.WorkflowStep{
				{Name: "step1", Tool: "tool1"},
				{Name: "step2", Tool: "tool2"},
				{Name: "step3", Tool: "tool3", DependsOn: []string{"step1", "step2"}},
				{Name: "step4", Tool: "tool4", DependsOn: []string{"step1"}},
			},
			expectedGraph: [][]string{
				{"step1", "step2"},
				{"step3", "step4"},
			},
			wantErr: false,
		},
		{
			name: "circular dependency",
			steps: []crd.WorkflowStep{
				{Name: "step1", Tool: "tool1", DependsOn: []string{"step2"}},
				{Name: "step2", Tool: "tool2", DependsOn: []string{"step1"}},
			},
			expectedGraph: nil,
			wantErr:       true,
		},
		{
			name: "invalid dependency",
			steps: []crd.WorkflowStep{
				{Name: "step1", Tool: "tool1", DependsOn: []string{"nonexistent"}},
			},
			expectedGraph: nil,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph, err := engine.buildDependencyGraph(tt.steps)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, len(tt.expectedGraph), len(graph))
				
				for i, expectedLevel := range tt.expectedGraph {
					assert.ElementsMatch(t, expectedLevel, graph[i])
				}
			}
		})
	}
}

func TestWorkflowEngine_ShouldSkipStep(t *testing.T) {
	logger := logr.Discard()
	engine := NewWorkflowEngine(nil, logger)

	tests := []struct {
		name           string
		step           *crd.WorkflowStep
		execution      *WorkflowExecution
		expectedSkip   bool
		expectedReason string
	}{
		{
			name: "always run policy",
			step: &crd.WorkflowStep{
				Name:      "step1",
				Tool:      "tool1",
				RunPolicy: crd.StepRunPolicyAlways,
			},
			execution: &WorkflowExecution{
				Context: &WorkflowExecutionContext{
					FailureCount: 1,
				},
			},
			expectedSkip: false,
		},
		{
			name: "on success policy with no failures",
			step: &crd.WorkflowStep{
				Name:      "step1",
				Tool:      "tool1",
				RunPolicy: crd.StepRunPolicyOnSuccess,
			},
			execution: &WorkflowExecution{
				Context: &WorkflowExecutionContext{
					FailureCount: 0,
				},
			},
			expectedSkip: false,
		},
		{
			name: "on success policy with failures",
			step: &crd.WorkflowStep{
				Name:      "step1",
				Tool:      "tool1",
				RunPolicy: crd.StepRunPolicyOnSuccess,
			},
			execution: &WorkflowExecution{
				Context: &WorkflowExecutionContext{
					FailureCount: 1,
				},
			},
			expectedSkip:   true,
			expectedReason: "Previous steps have failures and run policy is OnSuccess",
		},
		{
			name: "on failure policy with no failures",
			step: &crd.WorkflowStep{
				Name:      "step1",
				Tool:      "tool1",
				RunPolicy: crd.StepRunPolicyOnFailure,
			},
			execution: &WorkflowExecution{
				Context: &WorkflowExecutionContext{
					FailureCount: 0,
				},
			},
			expectedSkip:   true,
			expectedReason: "No previous failures and run policy is OnFailure",
		},
		{
			name: "on failure policy with failures",
			step: &crd.WorkflowStep{
				Name:      "step1",
				Tool:      "tool1",
				RunPolicy: crd.StepRunPolicyOnFailure,
			},
			execution: &WorkflowExecution{
				Context: &WorkflowExecutionContext{
					FailureCount: 1,
				},
			},
			expectedSkip: false,
		},
		{
			name: "on condition policy",
			step: &crd.WorkflowStep{
				Name:      "step1",
				Tool:      "tool1",
				RunPolicy: crd.StepRunPolicyOnCondition,
			},
			execution: &WorkflowExecution{
				Context: &WorkflowExecutionContext{
					FailureCount: 0,
				},
			},
			expectedSkip: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			skip, reason := engine.shouldSkipStep(tt.step, tt.execution)
			assert.Equal(t, tt.expectedSkip, skip)
			if tt.expectedSkip {
				assert.Equal(t, tt.expectedReason, reason)
			}
		})
	}
}

func TestWorkflowEngine_GetExecutionSummary(t *testing.T) {
	logger := logr.Discard()
	engine := NewWorkflowEngine(nil, logger)

	startTime := time.Now()
	endTime := startTime.Add(5 * time.Minute)

	execution := &WorkflowExecution{
		ID:                "test-execution-1",
		WorkflowName:      "test-workflow",
		WorkflowNamespace: "default",
		StartTime:         startTime,
		EndTime:           &endTime,
		Status:            WorkflowExecutionStatusSucceeded,
		Steps: []StepExecution{
			{
				Name:     "step1",
				Tool:     "tool1",
				Status:   StepExecutionStatusSucceeded,
				Output:   "success",
				Duration: 2 * time.Minute,
				Attempts: 1,
			},
			{
				Name:     "step2",
				Tool:     "tool2",
				Status:   StepExecutionStatusFailed,
				Error:    "execution failed",
				Duration: 3 * time.Minute,
				Attempts: 3,
			},
		},
		Error: "",
	}

	summary := engine.GetExecutionSummary(execution)

	assert.Equal(t, "test-execution-1", summary.ID)
	assert.Equal(t, startTime, summary.StartTime.Time)
	assert.Equal(t, endTime, summary.EndTime.Time)
	assert.Equal(t, crd.WorkflowPhase(WorkflowExecutionStatusSucceeded), summary.Phase)
	assert.Equal(t, 5*time.Minute, summary.Duration.Duration)
	assert.Equal(t, "", summary.Message)

	assert.Len(t, summary.StepResults, 2)
	
	step1Result := summary.StepResults["step1"]
	assert.Equal(t, crd.StepPhase(StepExecutionStatusSucceeded), step1Result.Phase)
	assert.Equal(t, "success", step1Result.Output)
	assert.Equal(t, 2*time.Minute, step1Result.Duration)
	assert.Equal(t, int32(1), step1Result.Attempts)
	assert.Equal(t, "", step1Result.Error)

	step2Result := summary.StepResults["step2"]
	assert.Equal(t, crd.StepPhase(StepExecutionStatusFailed), step2Result.Phase)
	assert.Equal(t, 3*time.Minute, step2Result.Duration)
	assert.Equal(t, int32(3), step2Result.Attempts)
	assert.Equal(t, "execution failed", step2Result.Error)
}

func TestWorkflowEngine_ValidateWorkflowSpec(t *testing.T) {
	logger := logr.Discard()
	engine := NewWorkflowEngine(nil, logger)

	tests := []struct {
		name    string
		spec    *crd.WorkflowSpec
		wantErr bool
	}{
		{
			name: "valid workflow spec",
			spec: &crd.WorkflowSpec{
				Schedule: "0 0 * * *",
				Steps: []crd.WorkflowStep{
					{
						Name: "step1",
						Tool: "tool1",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "missing schedule",
			spec: &crd.WorkflowSpec{
				Steps: []crd.WorkflowStep{
					{
						Name: "step1",
						Tool: "tool1",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid schedule",
			spec: &crd.WorkflowSpec{
				Schedule: "invalid cron",
				Steps: []crd.WorkflowStep{
					{
						Name: "step1",
						Tool: "tool1",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "no steps",
			spec: &crd.WorkflowSpec{
				Schedule: "0 0 * * *",
				Steps:    []crd.WorkflowStep{},
			},
			wantErr: true,
		},
		{
			name: "step with empty name",
			spec: &crd.WorkflowSpec{
				Schedule: "0 0 * * *",
				Steps: []crd.WorkflowStep{
					{
						Name: "",
						Tool: "tool1",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "step with empty tool",
			spec: &crd.WorkflowSpec{
				Schedule: "0 0 * * *",
				Steps: []crd.WorkflowStep{
					{
						Name: "step1",
						Tool: "",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "duplicate step names",
			spec: &crd.WorkflowSpec{
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
			wantErr: true,
		},
		{
			name: "invalid dependency",
			spec: &crd.WorkflowSpec{
				Schedule: "0 0 * * *",
				Steps: []crd.WorkflowStep{
					{
						Name:      "step1",
						Tool:      "tool1",
						DependsOn: []string{"nonexistent"},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "valid dependencies",
			spec: &crd.WorkflowSpec{
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
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := engine.ValidateWorkflowSpec(tt.spec)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestWorkflowEngine_ExecuteStepWithRetry(t *testing.T) {
	logger := logr.Discard()
	mockToolExecutor := &MockToolExecutor{}
	engine := NewWorkflowEngine(mockToolExecutor, logger)

	step := &crd.WorkflowStep{
		Name: "retry-step",
		Tool: "flaky-tool",
		Parameters: map[string]interface{}{
			"param1": "value1",
		},
		RetryPolicy: &crd.RetryPolicy{
			MaxRetries:    3,
			RetryDelay:    metav1.Duration{Duration: 100 * time.Millisecond},
			BackoffPolicy: crd.BackoffPolicyFixed,
		},
	}

	execution := &WorkflowExecution{
		ID:                "test-execution",
		WorkflowName:      "test-workflow",
		WorkflowNamespace: "default",
		Context: &WorkflowExecutionContext{
			WorkflowSpec: &crd.WorkflowSpec{
				Steps: []crd.WorkflowStep{*step},
			},
			StepOutputs: make(map[string]interface{}),
		},
	}

	// Mock first two attempts to fail, third to succeed
	mockToolExecutor.On("ExecuteStepWithContext", mock.Anything, mock.Anything, "flaky-tool", mock.Anything, mock.Anything).Return(&ToolExecutionResult{
		Success: false,
		Error:   "Tool execution failed",
		Duration: 100 * time.Millisecond,
	}, nil).Twice()

	mockToolExecutor.On("ExecuteStepWithContext", mock.Anything, mock.Anything, "flaky-tool", mock.Anything, mock.Anything).Return(&ToolExecutionResult{
		Success: true,
		Output: map[string]interface{}{
			"result": "success on retry",
		},
		Duration: 100 * time.Millisecond,
	}, nil).Once()

	result, err := engine.executeStep(context.Background(), execution, step)

	assert.NoError(t, err)
	assert.Equal(t, StepExecutionStatusSucceeded, result.Status)
	assert.Equal(t, 3, result.Attempts)
	assert.Equal(t, "success on retry", result.Output.(map[string]interface{})["result"])

	mockToolExecutor.AssertExpectations(t)
}

func TestWorkflowEngine_ExecuteStepWithTimeout(t *testing.T) {
	logger := logr.Discard()
	mockToolExecutor := &MockToolExecutor{}
	engine := NewWorkflowEngine(mockToolExecutor, logger)

	step := &crd.WorkflowStep{
		Name: "timeout-step",
		Tool: "slow-tool",
		Parameters: map[string]interface{}{
			"param1": "value1",
		},
		Timeout: &metav1.Duration{Duration: 2 * time.Second},
	}

	execution := &WorkflowExecution{
		ID:                "test-execution",
		WorkflowName:      "test-workflow",
		WorkflowNamespace: "default",
		Context: &WorkflowExecutionContext{
			WorkflowSpec: &crd.WorkflowSpec{
				Steps: []crd.WorkflowStep{*step},
			},
			StepOutputs: make(map[string]interface{}),
		},
	}

	// Mock tool execution to complete within timeout
	mockToolExecutor.On("ExecuteStepWithContext", mock.Anything, mock.Anything, "slow-tool", mock.Anything, mock.Anything).Return(&ToolExecutionResult{
		Success: true,
		Output: map[string]interface{}{
			"result": "completed within timeout",
		},
		Duration: 1 * time.Second,
	}, nil)

	result, err := engine.executeStep(context.Background(), execution, step)

	assert.NoError(t, err)
	assert.Equal(t, StepExecutionStatusSucceeded, result.Status)
	assert.Equal(t, "completed within timeout", result.Output.(map[string]interface{})["result"])

	mockToolExecutor.AssertExpectations(t)
}

func TestWorkflowEngine_ExecuteStepWithCondition(t *testing.T) {
	logger := logr.Discard()
	mockToolExecutor := &MockToolExecutor{}
	engine := NewWorkflowEngine(mockToolExecutor, logger)

	tests := []struct {
		name              string
		condition         string
		stepOutputs       map[string]interface{}
		expectedStatus    StepExecutionStatus
		expectedSkipped   bool
		shouldMockExecute bool
	}{
		{
			name:              "no condition",
			condition:         "",
			stepOutputs:       map[string]interface{}{},
			expectedStatus:    StepExecutionStatusSucceeded,
			expectedSkipped:   false,
			shouldMockExecute: true,
		},
		{
			name:              "condition evaluates to true",
			condition:         "true",
			stepOutputs:       map[string]interface{}{},
			expectedStatus:    StepExecutionStatusSucceeded,
			expectedSkipped:   false,
			shouldMockExecute: true,
		},
		{
			name:              "condition evaluates to false",
			condition:         "false",
			stepOutputs:       map[string]interface{}{},
			expectedStatus:    StepExecutionStatusSkipped,
			expectedSkipped:   true,
			shouldMockExecute: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := &crd.WorkflowStep{
				Name:      "condition-step",
				Tool:      "test-tool",
				Condition: tt.condition,
				Parameters: map[string]interface{}{
					"param1": "value1",
				},
			}

			execution := &WorkflowExecution{
				ID:                "test-execution",
				WorkflowName:      "test-workflow",
				WorkflowNamespace: "default",
				Context: &WorkflowExecutionContext{
					WorkflowSpec: &crd.WorkflowSpec{
						Steps: []crd.WorkflowStep{*step},
					},
					StepOutputs: tt.stepOutputs,
				},
			}

			if tt.shouldMockExecute {
				mockToolExecutor.On("ExecuteStepWithContext", mock.Anything, mock.Anything, "test-tool", mock.Anything, mock.Anything).Return(&ToolExecutionResult{
					Success: true,
					Output: map[string]interface{}{
						"result": "success",
					},
					Duration: 1 * time.Second,
				}, nil).Once()
			}

			result, err := engine.executeStep(context.Background(), execution, step)

			assert.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, result.Status)
			assert.Equal(t, tt.expectedSkipped, result.Skipped)

			if tt.shouldMockExecute {
				mockToolExecutor.AssertExpectations(t)
			}
		})
	}
}

func TestWorkflowExecutionContext_Structure(t *testing.T) {
	ctx := &WorkflowExecutionContext{
		WorkflowSpec: &crd.WorkflowSpec{
			Schedule: "0 0 * * *",
			Steps: []crd.WorkflowStep{
				{Name: "step1", Tool: "tool1"},
			},
		},
		ExecutionTimeout:  30 * time.Minute,
		StepOutputs:       make(map[string]interface{}),
		FailureThreshold:  3,
		FailureCount:      0,
		ContinueOnFailure: false,
	}

	assert.Equal(t, 30*time.Minute, ctx.ExecutionTimeout)
	assert.Equal(t, 3, ctx.FailureThreshold)
	assert.Equal(t, 0, ctx.FailureCount)
	assert.False(t, ctx.ContinueOnFailure)
	assert.NotNil(t, ctx.StepOutputs)
	assert.NotNil(t, ctx.WorkflowSpec)
}

func TestStepExecution_Structure(t *testing.T) {
	startTime := time.Now()
	endTime := startTime.Add(2 * time.Minute)

	step := &StepExecution{
		Name:       "test-step",
		Tool:       "test-tool",
		StartTime:  startTime,
		EndTime:    &endTime,
		Status:     StepExecutionStatusSucceeded,
		Output:     "success",
		Duration:   2 * time.Minute,
		Attempts:   1,
		Condition:  "true",
		Skipped:    false,
		SkipReason: "",
	}

	assert.Equal(t, "test-step", step.Name)
	assert.Equal(t, "test-tool", step.Tool)
	assert.Equal(t, startTime, step.StartTime)
	assert.Equal(t, endTime, *step.EndTime)
	assert.Equal(t, StepExecutionStatusSucceeded, step.Status)
	assert.Equal(t, "success", step.Output)
	assert.Equal(t, 2*time.Minute, step.Duration)
	assert.Equal(t, 1, step.Attempts)
	assert.Equal(t, "true", step.Condition)
	assert.False(t, step.Skipped)
	assert.Equal(t, "", step.SkipReason)
}

func TestWorkflowExecution_Structure(t *testing.T) {
	startTime := time.Now()
	endTime := startTime.Add(5 * time.Minute)

	execution := &WorkflowExecution{
		ID:                "test-execution-1",
		WorkflowName:      "test-workflow",
		WorkflowNamespace: "default",
		StartTime:         startTime,
		EndTime:           &endTime,
		Status:            WorkflowExecutionStatusSucceeded,
		Steps:             []StepExecution{},
		Error:             "",
		Context: &WorkflowExecutionContext{
			StepOutputs: make(map[string]interface{}),
		},
	}

	assert.Equal(t, "test-execution-1", execution.ID)
	assert.Equal(t, "test-workflow", execution.WorkflowName)
	assert.Equal(t, "default", execution.WorkflowNamespace)
	assert.Equal(t, startTime, execution.StartTime)
	assert.Equal(t, endTime, *execution.EndTime)
	assert.Equal(t, WorkflowExecutionStatusSucceeded, execution.Status)
	assert.Empty(t, execution.Steps)
	assert.Empty(t, execution.Error)
	assert.NotNil(t, execution.Context)
}

func TestWorkflowExecutionStatus_Constants(t *testing.T) {
	assert.Equal(t, WorkflowExecutionStatus("Running"), WorkflowExecutionStatusRunning)
	assert.Equal(t, WorkflowExecutionStatus("Succeeded"), WorkflowExecutionStatusSucceeded)
	assert.Equal(t, WorkflowExecutionStatus("Failed"), WorkflowExecutionStatusFailed)
	assert.Equal(t, WorkflowExecutionStatus("Cancelled"), WorkflowExecutionStatusCancelled)
}

func TestStepExecutionStatus_Constants(t *testing.T) {
	assert.Equal(t, StepExecutionStatus("Pending"), StepExecutionStatusPending)
	assert.Equal(t, StepExecutionStatus("Running"), StepExecutionStatusRunning)
	assert.Equal(t, StepExecutionStatus("Succeeded"), StepExecutionStatusSucceeded)
	assert.Equal(t, StepExecutionStatus("Failed"), StepExecutionStatusFailed)
	assert.Equal(t, StepExecutionStatus("Skipped"), StepExecutionStatusSkipped)
	assert.Equal(t, StepExecutionStatus("Retrying"), StepExecutionStatusRetrying)
}