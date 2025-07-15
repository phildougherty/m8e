// internal/crd/mcptoolbox.go
package crd

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// MCPToolbox represents a collection of MCP servers working together as a cohesive AI workflow
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=toolbox
// +kubebuilder:printcolumn:name="Template",type="string",JSONPath=".spec.template"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Servers",type="integer",JSONPath=".status.serverCount"
// +kubebuilder:printcolumn:name="Ready",type="integer",JSONPath=".status.readyServers"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type MCPToolbox struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPToolboxSpec   `json:"spec,omitempty"`
	Status MCPToolboxStatus `json:"status,omitempty"`
}

// GroupVersionKind returns the GroupVersionKind for MCPToolbox
func (m *MCPToolbox) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   GroupName,
		Version: Version,
		Kind:    "MCPToolbox",
	}
}

// MCPToolboxSpec defines the desired state of MCPToolbox
type MCPToolboxSpec struct {
	// Template defines the pre-built toolbox configuration to use
	// Examples: "coding-assistant", "rag-stack", "research-agent", "content-creator"
	Template string `json:"template,omitempty"`

	// Description provides a human-readable description of this toolbox
	Description string `json:"description,omitempty"`

	// Servers defines the MCP servers to include in this toolbox
	Servers map[string]ToolboxServerSpec `json:"servers,omitempty"`

	// Dependencies defines the startup order and health dependencies between servers
	Dependencies []ToolboxDependency `json:"dependencies,omitempty"`

	// Collaboration defines team access and sharing settings
	Collaboration *CollaborationConfig `json:"collaboration,omitempty"`

	// OAuth defines OAuth client integration for this toolbox
	OAuth *ToolboxOAuthConfig `json:"oauth,omitempty"`

	// Resources defines toolbox-level resource limits and requests
	Resources *ToolboxResourceConfig `json:"resources,omitempty"`

	// Security defines toolbox-wide security settings
	Security *ToolboxSecurityConfig `json:"security,omitempty"`

	// Monitoring defines observability and alerting configuration
	Monitoring *ToolboxMonitoringConfig `json:"monitoring,omitempty"`

	// Networking defines how servers communicate within the toolbox
	Networking *ToolboxNetworkingConfig `json:"networking,omitempty"`

	// Persistence defines shared storage and data persistence
	Persistence *ToolboxPersistenceConfig `json:"persistence,omitempty"`

	// AutoScaling defines automatic scaling behavior for the toolbox
	AutoScaling *ToolboxAutoScalingConfig `json:"autoScaling,omitempty"`
}

// ToolboxServerSpec defines an MCP server within a toolbox
type ToolboxServerSpec struct {
	// ServerTemplate allows using a pre-defined server configuration
	ServerTemplate string `json:"serverTemplate,omitempty"`

	// Enabled controls whether this server should be deployed
	Enabled *bool `json:"enabled,omitempty"`

	// MCPServerSpec embeds the full server specification
	MCPServerSpec `json:",inline"`

	// ToolboxRole defines the role of this server within the toolbox workflow
	ToolboxRole string `json:"toolboxRole,omitempty"`

	// Priority defines startup order (lower numbers start first)
	Priority int32 `json:"priority,omitempty"`

	// HealthChecks defines custom health checking for this server
	HealthChecks *ServerHealthCheckConfig `json:"healthChecks,omitempty"`

	// Scaling defines server-specific scaling settings
	Scaling *ServerScalingConfig `json:"scaling,omitempty"`
}

// ToolboxDependency defines a dependency relationship between servers
type ToolboxDependency struct {
	// Server is the name of the server that has the dependency
	Server string `json:"server"`

	// DependsOn lists the servers that must be ready before this server starts
	DependsOn []string `json:"dependsOn"`

	// WaitTimeout defines how long to wait for dependencies
	WaitTimeout *metav1.Duration `json:"waitTimeout,omitempty"`

	// Optional indicates whether this dependency is optional
	Optional bool `json:"optional,omitempty"`
}

// CollaborationConfig defines team access and sharing settings
type CollaborationConfig struct {
	// SharedWith defines teams or users who have access to this toolbox
	SharedWith []string `json:"sharedWith,omitempty"`

	// Permissions defines the default permissions for shared access
	Permissions []string `json:"permissions,omitempty"`

	// AllowGuests indicates whether guest users can access this toolbox
	AllowGuests bool `json:"allowGuests,omitempty"`

	// RequireApproval indicates whether access requests require approval
	RequireApproval bool `json:"requireApproval,omitempty"`

	// AccessExpiry defines when shared access expires
	AccessExpiry *metav1.Duration `json:"accessExpiry,omitempty"`
}

// ToolboxOAuthConfig defines OAuth integration for the toolbox
type ToolboxOAuthConfig struct {
	// Enabled controls whether OAuth is enabled for this toolbox
	Enabled bool `json:"enabled,omitempty"`

	// ClientID references an OAuth client for this toolbox
	ClientID string `json:"clientID,omitempty"`

	// RequiredScopes defines the minimum scopes required to access this toolbox
	RequiredScopes []string `json:"requiredScopes,omitempty"`

	// OptionalScopes defines additional scopes that may be requested
	OptionalScopes []string `json:"optionalScopes,omitempty"`

	// AllowAPIKey indicates whether API key authentication is allowed as fallback
	AllowAPIKey bool `json:"allowAPIKey,omitempty"`

	// TokenExpiry defines custom token expiration for this toolbox
	TokenExpiry *metav1.Duration `json:"tokenExpiry,omitempty"`
}

