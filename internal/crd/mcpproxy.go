// internal/crd/mcpproxy.go
package crd

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// MCPProxy represents an MCP proxy deployment that manages routing and service discovery
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=mcpproxy
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Servers",type="integer",JSONPath=".status.discoveredServers"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type MCPProxy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPProxySpec   `json:"spec,omitempty"`
	Status MCPProxyStatus `json:"status,omitempty"`
}

// GroupVersionKind returns the GroupVersionKind for MCPProxy
func (m *MCPProxy) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   GroupName,
		Version: Version,
		Kind:    "MCPProxy",
	}
}

// MCPProxySpec defines the desired state of MCPProxy
type MCPProxySpec struct {
	// Server discovery configuration
	ServerSelector *ServerSelector `json:"serverSelector,omitempty"`

	// Proxy configuration
	Port int32  `json:"port,omitempty"`
	Host string `json:"host,omitempty"`

	// Authentication configuration
	Auth   *ProxyAuthConfig  `json:"auth,omitempty"`
	OAuth  *OAuthConfig      `json:"oauth,omitempty"`
	RBAC   *RBACConfig       `json:"rbac,omitempty"`

	// Connection management
	Connections *ConnectionConfig `json:"connections,omitempty"`

	// Ingress configuration
	Ingress *IngressConfig `json:"ingress,omitempty"`

	// Monitoring and observability
	Monitoring *MonitoringConfig `json:"monitoring,omitempty"`

	// Audit configuration
	Audit *AuditConfig `json:"audit,omitempty"`

	// Dashboard configuration
	Dashboard *DashboardConfig `json:"dashboard,omitempty"`

	// TLS configuration
	TLS *TLSConfig `json:"tls,omitempty"`

	// Resource requirements for the proxy
	Resources ResourceRequirements `json:"resources,omitempty"`

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

// MCPProxyStatus defines the observed state of MCPProxy
type MCPProxyStatus struct {
	// Phase represents the current phase of the MCPProxy
	Phase MCPProxyPhase `json:"phase,omitempty"`

	// Conditions represent the latest available observations of an object's state
	Conditions []MCPProxyCondition `json:"conditions,omitempty"`

	// Discovered servers
	DiscoveredServers []DiscoveredServer `json:"discoveredServers,omitempty"`

	// Endpoints exposed by the proxy
	Endpoints []ProxyEndpoint `json:"endpoints,omitempty"`

	// Connection statistics
	ConnectionStats *ConnectionStats `json:"connectionStats,omitempty"`

	// Health status
	HealthStatus string `json:"healthStatus,omitempty"`

	// Deployment status
	Replicas      int32 `json:"replicas,omitempty"`
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Service information
	ServiceEndpoints []ServiceEndpoint `json:"serviceEndpoints,omitempty"`

	// Ingress information
	IngressEndpoints []IngressEndpoint `json:"ingressEndpoints,omitempty"`

	// Observed generation
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// MCPProxyPhase represents the phase of an MCPProxy
type MCPProxyPhase string

const (
	MCPProxyPhasePending     MCPProxyPhase = "Pending"
	MCPProxyPhaseCreating    MCPProxyPhase = "Creating"
	MCPProxyPhaseStarting    MCPProxyPhase = "Starting"
	MCPProxyPhaseRunning     MCPProxyPhase = "Running"
	MCPProxyPhaseFailed      MCPProxyPhase = "Failed"
	MCPProxyPhaseTerminating MCPProxyPhase = "Terminating"
)

// MCPProxyCondition describes the state of an MCPProxy at a certain point
type MCPProxyCondition struct {
	// Type of MCPProxy condition
	Type MCPProxyConditionType `json:"type"`
	// Status of the condition, one of True, False, Unknown
	Status metav1.ConditionStatus `json:"status"`
	// Last time the condition transitioned from one status to another
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// The reason for the condition's last transition
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition
	Message string `json:"message,omitempty"`
}

// MCPProxyConditionType represents a MCPProxy condition value
type MCPProxyConditionType string

const (
	// MCPProxyConditionReady indicates whether the proxy is ready to serve requests
	MCPProxyConditionReady MCPProxyConditionType = "Ready"
	// MCPProxyConditionHealthy indicates whether the proxy is healthy
	MCPProxyConditionHealthy MCPProxyConditionType = "Healthy"
	// MCPProxyConditionServersDiscovered indicates whether servers have been discovered
	MCPProxyConditionServersDiscovered MCPProxyConditionType = "ServersDiscovered"
)

// ServerSelector defines how to select MCP servers
type ServerSelector struct {
	// Label selector for MCPServer resources
	LabelSelector *LabelSelector `json:"labelSelector,omitempty"`
	// Namespace selector for cross-namespace discovery
	NamespaceSelector *LabelSelector `json:"namespaceSelector,omitempty"`
	// Include servers by name
	IncludeServers []string `json:"includeServers,omitempty"`
	// Exclude servers by name
	ExcludeServers []string `json:"excludeServers,omitempty"`
}

// ProxyAuthConfig defines authentication settings for the proxy
type ProxyAuthConfig struct {
	Enabled       bool   `json:"enabled,omitempty"`
	APIKey        string `json:"apiKey,omitempty"`
	OAuthFallback bool   `json:"oauthFallback,omitempty"`
}

// OAuthConfig defines OAuth 2.1 configuration
type OAuthConfig struct {
	Enabled         bool                `json:"enabled,omitempty"`
	Issuer          string              `json:"issuer,omitempty"`
	Endpoints       OAuthEndpoints      `json:"endpoints,omitempty"`
	Tokens          TokenConfig         `json:"tokens,omitempty"`
	Security        OAuthSecurityConfig `json:"security,omitempty"`
	GrantTypes      []string            `json:"grantTypes,omitempty"`
	ResponseTypes   []string            `json:"responseTypes,omitempty"`
	ScopesSupported []string            `json:"scopesSupported,omitempty"`
}

// OAuthEndpoints defines OAuth endpoint configuration
type OAuthEndpoints struct {
	Authorization string `json:"authorization,omitempty"`
	Token         string `json:"token,omitempty"`
	UserInfo      string `json:"userinfo,omitempty"`
	Revoke        string `json:"revoke,omitempty"`
	Discovery     string `json:"discovery,omitempty"`
}

// TokenConfig defines token configuration
type TokenConfig struct {
	AccessTokenTTL  string `json:"accessTokenTTL,omitempty"`
	RefreshTokenTTL string `json:"refreshTokenTTL,omitempty"`
	CodeTTL         string `json:"authorizationCodeTTL,omitempty"`
	Algorithm       string `json:"algorithm,omitempty"`
}

// OAuthSecurityConfig defines OAuth security configuration
type OAuthSecurityConfig struct {
	RequirePKCE bool `json:"requirePKCE,omitempty"`
}

// RBACConfig defines RBAC configuration
type RBACConfig struct {
	Enabled bool            `json:"enabled,omitempty"`
	Scopes  []Scope         `json:"scopes,omitempty"`
	Roles   map[string]Role `json:"roles,omitempty"`
}

// Scope defines a permission scope
type Scope struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// Role defines a role with permissions
type Role struct {
	Scopes  []string `json:"scopes,omitempty"`
	Servers []string `json:"servers,omitempty"`
}

// ConnectionConfig defines connection management configuration
type ConnectionConfig struct {
	MaxConnections    int32                    `json:"maxConnections,omitempty"`
	MaxIdleTime       string                   `json:"maxIdleTime,omitempty"`
	HealthCheckInterval string                 `json:"healthCheckInterval,omitempty"`
	Timeouts          map[string]string        `json:"timeouts,omitempty"`
	PoolSize          int32                    `json:"poolSize,omitempty"`
	EnableMetrics     bool                     `json:"enableMetrics,omitempty"`
}

// IngressConfig defines ingress configuration
type IngressConfig struct {
	Enabled     bool              `json:"enabled,omitempty"`
	Class       string            `json:"class,omitempty"`
	Host        string            `json:"host,omitempty"`
	TLS         bool              `json:"tls,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Paths       []IngressPath     `json:"paths,omitempty"`
}

// IngressPath defines an ingress path
type IngressPath struct {
	Path     string `json:"path,omitempty"`
	PathType string `json:"pathType,omitempty"`
	Backend  string `json:"backend,omitempty"`
}

// MonitoringConfig defines monitoring configuration
type MonitoringConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Port    int32  `json:"port,omitempty"`
	Path    string `json:"path,omitempty"`
}

// AuditConfig defines audit configuration
type AuditConfig struct {
	Enabled   bool            `json:"enabled,omitempty"`
	LogLevel  string          `json:"logLevel,omitempty"`
	Storage   string          `json:"storage,omitempty"`
	Retention RetentionConfig `json:"retention,omitempty"`
	Events    []string        `json:"events,omitempty"`
}

// RetentionConfig defines retention configuration
type RetentionConfig struct {
	MaxEntries int    `json:"maxEntries,omitempty"`
	MaxAge     string `json:"maxAge,omitempty"`
}

// DashboardConfig defines dashboard configuration
type DashboardConfig struct {
	Enabled bool   `json:"enabled,omitempty"`
	Port    int32  `json:"port,omitempty"`
	Path    string `json:"path,omitempty"`
}

// TLSConfig defines TLS configuration
type TLSConfig struct {
	Enabled    bool   `json:"enabled,omitempty"`
	SecretName string `json:"secretName,omitempty"`
	CertFile   string `json:"certFile,omitempty"`
	KeyFile    string `json:"keyFile,omitempty"`
}

// DiscoveredServer represents a discovered MCP server
type DiscoveredServer struct {
	Name              string   `json:"name,omitempty"`
	Namespace         string   `json:"namespace,omitempty"`
	Endpoint          string   `json:"endpoint,omitempty"`
	Protocol          string   `json:"protocol,omitempty"`
	Capabilities      []string `json:"capabilities,omitempty"`
	Status            string   `json:"status,omitempty"`
	LastDiscovered    metav1.Time `json:"lastDiscovered,omitempty"`
	HealthStatus      string   `json:"healthStatus,omitempty"`
}

// ProxyEndpoint represents an endpoint exposed by the proxy
type ProxyEndpoint struct {
	Name     string `json:"name,omitempty"`
	URL      string `json:"url,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Port     int32  `json:"port,omitempty"`
}

// ConnectionStats represents connection statistics
type ConnectionStats struct {
	TotalConnections    int32   `json:"totalConnections,omitempty"`
	ActiveConnections   int32   `json:"activeConnections,omitempty"`
	FailedConnections   int32   `json:"failedConnections,omitempty"`
	AverageResponseTime string  `json:"averageResponseTime,omitempty"`
	RequestsPerSecond   float64 `json:"requestsPerSecond,omitempty"`
	ErrorRate           float64 `json:"errorRate,omitempty"`
}

// IngressEndpoint represents an ingress endpoint
type IngressEndpoint struct {
	Host string `json:"host,omitempty"`
	Path string `json:"path,omitempty"`
	URL  string `json:"url,omitempty"`
}

// MCPProxyList contains a list of MCPProxy
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type MCPProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPProxy `json:"items"`
}

// GroupVersionKind returns the GroupVersionKind for MCPProxyList
func (m *MCPProxyList) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   GroupName,
		Version: Version,
		Kind:    "MCPProxyList",
	}
}

