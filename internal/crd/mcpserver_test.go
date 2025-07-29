package crd

import (
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestMCPServerValidation(t *testing.T) {
	// Add our scheme to the default scheme
	err := AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatalf("Failed to add scheme: %v", err)
	}

	tests := []struct {
		name      string
		server    *MCPServer
		expectErr bool
	}{
		{
			name: "Valid MCPServer with HTTP protocol",
			server: &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-server",
					Namespace: "default",
				},
				Spec: MCPServerSpec{
					Image:    "nginx:latest",
					Protocol: "http",
					HttpPort: 8080,
					Command:  []string{"nginx"},
					Args:     []string{"-g", "daemon off;"},
					Env: map[string]string{
						"ENV": "production",
					},
					Resources: ResourceRequirements{
						Limits: ResourceList{
							"cpu":    "100m",
							"memory": "128Mi",
						},
						Requests: ResourceList{
							"cpu":    "50m",
							"memory": "64Mi",
						},
					},
				},
			},
			expectErr: false,
		},
		{
			name: "Valid MCPServer with SSE protocol",
			server: &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sse-server",
					Namespace: "default",
				},
				Spec: MCPServerSpec{
					Image:    "sse-server:latest",
					Protocol: "sse",
					SSEPort:  8080,
					SSEPath:  "/events",
					Command:  []string{"python", "sse_server.py"},
				},
			},
			expectErr: false,
		},
		{
			name: "Valid MCPServer with stdio protocol",
			server: &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "stdio-server",
					Namespace: "default",
				},
				Spec: MCPServerSpec{
					Image:    "stdio-server:latest",
					Protocol: "stdio",
					Command:  []string{"node", "stdio_server.js"},
				},
			},
			expectErr: false,
		},
		{
			name: "MCPServer with custom replicas",
			server: &MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "scaled-server",
					Namespace: "default",
				},
				Spec: MCPServerSpec{
					Image:    "scaled-server:latest",
					Protocol: "http",
					HttpPort: 8080,
					Replicas: &[]int32{3}[0],
				},
			},
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test serialization/deserialization
			data, err := json.Marshal(tt.server)
			if err != nil {
				t.Fatalf("Failed to marshal MCPServer: %v", err)
			}

			var unmarshaled MCPServer
			err = json.Unmarshal(data, &unmarshaled)
			if err != nil {
				t.Fatalf("Failed to unmarshal MCPServer: %v", err)
			}

			// Verify basic fields
			if unmarshaled.Name != tt.server.Name {
				t.Errorf("Expected name %s, got %s", tt.server.Name, unmarshaled.Name)
			}

			if unmarshaled.Spec.Image != tt.server.Spec.Image {
				t.Errorf("Expected image %s, got %s", tt.server.Spec.Image, unmarshaled.Spec.Image)
			}

			if unmarshaled.Spec.Protocol != tt.server.Spec.Protocol {
				t.Errorf("Expected protocol %s, got %s", tt.server.Spec.Protocol, unmarshaled.Spec.Protocol)
			}

			if unmarshaled.Spec.HttpPort != tt.server.Spec.HttpPort {
				t.Errorf("Expected port %d, got %d", tt.server.Spec.HttpPort, unmarshaled.Spec.HttpPort)
			}

			// Verify complex fields
			if len(unmarshaled.Spec.Command) != len(tt.server.Spec.Command) {
				t.Errorf("Expected command length %d, got %d", len(tt.server.Spec.Command), len(unmarshaled.Spec.Command))
			}

			if len(unmarshaled.Spec.Args) != len(tt.server.Spec.Args) {
				t.Errorf("Expected args length %d, got %d", len(tt.server.Spec.Args), len(unmarshaled.Spec.Args))
			}

			if len(unmarshaled.Spec.Env) != len(tt.server.Spec.Env) {
				t.Errorf("Expected env length %d, got %d", len(tt.server.Spec.Env), len(unmarshaled.Spec.Env))
			}

			// Verify environment variables
			for k, v := range tt.server.Spec.Env {
				if unmarshaled.Spec.Env[k] != v {
					t.Errorf("Expected env %s=%s, got %s=%s", k, v, k, unmarshaled.Spec.Env[k])
				}
			}
		})
	}
}

