package crd

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Test MCPProxy DeepCopy methods
func TestMCPProxy_DeepCopy(t *testing.T) {
	replicas := int32(2)
	original := &MCPProxy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MCPProxy",
			APIVersion: "mcp.matey.ai/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-proxy",
			Namespace: "default",
		},
		Spec: MCPProxySpec{
			Port: 8080,
			Host: "localhost",
			TLS: &TLSConfig{
				Enabled:    true,
				CertFile:   "/certs/tls.crt",
				KeyFile:    "/certs/tls.key",
				SecretName: "tls-secret",
			},
			Replicas: &replicas,
		},
		Status: MCPProxyStatus{
			Phase: "Running",
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

	// Test deep copy independence
	original.Spec.Host = "modified"
	if copied.Spec.Host == "modified" {
		t.Error("DeepCopy did not create independent copy")
	}
}

func TestMCPProxy_DeepCopyInto(t *testing.T) {
	original := &MCPProxy{
		ObjectMeta: metav1.ObjectMeta{Name: "test-proxy"},
		Spec:       MCPProxySpec{Port: 8080},
	}

	var target MCPProxy
	original.DeepCopyInto(&target)

	if !reflect.DeepEqual(*original, target) {
		t.Error("DeepCopyInto did not create equal copy")
	}
}

func TestMCPProxy_DeepCopyObject(t *testing.T) {
	original := &MCPProxy{
		ObjectMeta: metav1.ObjectMeta{Name: "test-proxy"},
	}

	obj := original.DeepCopyObject()
	if obj == nil {
		t.Fatal("DeepCopyObject returned nil")
	}

	copied, ok := obj.(*MCPProxy)
	if !ok {
		t.Fatal("DeepCopyObject returned wrong type")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopyObject did not create equal copy")
	}
}

func TestMCPProxy_GroupVersionKind(t *testing.T) {
	proxy := &MCPProxy{}
	gvk := proxy.GroupVersionKind()

	expected := schema.GroupVersionKind{
		Group:   "mcp.matey.ai",
		Version: "v1",
		Kind:    "MCPProxy",
	}

	if gvk != expected {
		t.Errorf("GroupVersionKind() = %v, want %v", gvk, expected)
	}
}

// Test MCPProxyList DeepCopy methods
func TestMCPProxyList_DeepCopy(t *testing.T) {
	original := &MCPProxyList{
		Items: []MCPProxy{
			{ObjectMeta: metav1.ObjectMeta{Name: "proxy1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "proxy2"}},
		},
	}

	copied := original.DeepCopy()
	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopy did not create equal copy")
	}
}

func TestMCPProxyList_DeepCopyInto(t *testing.T) {
	original := &MCPProxyList{
		Items: []MCPProxy{{ObjectMeta: metav1.ObjectMeta{Name: "proxy1"}}},
	}

	var target MCPProxyList
	original.DeepCopyInto(&target)

	if !reflect.DeepEqual(*original, target) {
		t.Error("DeepCopyInto did not create equal copy")
	}
}

func TestMCPProxyList_DeepCopyObject(t *testing.T) {
	original := &MCPProxyList{}

	obj := original.DeepCopyObject()
	if obj == nil {
		t.Fatal("DeepCopyObject returned nil")
	}

	if _, ok := obj.(*MCPProxyList); !ok {
		t.Fatal("DeepCopyObject returned wrong type")
	}
}

func TestMCPProxyList_GroupVersionKind(t *testing.T) {
	proxyList := &MCPProxyList{}
	gvk := proxyList.GroupVersionKind()

	expected := schema.GroupVersionKind{
		Group:   "mcp.matey.ai",
		Version: "v1",
		Kind:    "MCPProxyList",
	}

	if gvk != expected {
		t.Errorf("GroupVersionKind() = %v, want %v", gvk, expected)
	}
}

// Test MCPProxySpec DeepCopy methods
func TestMCPProxySpec_DeepCopy(t *testing.T) {
	replicas := int32(3)
	original := &MCPProxySpec{
		Port: 8080,
		Host: "localhost",
		TLS: &TLSConfig{
			Enabled:    true,
			CertFile:   "/certs/tls.crt",
			KeyFile:    "/certs/tls.key",
			SecretName: "tls-secret",
		},
		Replicas: &replicas,
		Labels: map[string]string{
			"app": "proxy",
		},
	}

	copied := original.DeepCopy()
	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopy did not create equal copy")
	}

	// Test deep copy independence  
	original.Host = "modified-host"
	if copied.Host == "modified-host" {
		t.Error("DeepCopy did not create independent copy")
	}
}