// DeepCopy methods (would typically be generated by code-generator tools)

func (in *MCPProxy) DeepCopyInto(out *MCPProxy) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *MCPProxy) DeepCopy() *MCPProxy {
	if in == nil {
		return nil
	}
	out := new(MCPProxy)
	in.DeepCopyInto(out)
	return out
}

func (in *MCPProxy) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *MCPProxyList) DeepCopyInto(out *MCPProxyList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]MCPProxy, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *MCPProxyList) DeepCopy() *MCPProxyList {
	if in == nil {
		return nil
	}
	out := new(MCPProxyList)
	in.DeepCopyInto(out)
	return out
}

func (in *MCPProxyList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *MCPProxySpec) DeepCopyInto(out *MCPProxySpec) {
	*out = *in
	// Add deep copy logic for nested structs...
}

func (in *MCPProxySpec) DeepCopy() *MCPProxySpec {
	if in == nil {
		return nil
	}
	out := new(MCPProxySpec)
	in.DeepCopyInto(out)
	return out
}

func (in *MCPProxyStatus) DeepCopyInto(out *MCPProxyStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]MCPProxyCondition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	// Add deep copy logic for other fields...
}

func (in *MCPProxyStatus) DeepCopy() *MCPProxyStatus {
	if in == nil {
		return nil
	}
	out := new(MCPProxyStatus)
	in.DeepCopyInto(out)
	return out
}

func (in *MCPProxyCondition) DeepCopyInto(out *MCPProxyCondition) {
	*out = *in
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

func (in *MCPProxyCondition) DeepCopy() *MCPProxyCondition {
	if in == nil {
		return nil
	}
	out := new(MCPProxyCondition)
	in.DeepCopyInto(out)
	return out
}

// Add MCPProxy types to scheme
func AddMCPProxyToScheme(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&MCPProxy{},
		&MCPProxyList{},
	)
	return nil
}