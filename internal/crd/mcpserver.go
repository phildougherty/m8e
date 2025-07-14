// internal/crd/mcpserver.go
package crd

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	GroupName = "mcp.matey.ai"
	Version   = "v1"
)

// SchemeGroupVersion is group version used to register these objects
var SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: Version}

// MCPServer represents an MCP server deployment
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=mcpsrv
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Protocol",type="string",JSONPath=".spec.protocol"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type MCPServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPServerSpec   `json:"spec,omitempty"`
	Status MCPServerStatus `json:"status,omitempty"`
}

// MCPServerSpec defines the desired state of MCPServer
type MCPServerSpec struct {
	// Container configuration
	Image       string            `json:"image,omitempty"`
	Command     []string          `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	WorkingDir  string            `json:"workingDir,omitempty"`

	// MCP-specific configuration
	Protocol      string   `json:"protocol,omitempty"`      // "http", "sse", "stdio"
	HttpPort      int32    `json:"httpPort,omitempty"`
	HttpPath      string   `json:"httpPath,omitempty"`
	SSEPath       string   `json:"ssePath,omitempty"`
	SSEPort       int32    `json:"ssePort,omitempty"`
	SSEHeartbeat  int32    `json:"sseHeartbeat,omitempty"`
	Capabilities  []string `json:"capabilities,omitempty"`

	// Dependencies and relationships
	DependsOn []string `json:"dependsOn,omitempty"`

	// Resource requirements
	Resources ResourceRequirements `json:"resources,omitempty"`

	// Security configuration
	Authentication *AuthenticationConfig `json:"authentication,omitempty"`
	Security       *SecurityConfig       `json:"security,omitempty"`

	// Deployment configuration
	Replicas      *int32            `json:"replicas,omitempty"`
	NodeSelector  map[string]string `json:"nodeSelector,omitempty"`
	Tolerations   []Toleration      `json:"tolerations,omitempty"`
	Affinity      *Affinity         `json:"affinity,omitempty"`

	// Service configuration
	ServiceType        string            `json:"serviceType,omitempty"`        // ClusterIP, NodePort, LoadBalancer
	ServiceAnnotations map[string]string `json:"serviceAnnotations,omitempty"`
	
	// Storage configuration
	Volumes      []VolumeSpec `json:"volumes,omitempty"`
	StorageClass string       `json:"storageClass,omitempty"`

	// Kubernetes-specific configuration
	ServiceAccount   string            `json:"serviceAccount,omitempty"`
	ImagePullSecrets []string          `json:"imagePullSecrets,omitempty"`
	PodAnnotations   map[string]string `json:"podAnnotations,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	
	// Service discovery configuration
	ServiceDiscovery ServiceDiscoveryConfig `json:"serviceDiscovery,omitempty"`
}

