// internal/scheduler/workflow_engine.go
package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/phildougherty/m8e/internal/crd"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type WorkflowEngine struct {
	toolExecutor   *ToolExecutor
	templateEngine *TemplateEngine
	logger         logr.Logger
}

type WorkflowExecution struct {
	ID                string
	WorkflowName      string
	WorkflowNamespace string
	StartTime         time.Time
	EndTime           *time.Time
	Status            WorkflowExecutionStatus
	Steps             []StepExecution
	Error             string
	Context           *WorkflowExecutionContext
}

type WorkflowExecutionStatus string

const (
	WorkflowExecutionStatusRunning   WorkflowExecutionStatus = "Running"
	WorkflowExecutionStatusSucceeded WorkflowExecutionStatus = "Succeeded"
	WorkflowExecutionStatusFailed    WorkflowExecutionStatus = "Failed"
	WorkflowExecutionStatusCancelled WorkflowExecutionStatus = "Cancelled"
)

type StepExecution struct {
	Name        string
	Tool        string
	Parameters  map[string]interface{}
	StartTime   time.Time
	EndTime     *time.Time
	Status      StepExecutionStatus
	Output      interface{}
	Error       string
	Attempts    int
	Duration    time.Duration
	Condition   string
	Skipped     bool
	SkipReason  string
}

type StepExecutionStatus string

const (
	StepExecutionStatusPending   StepExecutionStatus = "Pending"
	StepExecutionStatusRunning   StepExecutionStatus = "Running"
	StepExecutionStatusSucceeded StepExecutionStatus = "Succeeded"
	StepExecutionStatusFailed    StepExecutionStatus = "Failed"
	StepExecutionStatusSkipped   StepExecutionStatus = "Skipped"
	StepExecutionStatusRetrying  StepExecutionStatus = "Retrying"
)

type WorkflowExecutionContext struct {
	WorkflowSpec      *crd.WorkflowSpec
	ExecutionTimeout  time.Duration
	StepOutputs       map[string]interface{}
	FailureThreshold  int
	FailureCount      int
	ContinueOnFailure bool
}

func NewWorkflowEngine(toolExecutor *ToolExecutor, logger logr.Logger) *WorkflowEngine {
	return &WorkflowEngine{
		toolExecutor:   toolExecutor,
		templateEngine: NewTemplateEngine(logger),
		logger:         logger,
	}
}

func (we *WorkflowEngine) ExecuteWorkflow(ctx context.Context, workflowSpec *crd.WorkflowSpec, workflowName, workflowNamespace string) (*WorkflowExecution, error) {
	executionID := fmt.Sprintf("%s-%d", workflowName, time.Now().Unix())
	
	execution := &WorkflowExecution{
		ID:                executionID,
		WorkflowName:      workflowName,
		WorkflowNamespace: workflowNamespace,
		StartTime:         time.Now(),
		Status:            WorkflowExecutionStatusRunning,
		Steps:             make([]StepExecution, 0, len(workflowSpec.Steps)),
		Context: &WorkflowExecutionContext{
			WorkflowSpec:      workflowSpec,
			StepOutputs:       make(map[string]interface{}),
			FailureThreshold:  len(workflowSpec.Steps),
			ContinueOnFailure: false,
		},
	}

	// Set execution timeout
	if workflowSpec.Timeout != nil {
		execution.Context.ExecutionTimeout = workflowSpec.Timeout.Duration
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, execution.Context.ExecutionTimeout)
		defer cancel()
	}

	we.logger.Info("Starting workflow execution", 
		"executionID", executionID, 
		"workflow", workflowName, 
		"steps", len(workflowSpec.Steps))

	// Build dependency graph
	dependencyGraph, err := we.buildDependencyGraph(workflowSpec.Steps)
	if err != nil {
		execution.Status = WorkflowExecutionStatusFailed
		execution.Error = fmt.Sprintf("Failed to build dependency graph: %v", err)
		endTime := time.Now()
		execution.EndTime = &endTime
		return execution, fmt.Errorf("dependency graph error: %w", err)
	}

	// Execute steps in dependency order
	if err := we.executeStepsInOrder(ctx, execution, dependencyGraph); err != nil {
		execution.Status = WorkflowExecutionStatusFailed
		execution.Error = err.Error()
		we.logger.Error(err, "Workflow execution failed", "executionID", executionID)
	} else {
		execution.Status = WorkflowExecutionStatusSucceeded
		we.logger.Info("Workflow execution completed successfully", "executionID", executionID)
	}

	endTime := time.Now()
	execution.EndTime = &endTime
	
	return execution, nil
}

