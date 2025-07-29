package crd

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Test MCPServer DeepCopy methods
func TestMCPServer_DeepCopy(t *testing.T) {
	original := &MCPServer{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MCPServer",
			APIVersion: "mcp.matey.ai/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "default",
			Labels: map[string]string{
				"app": "test",
			},
			CreationTimestamp: metav1.Now(),
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
		Status: MCPServerStatus{
			Phase: "Running",
			Conditions: []MCPServerCondition{
				{
					Type:   "Ready",
					Status: "True",
				},
			},
		},
	}

	// Test DeepCopy
	copied := original.DeepCopy()
	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied == original {
		t.Error("DeepCopy returned same instance")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopy did not create equal copy")
	}

	// Modify original to ensure deep copy
	original.Spec.Image = "modified"
	if copied.Spec.Image == "modified" {
		t.Error("DeepCopy did not create independent copy")
	}
}

func TestMCPServer_DeepCopyInto(t *testing.T) {
	original := &MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-server",
		},
		Spec: MCPServerSpec{
			Image: "nginx:latest",
		},
	}

	var target MCPServer
	original.DeepCopyInto(&target)

	if !reflect.DeepEqual(*original, target) {
		t.Error("DeepCopyInto did not create equal copy")
	}

	// Modify original to ensure deep copy
	original.Spec.Image = "modified"
	if target.Spec.Image == "modified" {
		t.Error("DeepCopyInto did not create independent copy")
	}
}

func TestMCPServer_DeepCopyObject(t *testing.T) {
	original := &MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-server",
		},
		Spec: MCPServerSpec{
			Image: "nginx:latest",
		},
	}

	obj := original.DeepCopyObject()
	if obj == nil {
		t.Fatal("DeepCopyObject returned nil")
	}

	copied, ok := obj.(*MCPServer)
	if !ok {
		t.Fatal("DeepCopyObject returned wrong type")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopyObject did not create equal copy")
	}
}

func TestMCPServer_GroupVersionKind(t *testing.T) {
	server := &MCPServer{}
	gvk := server.GroupVersionKind()

	expected := schema.GroupVersionKind{
		Group:   "mcp.matey.ai",
		Version: "v1",
		Kind:    "MCPServer",
	}

	if gvk != expected {
		t.Errorf("GroupVersionKind() = %v, want %v", gvk, expected)
	}
}

// Test MCPServerList DeepCopy methods
func TestMCPServerList_DeepCopy(t *testing.T) {
	original := &MCPServerList{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MCPServerList",
			APIVersion: "mcp.matey.ai/v1",
		},
		ListMeta: metav1.ListMeta{
			ResourceVersion: "123",
		},
		Items: []MCPServer{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "server1",
				},
				Spec: MCPServerSpec{
					Image: "nginx:latest",
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "server2",
				},
				Spec: MCPServerSpec{
					Image: "apache:latest",
				},
			},
		},
	}

	copied := original.DeepCopy()
	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied == original {
		t.Error("DeepCopy returned same instance")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopy did not create equal copy")
	}

	// Modify original to ensure deep copy
	original.Items[0].Spec.Image = "modified"
	if copied.Items[0].Spec.Image == "modified" {
		t.Error("DeepCopy did not create independent copy")
	}
}

func TestMCPServerList_DeepCopyInto(t *testing.T) {
	original := &MCPServerList{
		Items: []MCPServer{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "server1",
				},
			},
		},
	}

	var target MCPServerList
	original.DeepCopyInto(&target)

	if !reflect.DeepEqual(*original, target) {
		t.Error("DeepCopyInto did not create equal copy")
	}
}

func TestMCPServerList_DeepCopyObject(t *testing.T) {
	original := &MCPServerList{
		Items: []MCPServer{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "server1",
				},
			},
		},
	}

	obj := original.DeepCopyObject()
	if obj == nil {
		t.Fatal("DeepCopyObject returned nil")
	}

	copied, ok := obj.(*MCPServerList)
	if !ok {
		t.Fatal("DeepCopyObject returned wrong type")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopyObject did not create equal copy")
	}
}