// ToolboxResourceConfig defines toolbox-level resource configuration
type ToolboxResourceConfig struct {
	// TotalLimits defines the total resource limits for all servers in the toolbox
	TotalLimits ResourceList `json:"totalLimits,omitempty"`

	// TotalRequests defines the total resource requests for all servers in the toolbox
	TotalRequests ResourceList `json:"totalRequests,omitempty"`

	// DefaultServerLimits defines default limits for servers without explicit limits
	DefaultServerLimits ResourceList `json:"defaultServerLimits,omitempty"`

	// PriorityClass defines the priority class for all servers in the toolbox
	PriorityClass string `json:"priorityClass,omitempty"`
}

// ToolboxSecurityConfig defines toolbox-wide security settings
type ToolboxSecurityConfig struct {
	// NetworkPolicy defines network isolation policies
	NetworkPolicy *NetworkPolicyConfig `json:"networkPolicy,omitempty"`

	// PodSecurityStandards defines pod security standard compliance
	PodSecurityStandards string `json:"podSecurityStandards,omitempty"` // "restricted", "baseline", "privileged"

	// ImagePullPolicy defines the image pull policy for all servers
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// AllowPrivilegedServers indicates whether any server in the toolbox can run privileged
	AllowPrivilegedServers bool `json:"allowPrivilegedServers,omitempty"`

	// RequiredImageSignatures indicates whether all images must be signed
	RequiredImageSignatures bool `json:"requiredImageSignatures,omitempty"`
}

// NetworkPolicyConfig defines network isolation settings
type NetworkPolicyConfig struct {
	// Enabled controls whether network policies are applied
	Enabled bool `json:"enabled,omitempty"`

	// AllowInbound defines allowed inbound traffic rules
	AllowInbound []NetworkPolicyRule `json:"allowInbound,omitempty"`

	// AllowOutbound defines allowed outbound traffic rules
	AllowOutbound []NetworkPolicyRule `json:"allowOutbound,omitempty"`

	// IsolationMode defines the level of network isolation
	IsolationMode string `json:"isolationMode,omitempty"` // "strict", "moderate", "permissive"
}

// NetworkPolicyRule defines a network policy rule
type NetworkPolicyRule struct {
	// From defines the source of allowed traffic
	From []NetworkPolicyPeer `json:"from,omitempty"`

	// To defines the destination of allowed traffic
	To []NetworkPolicyPeer `json:"to,omitempty"`

	// Ports defines the allowed ports
	Ports []NetworkPolicyPort `json:"ports,omitempty"`
}

// NetworkPolicyPeer defines a network policy peer
type NetworkPolicyPeer struct {
	// PodSelector selects pods by label
	PodSelector *metav1.LabelSelector `json:"podSelector,omitempty"`

	// NamespaceSelector selects namespaces by label
	NamespaceSelector *metav1.LabelSelector `json:"namespaceSelector,omitempty"`

	// IPBlock defines an IP block
	IPBlock *IPBlock `json:"ipBlock,omitempty"`
}

// NetworkPolicyPort defines a network policy port
type NetworkPolicyPort struct {
	// Protocol defines the protocol (TCP, UDP, SCTP)
	Protocol string `json:"protocol,omitempty"`

	// Port defines the port number or name
	Port string `json:"port,omitempty"`
}

// IPBlock defines an IP block for network policies
type IPBlock struct {
	// CIDR defines the IP range
	CIDR string `json:"cidr"`

	// Except defines IP ranges to exclude
	Except []string `json:"except,omitempty"`
}

// ToolboxMonitoringConfig defines observability configuration
type ToolboxMonitoringConfig struct {
	// Enabled controls whether monitoring is enabled
	Enabled bool `json:"enabled,omitempty"`

	// MetricsEndpoint defines where to send metrics
	MetricsEndpoint string `json:"metricsEndpoint,omitempty"`

	// LogLevel defines the logging level for all servers
	LogLevel string `json:"logLevel,omitempty"`

	// Alerts defines alerting rules for the toolbox
	Alerts []ToolboxAlert `json:"alerts,omitempty"`

	// Dashboards defines monitoring dashboards to create
	Dashboards []string `json:"dashboards,omitempty"`
}

// ToolboxAlert defines an alerting rule
type ToolboxAlert struct {
	// Name defines the alert name
	Name string `json:"name"`

	// Condition defines the alert condition
	Condition string `json:"condition"`

	// Severity defines the alert severity
	Severity string `json:"severity"`

	// Recipients defines who should receive the alert
	Recipients []string `json:"recipients,omitempty"`
}

// ToolboxNetworkingConfig defines networking within the toolbox
type ToolboxNetworkingConfig struct {
	// ServiceMesh indicates whether to enable service mesh for inter-server communication
	ServiceMesh bool `json:"serviceMesh,omitempty"`

	// LoadBalancing defines load balancing strategy for servers with multiple replicas
	LoadBalancing string `json:"loadBalancing,omitempty"` // "round-robin", "least-connections", "ip-hash"

	// IngressEnabled controls whether to create an ingress for external access
	IngressEnabled bool `json:"ingressEnabled,omitempty"`

	// IngressClass defines the ingress class to use
	IngressClass string `json:"ingressClass,omitempty"`

	// CustomDomain defines a custom domain for the toolbox
	CustomDomain string `json:"customDomain,omitempty"`

	// TLSEnabled controls whether TLS is enabled for external access
	TLSEnabled bool `json:"tlsEnabled,omitempty"`
}

