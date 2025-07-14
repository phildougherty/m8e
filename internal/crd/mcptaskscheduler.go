// internal/crd/mcptaskscheduler.go
package crd

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// MCPTaskScheduler represents a task scheduler deployment
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=mcpts
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Tasks",type="integer",JSONPath=".status.taskStats.runningTasks"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type MCPTaskScheduler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPTaskSchedulerSpec   `json:"spec,omitempty"`
	Status MCPTaskSchedulerStatus `json:"status,omitempty"`
}

// MCPTaskSchedulerSpec defines the desired state of MCPTaskScheduler
type MCPTaskSchedulerSpec struct {
	// Container configuration
	Image       string            `json:"image,omitempty"`
	Command     []string          `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	WorkingDir  string            `json:"workingDir,omitempty"`

	// Task scheduler specific configuration
	Port         int32  `json:"port,omitempty"`
	Host         string `json:"host,omitempty"`
	DatabasePath string `json:"databasePath,omitempty"`
	LogLevel     string `json:"logLevel,omitempty"`

	// AI/LLM integration
	OpenRouterAPIKey string `json:"openRouterAPIKey,omitempty"`
	OpenRouterModel  string `json:"openRouterModel,omitempty"`
	OllamaURL        string `json:"ollamaURL,omitempty"`
	OllamaModel      string `json:"ollamaModel,omitempty"`

	// MCP integration
	MCPProxyURL    string `json:"mcpProxyURL,omitempty"`
	MCPProxyAPIKey string `json:"mcpProxyAPIKey,omitempty"`

	// OpenWebUI integration
	OpenWebUIEnabled bool `json:"openWebUIEnabled,omitempty"`

	// Workspace configuration
	Workspace string `json:"workspace,omitempty"`

	// Custom volumes
	Volumes []string `json:"volumes,omitempty"`

	// Resource requirements
	Resources ResourceRequirements `json:"resources,omitempty"`

	// Security configuration
	Security *SecurityConfig `json:"security,omitempty"`

	// Deployment configuration
	Replicas      *int32            `json:"replicas,omitempty"`
	NodeSelector  map[string]string `json:"nodeSelector,omitempty"`
	Tolerations   []Toleration      `json:"tolerations,omitempty"`
	Affinity      *Affinity         `json:"affinity,omitempty"`

	// Service configuration
	ServiceType        string            `json:"serviceType,omitempty"`
	ServiceAnnotations map[string]string `json:"serviceAnnotations,omitempty"`

	// Storage configuration
	StorageClass string `json:"storageClass,omitempty"`

	// Kubernetes-specific configuration
	ServiceAccount   string            `json:"serviceAccount,omitempty"`
	ImagePullSecrets []string          `json:"imagePullSecrets,omitempty"`
	PodAnnotations   map[string]string `json:"podAnnotations,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`

	// Scheduling configuration
	SchedulerConfig TaskSchedulerConfig `json:"schedulerConfig,omitempty"`
}

// TaskSchedulerConfig defines task scheduler specific settings
type TaskSchedulerConfig struct {
	// Default timeout for tasks
	DefaultTimeout string `json:"defaultTimeout,omitempty"`

	// Maximum concurrent tasks
	MaxConcurrentTasks int32 `json:"maxConcurrentTasks,omitempty"`

	// Task retry configuration
	RetryPolicy TaskRetryPolicy `json:"retryPolicy,omitempty"`

	// Activity webhook configuration
	ActivityWebhook string `json:"activityWebhook,omitempty"`

	// Task storage configuration
	TaskStorageEnabled bool   `json:"taskStorageEnabled,omitempty"`
	TaskHistoryLimit   int32  `json:"taskHistoryLimit,omitempty"`
	TaskCleanupPolicy  string `json:"taskCleanupPolicy,omitempty"`
}

// TaskRetryPolicy defines retry behavior for failed tasks
type TaskRetryPolicy struct {
	MaxRetries      int32  `json:"maxRetries,omitempty"`
	RetryDelay      string `json:"retryDelay,omitempty"`
	BackoffStrategy string `json:"backoffStrategy,omitempty"`
}

// MCPTaskSchedulerStatus defines the observed state of MCPTaskScheduler
type MCPTaskSchedulerStatus struct {
	// Phase represents the current phase of the MCPTaskScheduler
	Phase MCPTaskSchedulerPhase `json:"phase,omitempty"`

	// Conditions represent the latest available observations of an object's state
	Conditions []MCPTaskSchedulerCondition `json:"conditions,omitempty"`

	// Connection information
	ConnectionInfo *ConnectionInfo `json:"connectionInfo,omitempty"`

	// Health status
	HealthStatus string `json:"healthStatus,omitempty"`

	// Task statistics
	TaskStats TaskStatistics `json:"taskStats,omitempty"`

	// Deployment status
	Replicas      int32 `json:"replicas,omitempty"`
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Service information
	ServiceEndpoints []ServiceEndpoint `json:"serviceEndpoints,omitempty"`

	// Observed generation
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Last successful configuration
	LastSuccessfulConfig string `json:"lastSuccessfulConfig,omitempty"`
}