func TestMCPProxySpec_DeepCopyInto(t *testing.T) {
	original := &MCPProxySpec{Port: 8080, Host: "localhost"}

	var target MCPProxySpec
	original.DeepCopyInto(&target)

	if !reflect.DeepEqual(*original, target) {
		t.Error("DeepCopyInto did not create equal copy")
	}
}

// Test MCPProxyStatus DeepCopy methods
func TestMCPProxyStatus_DeepCopy(t *testing.T) {
	original := &MCPProxyStatus{
		Phase: "Running",
	}

	copied := original.DeepCopy()
	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopy did not create equal copy")
	}
}

func TestMCPProxyStatus_DeepCopyInto(t *testing.T) {
	original := &MCPProxyStatus{Phase: "Running"}

	var target MCPProxyStatus
	original.DeepCopyInto(&target)

	if !reflect.DeepEqual(*original, target) {
		t.Error("DeepCopyInto did not create equal copy")
	}
}

// Test MCPMemory DeepCopy methods
func TestMCPMemory_DeepCopy(t *testing.T) {
	original := &MCPMemory{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MCPMemory",
			APIVersion: "mcp.matey.ai/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-memory",
			Namespace: "default",
		},
		Spec: MCPMemorySpec{
			Port:            5432,
			Host:            "localhost",
			PostgresEnabled: true,
		},
		Status: MCPMemoryStatus{
			Phase: "Bound",
		},
	}

	copied := original.DeepCopy()
	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopy did not create equal copy")
	}
}

func TestMCPMemory_DeepCopyInto(t *testing.T) {
	original := &MCPMemory{
		ObjectMeta: metav1.ObjectMeta{Name: "test-memory"},
		Spec:       MCPMemorySpec{Port: 5432},
	}

	var target MCPMemory
	original.DeepCopyInto(&target)

	if !reflect.DeepEqual(*original, target) {
		t.Error("DeepCopyInto did not create equal copy")
	}
}

func TestMCPMemory_DeepCopyObject(t *testing.T) {
	original := &MCPMemory{
		ObjectMeta: metav1.ObjectMeta{Name: "test-memory"},
	}

	obj := original.DeepCopyObject()
	if obj == nil {
		t.Fatal("DeepCopyObject returned nil")
	}

	copied, ok := obj.(*MCPMemory)
	if !ok {
		t.Fatal("DeepCopyObject returned wrong type")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopyObject did not create equal copy")
	}
}

func TestMCPMemory_GroupVersionKind(t *testing.T) {
	memory := &MCPMemory{}
	gvk := memory.GroupVersionKind()

	expected := schema.GroupVersionKind{
		Group:   "mcp.matey.ai",
		Version: "v1",
		Kind:    "MCPMemory",
	}

	if gvk != expected {
		t.Errorf("GroupVersionKind() = %v, want %v", gvk, expected)
	}
}

// Test MCPMemoryList DeepCopy methods
func TestMCPMemoryList_DeepCopy(t *testing.T) {
	original := &MCPMemoryList{
		Items: []MCPMemory{
			{ObjectMeta: metav1.ObjectMeta{Name: "memory1"}},
		},
	}

	copied := original.DeepCopy()
	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopy did not create equal copy")
	}
}

func TestMCPMemoryList_DeepCopyInto(t *testing.T) {
	original := &MCPMemoryList{
		Items: []MCPMemory{{ObjectMeta: metav1.ObjectMeta{Name: "memory1"}}},
	}

	var target MCPMemoryList
	original.DeepCopyInto(&target)

	if !reflect.DeepEqual(*original, target) {
		t.Error("DeepCopyInto did not create equal copy")
	}
}

func TestMCPMemoryList_DeepCopyObject(t *testing.T) {
	original := &MCPMemoryList{}

	obj := original.DeepCopyObject()
	if obj == nil {
		t.Fatal("DeepCopyObject returned nil")
	}

	if _, ok := obj.(*MCPMemoryList); !ok {
		t.Fatal("DeepCopyObject returned wrong type")
	}
}