func (we *WorkflowEngine) buildDependencyGraph(steps []crd.WorkflowStep) ([][]string, error) {
	stepMap := make(map[string]int)
	for i, step := range steps {
		stepMap[step.Name] = i
	}

	// Validate dependencies exist
	for _, step := range steps {
		for _, dep := range step.DependsOn {
			if _, exists := stepMap[dep]; !exists {
				return nil, fmt.Errorf("step %q depends on non-existent step %q", step.Name, dep)
			}
		}
	}

	// Build execution order using topological sort
	inDegree := make(map[string]int)
	adjList := make(map[string][]string)

	// Initialize
	for _, step := range steps {
		inDegree[step.Name] = 0
		adjList[step.Name] = []string{}
	}

	// Build adjacency list and in-degree count
	for _, step := range steps {
		for _, dep := range step.DependsOn {
			adjList[dep] = append(adjList[dep], step.Name)
			inDegree[step.Name]++
		}
	}

	// Topological sort
	var result [][]string
	queue := []string{}

	// Find steps with no dependencies
	for stepName, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, stepName)
		}
	}

	for len(queue) > 0 {
		// Process current level (steps that can run in parallel)
		currentLevel := make([]string, len(queue))
		copy(currentLevel, queue)
		result = append(result, currentLevel)

		// Prepare next level
		nextQueue := []string{}
		for _, stepName := range queue {
			for _, neighbor := range adjList[stepName] {
				inDegree[neighbor]--
				if inDegree[neighbor] == 0 {
					nextQueue = append(nextQueue, neighbor)
				}
			}
		}
		queue = nextQueue
	}

	// Check for cycles
	totalSteps := 0
	for _, level := range result {
		totalSteps += len(level)
	}
	if totalSteps != len(steps) {
		return nil, fmt.Errorf("circular dependency detected in workflow steps")
	}

	return result, nil
}

func (we *WorkflowEngine) executeStepsInOrder(ctx context.Context, execution *WorkflowExecution, dependencyGraph [][]string) error {
	stepMap := make(map[string]*crd.WorkflowStep)
	for _, step := range execution.Context.WorkflowSpec.Steps {
		stepCopy := step
		stepMap[step.Name] = &stepCopy
	}

	for levelIndex, level := range dependencyGraph {
		we.logger.Info("Executing workflow level", "level", levelIndex, "steps", level)

		// Execute steps in parallel within the same level
		stepResults := make(chan *StepExecution, len(level))
		stepErrors := make(chan error, len(level))

		for _, stepName := range level {
			go func(name string) {
				step := stepMap[name]
				result, err := we.executeStep(ctx, execution, step)
				if err != nil {
					stepErrors <- err
				} else {
					stepResults <- result
				}
			}(stepName)
		}

		// Wait for all steps in this level to complete
		completedSteps := 0
		var levelError error

		for completedSteps < len(level) {
			select {
			case result := <-stepResults:
				execution.Steps = append(execution.Steps, *result)
				
				// Store step output for templating
				if result.Status == StepExecutionStatusSucceeded {
					we.templateEngine.SetStepOutput(result.Name, result.Output)
					execution.Context.StepOutputs[result.Name] = result.Output
				}
				
				completedSteps++

			case err := <-stepErrors:
				if levelError == nil {
					levelError = err
				}
				completedSteps++

			case <-ctx.Done():
				return fmt.Errorf("workflow execution cancelled: %w", ctx.Err())
			}
		}

		// Handle level failures
		if levelError != nil {
			return fmt.Errorf("level %d execution failed: %w", levelIndex, levelError)
		}

		// Check if we should continue after failures
		hasFailures := false
		for _, step := range execution.Steps {
			if step.Status == StepExecutionStatusFailed {
				hasFailures = true
				execution.Context.FailureCount++
			}
		}

		if hasFailures && !execution.Context.ContinueOnFailure {
			return fmt.Errorf("workflow stopped due to step failures")
		}
	}

	return nil
}