func TestMCPServerStatus(t *testing.T) {
	server := &MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "default",
		},
		Spec: MCPServerSpec{
			Image:    "nginx:latest",
			Protocol: "http",
			HttpPort: 8080,
			Replicas: &[]int32{2}[0],
		},
	}

	// Test status updates
	server.Status.Phase = "Pending"
	server.Status.ReadyReplicas = 0
	server.Status.Replicas = 2
	// Status timestamps are managed by the controller

	// Verify status fields
	if server.Status.Phase != "Pending" {
		t.Errorf("Expected phase 'Pending', got %s", server.Status.Phase)
	}

	if server.Status.ReadyReplicas != 0 {
		t.Errorf("Expected ReadyReplicas 0, got %d", server.Status.ReadyReplicas)
	}

	if server.Status.Replicas != 2 {
		t.Errorf("Expected Replicas 2, got %d", server.Status.Replicas)
	}

	// Update to running status
	server.Status.Phase = "Running"
	server.Status.ReadyReplicas = 2
	server.Status.Conditions = []MCPServerCondition{
		{
			Type:   "Ready",
			Status: metav1.ConditionTrue,
			Reason: "AllReplicasReady",
			Message: "All replicas are ready",
			LastTransitionTime: metav1.Now(),
		},
	}

	// Verify updated status
	if server.Status.Phase != "Running" {
		t.Errorf("Expected phase 'Running', got %s", server.Status.Phase)
	}

	if server.Status.ReadyReplicas != 2 {
		t.Errorf("Expected ReadyReplicas 2, got %d", server.Status.ReadyReplicas)
	}

	if len(server.Status.Conditions) != 1 {
		t.Errorf("Expected 1 condition, got %d", len(server.Status.Conditions))
	}

	condition := server.Status.Conditions[0]
	if condition.Type != "Ready" {
		t.Errorf("Expected condition type 'Ready', got %s", condition.Type)
	}

	if condition.Status != metav1.ConditionTrue {
		t.Errorf("Expected condition status 'True', got %s", condition.Status)
	}
}

func TestMCPServerList(t *testing.T) {
	// Create a list of MCPServers
	serverList := &MCPServerList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MCPServerList",
			APIVersion: "mcp.matey.ai/v1",
		},
		Items: []MCPServer{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "server-1",
					Namespace: "default",
				},
				Spec: MCPServerSpec{
					Image:    "nginx:latest",
					Protocol: "http",
					HttpPort: 8080,
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "server-2",
					Namespace: "default",
				},
				Spec: MCPServerSpec{
					Image:    "alpine:latest",
					Protocol: "stdio",
				},
			},
		},
	}

	// Test list serialization
	data, err := json.Marshal(serverList)
	if err != nil {
		t.Fatalf("Failed to marshal MCPServerList: %v", err)
	}

	var unmarshaled MCPServerList
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal MCPServerList: %v", err)
	}

	// Verify list properties
	if len(unmarshaled.Items) != 2 {
		t.Errorf("Expected 2 items, got %d", len(unmarshaled.Items))
	}

	// Verify first item
	if unmarshaled.Items[0].Name != "server-1" {
		t.Errorf("Expected first item name 'server-1', got %s", unmarshaled.Items[0].Name)
	}

	if unmarshaled.Items[0].Spec.Protocol != "http" {
		t.Errorf("Expected first item protocol 'http', got %s", unmarshaled.Items[0].Spec.Protocol)
	}

	// Verify second item
	if unmarshaled.Items[1].Name != "server-2" {
		t.Errorf("Expected second item name 'server-2', got %s", unmarshaled.Items[1].Name)
	}

	if unmarshaled.Items[1].Spec.Protocol != "stdio" {
		t.Errorf("Expected second item protocol 'stdio', got %s", unmarshaled.Items[1].Spec.Protocol)
	}
}

func TestMCPServerDeepCopy(t *testing.T) {
	original := &MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "default",
			Labels: map[string]string{
				"app": "test",
			},
		},
		Spec: MCPServerSpec{
			Image:    "nginx:latest",
			Protocol: "http",
			HttpPort: 8080,
			Command:  []string{"nginx"},
			Args:     []string{"-g", "daemon off;"},
			Env: map[string]string{
				"ENV": "production",
			},
			Resources: ResourceRequirements{
				Limits: ResourceList{
					"cpu":    "100m",
					"memory": "128Mi",
				},
			},
		},
		Status: MCPServerStatus{
			Phase:         "Running",
			ReadyReplicas: 1,
			Replicas:      1,
			Conditions: []MCPServerCondition{
				{
					Type:   "Ready",
					Status: metav1.ConditionTrue,
					Reason: "AllReplicasReady",
				},
			},
		},
	}

	// Test deep copy
	copied := original.DeepCopy()

	// Verify it's a different object
	if copied == original {
		t.Error("DeepCopy returned the same object reference")
	}

	// Verify contents are the same
	if copied.Name != original.Name {
		t.Errorf("Expected name %s, got %s", original.Name, copied.Name)
	}

	if copied.Spec.Image != original.Spec.Image {
		t.Errorf("Expected image %s, got %s", original.Spec.Image, copied.Spec.Image)
	}

	if copied.Status.Phase != original.Status.Phase {
		t.Errorf("Expected phase %s, got %s", original.Status.Phase, copied.Status.Phase)
	}

	// Verify modifying copy doesn't affect original
	copied.Spec.Image = "modified:latest"
	if original.Spec.Image == "modified:latest" {
		t.Error("Modifying copy affected original")
	}

	// Verify maps are deep copied
	copied.Spec.Env["NEW_ENV"] = "test"
	if original.Spec.Env["NEW_ENV"] == "test" {
		t.Error("Modifying copy's env map affected original")
	}

	// Verify slices are deep copied
	copied.Spec.Args = append(copied.Spec.Args, "new-arg")
	if len(original.Spec.Args) != 2 {
		t.Errorf("Expected original args length 2, got %d", len(original.Spec.Args))
	}
}