// ToolboxPersistenceConfig defines shared storage configuration
type ToolboxPersistenceConfig struct {
	// SharedVolumes defines volumes shared between servers
	SharedVolumes []SharedVolumeSpec `json:"sharedVolumes,omitempty"`

	// BackupEnabled controls whether backups are enabled
	BackupEnabled bool `json:"backupEnabled,omitempty"`

	// BackupSchedule defines the backup schedule (cron format)
	BackupSchedule string `json:"backupSchedule,omitempty"`

	// RetentionPolicy defines how long to keep backups
	RetentionPolicy string `json:"retentionPolicy,omitempty"`
}

// SharedVolumeSpec defines a shared volume
type SharedVolumeSpec struct {
	// Name defines the volume name
	Name string `json:"name"`

	// Size defines the volume size
	Size string `json:"size"`

	// AccessModes defines the access modes
	AccessModes []string `json:"accessModes,omitempty"`

	// StorageClass defines the storage class to use
	StorageClass string `json:"storageClass,omitempty"`

	// MountedBy defines which servers mount this volume
	MountedBy []VolumeMountSpec `json:"mountedBy,omitempty"`
}

// VolumeMountSpec defines how a server mounts a shared volume
type VolumeMountSpec struct {
	// Server defines the server name
	Server string `json:"server"`

	// MountPath defines where to mount the volume
	MountPath string `json:"mountPath"`

	// ReadOnly indicates whether the mount is read-only
	ReadOnly bool `json:"readOnly,omitempty"`

	// SubPath defines a sub-path within the volume
	SubPath string `json:"subPath,omitempty"`
}

// ToolboxAutoScalingConfig defines auto-scaling configuration
type ToolboxAutoScalingConfig struct {
	// Enabled controls whether auto-scaling is enabled
	Enabled bool `json:"enabled,omitempty"`

	// MinServers defines the minimum number of server instances
	MinServers int32 `json:"minServers,omitempty"`

	// MaxServers defines the maximum number of server instances
	MaxServers int32 `json:"maxServers,omitempty"`

	// Metrics defines the metrics to use for scaling decisions
	Metrics []AutoScalingMetric `json:"metrics,omitempty"`

	// Behavior defines scaling behavior
	Behavior *AutoScalingBehavior `json:"behavior,omitempty"`
}

// AutoScalingMetric defines a metric for auto-scaling
type AutoScalingMetric struct {
	// Type defines the metric type
	Type string `json:"type"` // "cpu", "memory", "requests", "custom"

	// TargetValue defines the target value for the metric
	TargetValue string `json:"targetValue"`

	// AverageUtilization defines the target average utilization
	AverageUtilization *int32 `json:"averageUtilization,omitempty"`
}

// AutoScalingBehavior defines auto-scaling behavior
type AutoScalingBehavior struct {
	// ScaleUp defines scale-up behavior
	ScaleUp *ScalingPolicy `json:"scaleUp,omitempty"`

	// ScaleDown defines scale-down behavior
	ScaleDown *ScalingPolicy `json:"scaleDown,omitempty"`
}

// ScalingPolicy defines a scaling policy
type ScalingPolicy struct {
	// StabilizationWindowSeconds defines the stabilization window
	StabilizationWindowSeconds *int32 `json:"stabilizationWindowSeconds,omitempty"`

	// Policies defines the scaling policies
	Policies []ScalingPolicyRule `json:"policies,omitempty"`
}

// ScalingPolicyRule defines a scaling policy rule
type ScalingPolicyRule struct {
	// Type defines the policy type
	Type string `json:"type"` // "percent", "pods"

	// Value defines the scaling value
	Value int32 `json:"value"`

	// PeriodSeconds defines the period for this policy
	PeriodSeconds int32 `json:"periodSeconds"`
}

