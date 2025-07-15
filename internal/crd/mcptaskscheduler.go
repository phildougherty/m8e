// internal/crd/mcptaskscheduler.go
package crd

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

// GroupVersionKind returns the GroupVersionKind for MCPTaskScheduler
func (m *MCPTaskScheduler) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   GroupName,
		Version: Version,
		Kind:    "MCPTaskScheduler",
	}
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

	// Event-driven workflow triggers
	EventTriggers []EventTrigger `json:"eventTriggers,omitempty"`

	// Conditional dependency configuration
	ConditionalDependencies ConditionalDependencyConfig `json:"conditionalDependencies,omitempty"`

	// Auto-scaling configuration
	AutoScaling AutoScalingConfig `json:"autoScaling,omitempty"`
}

// TaskRetryPolicy defines retry behavior for failed tasks
type TaskRetryPolicy struct {
	MaxRetries      int32  `json:"maxRetries,omitempty"`
	RetryDelay      string `json:"retryDelay,omitempty"`
	BackoffStrategy string `json:"backoffStrategy,omitempty"`
}

// EventTrigger defines event-driven workflow triggers
type EventTrigger struct {
	// Type of event trigger (k8s-event, webhook, file-watch, etc.)
	Type string `json:"type"`

	// Name of the trigger for identification
	Name string `json:"name"`

	// Workflow to trigger when event occurs
	Workflow string `json:"workflow"`

	// Kubernetes event configuration
	KubernetesEvent *KubernetesEventConfig `json:"kubernetesEvent,omitempty"`

	// Webhook configuration
	Webhook *WebhookConfig `json:"webhook,omitempty"`

	// File watch configuration
	FileWatch *FileWatchConfig `json:"fileWatch,omitempty"`

	// Conditions for triggering
	Conditions []TriggerCondition `json:"conditions,omitempty"`

	// Cooldown period to prevent rapid triggering
	CooldownDuration string `json:"cooldownDuration,omitempty"`
}

// KubernetesEventConfig defines configuration for Kubernetes event watching
type KubernetesEventConfig struct {
	// Resource kind to watch (Pod, Service, etc.)
	Kind string `json:"kind"`

	// Event reason filter
	Reason string `json:"reason,omitempty"`

	// Namespace filter
	Namespace string `json:"namespace,omitempty"`

	// Label selector
	LabelSelector string `json:"labelSelector,omitempty"`

	// Field selector
	FieldSelector string `json:"fieldSelector,omitempty"`
}

// WebhookConfig defines webhook trigger configuration
type WebhookConfig struct {
	// Endpoint path for webhook
	Endpoint string `json:"endpoint"`

	// Authentication method (bearer-token, basic, etc.)
	Authentication string `json:"authentication,omitempty"`

	// HTTP method (POST, GET, etc.)
	Method string `json:"method,omitempty"`

	// Expected headers
	Headers map[string]string `json:"headers,omitempty"`
}

// FileWatchConfig defines file watching configuration
type FileWatchConfig struct {
	// Path to watch
	Path string `json:"path"`

	// File pattern to match
	Pattern string `json:"pattern,omitempty"`

	// Watch mode (create, modify, delete, all)
	Events []string `json:"events,omitempty"`

	// Recursive watching
	Recursive bool `json:"recursive,omitempty"`
}

// TriggerCondition defines conditions for event triggering
type TriggerCondition struct {
	// Field to check
	Field string `json:"field"`

	// Operator (equals, contains, regex, etc.)
	Operator string `json:"operator"`

	// Value to compare against
	Value string `json:"value"`
}

// ConditionalDependencyConfig defines conditional dependency configuration
type ConditionalDependencyConfig struct {
	// Enable conditional dependencies
	Enabled bool `json:"enabled,omitempty"`

	// Default dependency resolution strategy
	DefaultStrategy string `json:"defaultStrategy,omitempty"`

	// Timeout for dependency resolution
	ResolutionTimeout string `json:"resolutionTimeout,omitempty"`

	// Enable cross-workflow dependencies
	CrossWorkflowEnabled bool `json:"crossWorkflowEnabled,omitempty"`
}