func (we *WorkflowEngine) executeStep(ctx context.Context, execution *WorkflowExecution, step *crd.WorkflowStep) (*StepExecution, error) {
	stepExecution := &StepExecution{
		Name:       step.Name,
		Tool:       step.Tool,
		StartTime:  time.Now(),
		Status:     StepExecutionStatusRunning,
		Parameters: step.Parameters,
		Condition:  step.Condition,
		Attempts:   0,
	}

	we.logger.Info("Executing step", "step", step.Name, "tool", step.Tool)

	// Check if step should be skipped based on run policy
	shouldSkip, skipReason := we.shouldSkipStep(step, execution)
	if shouldSkip {
		stepExecution.Status = StepExecutionStatusSkipped
		stepExecution.Skipped = true
		stepExecution.SkipReason = skipReason
		endTime := time.Now()
		stepExecution.EndTime = &endTime
		stepExecution.Duration = endTime.Sub(stepExecution.StartTime)
		we.logger.Info("Step skipped", "step", step.Name, "reason", skipReason)
		return stepExecution, nil
	}

	// Evaluate condition
	if step.Condition != "" {
		shouldExecute, err := we.templateEngine.EvaluateCondition(step.Condition)
		if err != nil {
			stepExecution.Status = StepExecutionStatusFailed
			stepExecution.Error = fmt.Sprintf("Failed to evaluate condition: %v", err)
			endTime := time.Now()
			stepExecution.EndTime = &endTime
			stepExecution.Duration = endTime.Sub(stepExecution.StartTime)
			return stepExecution, fmt.Errorf("condition evaluation failed: %w", err)
		}

		if !shouldExecute {
			stepExecution.Status = StepExecutionStatusSkipped
			stepExecution.Skipped = true
			stepExecution.SkipReason = "Condition evaluated to false"
			endTime := time.Now()
			stepExecution.EndTime = &endTime
			stepExecution.Duration = endTime.Sub(stepExecution.StartTime)
			we.logger.Info("Step skipped due to condition", "step", step.Name, "condition", step.Condition)
			return stepExecution, nil
		}
	}

	// Render parameters with template engine
	renderedParams, err := we.templateEngine.RenderParameters(step.Parameters)
	if err != nil {
		stepExecution.Status = StepExecutionStatusFailed
		stepExecution.Error = fmt.Sprintf("Failed to render parameters: %v", err)
		endTime := time.Now()
		stepExecution.EndTime = &endTime
		stepExecution.Duration = endTime.Sub(stepExecution.StartTime)
		return stepExecution, fmt.Errorf("parameter rendering failed: %w", err)
	}
	stepExecution.Parameters = renderedParams

	// Execute with retry logic
	maxRetries := 3
	retryDelay := 30 * time.Second

	if step.RetryPolicy != nil {
		maxRetries = int(step.RetryPolicy.MaxRetries)
		retryDelay = step.RetryPolicy.RetryDelay.Duration
	}

	var lastError error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		stepExecution.Attempts = attempt

		if attempt > 1 {
			stepExecution.Status = StepExecutionStatusRetrying
			we.logger.Info("Retrying step", "step", step.Name, "attempt", attempt)
			time.Sleep(retryDelay)
		}

		// Set step timeout
		stepCtx := ctx
		if step.Timeout != nil {
			var cancel context.CancelFunc
			stepCtx, cancel = context.WithTimeout(ctx, step.Timeout.Duration)
			defer cancel()
		}

		// Create step context
		stepContext := &StepContext{
			WorkflowName:      execution.WorkflowName,
			WorkflowNamespace: execution.WorkflowNamespace,
			StepName:          step.Name,
			Attempt:           attempt,
			PreviousOutputs:   execution.Context.StepOutputs,
		}

		// Execute the tool
		result, err := we.toolExecutor.ExecuteStepWithContext(
			stepCtx,
			stepContext,
			step.Tool,
			renderedParams,
			step.Timeout.Duration,
		)

		if err != nil {
			lastError = err
			we.logger.Error(err, "Step execution attempt failed", "step", step.Name, "attempt", attempt)
			continue
		}

		if result.Success {
			stepExecution.Status = StepExecutionStatusSucceeded
			stepExecution.Output = result.Output
			stepExecution.Duration = result.Duration
			endTime := time.Now()
			stepExecution.EndTime = &endTime
			we.logger.Info("Step execution succeeded", "step", step.Name, "attempt", attempt, "duration", result.Duration)
			return stepExecution, nil
		} else {
			lastError = fmt.Errorf("tool execution failed: %s", result.Error)
			we.logger.Info("Step execution failed", "step", step.Name, "attempt", attempt, "error", result.Error)
		}
	}

	// All attempts failed
	stepExecution.Status = StepExecutionStatusFailed
	stepExecution.Error = fmt.Sprintf("Failed after %d attempts: %v", maxRetries, lastError)
	endTime := time.Now()
	stepExecution.EndTime = &endTime
	stepExecution.Duration = endTime.Sub(stepExecution.StartTime)

	// Check if workflow should continue on this step's failure
	if step.ContinueOnError {
		execution.Context.ContinueOnFailure = true
		we.logger.Info("Step failed but workflow will continue", "step", step.Name)
		return stepExecution, nil
	}

	return stepExecution, fmt.Errorf("step %q failed: %v", step.Name, lastError)
}