// ServerHealthCheckConfig defines health checking configuration for a server
type ServerHealthCheckConfig struct {
	// HTTPGet defines an HTTP health check
	HTTPGet *HTTPHealthCheck `json:"httpGet,omitempty"`

	// TCPSocket defines a TCP health check
	TCPSocket *TCPHealthCheck `json:"tcpSocket,omitempty"`

	// Exec defines a command-based health check
	Exec *ExecHealthCheck `json:"exec,omitempty"`

	// InitialDelaySeconds defines the initial delay before starting health checks
	InitialDelaySeconds int32 `json:"initialDelaySeconds,omitempty"`

	// PeriodSeconds defines how often to perform the health check
	PeriodSeconds int32 `json:"periodSeconds,omitempty"`

	// TimeoutSeconds defines the timeout for each health check
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`

	// SuccessThreshold defines how many consecutive successes are needed
	SuccessThreshold int32 `json:"successThreshold,omitempty"`

	// FailureThreshold defines how many consecutive failures are needed
	FailureThreshold int32 `json:"failureThreshold,omitempty"`
}

// HTTPHealthCheck defines an HTTP-based health check
type HTTPHealthCheck struct {
	// Path defines the HTTP path to check
	Path string `json:"path"`

	// Port defines the port to check
	Port int32 `json:"port"`

	// Scheme defines the scheme (HTTP or HTTPS)
	Scheme string `json:"scheme,omitempty"`

	// HTTPHeaders defines custom HTTP headers
	HTTPHeaders []HTTPHeader `json:"httpHeaders,omitempty"`
}

// HTTPHeader defines an HTTP header
type HTTPHeader struct {
	// Name defines the header name
	Name string `json:"name"`

	// Value defines the header value
	Value string `json:"value"`
}

// TCPHealthCheck defines a TCP-based health check
type TCPHealthCheck struct {
	// Port defines the port to check
	Port int32 `json:"port"`
}

// ExecHealthCheck defines a command-based health check
type ExecHealthCheck struct {
	// Command defines the command to execute
	Command []string `json:"command"`
}

// ServerScalingConfig defines scaling configuration for a specific server
type ServerScalingConfig struct {
	// MinReplicas defines the minimum number of replicas
	MinReplicas *int32 `json:"minReplicas,omitempty"`

	// MaxReplicas defines the maximum number of replicas
	MaxReplicas *int32 `json:"maxReplicas,omitempty"`

	// TargetCPUUtilization defines the target CPU utilization percentage
	TargetCPUUtilization *int32 `json:"targetCPUUtilization,omitempty"`

	// TargetMemoryUtilization defines the target memory utilization percentage
	TargetMemoryUtilization *int32 `json:"targetMemoryUtilization,omitempty"`
}

// MCPToolboxStatus defines the observed state of MCPToolbox
type MCPToolboxStatus struct {
	// Phase represents the current phase of the MCPToolbox
	Phase MCPToolboxPhase `json:"phase,omitempty"`

	// Conditions represent the latest available observations of the toolbox's state
	Conditions []MCPToolboxCondition `json:"conditions,omitempty"`

	// ServerCount represents the total number of servers in the toolbox
	ServerCount int32 `json:"serverCount,omitempty"`

	// ReadyServers represents the number of servers that are ready
	ReadyServers int32 `json:"readyServers,omitempty"`

	// ServerStatuses provides status for each server in the toolbox
	ServerStatuses map[string]ToolboxServerStatus `json:"serverStatuses,omitempty"`

	// ConnectionInfo provides connection details for accessing the toolbox
	ConnectionInfo *ToolboxConnectionInfo `json:"connectionInfo,omitempty"`

	// ResourceUsage provides current resource usage across all servers
	ResourceUsage *ToolboxResourceUsage `json:"resourceUsage,omitempty"`

	// LastReconcileTime represents when the toolbox was last reconciled
	LastReconcileTime metav1.Time `json:"lastReconcileTime,omitempty"`

	// ObservedGeneration represents the generation observed by the controller
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// MCPToolboxPhase represents the phase of a MCPToolbox
type MCPToolboxPhase string

const (
	MCPToolboxPhasePending     MCPToolboxPhase = "Pending"
	MCPToolboxPhaseCreating    MCPToolboxPhase = "Creating"
	MCPToolboxPhaseStarting    MCPToolboxPhase = "Starting"
	MCPToolboxPhaseRunning     MCPToolboxPhase = "Running"
	MCPToolboxPhaseDegraded    MCPToolboxPhase = "Degraded"
	MCPToolboxPhaseFailed      MCPToolboxPhase = "Failed"
	MCPToolboxPhaseTerminating MCPToolboxPhase = "Terminating"
)

// MCPToolboxCondition describes the state of a MCPToolbox at a certain point
type MCPToolboxCondition struct {
	// Type of MCPToolbox condition
	Type MCPToolboxConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown
	Status metav1.ConditionStatus `json:"status"`
	// Last time the condition transitioned from one status to another
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// The reason for the condition's last transition
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition
	Message string `json:"message,omitempty"`
}

// MCPToolboxConditionType represents a MCPToolbox condition value
type MCPToolboxConditionType string

const (
	// MCPToolboxConditionReady indicates whether the toolbox is ready to serve requests
	MCPToolboxConditionReady MCPToolboxConditionType = "Ready"
	// MCPToolboxConditionServersReady indicates whether all servers are ready
	MCPToolboxConditionServersReady MCPToolboxConditionType = "ServersReady"
	// MCPToolboxConditionDependenciesReady indicates whether all dependencies are satisfied
	MCPToolboxConditionDependenciesReady MCPToolboxConditionType = "DependenciesReady"
	// MCPToolboxConditionHealthy indicates whether the toolbox is healthy
	MCPToolboxConditionHealthy MCPToolboxConditionType = "Healthy"
)

// ToolboxServerStatus represents the status of a server within the toolbox
type ToolboxServerStatus struct {
	// Phase represents the current phase of the server
	Phase MCPServerPhase `json:"phase,omitempty"`

	// Ready indicates whether the server is ready
	Ready bool `json:"ready,omitempty"`

	// Health indicates the health status of the server
	Health string `json:"health,omitempty"`

	// LastHealthCheck represents when the server was last health checked
	LastHealthCheck metav1.Time `json:"lastHealthCheck,omitempty"`

	// Replicas represents the number of replicas for this server
	Replicas int32 `json:"replicas,omitempty"`

	// ReadyReplicas represents the number of ready replicas
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`
}

