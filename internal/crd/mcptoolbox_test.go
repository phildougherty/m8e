package crd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestMCPToolbox_GroupVersionKind(t *testing.T) {
	toolbox := &MCPToolbox{}
	gvk := toolbox.GroupVersionKind()
	
	assert.Equal(t, GroupName, gvk.Group)
	assert.Equal(t, Version, gvk.Version)
	assert.Equal(t, "MCPToolbox", gvk.Kind)
}

func TestMCPToolbox_JSON_Serialization(t *testing.T) {
	toolbox := &MCPToolbox{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MCPToolbox",
			APIVersion: GroupName + "/" + Version,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-toolbox",
			Namespace: "default",
		},
		Spec: MCPToolboxSpec{
			Template:    "coding-assistant",
			Description: "A coding assistant toolbox",
			Servers: map[string]ToolboxServerSpec{
				"code-server": {
					MCPServerSpec: MCPServerSpec{
						Image:    "mcpcompose/code-server:latest",
						Protocol: "http",
						HttpPort: 8080,
					},
				},
				"docs-server": {
					MCPServerSpec: MCPServerSpec{
						Image:    "mcpcompose/docs-server:latest",
						Protocol: "sse",
						HttpPort: 8081,
					},
				},
			},
			Dependencies: []ToolboxDependency{
				{
					Server:    "docs-server",
					DependsOn: []string{"code-server"},
				},
			},
		},
		Status: MCPToolboxStatus{
			Phase:        MCPToolboxPhaseRunning,
			ServerCount:  2,
			ReadyServers: 2,
			Conditions: []MCPToolboxCondition{
				{
					Type:   MCPToolboxConditionReady,
					Status: metav1.ConditionTrue,
					Reason: "AllServersReady",
				},
			},
		},
	}

	// Test serialization
	data, err := json.Marshal(toolbox)
	require.NoError(t, err)
	assert.Contains(t, string(data), "coding-assistant")
	assert.Contains(t, string(data), "code-server")
	assert.Contains(t, string(data), "docs-server")

	// Test deserialization
	var restored MCPToolbox
	err = json.Unmarshal(data, &restored)
	require.NoError(t, err)
	
	assert.Equal(t, toolbox.Name, restored.Name)
	assert.Equal(t, toolbox.Spec.Template, restored.Spec.Template)
	assert.Equal(t, toolbox.Spec.Description, restored.Spec.Description)
	assert.Equal(t, len(toolbox.Spec.Servers), len(restored.Spec.Servers))
	assert.Equal(t, toolbox.Status.Phase, restored.Status.Phase)
	assert.Equal(t, toolbox.Status.ServerCount, restored.Status.ServerCount)
}

func TestMCPToolboxSpec_Structure(t *testing.T) {
	tests := []struct {
		name string
		spec MCPToolboxSpec
	}{
		{
			name: "basic spec with template",
			spec: MCPToolboxSpec{
				Template: "coding-assistant",
				Servers: map[string]ToolboxServerSpec{
					"server1": {
						MCPServerSpec: MCPServerSpec{
							Image:    "test:latest",
							Protocol: "http",
							HttpPort: 8080,
						},
					},
				},
			},
		},
		{
			name: "custom spec with multiple servers",
			spec: MCPToolboxSpec{
				Description: "Custom toolbox",
				Servers: map[string]ToolboxServerSpec{
					"custom-server": {
						MCPServerSpec: MCPServerSpec{
							Image:    "custom:latest",
							Protocol: "sse",
							HttpPort: 9000,
							Command:  []string{"node", "server.js"},
							Env: map[string]string{
								"NODE_ENV": "production",
							},
						},
						ToolboxRole: "primary",
						Priority:    1,
					},
					"helper-server": {
						MCPServerSpec: MCPServerSpec{
							Image:    "helper:latest",
							Protocol: "http",
							HttpPort: 9001,
						},
						ToolboxRole: "helper",
						Priority:    2,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify the structure is well-formed
			assert.NotEmpty(t, tt.spec)
			if tt.spec.Template != "" {
				assert.NotEmpty(t, tt.spec.Template)
			}
			if tt.spec.Servers != nil {
				assert.NotEmpty(t, tt.spec.Servers)
			}
		})
	}
}

func TestMCPToolboxStatus_Structure(t *testing.T) {
	status := &MCPToolboxStatus{
		Phase:        MCPToolboxPhaseStarting,
		ServerCount:  3,
		ReadyServers: 1,
		Conditions: []MCPToolboxCondition{
			{
				Type:   MCPToolboxConditionReady,
				Status: metav1.ConditionFalse,
				Reason: "ServersStarting",
			},
		},
		ServerStatuses: map[string]ToolboxServerStatus{
			"server1": {
				Phase:  MCPServerPhaseRunning,
				Ready:  true,
				Health: "Healthy",
			},
		},
	}

	// Test structure
	assert.Equal(t, MCPToolboxPhaseStarting, status.Phase)
	assert.Equal(t, int32(3), status.ServerCount)
	assert.Equal(t, int32(1), status.ReadyServers)
	assert.Len(t, status.Conditions, 1)
	assert.Equal(t, MCPToolboxConditionReady, status.Conditions[0].Type)
	assert.Equal(t, metav1.ConditionFalse, status.Conditions[0].Status)
	assert.Len(t, status.ServerStatuses, 1)
	assert.True(t, status.ServerStatuses["server1"].Ready)

	// Test adding more conditions
	status.Conditions = append(status.Conditions, MCPToolboxCondition{
		Type:   MCPToolboxConditionHealthy,
		Status: metav1.ConditionTrue,
		Reason: "AllServersHealthy",
	})

	assert.Len(t, status.Conditions, 2)
}

func TestMCPToolboxList_DeepCopy(t *testing.T) {
	original := &MCPToolboxList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MCPToolboxList",
			APIVersion: GroupName + "/" + Version,
		},
		ListMeta: metav1.ListMeta{
			ResourceVersion: "123",
		},
		Items: []MCPToolbox{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "toolbox1",
					Namespace: "default",
				},
				Spec: MCPToolboxSpec{
					Template: "coding-assistant",
					Servers: map[string]ToolboxServerSpec{
						"server1": {
							MCPServerSpec: MCPServerSpec{
								Image:    "test:latest",
								Protocol: "http",
								HttpPort: 8080,
							},
						},
					},
				},
			},
		},
	}

	// Test deep copy
	copied := original.DeepCopy()
	
	// Verify they are different objects
	assert.NotSame(t, original, copied)
	assert.NotSame(t, &original.Items[0], &copied.Items[0])
	
	// Verify content is the same
	assert.Equal(t, original.ResourceVersion, copied.ResourceVersion)
	assert.Equal(t, len(original.Items), len(copied.Items))
	assert.Equal(t, original.Items[0].Name, copied.Items[0].Name)
	assert.Equal(t, original.Items[0].Spec.Template, copied.Items[0].Spec.Template)
	
	// Verify modifying copy doesn't affect original
	copied.Items[0].Name = "modified"
	assert.NotEqual(t, original.Items[0].Name, copied.Items[0].Name)
}