// AutoScalingConfig defines auto-scaling configuration
type AutoScalingConfig struct {
	// Enable auto-scaling
	Enabled bool `json:"enabled,omitempty"`

	// Minimum concurrent tasks
	MinConcurrentTasks int32 `json:"minConcurrentTasks,omitempty"`

	// Maximum concurrent tasks
	MaxConcurrentTasks int32 `json:"maxConcurrentTasks,omitempty"`

	// Target CPU utilization percentage
	TargetCPUUtilization int32 `json:"targetCPUUtilization,omitempty"`

	// Target memory utilization percentage
	TargetMemoryUtilization int32 `json:"targetMemoryUtilization,omitempty"`

	// Scale up cooldown period
	ScaleUpCooldown string `json:"scaleUpCooldown,omitempty"`

	// Scale down cooldown period
	ScaleDownCooldown string `json:"scaleDownCooldown,omitempty"`

	// Metrics collection interval
	MetricsInterval string `json:"metricsInterval,omitempty"`

	// Custom metrics for scaling decisions
	CustomMetrics []CustomMetric `json:"customMetrics,omitempty"`
}

// CustomMetric defines custom metrics for auto-scaling
type CustomMetric struct {
	// Name of the metric
	Name string `json:"name"`

	// Metric type (resource, pods, object, external)
	Type string `json:"type"`

	// Target value for the metric
	TargetValue string `json:"targetValue"`

	// Metric selector
	Selector map[string]string `json:"selector,omitempty"`
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

// GroupVersionKind returns the GroupVersionKind for MCPTaskSchedulerList
func (m *MCPTaskSchedulerList) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   GroupName,
		Version: Version,
		Kind:    "MCPTaskSchedulerList",
	}
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
	if in.EventTriggers != nil {
		in, out := &in.EventTriggers, &out.EventTriggers
		*out = make([]EventTrigger, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	in.ConditionalDependencies.DeepCopyInto(&out.ConditionalDependencies)
	in.AutoScaling.DeepCopyInto(&out.AutoScaling)
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

// DeepCopy methods for new structs
func (in *EventTrigger) DeepCopyInto(out *EventTrigger) {
	*out = *in
	if in.KubernetesEvent != nil {
		in, out := &in.KubernetesEvent, &out.KubernetesEvent
		*out = new(KubernetesEventConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.Webhook != nil {
		in, out := &in.Webhook, &out.Webhook
		*out = new(WebhookConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.FileWatch != nil {
		in, out := &in.FileWatch, &out.FileWatch
		*out = new(FileWatchConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]TriggerCondition, len(*in))
		copy(*out, *in)
	}
}

func (in *EventTrigger) DeepCopy() *EventTrigger {
	if in == nil {
		return nil
	}
	out := new(EventTrigger)
	in.DeepCopyInto(out)
	return out
}

func (in *KubernetesEventConfig) DeepCopyInto(out *KubernetesEventConfig) {
	*out = *in
}

func (in *KubernetesEventConfig) DeepCopy() *KubernetesEventConfig {
	if in == nil {
		return nil
	}
	out := new(KubernetesEventConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *WebhookConfig) DeepCopyInto(out *WebhookConfig) {
	*out = *in
	if in.Headers != nil {
		in, out := &in.Headers, &out.Headers
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

func (in *WebhookConfig) DeepCopy() *WebhookConfig {
	if in == nil {
		return nil
	}
	out := new(WebhookConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *FileWatchConfig) DeepCopyInto(out *FileWatchConfig) {
	*out = *in
	if in.Events != nil {
		in, out := &in.Events, &out.Events
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

func (in *FileWatchConfig) DeepCopy() *FileWatchConfig {
	if in == nil {
		return nil
	}
	out := new(FileWatchConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *TriggerCondition) DeepCopyInto(out *TriggerCondition) {
	*out = *in
}

func (in *TriggerCondition) DeepCopy() *TriggerCondition {
	if in == nil {
		return nil
	}
	out := new(TriggerCondition)
	in.DeepCopyInto(out)
	return out
}

func (in *ConditionalDependencyConfig) DeepCopyInto(out *ConditionalDependencyConfig) {
	*out = *in
}

func (in *ConditionalDependencyConfig) DeepCopy() *ConditionalDependencyConfig {
	if in == nil {
		return nil
	}
	out := new(ConditionalDependencyConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *AutoScalingConfig) DeepCopyInto(out *AutoScalingConfig) {
	*out = *in
	if in.CustomMetrics != nil {
		in, out := &in.CustomMetrics, &out.CustomMetrics
		*out = make([]CustomMetric, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *AutoScalingConfig) DeepCopy() *AutoScalingConfig {
	if in == nil {
		return nil
	}
	out := new(AutoScalingConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *CustomMetric) DeepCopyInto(out *CustomMetric) {
	*out = *in
	if in.Selector != nil {
		in, out := &in.Selector, &out.Selector
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

func (in *CustomMetric) DeepCopy() *CustomMetric {
	if in == nil {
		return nil
	}
	out := new(CustomMetric)
	in.DeepCopyInto(out)
	return out
}