// ToolboxConnectionInfo provides connection details for the toolbox
type ToolboxConnectionInfo struct {
	// Endpoint provides the main endpoint for accessing the toolbox
	Endpoint string `json:"endpoint,omitempty"`

	// IngressURL provides the ingress URL if enabled
	IngressURL string `json:"ingressURL,omitempty"`

	// ServerEndpoints provides individual server endpoints
	ServerEndpoints map[string]string `json:"serverEndpoints,omitempty"`

	// OAuthEndpoint provides the OAuth endpoint for authentication
	OAuthEndpoint string `json:"oauthEndpoint,omitempty"`
}

// ToolboxResourceUsage provides resource usage information
type ToolboxResourceUsage struct {
	// CPU usage across all servers
	CPU string `json:"cpu,omitempty"`

	// Memory usage across all servers
	Memory string `json:"memory,omitempty"`

	// Storage usage across all servers
	Storage string `json:"storage,omitempty"`

	// NetworkIn shows inbound network traffic
	NetworkIn string `json:"networkIn,omitempty"`

	// NetworkOut shows outbound network traffic
	NetworkOut string `json:"networkOut,omitempty"`
}

// MCPToolboxList contains a list of MCPToolbox
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type MCPToolboxList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPToolbox `json:"items"`
}

// GroupVersionKind returns the GroupVersionKind for MCPToolboxList
func (m *MCPToolboxList) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   GroupName,
		Version: Version,
		Kind:    "MCPToolboxList",
	}
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MCPToolbox) DeepCopyInto(out *MCPToolbox) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MCPToolbox.
func (in *MCPToolbox) DeepCopy() *MCPToolbox {
	if in == nil {
		return nil
	}
	out := new(MCPToolbox)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *MCPToolbox) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MCPToolboxList) DeepCopyInto(out *MCPToolboxList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]MCPToolbox, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MCPToolboxList.
