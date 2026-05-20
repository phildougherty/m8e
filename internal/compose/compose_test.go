// internal/compose/compose_test.go
package compose

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/logging"
)

// newTestScheme builds a runtime.Scheme with the core k8s types and the matey
// CRDs registered, mirroring createK8sClient's scheme so the fake client can
// store the same objects the production client does.
func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 to scheme: %v", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("add appsv1 to scheme: %v", err)
	}
	if err := crd.AddToScheme(scheme); err != nil {
		t.Fatalf("add crd to scheme: %v", err)
	}
	return scheme
}

// newTestComposer returns a K8sComposer wired to a controller-runtime fake
// client seeded with the supplied objects. It deliberately avoids
// NewK8sComposer, which requires a live kubeconfig.
func newTestComposer(t *testing.T, cfg *config.ComposeConfig, namespace string, objs ...client.Object) *K8sComposer {
	t.Helper()
	if cfg == nil {
		cfg = &config.ComposeConfig{Version: "1"}
	}
	if namespace == "" {
		namespace = constants.MateyNamespace
	}
	scheme := newTestScheme(t)
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		Build()
	return &K8sComposer{
		config:    cfg,
		k8sClient: fakeClient,
		namespace: namespace,
		logger:    logging.NewLogger("error"),
	}
}

func TestGetDeploymentName(t *testing.T) {
	tests := []struct {
		name        string
		serviceName string
		want        string
	}{
		{"proxy alias maps to deployment", "proxy", "matey-proxy"},
		{"memory alias maps to deployment", "memory", "matey-memory"},
		{"controller-manager alias maps to deployment", "controller-manager", "matey-controller-manager"},
		{"unknown service passes through unchanged", "my-custom-server", "my-custom-server"},
		{"already-qualified name passes through", "matey-proxy", "matey-proxy"},
		{"empty string passes through", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getDeploymentName(tt.serviceName); got != tt.want {
				t.Errorf("getDeploymentName(%q) = %q, want %q", tt.serviceName, got, tt.want)
			}
		})
	}
}

func TestGetPodHealthStatus(t *testing.T) {
	readyContainers := []corev1.ContainerStatus{{Ready: true}, {Ready: true}}
	notReadyContainers := []corev1.ContainerStatus{{Ready: true}, {Ready: false}}

	tests := []struct {
		name string
		pod  *corev1.Pod
		want string
	}{
		{
			name: "running with all containers ready is healthy",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: readyContainers,
			}},
			want: "healthy",
		},
		{
			name: "running with a not-ready container is unhealthy",
			pod: &corev1.Pod{Status: corev1.PodStatus{
				Phase:             corev1.PodRunning,
				ContainerStatuses: notReadyContainers,
			}},
			want: "unhealthy",
		},
		{
			name: "pending pod is starting",
			pod:  &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodPending}},
			want: "starting",
		},
		{
			name: "succeeded pod is completed",
			pod:  &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodSucceeded}},
			want: "completed",
		},
		{
			name: "failed pod is failed",
			pod:  &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodFailed}},
			want: "failed",
		},
		{
			name: "unknown phase is unknown",
			pod:  &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodUnknown}},
			want: "unknown",
		},
	}

	c := &K8sComposer{logger: logging.NewLogger("error")}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.getPodHealthStatus(tt.pod); got != tt.want {
				t.Errorf("getPodHealthStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}
