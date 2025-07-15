// internal/crd/workflow.go
package crd

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	WorkflowGroup   = "mcp.matey.ai"
	WorkflowVersion = "v1"
	WorkflowKind    = "Workflow"
	WorkflowPlural  = "workflows"
)

var (
	WorkflowGVK = schema.GroupVersionKind{
		Group:   WorkflowGroup,
		Version: WorkflowVersion,
		Kind:    WorkflowKind,
	}
	WorkflowGVR = schema.GroupVersionResource{
		Group:    WorkflowGroup,
		Version:  WorkflowVersion,
		Resource: WorkflowPlural,
	}
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type Workflow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkflowSpec   `json:"spec,omitempty"`
	Status WorkflowStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type WorkflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []Workflow `json:"items"`
}

type WorkflowSpec struct {
	// Schedule defines when the workflow should run (cron expression)
	Schedule string `json:"schedule"`

	// Timezone for schedule interpretation (defaults to UTC)
	Timezone string `json:"timezone,omitempty"`

	// Enabled indicates if the workflow is active
	Enabled bool `json:"enabled"`

	// Suspend prevents new executions while keeping the workflow definition
	Suspend bool `json:"suspend,omitempty"`

	// Steps define the sequence of actions to execute
	Steps []WorkflowStep `json:"steps"`

	// RetryPolicy defines retry behavior for the entire workflow
	RetryPolicy *RetryPolicy `json:"retryPolicy,omitempty"`

	// Timeout for the entire workflow execution
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// Concurrency defines how many workflow executions can run simultaneously
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`

	// SuccessfulJobsHistoryLimit limits the number of successful job executions to keep
	SuccessfulJobsHistoryLimit *int32 `json:"successfulJobsHistoryLimit,omitempty"`

	// FailedJobsHistoryLimit limits the number of failed job executions to keep
	FailedJobsHistoryLimit *int32 `json:"failedJobsHistoryLimit,omitempty"`
}

type WorkflowStep struct {
	// Name is a unique identifier for this step within the workflow
	Name string `json:"name"`

	// Tool specifies the MCP tool to execute
	Tool string `json:"tool"`

	// Parameters for the tool execution (supports templating)
	Parameters map[string]interface{} `json:"parameters,omitempty"`

	// Condition determines if this step should execute (supports templating)
	Condition string `json:"condition,omitempty"`

	// Timeout for this specific step
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	// RetryPolicy specific to this step
	RetryPolicy *RetryPolicy `json:"retryPolicy,omitempty"`

	// DependsOn specifies which steps must complete successfully before this step
	DependsOn []string `json:"dependsOn,omitempty"`

	// ContinueOnError allows the workflow to continue even if this step fails
	ContinueOnError bool `json:"continueOnError,omitempty"`

	// RunPolicy defines when this step should run
	RunPolicy StepRunPolicy `json:"runPolicy,omitempty"`
}

type RetryPolicy struct {
	// MaxRetries is the maximum number of retry attempts
	MaxRetries int32 `json:"maxRetries"`

	// RetryDelay is the delay between retry attempts
	RetryDelay metav1.Duration `json:"retryDelay"`

	// BackoffPolicy defines how retry delays are calculated
	BackoffPolicy BackoffPolicy `json:"backoffPolicy,omitempty"`

	// MaxRetryDelay is the maximum delay between retries
	MaxRetryDelay *metav1.Duration `json:"maxRetryDelay,omitempty"`
}

type BackoffPolicy string

const (
	BackoffPolicyLinear      BackoffPolicy = "Linear"
	BackoffPolicyExponential BackoffPolicy = "Exponential"
	BackoffPolicyFixed       BackoffPolicy = "Fixed"
)

type ConcurrencyPolicy string

const (
	ConcurrencyPolicyAllow   ConcurrencyPolicy = "Allow"
	ConcurrencyPolicyForbid  ConcurrencyPolicy = "Forbid"
	ConcurrencyPolicyReplace ConcurrencyPolicy = "Replace"
)

type StepRunPolicy string

const (
	StepRunPolicyAlways      StepRunPolicy = "Always"
	StepRunPolicyOnSuccess   StepRunPolicy = "OnSuccess"
	StepRunPolicyOnFailure   StepRunPolicy = "OnFailure"
	StepRunPolicyOnCondition StepRunPolicy = "OnCondition"
)

type WorkflowStatus struct {
	// Phase represents the current phase of the workflow
	Phase WorkflowPhase `json:"phase,omitempty"`

	// Conditions represent the latest available observations of a workflow's current state
	Conditions []WorkflowCondition `json:"conditions,omitempty"`

	// LastScheduleTime is the last time the workflow was scheduled
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`

	// NextScheduleTime is the next time the workflow will be scheduled
	NextScheduleTime *metav1.Time `json:"nextScheduleTime,omitempty"`

	// LastExecutionTime is when the most recent execution started
	LastExecutionTime *metav1.Time `json:"lastExecutionTime,omitempty"`

	// LastSuccessTime is when the workflow last completed successfully
	LastSuccessTime *metav1.Time `json:"lastSuccessTime,omitempty"`

	// ActiveJobs contains references to currently running jobs for this workflow
	ActiveJobs []JobReference `json:"activeJobs,omitempty"`

	// ExecutionHistory contains the status of recent workflow executions
	ExecutionHistory []WorkflowExecution `json:"executionHistory,omitempty"`

	// StepStatuses contains the status of each step in the most recent execution
	StepStatuses map[string]StepStatus `json:"stepStatuses,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Message provides additional information about the workflow status
	Message string `json:"message,omitempty"`
}

type WorkflowPhase string

const (
	WorkflowPhaseUnknown   WorkflowPhase = ""
	WorkflowPhasePending   WorkflowPhase = "Pending"
	WorkflowPhaseRunning   WorkflowPhase = "Running"
	WorkflowPhaseSucceeded WorkflowPhase = "Succeeded"
	WorkflowPhaseFailed    WorkflowPhase = "Failed"
	WorkflowPhaseSuspended WorkflowPhase = "Suspended"
	WorkflowPhaseCompleted WorkflowPhase = "Completed"
)

type WorkflowCondition struct {
	// Type of workflow condition
	Type WorkflowConditionType `json:"type"`

	// Status of the condition
	Status metav1.ConditionStatus `json:"status"`

	// Last time the condition transitioned from one status to another
	LastTransitionTime metav1.Time `json:"lastTransitionTime"`

	// The reason for the condition's last transition
	Reason string `json:"reason,omitempty"`

	// A human readable message indicating details about the transition
	Message string `json:"message,omitempty"`
}

type WorkflowConditionType string

const (
	WorkflowConditionScheduled  WorkflowConditionType = "Scheduled"
	WorkflowConditionExecuting  WorkflowConditionType = "Executing"
	WorkflowConditionSucceeded  WorkflowConditionType = "Succeeded"
	WorkflowConditionFailed     WorkflowConditionType = "Failed"
	WorkflowConditionSuspended  WorkflowConditionType = "Suspended"
	WorkflowConditionValidated  WorkflowConditionType = "Validated"
	WorkflowConditionProgressing WorkflowConditionType = "Progressing"
)

type JobReference struct {
	// Name of the job
	Name string `json:"name"`

	// Namespace of the job
	Namespace string `json:"namespace"`

	// UID of the job
	UID string `json:"uid"`

	// API version of the job
	APIVersion string `json:"apiVersion"`

	// Kind of the job
	Kind string `json:"kind"`

	// StartTime when the job started
	StartTime *metav1.Time `json:"startTime,omitempty"`
}

type WorkflowExecution struct {
	// ID is a unique identifier for this execution
	ID string `json:"id"`

	// StartTime when the execution started
	StartTime metav1.Time `json:"startTime"`

	// EndTime when the execution completed (nil if still running)
	EndTime *metav1.Time `json:"endTime,omitempty"`

	// Phase of the execution
	Phase WorkflowPhase `json:"phase"`

	// Message provides additional information about the execution
	Message string `json:"message,omitempty"`

	// Duration of the execution
	Duration *metav1.Duration `json:"duration,omitempty"`

	// JobReference points to the Kubernetes Job created for this execution
	JobReference *JobReference `json:"jobReference,omitempty"`

	// StepResults contains the results of each step
	StepResults map[string]StepResult `json:"stepResults,omitempty"`
}

type StepStatus struct {
	// Phase of the step
	Phase StepPhase `json:"phase"`

	// StartTime when the step started
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// EndTime when the step completed
	EndTime *metav1.Time `json:"endTime,omitempty"`

	// Message provides additional information about the step
	Message string `json:"message,omitempty"`

	// RetryCount is the number of times this step has been retried
	RetryCount int32 `json:"retryCount,omitempty"`

	// Output contains the result data from the step execution
	Output *runtime.RawExtension `json:"output,omitempty"`

	// Error contains error information if the step failed
	Error string `json:"error,omitempty"`
}

type StepPhase string

const (
	StepPhasePending   StepPhase = "Pending"
	StepPhaseRunning   StepPhase = "Running"
	StepPhaseSucceeded StepPhase = "Succeeded"
	StepPhaseFailed    StepPhase = "Failed"
	StepPhaseSkipped   StepPhase = "Skipped"
	StepPhaseRetrying  StepPhase = "Retrying"
)

type StepResult struct {
	// Phase of the step execution
	Phase StepPhase `json:"phase"`

	// Output from the step execution
	Output interface{} `json:"output,omitempty"`

	// Error message if the step failed
	Error string `json:"error,omitempty"`

	// Duration of the step execution
	Duration time.Duration `json:"duration,omitempty"`

	// Attempts is the number of execution attempts
	Attempts int32 `json:"attempts,omitempty"`
}

// DeepCopyInto is a deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Workflow) DeepCopyInto(out *Workflow) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is a deepcopy function, copying the receiver, creating a new Workflow.
func (in *Workflow) DeepCopy() *Workflow {
	if in == nil {
		return nil
	}
	out := new(Workflow)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is a deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Workflow) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is a deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WorkflowList) DeepCopyInto(out *WorkflowList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in.Items = make([]Workflow, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

// DeepCopy is a deepcopy function, copying the receiver, creating a new WorkflowList.
func (in *WorkflowList) DeepCopy() *WorkflowList {
	if in == nil {
		return nil
	}
	out := new(WorkflowList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is a deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *WorkflowList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is a deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WorkflowSpec) DeepCopyInto(out *WorkflowSpec) {
	*out = *in
	if in.Steps != nil {
		in.Steps = make([]WorkflowStep, len(in.Steps))
		for i := range in.Steps {
			in.Steps[i].DeepCopyInto(&out.Steps[i])
		}
	}
	if in.RetryPolicy != nil {
		in.RetryPolicy = &RetryPolicy{}
		in.RetryPolicy.DeepCopyInto(out.RetryPolicy)
	}
	if in.Timeout != nil {
		in.Timeout = &metav1.Duration{}
		*out.Timeout = *in.Timeout
	}
	if in.SuccessfulJobsHistoryLimit != nil {
		in.SuccessfulJobsHistoryLimit = new(int32)
		*out.SuccessfulJobsHistoryLimit = *in.SuccessfulJobsHistoryLimit
	}
	if in.FailedJobsHistoryLimit != nil {
		in.FailedJobsHistoryLimit = new(int32)
		*out.FailedJobsHistoryLimit = *in.FailedJobsHistoryLimit
	}
}

// DeepCopy is a deepcopy function, copying the receiver, creating a new WorkflowSpec.
func (in *WorkflowSpec) DeepCopy() *WorkflowSpec {
	if in == nil {
		return nil
	}
	out := new(WorkflowSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is a deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WorkflowStep) DeepCopyInto(out *WorkflowStep) {
	*out = *in
	if in.Parameters != nil {
		in.Parameters = make(map[string]interface{})
		for key, val := range in.Parameters {
			out.Parameters[key] = val
		}
	}
	if in.Timeout != nil {
		in.Timeout = &metav1.Duration{}
		*out.Timeout = *in.Timeout
	}
	if in.RetryPolicy != nil {
		in.RetryPolicy = &RetryPolicy{}
		in.RetryPolicy.DeepCopyInto(out.RetryPolicy)
	}
	if in.DependsOn != nil {
		in.DependsOn = make([]string, len(in.DependsOn))
		copy(out.DependsOn, in.DependsOn)
	}
}

// DeepCopy is a deepcopy function, copying the receiver, creating a new WorkflowStep.
func (in *WorkflowStep) DeepCopy() *WorkflowStep {
	if in == nil {
		return nil
	}
	out := new(WorkflowStep)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is a deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *RetryPolicy) DeepCopyInto(out *RetryPolicy) {
	*out = *in
	if in.MaxRetryDelay != nil {
		in.MaxRetryDelay = &metav1.Duration{}
		*out.MaxRetryDelay = *in.MaxRetryDelay
	}
}

// DeepCopy is a deepcopy function, copying the receiver, creating a new RetryPolicy.
func (in *RetryPolicy) DeepCopy() *RetryPolicy {
	if in == nil {
		return nil
	}
	out := new(RetryPolicy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is a deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WorkflowStatus) DeepCopyInto(out *WorkflowStatus) {
	*out = *in
	if in.Conditions != nil {
		in.Conditions = make([]WorkflowCondition, len(in.Conditions))
		for i := range in.Conditions {
			in.Conditions[i].DeepCopyInto(&out.Conditions[i])
		}
	}
	if in.LastScheduleTime != nil {
		in.LastScheduleTime = &metav1.Time{}
		in.LastScheduleTime.DeepCopyInto(out.LastScheduleTime)
	}
	if in.NextScheduleTime != nil {
		in.NextScheduleTime = &metav1.Time{}
		in.NextScheduleTime.DeepCopyInto(out.NextScheduleTime)
	}
	if in.LastExecutionTime != nil {
		in.LastExecutionTime = &metav1.Time{}
		in.LastExecutionTime.DeepCopyInto(out.LastExecutionTime)
	}
	if in.LastSuccessTime != nil {
		in.LastSuccessTime = &metav1.Time{}
		in.LastSuccessTime.DeepCopyInto(out.LastSuccessTime)
	}
	if in.ActiveJobs != nil {
		in.ActiveJobs = make([]JobReference, len(in.ActiveJobs))
		for i := range in.ActiveJobs {
			in.ActiveJobs[i].DeepCopyInto(&out.ActiveJobs[i])
		}
	}
	if in.ExecutionHistory != nil {
		in.ExecutionHistory = make([]WorkflowExecution, len(in.ExecutionHistory))
		for i := range in.ExecutionHistory {
			in.ExecutionHistory[i].DeepCopyInto(&out.ExecutionHistory[i])
		}
	}
	if in.StepStatuses != nil {
		in.StepStatuses = make(map[string]StepStatus)
		for key, val := range in.StepStatuses {
			in.StepStatuses[key] = val
		}
	}
}

// DeepCopy is a deepcopy function, copying the receiver, creating a new WorkflowStatus.
func (in *WorkflowStatus) DeepCopy() *WorkflowStatus {
	if in == nil {
		return nil
	}
	out := new(WorkflowStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is a deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WorkflowCondition) DeepCopyInto(out *WorkflowCondition) {
	*out = *in
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

// DeepCopy is a deepcopy function, copying the receiver, creating a new WorkflowCondition.
func (in *WorkflowCondition) DeepCopy() *WorkflowCondition {
	if in == nil {
		return nil
	}
	out := new(WorkflowCondition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is a deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *JobReference) DeepCopyInto(out *JobReference) {
	*out = *in
	if in.StartTime != nil {
		in.StartTime = &metav1.Time{}
		in.StartTime.DeepCopyInto(out.StartTime)
	}
}

// DeepCopy is a deepcopy function, copying the receiver, creating a new JobReference.
func (in *JobReference) DeepCopy() *JobReference {
	if in == nil {
		return nil
	}
	out := new(JobReference)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is a deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *WorkflowExecution) DeepCopyInto(out *WorkflowExecution) {
	*out = *in
	in.StartTime.DeepCopyInto(&out.StartTime)
	if in.EndTime != nil {
		in.EndTime = &metav1.Time{}
		in.EndTime.DeepCopyInto(out.EndTime)
	}
	if in.Duration != nil {
		in.Duration = &metav1.Duration{}
		*out.Duration = *in.Duration
	}
	if in.JobReference != nil {
		in.JobReference = &JobReference{}
		in.JobReference.DeepCopyInto(out.JobReference)
	}
	if in.StepResults != nil {
		in.StepResults = make(map[string]StepResult)
		for key, val := range in.StepResults {
			in.StepResults[key] = val
		}
	}
}

// DeepCopy is a deepcopy function, copying the receiver, creating a new WorkflowExecution.
func (in *WorkflowExecution) DeepCopy() *WorkflowExecution {
	if in == nil {
		return nil
	}
	out := new(WorkflowExecution)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is a deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *StepStatus) DeepCopyInto(out *StepStatus) {
	*out = *in
	if in.StartTime != nil {
		in.StartTime = &metav1.Time{}
		in.StartTime.DeepCopyInto(out.StartTime)
	}
	if in.EndTime != nil {
		in.EndTime = &metav1.Time{}
		in.EndTime.DeepCopyInto(out.EndTime)
	}
	if in.Output != nil {
		in.Output = &runtime.RawExtension{}
		in.Output.DeepCopyInto(out.Output)
	}
}

// DeepCopy is a deepcopy function, copying the receiver, creating a new StepStatus.
func (in *StepStatus) DeepCopy() *StepStatus {
	if in == nil {
		return nil
	}
	out := new(StepStatus)
	in.DeepCopyInto(out)
	return out
}