// MCPServerStatus defines the observed state of MCPServer
type MCPServerStatus struct {
	// Phase represents the current phase of the MCPServer
	Phase MCPServerPhase `json:"phase,omitempty"`

	// Conditions represent the latest available observations of an object's state
	Conditions []MCPServerCondition `json:"conditions,omitempty"`

	// Connection information
	ConnectionInfo *ConnectionInfo `json:"connectionInfo,omitempty"`

	// Health status
	HealthStatus string `json:"healthStatus,omitempty"`

	// Discovered capabilities
	DiscoveredCapabilities []string `json:"discoveredCapabilities,omitempty"`

	// Deployment status
	Replicas      int32 `json:"replicas,omitempty"`
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Service information
	ServiceEndpoints []ServiceEndpoint `json:"serviceEndpoints,omitempty"`

	// Observed generation
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// MCPServerPhase represents the phase of an MCPServer
type MCPServerPhase string

const (
	MCPServerPhasePending   MCPServerPhase = "Pending"
	MCPServerPhaseCreating  MCPServerPhase = "Creating"
	MCPServerPhaseStarting  MCPServerPhase = "Starting"
	MCPServerPhaseRunning   MCPServerPhase = "Running"
	MCPServerPhaseFailed    MCPServerPhase = "Failed"
	MCPServerPhaseTerminating MCPServerPhase = "Terminating"
)

// MCPServerCondition describes the state of an MCPServer at a certain point
type MCPServerCondition struct {
	// Type of MCPServer condition
	Type MCPServerConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown
	Status metav1.ConditionStatus `json:"status"`
	// Last time the condition transitioned from one status to another
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// The reason for the condition's last transition
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition
	Message string `json:"message,omitempty"`
}

// MCPServerConditionType represents a MCPServer condition value
type MCPServerConditionType string

const (
	// MCPServerConditionReady indicates whether the server is ready to serve requests
	MCPServerConditionReady MCPServerConditionType = "Ready"
	// MCPServerConditionHealthy indicates whether the server is healthy
	MCPServerConditionHealthy MCPServerConditionType = "Healthy"
	// MCPServerConditionDependenciesReady indicates whether all dependencies are ready
	MCPServerConditionDependenciesReady MCPServerConditionType = "DependenciesReady"
)

// ResourceRequirements describes the compute resource requirements
type ResourceRequirements struct {
	Limits   ResourceList `json:"limits,omitempty"`
	Requests ResourceList `json:"requests,omitempty"`
}

// ResourceList is a set of (resource name, quantity) pairs
type ResourceList map[string]string

// AuthenticationConfig defines authentication settings for the server
type AuthenticationConfig struct {
	Enabled       bool     `json:"enabled,omitempty"`
	RequiredScope string   `json:"requiredScope,omitempty"`
	OptionalAuth  bool     `json:"optionalAuth,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
	AllowAPIKey   *bool    `json:"allowAPIKey,omitempty"`
}

// SecurityConfig defines security settings for the server
type SecurityConfig struct {
	AllowDockerSocket  bool     `json:"allowDockerSocket,omitempty"`
	AllowHostMounts    []string `json:"allowHostMounts,omitempty"`
	AllowPrivilegedOps bool     `json:"allowPrivilegedOps,omitempty"`
	TrustedImage       bool     `json:"trustedImage,omitempty"`
	NoNewPrivileges    bool     `json:"noNewPrivileges,omitempty"`
	RunAsUser          *int64   `json:"runAsUser,omitempty"`
	RunAsGroup         *int64   `json:"runAsGroup,omitempty"`
	ReadOnlyRootFS     bool     `json:"readOnlyRootFS,omitempty"`
	CapAdd             []string `json:"capAdd,omitempty"`
	CapDrop            []string `json:"capDrop,omitempty"`
}

// Toleration represents a Kubernetes toleration
type Toleration struct {
	Key               string `json:"key,omitempty"`
	Operator          string `json:"operator,omitempty"`
	Value             string `json:"value,omitempty"`
	Effect            string `json:"effect,omitempty"`
	TolerationSeconds *int64 `json:"tolerationSeconds,omitempty"`
}

// Affinity represents Kubernetes affinity configuration
type Affinity struct {
	NodeAffinity    *NodeAffinity `json:"nodeAffinity,omitempty"`
	PodAffinity     *PodAffinity  `json:"podAffinity,omitempty"`
	PodAntiAffinity *PodAffinity  `json:"podAntiAffinity,omitempty"`
}

// NodeAffinity represents Kubernetes node affinity
type NodeAffinity struct {
	RequiredDuringSchedulingIgnoredDuringExecution  *NodeSelector             `json:"requiredDuringSchedulingIgnoredDuringExecution,omitempty"`
	PreferredDuringSchedulingIgnoredDuringExecution []PreferredSchedulingTerm `json:"preferredDuringSchedulingIgnoredDuringExecution,omitempty"`
}

// NodeSelector represents a Kubernetes node selector
type NodeSelector struct {
	NodeSelectorTerms []NodeSelectorTerm `json:"nodeSelectorTerms,omitempty"`
}

// NodeSelectorTerm represents a Kubernetes node selector term
type NodeSelectorTerm struct {
	MatchExpressions []NodeSelectorRequirement `json:"matchExpressions,omitempty"`
	MatchFields      []NodeSelectorRequirement `json:"matchFields,omitempty"`
}

// NodeSelectorRequirement represents a Kubernetes node selector requirement
type NodeSelectorRequirement struct {
	Key      string   `json:"key"`
	Operator string   `json:"operator"`
	Values   []string `json:"values,omitempty"`
}

// PreferredSchedulingTerm represents a Kubernetes preferred scheduling term
type PreferredSchedulingTerm struct {
	Weight     int32        `json:"weight"`
	Preference NodeSelector `json:"preference"`
}

// PodAffinity represents Kubernetes pod affinity/anti-affinity
type PodAffinity struct {
	RequiredDuringSchedulingIgnoredDuringExecution  []PodAffinityTerm         `json:"requiredDuringSchedulingIgnoredDuringExecution,omitempty"`
	PreferredDuringSchedulingIgnoredDuringExecution []WeightedPodAffinityTerm `json:"preferredDuringSchedulingIgnoredDuringExecution,omitempty"`
}

// PodAffinityTerm represents a Kubernetes pod affinity term
type PodAffinityTerm struct {
	LabelSelector *LabelSelector `json:"labelSelector,omitempty"`
	Namespaces    []string       `json:"namespaces,omitempty"`
	TopologyKey   string         `json:"topologyKey"`
}

// WeightedPodAffinityTerm represents a Kubernetes weighted pod affinity term
type WeightedPodAffinityTerm struct {
	Weight          int32           `json:"weight"`
	PodAffinityTerm PodAffinityTerm `json:"podAffinityTerm"`
}

// LabelSelector represents a Kubernetes label selector
type LabelSelector struct {
	MatchLabels      map[string]string             `json:"matchLabels,omitempty"`
	MatchExpressions []LabelSelectorRequirement    `json:"matchExpressions,omitempty"`
}

// LabelSelectorRequirement represents a Kubernetes label selector requirement
type LabelSelectorRequirement struct {
	Key      string   `json:"key"`
	Operator string   `json:"operator"`
	Values   []string `json:"values,omitempty"`
}

// ServiceDiscoveryConfig defines service discovery settings
type ServiceDiscoveryConfig struct {
	// Enabled controls whether this server should be discoverable
	Enabled bool `json:"enabled,omitempty"`
	
	// ServiceName overrides the default service name (defaults to metadata.name)
	ServiceName string `json:"serviceName,omitempty"`
	
	// DiscoveryLabels are additional labels for service discovery
	DiscoveryLabels map[string]string `json:"discoveryLabels,omitempty"`
	
	// CrossNamespace allows discovery from other namespaces
	CrossNamespace bool `json:"crossNamespace,omitempty"`
}

// VolumeSpec represents a volume specification
type VolumeSpec struct {
	Name       string `json:"name"`
	MountPath  string `json:"mountPath"`
	Size       string `json:"size,omitempty"`
	AccessMode string `json:"accessMode,omitempty"`
	SubPath    string `json:"subPath,omitempty"`
}

// ConnectionInfo represents connection information for the server
type ConnectionInfo struct {
	Endpoint    string `json:"endpoint,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	Port        int32  `json:"port,omitempty"`
	Path        string `json:"path,omitempty"`
	SSEEndpoint string `json:"sseEndpoint,omitempty"`
}

// ServiceEndpoint represents a service endpoint
type ServiceEndpoint struct {
	Name string `json:"name"`
	Port int32  `json:"port"`
	URL  string `json:"url"`
}

// MCPServerList contains a list of MCPServer
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type MCPServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPServer `json:"items"`
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MCPServer) DeepCopyInto(out *MCPServer) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MCPServer.
func (in *MCPServer) DeepCopy() *MCPServer {
	if in == nil {
		return nil
	}
	out := new(MCPServer)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *MCPServer) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MCPServerList) DeepCopyInto(out *MCPServerList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]MCPServer, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MCPServerList.