func TestMCPMemoryList_GroupVersionKind(t *testing.T) {
	memoryList := &MCPMemoryList{}
	gvk := memoryList.GroupVersionKind()

	expected := schema.GroupVersionKind{
		Group:   "mcp.matey.ai",
		Version: "v1",
		Kind:    "MCPMemoryList",
	}

	if gvk != expected {
		t.Errorf("GroupVersionKind() = %v, want %v", gvk, expected)
	}
}

// Test MCPMemorySpec DeepCopy methods
func TestMCPMemorySpec_DeepCopy(t *testing.T) {
	original := &MCPMemorySpec{
		Port:            5432,
		Host:            "localhost",
		PostgresEnabled: true,
		PostgresDB:      "memory",
	}

	copied := original.DeepCopy()
	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopy did not create equal copy")
	}

	// Test field independence
	original.Host = "modified"
	if copied.Host == "modified" {
		t.Error("DeepCopy did not create independent copy")
	}
}

func TestMCPMemorySpec_DeepCopyInto(t *testing.T) {
	original := &MCPMemorySpec{Port: 5432}

	var target MCPMemorySpec
	original.DeepCopyInto(&target)

	if !reflect.DeepEqual(*original, target) {
		t.Error("DeepCopyInto did not create equal copy")
	}
}

// Test MCPMemoryStatus DeepCopy methods
func TestMCPMemoryStatus_DeepCopy(t *testing.T) {
	original := &MCPMemoryStatus{
		Phase: "Running",
	}

	copied := original.DeepCopy()
	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopy did not create equal copy")
	}
}

func TestMCPMemoryStatus_DeepCopyInto(t *testing.T) {
	original := &MCPMemoryStatus{Phase: "Running"}

	var target MCPMemoryStatus
	original.DeepCopyInto(&target)

	if !reflect.DeepEqual(*original, target) {
		t.Error("DeepCopyInto did not create equal copy")
	}
}

// Test MCPPostgres DeepCopy methods
func TestMCPPostgres_DeepCopy(t *testing.T) {
	original := &MCPPostgres{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MCPPostgres",
			APIVersion: "mcp.matey.ai/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-postgres",
			Namespace: "default",
		},
		Spec: MCPPostgresSpec{
			Database: "testdb",
			User:     "testuser",
			Password: "testpass",
			Port:     5432,
		},
		Status: MCPPostgresStatus{
			Phase: "Running",
		},
	}

	copied := original.DeepCopy()
	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopy did not create equal copy")
	}
}

func TestMCPPostgres_DeepCopyInto(t *testing.T) {
	original := &MCPPostgres{
		ObjectMeta: metav1.ObjectMeta{Name: "test-postgres"},
		Spec:       MCPPostgresSpec{Database: "testdb"},
	}

	var target MCPPostgres
	original.DeepCopyInto(&target)

	if !reflect.DeepEqual(*original, target) {
		t.Error("DeepCopyInto did not create equal copy")
	}
}

func TestMCPPostgres_DeepCopyObject(t *testing.T) {
	original := &MCPPostgres{
		ObjectMeta: metav1.ObjectMeta{Name: "test-postgres"},
	}

	obj := original.DeepCopyObject()
	if obj == nil {
		t.Fatal("DeepCopyObject returned nil")
	}

	copied, ok := obj.(*MCPPostgres)
	if !ok {
		t.Fatal("DeepCopyObject returned wrong type")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopyObject did not create equal copy")
	}
}

func TestMCPPostgres_GroupVersionKind(t *testing.T) {
	postgres := &MCPPostgres{}
	gvk := postgres.GroupVersionKind()

	expected := schema.GroupVersionKind{
		Group:   "mcp.matey.ai",
		Version: "v1",
		Kind:    "MCPPostgres",
	}

	if gvk != expected {
		t.Errorf("GroupVersionKind() = %v, want %v", gvk, expected)
	}
}

