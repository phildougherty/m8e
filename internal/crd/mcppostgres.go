// internal/crd/mcppostgres.go
package crd

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// MCPPostgres represents a PostgreSQL database deployment for MCP services
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=mcppg
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.readyReplicas"
// +kubebuilder:printcolumn:name="Database",type="string",JSONPath=".spec.database"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type MCPPostgres struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPPostgresSpec   `json:"spec,omitempty"`
	Status MCPPostgresStatus `json:"status,omitempty"`
}

// GroupVersionKind returns the GroupVersionKind for MCPPostgres
func (m *MCPPostgres) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   GroupName,
		Version: Version,
		Kind:    "MCPPostgres",
	}
}

// MCPPostgresSpec defines the desired state of MCPPostgres
type MCPPostgresSpec struct {
	// Database configuration
	Database string `json:"database,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	Port     int32  `json:"port,omitempty"`

	// Storage configuration
	StorageSize      string `json:"storageSize,omitempty"`
	StorageClassName string `json:"storageClassName,omitempty"`

	// Resource configuration
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// PostgreSQL version
	Version string `json:"version,omitempty"`

	// Additional PostgreSQL configuration
	PostgresConfig map[string]string `json:"postgresConfig,omitempty"`

	// Security configuration
	SecurityContext *SecurityConfig `json:"securityContext,omitempty"`

	// High availability settings
	Replicas int32 `json:"replicas,omitempty"`

	// Backup configuration
	BackupConfig *PostgresBackupConfig `json:"backupConfig,omitempty"`
}

// PostgresBackupConfig defines backup settings
type PostgresBackupConfig struct {
	Enabled   bool   `json:"enabled,omitempty"`
	Schedule  string `json:"schedule,omitempty"`
	Retention string `json:"retention,omitempty"`
	Location  string `json:"location,omitempty"`
}

// MCPPostgresStatus defines the observed state of MCPPostgres
type MCPPostgresStatus struct {
	// Phase indicates the current phase of the PostgreSQL deployment
	Phase PostgresPhase `json:"phase,omitempty"`

	// Ready replicas count
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Total replicas count
	Replicas int32 `json:"replicas,omitempty"`

	// Database connection status
	DatabaseReady bool   `json:"databaseReady,omitempty"`
	DatabaseError string `json:"databaseError,omitempty"`

	// Service endpoint information
	ServiceEndpoint string `json:"serviceEndpoint,omitempty"`
	ServicePort     int32  `json:"servicePort,omitempty"`

	// Storage information
	StorageStatus string `json:"storageStatus,omitempty"`
	StorageSize   string `json:"storageSize,omitempty"`

	// Backup status
	LastBackup      *metav1.Time `json:"lastBackup,omitempty"`
	BackupStatus    string       `json:"backupStatus,omitempty"`
	NextBackup      *metav1.Time `json:"nextBackup,omitempty"`

	// Conditions represent the latest available observations
	Conditions []PostgresCondition `json:"conditions,omitempty"`

	// Database statistics
	Stats *PostgresStats `json:"stats,omitempty"`

	// Observed generation
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// PostgresPhase represents the phase of PostgreSQL deployment
type PostgresPhase string

const (
	PostgresPhasePending     PostgresPhase = "Pending"
	PostgresPhaseCreating    PostgresPhase = "Creating"
	PostgresPhaseRunning     PostgresPhase = "Running"
	PostgresPhaseUpdating    PostgresPhase = "Updating"
	PostgresPhaseDegraded    PostgresPhase = "Degraded"
	PostgresPhaseTerminating PostgresPhase = "Terminating"
	PostgresPhaseFailed      PostgresPhase = "Failed"
)

// PostgresCondition describes the state of a PostgreSQL deployment at a certain point
type PostgresCondition struct {
	Type               PostgresConditionType `json:"type"`
	Status             metav1.ConditionStatus `json:"status"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime,omitempty"`
	Reason             string                 `json:"reason,omitempty"`
	Message            string                 `json:"message,omitempty"`
}

// PostgresConditionType represents the type of condition
type PostgresConditionType string

const (
	PostgresConditionReady             PostgresConditionType = "Ready"
	PostgresConditionDatabaseReady     PostgresConditionType = "DatabaseReady"
	PostgresConditionStorageReady      PostgresConditionType = "StorageReady"
	PostgresConditionBackupReady       PostgresConditionType = "BackupReady"
	PostgresConditionProgressing       PostgresConditionType = "Progressing"
)

// PostgresStats contains database statistics
type PostgresStats struct {
	DatabaseSize  string            `json:"databaseSize,omitempty"`
	TableCount    int32             `json:"tableCount,omitempty"`
	ConnectionCount int32           `json:"connectionCount,omitempty"`
	Databases     map[string]string `json:"databases,omitempty"`
	Extensions    []string          `json:"extensions,omitempty"`
	LastStatsUpdate *metav1.Time    `json:"lastStatsUpdate,omitempty"`
}