func (in *MCPServerList) DeepCopy() *MCPServerList {
	if in == nil {
		return nil
	}
	out := new(MCPServerList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *MCPServerList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// Add other DeepCopy methods for nested structs...
// (These would typically be generated by code-generator tools in a real implementation)

func (in *MCPServerSpec) DeepCopyInto(out *MCPServerSpec) {
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
	// Continue for other fields...
}

func (in *MCPServerSpec) DeepCopy() *MCPServerSpec {
	if in == nil {
		return nil
	}
	out := new(MCPServerSpec)
	in.DeepCopyInto(out)
	return out
}

func (in *MCPServerStatus) DeepCopyInto(out *MCPServerStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]MCPServerCondition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	// Continue for other fields...
}

func (in *MCPServerStatus) DeepCopy() *MCPServerStatus {
	if in == nil {
		return nil
	}
	out := new(MCPServerStatus)
	in.DeepCopyInto(out)
	return out
}

func (in *MCPServerCondition) DeepCopyInto(out *MCPServerCondition) {
	*out = *in
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

func (in *MCPServerCondition) DeepCopy() *MCPServerCondition {
	if in == nil {
		return nil
	}
	out := new(MCPServerCondition)
	in.DeepCopyInto(out)
	return out
}

// Resource returns the resource for MCPServer
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

// AddToScheme adds the types in this group-version to the given scheme.
func AddToScheme(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&MCPServer{},
		&MCPServerList{},
		&MCPTaskScheduler{},
		&MCPTaskSchedulerList{},
	)
	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}