// Test MCPTaskScheduler DeepCopy methods
func TestMCPTaskScheduler_DeepCopy(t *testing.T) {
	original := &MCPTaskScheduler{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MCPTaskScheduler",
			APIVersion: "mcp.matey.ai/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-scheduler",
			Namespace: "default",
		},
		Spec: MCPTaskSchedulerSpec{
			Image:           "scheduler:latest",
			Port:            8080,
			DatabasePath:    "/data/scheduler.db",
			PostgresEnabled: false,
			LogLevel:        "info",
			Resources: ResourceRequirements{
				Limits: ResourceList{
					"cpu":    "500m",
					"memory": "512Mi",
				},
			},
		},
		Status: MCPTaskSchedulerStatus{
			Phase: "Running",
			TaskStats: TaskStatistics{
				TotalTasks:     100,
				CompletedTasks: 90,
				RunningTasks:   5,
				FailedTasks:    5,
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

	// Test deep copy independence
	original.Spec.LogLevel = "debug"
	if copied.Spec.LogLevel == "debug" {
		t.Error("DeepCopy did not create independent copy")
	}
}

func TestMCPTaskScheduler_DeepCopyInto(t *testing.T) {
	original := &MCPTaskScheduler{
		ObjectMeta: metav1.ObjectMeta{Name: "test-scheduler"},
		Spec:       MCPTaskSchedulerSpec{Image: "scheduler:latest"},
	}

	var target MCPTaskScheduler
	original.DeepCopyInto(&target)

	if !reflect.DeepEqual(*original, target) {
		t.Error("DeepCopyInto did not create equal copy")
	}
}

func TestMCPTaskScheduler_DeepCopyObject(t *testing.T) {
	original := &MCPTaskScheduler{
		ObjectMeta: metav1.ObjectMeta{Name: "test-scheduler"},
	}

	obj := original.DeepCopyObject()
	if obj == nil {
		t.Fatal("DeepCopyObject returned nil")
	}

	copied, ok := obj.(*MCPTaskScheduler)
	if !ok {
		t.Fatal("DeepCopyObject returned wrong type")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopyObject did not create equal copy")
	}
}

func TestMCPTaskScheduler_GroupVersionKind(t *testing.T) {
	scheduler := &MCPTaskScheduler{}
	gvk := scheduler.GroupVersionKind()

	expected := schema.GroupVersionKind{
		Group:   "mcp.matey.ai",
		Version: "v1",
		Kind:    "MCPTaskScheduler",
	}

	if gvk != expected {
		t.Errorf("GroupVersionKind() = %v, want %v", gvk, expected)
	}
}

// Test MCPTaskSchedulerList DeepCopy methods
func TestMCPTaskSchedulerList_DeepCopy(t *testing.T) {
	original := &MCPTaskSchedulerList{
		Items: []MCPTaskScheduler{
			{ObjectMeta: metav1.ObjectMeta{Name: "scheduler1"}},
			{ObjectMeta: metav1.ObjectMeta{Name: "scheduler2"}},
		},
	}

	copied := original.DeepCopy()
	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}

	if !reflect.DeepEqual(original, copied) {
		t.Error("DeepCopy did not create equal copy")
	}
}

func TestMCPTaskSchedulerList_DeepCopyInto(t *testing.T) {
	original := &MCPTaskSchedulerList{
		Items: []MCPTaskScheduler{{ObjectMeta: metav1.ObjectMeta{Name: "scheduler1"}}},
	}

	var target MCPTaskSchedulerList
	original.DeepCopyInto(&target)

	if !reflect.DeepEqual(*original, target) {
		t.Error("DeepCopyInto did not create equal copy")
	}
}

func TestMCPTaskSchedulerList_DeepCopyObject(t *testing.T) {
	original := &MCPTaskSchedulerList{}

	obj := original.DeepCopyObject()
	if obj == nil {
		t.Fatal("DeepCopyObject returned nil")
	}

	if _, ok := obj.(*MCPTaskSchedulerList); !ok {
		t.Fatal("DeepCopyObject returned wrong type")
	}
}

func TestMCPTaskSchedulerList_GroupVersionKind(t *testing.T) {
	schedulerList := &MCPTaskSchedulerList{}
	gvk := schedulerList.GroupVersionKind()

	expected := schema.GroupVersionKind{
		Group:   "mcp.matey.ai",
		Version: "v1",
		Kind:    "MCPTaskSchedulerList",
	}

	if gvk != expected {
		t.Errorf("GroupVersionKind() = %v, want %v", gvk, expected)
	}
}
