// internal/crd/mcpmemory.go
package crd

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// MCPMemory represents a memory server deployment with PostgreSQL backend
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=mcpmem
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="PostgreSQL",type="string",JSONPath=".status.postgresStatus"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type MCPMemory struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPMemorySpec   `json:"spec,omitempty"`
	Status MCPMemoryStatus `json:"status,omitempty"`
}

// MCPMemorySpec defines the desired state of MCPMemory
type MCPMemorySpec struct {
	// Memory server configuration
	Port int32  `json:"port,omitempty"`
	Host string `json:"host,omitempty"`

	// PostgreSQL configuration
	PostgresEnabled  bool   `json:"postgresEnabled,omitempty"`
	PostgresPort     int32  `json:"postgresPort,omitempty"`
	PostgresDB       string `json:"postgresDB,omitempty"`
	PostgresUser     string `json:"postgresUser,omitempty"`
	PostgresPassword string `json:"postgresPassword,omitempty"`
	DatabaseURL      string `json:"databaseURL,omitempty"`

	// Resource requirements
	CPUs           string `json:"cpus,omitempty"`
	Memory         string `json:"memory,omitempty"`
	PostgresCPUs   string `json:"postgresCpus,omitempty"`
	PostgresMemory string `json:"postgresMemory,omitempty"`

	// Storage configuration
	Volumes []string `json:"volumes,omitempty"`

	// Authentication configuration
	Authentication *AuthenticationConfig `json:"authentication,omitempty"`

	// Deployment configuration
	Replicas     *int32            `json:"replicas,omitempty"`
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	Tolerations  []Toleration      `json:"tolerations,omitempty"`
	Affinity     *Affinity         `json:"affinity,omitempty"`

	// Service configuration
	ServiceType        string            `json:"serviceType,omitempty"`
	ServiceAnnotations map[string]string `json:"serviceAnnotations,omitempty"`

	// Kubernetes-specific configuration
	ServiceAccount   string            `json:"serviceAccount,omitempty"`
	ImagePullSecrets []string          `json:"imagePullSecrets,omitempty"`
	PodAnnotations   map[string]string `json:"podAnnotations,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
}

// MCPMemoryStatus defines the observed state of MCPMemory
type MCPMemoryStatus struct {
	// Phase represents the current phase of the MCPMemory
	Phase MCPMemoryPhase `json:"phase,omitempty"`

	// Conditions represent the latest available observations of an object's state
	Conditions []MCPMemoryCondition `json:"conditions,omitempty"`

	// Health status
	HealthStatus string `json:"healthStatus,omitempty"`

	// PostgreSQL status
	PostgresStatus string `json:"postgresStatus,omitempty"`

	// Connection information
	DatabaseConnectionInfo *ConnectionInfo `json:"databaseConnectionInfo,omitempty"`

	// Deployment status
	Replicas      int32 `json:"replicas,omitempty"`
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Service information
	ServiceEndpoints []ServiceEndpoint `json:"serviceEndpoints,omitempty"`

	// Observed generation
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// MCPMemoryPhase represents the phase of an MCPMemory
type MCPMemoryPhase string

const (
	MCPMemoryPhasePending     MCPMemoryPhase = "Pending"
	MCPMemoryPhaseCreating    MCPMemoryPhase = "Creating"
	MCPMemoryPhaseStarting    MCPMemoryPhase = "Starting"
	MCPMemoryPhaseRunning     MCPMemoryPhase = "Running"
	MCPMemoryPhaseFailed      MCPMemoryPhase = "Failed"
	MCPMemoryPhaseTerminating MCPMemoryPhase = "Terminating"
)

// MCPMemoryCondition describes the state of an MCPMemory at a certain point
type MCPMemoryCondition struct {
	// Type of MCPMemory condition
	Type MCPMemoryConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown
	Status metav1.ConditionStatus `json:"status"`
	// Last time the condition transitioned from one status to another
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// The reason for the condition's last transition
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition
	Message string `json:"message,omitempty"`
}

// MCPMemoryConditionType represents a MCPMemory condition value
type MCPMemoryConditionType string

const (
	// MCPMemoryConditionReady indicates whether the memory server is ready
	MCPMemoryConditionReady MCPMemoryConditionType = "Ready"
	// MCPMemoryConditionHealthy indicates whether the memory server is healthy
	MCPMemoryConditionHealthy MCPMemoryConditionType = "Healthy"
	// MCPMemoryConditionPostgresReady indicates whether PostgreSQL is ready
	MCPMemoryConditionPostgresReady MCPMemoryConditionType = "PostgresReady"
)

// MCPMemoryList contains a list of MCPMemory
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type MCPMemoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPMemory `json:"items"`
}

// DeepCopy methods

func (in *MCPMemory) DeepCopyInto(out *MCPMemory) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *MCPMemory) DeepCopy() *MCPMemory {
	if in == nil {
		return nil
	}
	out := new(MCPMemory)
	in.DeepCopyInto(out)
	return out
}

func (in *MCPMemory) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *MCPMemoryList) DeepCopyInto(out *MCPMemoryList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]MCPMemory, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *MCPMemoryList) DeepCopy() *MCPMemoryList {
	if in == nil {
		return nil
	}
	out := new(MCPMemoryList)
	in.DeepCopyInto(out)
	return out
}

func (in *MCPMemoryList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *MCPMemorySpec) DeepCopyInto(out *MCPMemorySpec) {
	*out = *in
	if in.Volumes != nil {
		in, out := &in.Volumes, &out.Volumes
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	// Deep copy other fields as needed
}

func (in *MCPMemorySpec) DeepCopy() *MCPMemorySpec {
	if in == nil {
		return nil
	}
	out := new(MCPMemorySpec)
	in.DeepCopyInto(out)
	return out
}

func (in *MCPMemoryStatus) DeepCopyInto(out *MCPMemoryStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]MCPMemoryCondition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.ServiceEndpoints != nil {
		in, out := &in.ServiceEndpoints, &out.ServiceEndpoints
		*out = make([]ServiceEndpoint, len(*in))
		copy(*out, *in)
	}
}

func (in *MCPMemoryStatus) DeepCopy() *MCPMemoryStatus {
	if in == nil {
		return nil
	}
	out := new(MCPMemoryStatus)
	in.DeepCopyInto(out)
	return out
}

func (in *MCPMemoryCondition) DeepCopyInto(out *MCPMemoryCondition) {
	*out = *in
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

func (in *MCPMemoryCondition) DeepCopy() *MCPMemoryCondition {
	if in == nil {
		return nil
	}
	out := new(MCPMemoryCondition)
	in.DeepCopyInto(out)
	return out
}