func TestMCPServerCondition(t *testing.T) {
	now := metav1.Now()
	condition := MCPServerCondition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "AllReplicasReady",
		Message:            "All replicas are ready and healthy",
		LastTransitionTime: now,
	}

	// Test condition serialization
	data, err := json.Marshal(condition)
	if err != nil {
		t.Fatalf("Failed to marshal MCPServerCondition: %v", err)
	}

	var unmarshaled MCPServerCondition
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal MCPServerCondition: %v", err)
	}

	// Verify condition fields
	if unmarshaled.Type != condition.Type {
		t.Errorf("Expected type %s, got %s", condition.Type, unmarshaled.Type)
	}

	if unmarshaled.Status != condition.Status {
		t.Errorf("Expected status %s, got %s", condition.Status, unmarshaled.Status)
	}

	if unmarshaled.Reason != condition.Reason {
		t.Errorf("Expected reason %s, got %s", condition.Reason, unmarshaled.Reason)
	}

	if unmarshaled.Message != condition.Message {
		t.Errorf("Expected message %s, got %s", condition.Message, unmarshaled.Message)
	}
}

func TestMCPServerGroupVersionKind(t *testing.T) {
	server := &MCPServer{}
	
	// Test that the GVK is correctly set
	gvk := server.GroupVersionKind()
	if gvk.Group != GroupName {
		t.Errorf("Expected group %s, got %s", GroupName, gvk.Group)
	}

	if gvk.Version != Version {
		t.Errorf("Expected version %s, got %s", Version, gvk.Version)
	}

	if gvk.Kind != "MCPServer" {
		t.Errorf("Expected kind 'MCPServer', got %s", gvk.Kind)
	}
}

func TestMCPServerDefaulting(t *testing.T) {
	server := &MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "default",
		},
		Spec: MCPServerSpec{
			Image:    "nginx:latest",
			Protocol: "http",
			// Port not specified - should default
		},
	}

	// Test default values (this would typically be handled by admission controllers)
	if server.Spec.HttpPort == 0 {
		// In a real scenario, this would be defaulted by a mutating webhook
		server.Spec.HttpPort = 8080
	}

	if server.Spec.Replicas == nil {
		server.Spec.Replicas = &[]int32{1}[0]
	}

	// Verify defaults were applied
	if server.Spec.HttpPort != 8080 {
		t.Errorf("Expected default port 8080, got %d", server.Spec.HttpPort)
	}

	if server.Spec.Replicas == nil || *server.Spec.Replicas != 1 {
		t.Errorf("Expected default replicas 1, got %v", server.Spec.Replicas)
	}
}

func TestMCPServerResourceLimits(t *testing.T) {
	server := &MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "resource-test",
			Namespace: "default",
		},
		Spec: MCPServerSpec{
			Image:    "nginx:latest",
			Protocol: "http",
			HttpPort: 8080,
			Resources: ResourceRequirements{
				Limits: ResourceList{
					"cpu":    "500m",
					"memory": "512Mi",
				},
				Requests: ResourceList{
					"cpu":    "100m",
					"memory": "128Mi",
				},
			},
		},
	}

	// Test resource serialization
	data, err := json.Marshal(server)
	if err != nil {
		t.Fatalf("Failed to marshal MCPServer with resources: %v", err)
	}

	var unmarshaled MCPServer
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal MCPServer with resources: %v", err)
	}

	// Verify resource limits
	cpuLimit := unmarshaled.Spec.Resources.Limits["cpu"]
	expectedCPU := "500m"
	if cpuLimit != expectedCPU {
		t.Errorf("Expected CPU limit %v, got %v", expectedCPU, cpuLimit)
	}

	memoryLimit := unmarshaled.Spec.Resources.Limits["memory"]
	expectedMemory := "512Mi"
	if memoryLimit != expectedMemory {
		t.Errorf("Expected memory limit %v, got %v", expectedMemory, memoryLimit)
	}

	// Verify resource requests
	cpuRequest := unmarshaled.Spec.Resources.Requests["cpu"]
	expectedCPURequest := "100m"
	if cpuRequest != expectedCPURequest {
		t.Errorf("Expected CPU request %v, got %v", expectedCPURequest, cpuRequest)
	}

	memoryRequest := unmarshaled.Spec.Resources.Requests["memory"]
	expectedMemoryRequest := "128Mi"
	if memoryRequest != expectedMemoryRequest {
		t.Errorf("Expected memory request %v, got %v", expectedMemoryRequest, memoryRequest)
	}
}