// TaskStatistics represents task execution statistics
type TaskStatistics struct {
	TotalTasks      int64 `json:"totalTasks,omitempty"`
	CompletedTasks  int64 `json:"completedTasks,omitempty"`
	FailedTasks     int64 `json:"failedTasks,omitempty"`
	RunningTasks    int64 `json:"runningTasks,omitempty"`
	ScheduledTasks  int64 `json:"scheduledTasks,omitempty"`
	LastTaskTime    string `json:"lastTaskTime,omitempty"`
	AverageTaskTime string `json:"averageTaskTime,omitempty"`
}

// MCPTaskSchedulerPhase represents the phase of an MCPTaskScheduler
type MCPTaskSchedulerPhase string

const (
	MCPTaskSchedulerPhasePending      MCPTaskSchedulerPhase = "Pending"
	MCPTaskSchedulerPhaseCreating     MCPTaskSchedulerPhase = "Creating"
	MCPTaskSchedulerPhaseStarting     MCPTaskSchedulerPhase = "Starting"
	MCPTaskSchedulerPhaseRunning      MCPTaskSchedulerPhase = "Running"
	MCPTaskSchedulerPhaseFailed       MCPTaskSchedulerPhase = "Failed"
	MCPTaskSchedulerPhaseTerminating  MCPTaskSchedulerPhase = "Terminating"
	MCPTaskSchedulerPhaseUpgrading    MCPTaskSchedulerPhase = "Upgrading"
)

// MCPTaskSchedulerCondition describes the state of an MCPTaskScheduler at a certain point
type MCPTaskSchedulerCondition struct {
	// Type of MCPTaskScheduler condition
	Type MCPTaskSchedulerConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown
	Status metav1.ConditionStatus `json:"status"`
	// Last time the condition transitioned from one status to another
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// The reason for the condition's last transition
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition
	Message string `json:"message,omitempty"`
}

// MCPTaskSchedulerConditionType represents a MCPTaskScheduler condition value
type MCPTaskSchedulerConditionType string

const (
	// MCPTaskSchedulerConditionReady indicates whether the scheduler is ready to schedule tasks
	MCPTaskSchedulerConditionReady MCPTaskSchedulerConditionType = "Ready"
	// MCPTaskSchedulerConditionHealthy indicates whether the scheduler is healthy
	MCPTaskSchedulerConditionHealthy MCPTaskSchedulerConditionType = "Healthy"
	// MCPTaskSchedulerConditionDatabaseReady indicates whether the database is ready
	MCPTaskSchedulerConditionDatabaseReady MCPTaskSchedulerConditionType = "DatabaseReady"
	// MCPTaskSchedulerConditionLLMConnected indicates whether LLM services are connected
	MCPTaskSchedulerConditionLLMConnected MCPTaskSchedulerConditionType = "LLMConnected"
	// MCPTaskSchedulerConditionMCPConnected indicates whether MCP proxy is connected
	MCPTaskSchedulerConditionMCPConnected MCPTaskSchedulerConditionType = "MCPConnected"
)

// MCPTaskSchedulerList contains a list of MCPTaskScheduler
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type MCPTaskSchedulerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPTaskScheduler `json:"items"`
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MCPTaskScheduler) DeepCopyInto(out *MCPTaskScheduler) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MCPTaskScheduler.
func (in *MCPTaskScheduler) DeepCopy() *MCPTaskScheduler {
	if in == nil {
		return nil
	}
	out := new(MCPTaskScheduler)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *MCPTaskScheduler) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MCPTaskSchedulerList) DeepCopyInto(out *MCPTaskSchedulerList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]MCPTaskScheduler, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MCPTaskSchedulerList.