func TestMCPToolbox_RuntimeObject(t *testing.T) {
	toolbox := &MCPToolbox{}
	
	// Test that it implements runtime.Object
	var obj runtime.Object = toolbox
	assert.NotNil(t, obj)
	
	// Test GetObjectKind
	gvk := obj.GetObjectKind().GroupVersionKind()
	assert.Equal(t, "", gvk.Group) // Empty until set
	assert.Equal(t, "", gvk.Version)
	assert.Equal(t, "", gvk.Kind)
	
	// Set and test
	obj.GetObjectKind().SetGroupVersionKind(schema.GroupVersionKind{
		Group:   GroupName,
		Version: Version,
		Kind:    "MCPToolbox",
	})
	
	gvk = obj.GetObjectKind().GroupVersionKind()
	assert.Equal(t, GroupName, gvk.Group)
	assert.Equal(t, Version, gvk.Version)
	assert.Equal(t, "MCPToolbox", gvk.Kind)
}

func TestToolboxServerSpec_Structure(t *testing.T) {
	tests := []struct {
		name   string
		server ToolboxServerSpec
	}{
		{
			name: "minimal server",
			server: ToolboxServerSpec{
				MCPServerSpec: MCPServerSpec{
					Image:    "test:latest",
					Protocol: "http",
					HttpPort: 8080,
				},
			},
		},
		{
			name: "full server with toolbox features",
			server: ToolboxServerSpec{
				MCPServerSpec: MCPServerSpec{
					Image:    "test:latest",
					Protocol: "sse",
					HttpPort: 9000,
					Command:  []string{"node", "server.js"},
					Env: map[string]string{
						"NODE_ENV": "production",
						"PORT":     "9000",
					},
					Resources: ResourceRequirements{
						Requests: ResourceList{
							"cpu":    "100m",
							"memory": "128Mi",
						},
						Limits: ResourceList{
							"cpu":    "500m",
							"memory": "512Mi",
						},
					},
				},
				ToolboxRole: "primary",
				Priority:    1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify structure
			assert.NotEmpty(t, tt.server.MCPServerSpec.Image)
			assert.NotEmpty(t, tt.server.MCPServerSpec.Protocol)
			assert.NotZero(t, tt.server.MCPServerSpec.HttpPort)
		})
	}
}

func TestToolboxDependency_Structure(t *testing.T) {
	tests := []struct {
		name       string
		dependency ToolboxDependency
	}{
		{
			name: "basic dependency",
			dependency: ToolboxDependency{
				Server:    "server2",
				DependsOn: []string{"server1"},
			},
		},
		{
			name: "dependency with options",
			dependency: ToolboxDependency{
				Server:    "server2",
				DependsOn: []string{"server1"},
				Optional:  true,
			},
		},
		{
			name: "multiple dependencies",
			dependency: ToolboxDependency{
				Server:    "server3",
				DependsOn: []string{"server1", "server2"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Verify structure
			assert.NotEmpty(t, tt.dependency.Server)
			assert.NotEmpty(t, tt.dependency.DependsOn)
		})
	}
}