func TestMCPServerList_GroupVersionKind(t *testing.T) {
	serverList := &MCPServerList{}
	gvk := serverList.GroupVersionKind()

	expected := schema.GroupVersionKind{
		Group:   "mcp.matey.ai",
		Version: "v1",
		Kind:    "MCPServerList",
	}

	if gvk != expected {
		t.Errorf("GroupVersionKind() = %v, want %v", gvk, expected)
	}
}

// Test MCPServerSpec DeepCopy methods
func TestMCPServerSpec_DeepCopy(t *testing.T) {
	original := &MCPServerSpec{
		Image:    "nginx:latest",
		Protocol: "http",
		HttpPort: 8080,
		Command:  []string{"nginx", "-g", "daemon off;"},
		Args:     []string{"-c", "/etc/nginx/nginx.conf"},
		Env: map[string]string{
			"ENV1": "value1",
			"ENV2": "value2",
		},
		Resources: ResourceRequirements{
			Limits: ResourceList{
				"cpu":    "100m",
				"memory": "128Mi",
			},
		},
	}

	copied := original.DeepCopy()
	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied == original {
		t.Error("DeepCopy returned same instance")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopy did not create equal copy")
	}

	// Test deep copy of slices and maps
	original.Command[0] = "modified"
	if copied.Command[0] == "modified" {
		t.Error("DeepCopy did not create independent slice copy")
	}

	original.Env["ENV1"] = "modified"
	if copied.Env["ENV1"] == "modified" {
		t.Error("DeepCopy did not create independent map copy")
	}
}

func TestMCPServerSpec_DeepCopyInto(t *testing.T) {
	original := &MCPServerSpec{
		Image: "nginx:latest",
		Env: map[string]string{
			"ENV": "value",
		},
	}

	var target MCPServerSpec
	original.DeepCopyInto(&target)

	if !reflect.DeepEqual(*original, target) {
		t.Error("DeepCopyInto did not create equal copy")
	}
}

// Test MCPServerStatus DeepCopy methods
func TestMCPServerStatus_DeepCopy(t *testing.T) {
	now := metav1.Now()
	original := &MCPServerStatus{
		Phase:         "Running",
		ReadyReplicas: 1,
		Conditions: []MCPServerCondition{
			{
				Type:               "Ready",
				Status:             "True",
				LastTransitionTime: now,
				Reason:             "ServerReady",
				Message:            "Server is ready",
			},
		},
	}

	copied := original.DeepCopy()
	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied == original {
		t.Error("DeepCopy returned same instance")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopy did not create equal copy")
	}

	// Test deep copy of conditions slice
	original.Conditions[0].Message = "modified"
	if copied.Conditions[0].Message == "modified" {
		t.Error("DeepCopy did not create independent conditions copy")
	}
}

func TestMCPServerStatus_DeepCopyInto(t *testing.T) {
	original := &MCPServerStatus{
		Phase: "Running",
	}

	var target MCPServerStatus
	original.DeepCopyInto(&target)

	if !reflect.DeepEqual(*original, target) {
		t.Error("DeepCopyInto did not create equal copy")
	}
}

// Test ResourceRequirements DeepCopy methods
func TestResourceRequirements_DeepCopy(t *testing.T) {
	original := &ResourceRequirements{
		Limits: ResourceList{
			"cpu":    "100m",
			"memory": "128Mi",
		},
		Requests: ResourceList{
			"cpu":    "50m",
			"memory": "64Mi",
		},
	}

	copied := original.DeepCopy()
	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if copied == original {
		t.Error("DeepCopy returned same instance")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopy did not create equal copy")
	}

	// Test deep copy of resource maps
	original.Limits["cpu"] = "200m"
	if copied.Limits["cpu"] == "200m" {
		t.Error("DeepCopy did not create independent limits copy")
	}

	original.Requests["memory"] = "128Mi"
	if copied.Requests["memory"] == "128Mi" {
		t.Error("DeepCopy did not create independent requests copy")
	}
}

func TestResourceRequirements_DeepCopyInto(t *testing.T) {
	original := &ResourceRequirements{
		Limits: ResourceList{
			"cpu": "100m",
		},
	}

	var target ResourceRequirements
	original.DeepCopyInto(&target)

	if !reflect.DeepEqual(*original, target) {
		t.Error("DeepCopyInto did not create equal copy")
	}
}