func (we *WorkflowEngine) shouldSkipStep(step *crd.WorkflowStep, execution *WorkflowExecution) (bool, string) {
	switch step.RunPolicy {
	case crd.StepRunPolicyAlways:
		return false, ""
	case crd.StepRunPolicyOnSuccess:
		if execution.Context.FailureCount > 0 {
			return true, "Previous steps have failures and run policy is OnSuccess"
		}
		return false, ""
	case crd.StepRunPolicyOnFailure:
		if execution.Context.FailureCount == 0 {
			return true, "No previous failures and run policy is OnFailure"
		}
		return false, ""
	case crd.StepRunPolicyOnCondition:
		// Will be handled by condition evaluation
		return false, ""
	default:
		return false, ""
	}
}

// GetExecutionSummary returns a summary of the workflow execution
func (we *WorkflowEngine) GetExecutionSummary(execution *WorkflowExecution) *crd.WorkflowExecution {
	summary := &crd.WorkflowExecution{
		ID:        execution.ID,
		StartTime: metav1.Time{Time: execution.StartTime},
		Phase:     crd.WorkflowPhase(execution.Status),
	}

	if execution.EndTime != nil {
		summary.EndTime = &metav1.Time{Time: *execution.EndTime}
		duration := execution.EndTime.Sub(execution.StartTime)
		summary.Duration = &metav1.Duration{Duration: duration}
	}

	if execution.Error != "" {
		summary.Message = execution.Error
	}

	// Convert step results
	summary.StepResults = make(map[string]crd.StepResult)
	for _, step := range execution.Steps {
		result := crd.StepResult{
			Phase:    crd.StepPhase(step.Status),
			Output:   step.Output,
			Duration: step.Duration,
			Attempts: int32(step.Attempts),
		}
		if step.Error != "" {
			result.Error = step.Error
		}
		summary.StepResults[step.Name] = result
	}

	return summary
}

// ValidateWorkflowSpec validates a workflow specification
func (we *WorkflowEngine) ValidateWorkflowSpec(spec *crd.WorkflowSpec) error {
	if spec.Schedule == "" {
		return fmt.Errorf("schedule is required")
	}

	if err := ParseCronExpression(spec.Schedule); err != nil {
		return fmt.Errorf("invalid schedule: %w", err)
	}

	if len(spec.Steps) == 0 {
		return fmt.Errorf("at least one step is required")
	}

	stepNames := make(map[string]bool)
	for i, step := range spec.Steps {
		if step.Name == "" {
			return fmt.Errorf("step %d: name is required", i)
		}

		if stepNames[step.Name] {
			return fmt.Errorf("duplicate step name: %s", step.Name)
		}
		stepNames[step.Name] = true

		if step.Tool == "" {
			return fmt.Errorf("step %s: tool is required", step.Name)
		}

		// Validate dependencies
		for _, dep := range step.DependsOn {
			if !stepNames[dep] {
				return fmt.Errorf("step %s: unknown dependency %s", step.Name, dep)
			}
		}
	}

	return nil
}