func (in *MCPTaskSchedulerList) DeepCopy() *MCPTaskSchedulerList {
	if in == nil {
		return nil
	}
	out := new(MCPTaskSchedulerList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *MCPTaskSchedulerList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopy methods for nested structs
func (in *MCPTaskSchedulerSpec) DeepCopyInto(out *MCPTaskSchedulerSpec) {
	*out = *in
	if in.Command != nil {
		in, out := &in.Command, &out.Command
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Args != nil {
		in, out := &in.Args, &out.Args
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Env != nil {
		in, out := &in.Env, &out.Env
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Volumes != nil {
		in, out := &in.Volumes, &out.Volumes
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Replicas != nil {
		in, out := &in.Replicas, &out.Replicas
		*out = new(int32)
		**out = **in
	}
	if in.NodeSelector != nil {
		in, out := &in.NodeSelector, &out.NodeSelector
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.ServiceAnnotations != nil {
		in, out := &in.ServiceAnnotations, &out.ServiceAnnotations
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.ImagePullSecrets != nil {
		in, out := &in.ImagePullSecrets, &out.ImagePullSecrets
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.PodAnnotations != nil {
		in, out := &in.PodAnnotations, &out.PodAnnotations
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Labels != nil {
		in, out := &in.Labels, &out.Labels
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	in.Resources.DeepCopyInto(&out.Resources)
	if in.Security != nil {
		in, out := &in.Security, &out.Security
		*out = new(SecurityConfig)
		(*in).DeepCopyInto(*out)
	}
	in.SchedulerConfig.DeepCopyInto(&out.SchedulerConfig)
}

func (in *MCPTaskSchedulerSpec) DeepCopy() *MCPTaskSchedulerSpec {
	if in == nil {
		return nil
	}
	out := new(MCPTaskSchedulerSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *MCPTaskSchedulerStatus) DeepCopyInto(out *MCPTaskSchedulerStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]MCPTaskSchedulerCondition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.ConnectionInfo != nil {
		in, out := &in.ConnectionInfo, &out.ConnectionInfo
		*out = new(ConnectionInfo)
		**out = **in
	}
	in.TaskStats.DeepCopyInto(&out.TaskStats)
	if in.ServiceEndpoints != nil {
		in, out := &in.ServiceEndpoints, &out.ServiceEndpoints
		*out = make([]ServiceEndpoint, len(*in))
		copy(*out, *in)
	}
}

func (in *MCPTaskSchedulerStatus) DeepCopy() *MCPTaskSchedulerStatus {
	if in == nil {
		return nil
	}
	out := new(MCPTaskSchedulerStatus)
	in.DeepCopyInto(out)
	return out
}

func (in *MCPTaskSchedulerCondition) DeepCopyInto(out *MCPTaskSchedulerCondition) {
	*out = *in
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

func (in *MCPTaskSchedulerCondition) DeepCopy() *MCPTaskSchedulerCondition {
	if in == nil {
		return nil
	}
	out := new(MCPTaskSchedulerCondition)
	in.DeepCopyInto(out)
	return out
}

func (in *TaskSchedulerConfig) DeepCopyInto(out *TaskSchedulerConfig) {
	*out = *in
	in.RetryPolicy.DeepCopyInto(&out.RetryPolicy)
}

func (in *TaskSchedulerConfig) DeepCopy() *TaskSchedulerConfig {
	if in == nil {
		return nil
	}
	out := new(TaskSchedulerConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *TaskRetryPolicy) DeepCopyInto(out *TaskRetryPolicy) {
	*out = *in
}

func (in *TaskRetryPolicy) DeepCopy() *TaskRetryPolicy {
	if in == nil {
		return nil
	}
	out := new(TaskRetryPolicy)
	in.DeepCopyInto(out)
	return out
}

func (in *TaskStatistics) DeepCopyInto(out *TaskStatistics) {
	*out = *in
}

func (in *TaskStatistics) DeepCopy() *TaskStatistics {
	if in == nil {
		return nil
	}
	out := new(TaskStatistics)
	in.DeepCopyInto(out)
	return out
}

func (in *SecurityConfig) DeepCopyInto(out *SecurityConfig) {
	*out = *in
	if in.AllowHostMounts != nil {
		in, out := &in.AllowHostMounts, &out.AllowHostMounts
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.RunAsUser != nil {
		in, out := &in.RunAsUser, &out.RunAsUser
		*out = new(int64)
		**out = **in
	}
	if in.RunAsGroup != nil {
		in, out := &in.RunAsGroup, &out.RunAsGroup
		*out = new(int64)
		**out = **in
	}
	if in.CapAdd != nil {
		in, out := &in.CapAdd, &out.CapAdd
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.CapDrop != nil {
		in, out := &in.CapDrop, &out.CapDrop
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

func (in *SecurityConfig) DeepCopy() *SecurityConfig {
	if in == nil {
		return nil
	}
	out := new(SecurityConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *ResourceRequirements) DeepCopyInto(out *ResourceRequirements) {
	*out = *in
	if in.Limits != nil {
		in, out := &in.Limits, &out.Limits
		*out = make(ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Requests != nil {
		in, out := &in.Requests, &out.Requests
		*out = make(ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

func (in *ResourceRequirements) DeepCopy() *ResourceRequirements {
	if in == nil {
		return nil
	}
	out := new(ResourceRequirements)
	in.DeepCopyInto(out)
	return out
}