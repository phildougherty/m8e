// internal/service/cluster_stats_test.go
package service

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	"github.com/phildougherty/m8e/internal/compose"
)

// stubStatusReader satisfies statusReader for tests.
type stubStatusReader struct {
	status *compose.ComposeStatus
	err    error
}

func (s stubStatusReader) Status() (*compose.ComposeStatus, error) {
	return s.status, s.err
}

func TestClusterStats_ServerInfos(t *testing.T) {
	status := &compose.ComposeStatus{
		Services: map[string]*compose.ServiceStatus{
			"filesystem": {Name: "filesystem", Status: "running", Type: "mcp-server"},
		},
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "filesystem",
			Namespace: "matey",
			Labels:    map[string]string{"mcp.matey.ai/protocol": "http"},
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "filesystem",
							Image: "matey/filesystem:latest",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
							},
							Ports: []corev1.ContainerPort{{ContainerPort: 8080}},
						},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{ReadyReplicas: 1, Replicas: 1},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "filesystem-abc",
			Namespace: "matey",
			Labels:    map[string]string{"app": "filesystem"},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{{RestartCount: 3}},
		},
	}

	k8sClient := k8sfake.NewSimpleClientset(dep, pod)
	stats := NewClusterStats(stubStatusReader{status: status}, k8sClient, "matey")

	infos, err := stats.ServerInfos(context.Background())
	if err != nil {
		t.Fatalf("ServerInfos: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("expected 1 server info, got %d", len(infos))
	}

	got := infos[0]
	if got.Name != "filesystem" || got.Status != "running" || got.Type != "mcp-server" {
		t.Errorf("unexpected base info: %+v", got)
	}
	if got.Image != "matey/filesystem:latest" {
		t.Errorf("expected image from deployment, got %q", got.Image)
	}
	if got.CPU != "100m" || got.Memory != "128Mi" {
		t.Errorf("expected resource requests, got cpu=%q mem=%q", got.CPU, got.Memory)
	}
	if got.Port != 8080 {
		t.Errorf("expected port 8080, got %d", got.Port)
	}
	if got.Protocol != "http" {
		t.Errorf("expected protocol http from label, got %q", got.Protocol)
	}
	if got.Replicas != "1/1" {
		t.Errorf("expected replicas 1/1, got %q", got.Replicas)
	}
	if !got.Ready {
		t.Error("expected Ready to be true")
	}
	if got.Restarts != 3 {
		t.Errorf("expected 3 restarts aggregated from pod, got %d", got.Restarts)
	}
}

func TestClusterStats_ServerInfos_NoDeployment(t *testing.T) {
	status := &compose.ComposeStatus{
		Services: map[string]*compose.ServiceStatus{
			"orphan": {Name: "orphan", Status: "pending", Type: "mcp-server"},
		},
	}
	k8sClient := k8sfake.NewSimpleClientset()
	stats := NewClusterStats(stubStatusReader{status: status}, k8sClient, "matey")

	infos, err := stats.ServerInfos(context.Background())
	if err != nil {
		t.Fatalf("ServerInfos: %v", err)
	}
	if len(infos) != 1 || infos[0].Name != "orphan" {
		t.Fatalf("expected single orphan server, got %+v", infos)
	}
	// No deployment means zero-value enrichment fields.
	if infos[0].Image != "" || infos[0].Port != 0 {
		t.Errorf("expected empty enrichment for server without deployment, got %+v", infos[0])
	}
}

func TestClusterStats_StatusError(t *testing.T) {
	k8sClient := k8sfake.NewSimpleClientset()
	stats := NewClusterStats(stubStatusReader{err: errStub}, k8sClient, "matey")

	if _, err := stats.ServerInfos(context.Background()); err == nil {
		t.Fatal("expected error when composer status fails")
	}
}

var errStub = &stubError{}

type stubError struct{}

func (e *stubError) Error() string { return "stub status failure" }