// MCPPostgresList contains a list of MCPPostgres resources
// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type MCPPostgresList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPPostgres `json:"items"`
}

// DeepCopyInto is an auto-generated deepcopy function, copying the receiver, writing into out
func (in *MCPPostgres) DeepCopyInto(out *MCPPostgres) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy is an auto-generated deepcopy function, copying the receiver, creating a new MCPPostgres
func (in *MCPPostgres) DeepCopy() *MCPPostgres {
	if in == nil {
		return nil
	}
	out := new(MCPPostgres)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an auto-generated deepcopy function, copying the receiver, creating a new runtime.Object
func (in *MCPPostgres) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an auto-generated deepcopy function, copying the receiver, writing into out
func (in *MCPPostgresList) DeepCopyInto(out *MCPPostgresList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]MCPPostgres, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy is an auto-generated deepcopy function, copying the receiver, creating a new MCPPostgresList
func (in *MCPPostgresList) DeepCopy() *MCPPostgresList {
	if in == nil {
		return nil
	}
	out := new(MCPPostgresList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an auto-generated deepcopy function, copying the receiver, creating a new runtime.Object
func (in *MCPPostgresList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an auto-generated deepcopy function, copying the receiver, writing into out
func (in *MCPPostgresSpec) DeepCopyInto(out *MCPPostgresSpec) {
	*out = *in
	if in.Resources != nil {
		in, out := &in.Resources, &out.Resources
		*out = new(ResourceRequirements)
		(*in).DeepCopyInto(*out)
	}
	if in.PostgresConfig != nil {
		in, out := &in.PostgresConfig, &out.PostgresConfig
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.SecurityContext != nil {
		in, out := &in.SecurityContext, &out.SecurityContext
		*out = new(SecurityConfig)
		(*in).DeepCopyInto(*out)
	}
	if in.BackupConfig != nil {
		in, out := &in.BackupConfig, &out.BackupConfig
		*out = new(PostgresBackupConfig)
		**out = **in
	}
}

// DeepCopy is an auto-generated deepcopy function, copying the receiver, creating a new MCPPostgresSpec
func (in *MCPPostgresSpec) DeepCopy() *MCPPostgresSpec {
	if in == nil {
		return nil
	}
	out := new(MCPPostgresSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an auto-generated deepcopy function, copying the receiver, writing into out
func (in *MCPPostgresStatus) DeepCopyInto(out *MCPPostgresStatus) {
	*out = *in
	if in.LastBackup != nil {
		in, out := &in.LastBackup, &out.LastBackup
		*out = (*in).DeepCopy()
	}
	if in.NextBackup != nil {
		in, out := &in.NextBackup, &out.NextBackup
		*out = (*in).DeepCopy()
	}
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]PostgresCondition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.Stats != nil {
		in, out := &in.Stats, &out.Stats
		*out = new(PostgresStats)
		(*in).DeepCopyInto(*out)
	}
}

// DeepCopy is an auto-generated deepcopy function, copying the receiver, creating a new MCPPostgresStatus
func (in *MCPPostgresStatus) DeepCopy() *MCPPostgresStatus {
	if in == nil {
		return nil
	}
	out := new(MCPPostgresStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an auto-generated deepcopy function, copying the receiver, writing into out
func (in *PostgresBackupConfig) DeepCopyInto(out *PostgresBackupConfig) {
	*out = *in
}

// DeepCopy is an auto-generated deepcopy function, copying the receiver, creating a new PostgresBackupConfig
func (in *PostgresBackupConfig) DeepCopy() *PostgresBackupConfig {
	if in == nil {
		return nil
	}
	out := new(PostgresBackupConfig)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an auto-generated deepcopy function, copying the receiver, writing into out
func (in *PostgresCondition) DeepCopyInto(out *PostgresCondition) {
	*out = *in
	in.LastTransitionTime.DeepCopyInto(&out.LastTransitionTime)
}

// DeepCopy is an auto-generated deepcopy function, copying the receiver, creating a new PostgresCondition
func (in *PostgresCondition) DeepCopy() *PostgresCondition {
	if in == nil {
		return nil
	}
	out := new(PostgresCondition)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an auto-generated deepcopy function, copying the receiver, writing into out
func (in *PostgresStats) DeepCopyInto(out *PostgresStats) {
	*out = *in
	if in.Databases != nil {
		in, out := &in.Databases, &out.Databases
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Extensions != nil {
		in, out := &in.Extensions, &out.Extensions
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if in.LastStatsUpdate != nil {
		in, out := &in.LastStatsUpdate, &out.LastStatsUpdate
		*out = (*in).DeepCopy()
	}
}

// DeepCopy is an auto-generated deepcopy function, copying the receiver, creating a new PostgresStats
func (in *PostgresStats) DeepCopy() *PostgresStats {
	if in == nil {
		return nil
	}
	out := new(PostgresStats)
	in.DeepCopyInto(out)
	return out
}

// This will be registered in AddToScheme function in mcpserver.go