func (in *MCPToolboxList) DeepCopy() *MCPToolboxList {
	if in == nil {
		return nil
	}
	out := new(MCPToolboxList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *MCPToolboxList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// Implement DeepCopy methods for all nested structs
func (in *MCPToolboxSpec) DeepCopyInto(out *MCPToolboxSpec) {
	*out = *in
	if in.Servers != nil {
		in, out := &in.Servers, &out.Servers
		*out = make(map[string]ToolboxServerSpec, len(*in))
		for key, val := range *in {
			(*out)[key] = *val.DeepCopy()
		}
	}
	if in.Dependencies != nil {
		in, out := &in.Dependencies, &out.Dependencies
		*out = make([]ToolboxDependency, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Collaboration != nil {
		in, out := &in.Collaboration, &out.Collaboration
		*out = new(CollaborationConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.OAuth != nil {
		in, out := &in.OAuth, &out.OAuth
		*out = new(ToolboxOAuthConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.Resources != nil {
		in, out := &in.Resources, &out.Resources
		*out = new(ToolboxResourceConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.Security != nil {
		in, out := &in.Security, &out.Security
		*out = new(ToolboxSecurityConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.Monitoring != nil {
		in, out := &in.Monitoring, &out.Monitoring
		*out = new(ToolboxMonitoringConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.Networking != nil {
		in, out := &in.Networking, &out.Networking
		*out = new(ToolboxNetworkingConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.Persistence != nil {
		in, out := &in.Persistence, &out.Persistence
		*out = new(ToolboxPersistenceConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.AutoScaling != nil {
		in, out := &in.AutoScaling, &out.AutoScaling
		*out = new(ToolboxAutoScalingConfig)
		(*in).DeepCopyInto(*out)
	}
}

func (in *MCPToolboxSpec) DeepCopy() *MCPToolboxSpec {
	if in == nil {
		return nil
	}
	out := new(MCPToolboxSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *ToolboxServerSpec) DeepCopyInto(out *ToolboxServerSpec) {
	*out = *in
	if in.Enabled != nil {
		in, out := &in.Enabled, &out.Enabled
		*out = new(bool)
		**out = **in
	}
	in.MCPServerSpec.DeepCopyInto(&out.MCPServerSpec)
	if in.HealthChecks != nil {
		in, out := &in.HealthChecks, &out.HealthChecks
		*out = new(ServerHealthCheckConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.Scaling != nil {
		in, out := &in.Scaling, &out.Scaling
		*out = new(ServerScalingConfig)
		(*in).DeepCopyInto(*out)
	}
}

func (in *ToolboxServerSpec) DeepCopy() *ToolboxServerSpec {
	if in == nil {
		return nil
	}
	out := new(ToolboxServerSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *MCPToolboxStatus) DeepCopyInto(out *MCPToolboxStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]MCPToolboxCondition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.ServerStatuses != nil {
		in, out := &in.ServerStatuses, &out.ServerStatuses
		*out = make(map[string]ToolboxServerStatus, len(*in))
		for key, val := range *in {
			(*out)[key] = *val.DeepCopy()
		}
	}
	if in.ConnectionInfo != nil {
		in, out := &in.ConnectionInfo, &out.ConnectionInfo
		*out = new(ToolboxConnectionInfo)
		(*in).DeepCopyInto(*out)
	}
	if in.ResourceUsage != nil {
		in, out := &in.ResourceUsage, &out.ResourceUsage
		*out = new(ToolboxResourceUsage)
		(*in).DeepCopyInto(*out)
	}
	in.LastReconcileTime.DeepCopyInto(&out.LastReconcileTime)
}

func (in *MCPToolboxStatus) DeepCopy() *MCPToolboxStatus {
	if in == nil {
		return nil
	}
	out := new(MCPToolboxStatus)
	in.DeepCopyInto(out)
	return out
}

func (in *MCPToolboxCondition) DeepCopyInto(out *MCPToolboxCondition) {
	*out = *in
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

func (in *MCPToolboxCondition) DeepCopy() *MCPToolboxCondition {
	if in == nil {
		return nil
	}
	out := new(MCPToolboxCondition)
	in.DeepCopyInto(out)
	return out
}

// Add more DeepCopy methods for other nested structs as needed...
// (In a real implementation, these would be generated by code-generator tools)

func (in *ToolboxDependency) DeepCopyInto(out *ToolboxDependency) {
	*out = *in
	if in.DependsOn != nil {
		in, out := &in.DependsOn, &out.DependsOn
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.WaitTimeout != nil {
		in, out := &in.WaitTimeout, &out.WaitTimeout
		*out = new(metav1.Duration)
		**out = **in
	}
}

func (in *ToolboxDependency) DeepCopy() *ToolboxDependency {
	if in == nil {
		return nil
	}
	out := new(ToolboxDependency)
	in.DeepCopyInto(out)
	return out
}

func (in *CollaborationConfig) DeepCopyInto(out *CollaborationConfig) {
	*out = *in
	if in.SharedWith != nil {
		in, out := &in.SharedWith, &out.SharedWith
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.Permissions != nil {
		in, out := &in.Permissions, &out.Permissions
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.AccessExpiry != nil {
		in, out := &in.AccessExpiry, &out.AccessExpiry
		*out = new(metav1.Duration)
		**out = **in
	}
}

func (in *CollaborationConfig) DeepCopy() *CollaborationConfig {
	if in == nil {
		return nil
	}
	out := new(CollaborationConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *ToolboxOAuthConfig) DeepCopyInto(out *ToolboxOAuthConfig) {
	*out = *in
	if in.RequiredScopes != nil {
		in, out := &in.RequiredScopes, &out.RequiredScopes
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.OptionalScopes != nil {
		in, out := &in.OptionalScopes, &out.OptionalScopes
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.TokenExpiry != nil {
		in, out := &in.TokenExpiry, &out.TokenExpiry
		*out = new(metav1.Duration)
		**out = **in
	}
}

func (in *ToolboxOAuthConfig) DeepCopy() *ToolboxOAuthConfig {
	if in == nil {
		return nil
	}
	out := new(ToolboxOAuthConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *ToolboxResourceConfig) DeepCopyInto(out *ToolboxResourceConfig) {
	*out = *in
	if in.TotalLimits != nil {
		in, out := &in.TotalLimits, &out.TotalLimits
		*out = make(ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.TotalRequests != nil {
		in, out := &in.TotalRequests, &out.TotalRequests
		*out = make(ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.DefaultServerLimits != nil {
		in, out := &in.DefaultServerLimits, &out.DefaultServerLimits
		*out = make(ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

func (in *ToolboxResourceConfig) DeepCopy() *ToolboxResourceConfig {
	if in == nil {
		return nil
	}
	out := new(ToolboxResourceConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *ToolboxSecurityConfig) DeepCopyInto(out *ToolboxSecurityConfig) {
	*out = *in
	if in.NetworkPolicy != nil {
		in, out := &in.NetworkPolicy, &out.NetworkPolicy
		*out = new(NetworkPolicyConfig)
		(*in).DeepCopyInto(*out)
	}
}

func (in *ToolboxSecurityConfig) DeepCopy() *ToolboxSecurityConfig {
	if in == nil {
		return nil
	}
	out := new(ToolboxSecurityConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *NetworkPolicyConfig) DeepCopyInto(out *NetworkPolicyConfig) {
	*out = *in
	if in.AllowInbound != nil {
		in, out := &in.AllowInbound, &out.AllowInbound
		*out = make([]NetworkPolicyRule, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.AllowOutbound != nil {
		in, out := &in.AllowOutbound, &out.AllowOutbound
		*out = make([]NetworkPolicyRule, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *NetworkPolicyConfig) DeepCopy() *NetworkPolicyConfig {
	if in == nil {
		return nil
	}
	out := new(NetworkPolicyConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *NetworkPolicyRule) DeepCopyInto(out *NetworkPolicyRule) {
	*out = *in
	if in.From != nil {
		in, out := &in.From, &out.From
		*out = make([]NetworkPolicyPeer, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.To != nil {
		in, out := &in.To, &out.To
		*out = make([]NetworkPolicyPeer, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Ports != nil {
		in, out := &in.Ports, &out.Ports
		*out = make([]NetworkPolicyPort, len(*in))
		copy(*out, *in)
	}
}

func (in *NetworkPolicyRule) DeepCopy() *NetworkPolicyRule {
	if in == nil {
		return nil
	}
	out := new(NetworkPolicyRule)
	in.DeepCopyInto(out)
	return out
}

func (in *NetworkPolicyPeer) DeepCopyInto(out *NetworkPolicyPeer) {
	*out = *in
	if in.PodSelector != nil {
		in, out := &in.PodSelector, &out.PodSelector
		*out = new(metav1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
	if in.NamespaceSelector != nil {
		in, out := &in.NamespaceSelector, &out.NamespaceSelector
		*out = new(metav1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
	if in.IPBlock != nil {
		in, out := &in.IPBlock, &out.IPBlock
		*out = new(IPBlock)
		(*in).DeepCopyInto(*out)
	}
}

func (in *NetworkPolicyPeer) DeepCopy() *NetworkPolicyPeer {
	if in == nil {
		return nil
	}
	out := new(NetworkPolicyPeer)
	in.DeepCopyInto(out)
	return out
}

func (in *IPBlock) DeepCopyInto(out *IPBlock) {
	*out = *in
	if in.Except != nil {
		in, out := &in.Except, &out.Except
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

func (in *IPBlock) DeepCopy() *IPBlock {
	if in == nil {
		return nil
	}
	out := new(IPBlock)
	in.DeepCopyInto(out)
	return out
}

func (in *ToolboxMonitoringConfig) DeepCopyInto(out *ToolboxMonitoringConfig) {
	*out = *in
	if in.Alerts != nil {
		in, out := &in.Alerts, &out.Alerts
		*out = make([]ToolboxAlert, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Dashboards != nil {
		in, out := &in.Dashboards, &out.Dashboards
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

func (in *ToolboxMonitoringConfig) DeepCopy() *ToolboxMonitoringConfig {
	if in == nil {
		return nil
	}
	out := new(ToolboxMonitoringConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *ToolboxAlert) DeepCopyInto(out *ToolboxAlert) {
	*out = *in
	if in.Recipients != nil {
		in, out := &in.Recipients, &out.Recipients
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

func (in *ToolboxAlert) DeepCopy() *ToolboxAlert {
	if in == nil {
		return nil
	}
	out := new(ToolboxAlert)
	in.DeepCopyInto(out)
	return out
}

func (in *ToolboxNetworkingConfig) DeepCopyInto(out *ToolboxNetworkingConfig) {
	*out = *in
}

func (in *ToolboxNetworkingConfig) DeepCopy() *ToolboxNetworkingConfig {
	if in == nil {
		return nil
	}
	out := new(ToolboxNetworkingConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *ToolboxPersistenceConfig) DeepCopyInto(out *ToolboxPersistenceConfig) {
	*out = *in
	if in.SharedVolumes != nil {
		in, out := &in.SharedVolumes, &out.SharedVolumes
		*out = make([]SharedVolumeSpec, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *ToolboxPersistenceConfig) DeepCopy() *ToolboxPersistenceConfig {
	if in == nil {
		return nil
	}
	out := new(ToolboxPersistenceConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *SharedVolumeSpec) DeepCopyInto(out *SharedVolumeSpec) {
	*out = *in
	if in.AccessModes != nil {
		in, out := &in.AccessModes, &out.AccessModes
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.MountedBy != nil {
		in, out := &in.MountedBy, &out.MountedBy
		*out = make([]VolumeMountSpec, len(*in))
		copy(*out, *in)
	}
}

func (in *SharedVolumeSpec) DeepCopy() *SharedVolumeSpec {
	if in == nil {
		return nil
	}
	out := new(SharedVolumeSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *VolumeMountSpec) DeepCopyInto(out *VolumeMountSpec) {
	*out = *in
}

func (in *VolumeMountSpec) DeepCopy() *VolumeMountSpec {
	if in == nil {
		return nil
	}
	out := new(VolumeMountSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *ToolboxAutoScalingConfig) DeepCopyInto(out *ToolboxAutoScalingConfig) {
	*out = *in
	if in.Metrics != nil {
		in, out := &in.Metrics, &out.Metrics
		*out = make([]AutoScalingMetric, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Behavior != nil {
		in, out := &in.Behavior, &out.Behavior
		*out = new(AutoScalingBehavior)
		(*in).DeepCopyInto(*out)
	}
}

func (in *ToolboxAutoScalingConfig) DeepCopy() *ToolboxAutoScalingConfig {
	if in == nil {
		return nil
	}
	out := new(ToolboxAutoScalingConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *AutoScalingMetric) DeepCopyInto(out *AutoScalingMetric) {
	*out = *in
	if in.AverageUtilization != nil {
		in, out := &in.AverageUtilization, &out.AverageUtilization
		*out = new(int32)
		**out = **in
	}
}

func (in *AutoScalingMetric) DeepCopy() *AutoScalingMetric {
	if in == nil {
		return nil
	}
	out := new(AutoScalingMetric)
	in.DeepCopyInto(out)
	return out
}

func (in *AutoScalingBehavior) DeepCopyInto(out *AutoScalingBehavior) {
	*out = *in
	if in.ScaleUp != nil {
		in, out := &in.ScaleUp, &out.ScaleUp
		*out = new(ScalingPolicy)
		(*in).DeepCopyInto(*out)
	}
	if in.ScaleDown != nil {
		in, out := &in.ScaleDown, &out.ScaleDown
		*out = new(ScalingPolicy)
		(*in).DeepCopyInto(*out)
	}
}

func (in *AutoScalingBehavior) DeepCopy() *AutoScalingBehavior {
	if in == nil {
		return nil
	}
	out := new(AutoScalingBehavior)
	in.DeepCopyInto(out)
	return out
}

func (in *ScalingPolicy) DeepCopyInto(out *ScalingPolicy) {
	*out = *in
	if in.StabilizationWindowSeconds != nil {
		in, out := &in.StabilizationWindowSeconds, &out.StabilizationWindowSeconds
		*out = new(int32)
		**out = **in
	}
	if in.Policies != nil {
		in, out := &in.Policies, &out.Policies
		*out = make([]ScalingPolicyRule, len(*in))
		copy(*out, *in)
	}
}

func (in *ScalingPolicy) DeepCopy() *ScalingPolicy {
	if in == nil {
		return nil
	}
	out := new(ScalingPolicy)
	in.DeepCopyInto(out)
	return out
}

func (in *ScalingPolicyRule) DeepCopyInto(out *ScalingPolicyRule) {
	*out = *in
}

func (in *ScalingPolicyRule) DeepCopy() *ScalingPolicyRule {
	if in == nil {
		return nil
	}
	out := new(ScalingPolicyRule)
	in.DeepCopyInto(out)
	return out
}

func (in *ServerHealthCheckConfig) DeepCopyInto(out *ServerHealthCheckConfig) {
	*out = *in
	if in.HTTPGet != nil {
		in, out := &in.HTTPGet, &out.HTTPGet
		*out = new(HTTPHealthCheck)
		(*in).DeepCopyInto(*out)
	}
	if in.TCPSocket != nil {
		in, out := &in.TCPSocket, &out.TCPSocket
		*out = new(TCPHealthCheck)
		(*in).DeepCopyInto(*out)
	}
	if in.Exec != nil {
		in, out := &in.Exec, &out.Exec
		*out = new(ExecHealthCheck)
		(*in).DeepCopyInto(*out)
	}
}

func (in *ServerHealthCheckConfig) DeepCopy() *ServerHealthCheckConfig {
	if in == nil {
		return nil
	}
	out := new(ServerHealthCheckConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *HTTPHealthCheck) DeepCopyInto(out *HTTPHealthCheck) {
	*out = *in
	if in.HTTPHeaders != nil {
		in, out := &in.HTTPHeaders, &out.HTTPHeaders
		*out = make([]HTTPHeader, len(*in))
		copy(*out, *in)
	}
}

func (in *HTTPHealthCheck) DeepCopy() *HTTPHealthCheck {
	if in == nil {
		return nil
	}
	out := new(HTTPHealthCheck)
	in.DeepCopyInto(out)
	return out
}

func (in *HTTPHeader) DeepCopyInto(out *HTTPHeader) {
	*out = *in
}

func (in *HTTPHeader) DeepCopy() *HTTPHeader {
	if in == nil {
		return nil
	}
	out := new(HTTPHeader)
	in.DeepCopyInto(out)
	return out
}

func (in *TCPHealthCheck) DeepCopyInto(out *TCPHealthCheck) {
	*out = *in
}

func (in *TCPHealthCheck) DeepCopy() *TCPHealthCheck {
	if in == nil {
		return nil
	}
	out := new(TCPHealthCheck)
	in.DeepCopyInto(out)
	return out
}

func (in *ExecHealthCheck) DeepCopyInto(out *ExecHealthCheck) {
	*out = *in
	if in.Command != nil {
		in, out := &in.Command, &out.Command
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

func (in *ExecHealthCheck) DeepCopy() *ExecHealthCheck {
	if in == nil {
		return nil
	}
	out := new(ExecHealthCheck)
	in.DeepCopyInto(out)
	return out
}

func (in *ServerScalingConfig) DeepCopyInto(out *ServerScalingConfig) {
	*out = *in
	if in.MinReplicas != nil {
		in, out := &in.MinReplicas, &out.MinReplicas
		*out = new(int32)
		**out = **in
	}
	if in.MaxReplicas != nil {
		in, out := &in.MaxReplicas, &out.MaxReplicas
		*out = new(int32)
		**out = **in
	}
	if in.TargetCPUUtilization != nil {
		in, out := &in.TargetCPUUtilization, &out.TargetCPUUtilization
		*out = new(int32)
		**out = **in
	}
	if in.TargetMemoryUtilization != nil {
		in, out := &in.TargetMemoryUtilization, &out.TargetMemoryUtilization
		*out = new(int32)
		**out = **in
	}
}

func (in *ServerScalingConfig) DeepCopy() *ServerScalingConfig {
	if in == nil {
		return nil
	}
	out := new(ServerScalingConfig)
	in.DeepCopyInto(out)
	return out
}

func (in *ToolboxServerStatus) DeepCopyInto(out *ToolboxServerStatus) {
	*out = *in
	in.LastHealthCheck.DeepCopyInto(&out.LastHealthCheck)
}

func (in *ToolboxServerStatus) DeepCopy() *ToolboxServerStatus {
	if in == nil {
		return nil
	}
	out := new(ToolboxServerStatus)
	in.DeepCopyInto(out)
	return out
}

func (in *ToolboxConnectionInfo) DeepCopyInto(out *ToolboxConnectionInfo) {
	*out = *in
	if in.ServerEndpoints != nil {
		in, out := &in.ServerEndpoints, &out.ServerEndpoints
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
}

func (in *ToolboxConnectionInfo) DeepCopy() *ToolboxConnectionInfo {
	if in == nil {
		return nil
	}
	out := new(ToolboxConnectionInfo)
	in.DeepCopyInto(out)
	return out
}

func (in *ToolboxResourceUsage) DeepCopyInto(out *ToolboxResourceUsage) {
	*out = *in
}

func (in *ToolboxResourceUsage) DeepCopy() *ToolboxResourceUsage {
	if in == nil {
		return nil
	}
	out := new(ToolboxResourceUsage)
	in.DeepCopyInto(out)
	return out
}