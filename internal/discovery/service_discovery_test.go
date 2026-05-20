// internal/discovery/service_discovery_test.go
package discovery

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/phildougherty/m8e/internal/logging"
)

// recordingHandler captures discovery events for assertion in tests.
type recordingHandler struct {
	added    []ServiceEndpoint
	modified []ServiceEndpoint
	deleted  []string
}

func (h *recordingHandler) OnServiceAdded(ep ServiceEndpoint)    { h.added = append(h.added, ep) }
func (h *recordingHandler) OnServiceModified(ep ServiceEndpoint) { h.modified = append(h.modified, ep) }
func (h *recordingHandler) OnServiceDeleted(name, _ string)      { h.deleted = append(h.deleted, name) }

// TestServiceDiscovery_EndpointSliceInformer verifies that the migrated
// EndpointSlice informer can be created, started, and observes slice events
// without error. The ServiceEndpoint map is driven by Service objects (not
// slices), but the slice informer is wired so downstream consumers can
// react to per-pod readiness via the shared cache.
func TestServiceDiscovery_EndpointSliceInformer(t *testing.T) {
	// Use the fake clientset directly so we can avoid the test-helper
	// indirection above (which had to thread runtime.Object through).
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "memory",
			Namespace: "matey",
			Labels: map[string]string{
				"mcp.matey.ai/role":     "server",
				"mcp.matey.ai/protocol": "http",
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 3001},
			},
		},
	}
	ready := true
	slice := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "memory-abc",
			Namespace: "matey",
			Labels: map[string]string{
				discoveryv1.LabelServiceName: "memory",
			},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses:  []string{"10.0.0.1"},
				Conditions: discoveryv1.EndpointConditions{Ready: &ready},
			},
		},
	}

	client := fake.NewSimpleClientset(svc, slice)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sd := &K8sServiceDiscovery{
		client:    client,
		namespace: "matey",
		logger:    logging.NewLogger("debug"),
		services:  map[string]ServiceEndpoint{},
		ctx:       ctx,
		cancel:    cancel,
	}
	handler := &recordingHandler{}
	sd.AddHandler(handler)

	if err := sd.setupInformers(); err != nil {
		t.Fatalf("setupInformers: %v", err)
	}

	if err := sd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sd.Stop()

	// Wait briefly for the service informer to dispatch the initial Add.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(handler.added) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(handler.added) == 0 {
		t.Fatalf("expected at least one OnServiceAdded callback, got 0")
	}
	if handler.added[0].Name != "memory" {
		t.Fatalf("expected service name=memory, got %q", handler.added[0].Name)
	}
}

// TestServiceDiscovery_DiscoverMCPServers verifies the public
// DiscoverMCPServers entrypoint (which the migration must leave intact).
func TestServiceDiscovery_DiscoverMCPServers(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "memory",
			Namespace: "matey",
			Labels: map[string]string{
				"mcp.matey.ai/role":     "server",
				"mcp.matey.ai/protocol": "http",
			},
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{{Port: 3001}},
		},
	}
	client := fake.NewSimpleClientset(svc)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sd := &K8sServiceDiscovery{
		client:    client,
		namespace: "matey",
		logger:    logging.NewLogger("debug"),
		services:  map[string]ServiceEndpoint{},
		ctx:       ctx,
		cancel:    cancel,
	}

	endpoints, err := sd.DiscoverMCPServers()
	if err != nil {
		t.Fatalf("DiscoverMCPServers: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}
	if endpoints[0].Protocol != "http" {
		t.Fatalf("expected protocol=http, got %q", endpoints[0].Protocol)
	}
}

// TestServiceDiscovery_HandleEndpointSliceChange exercises the
// EndpointSlice event handler directly to confirm it does not panic and
// gracefully ignores slices that lack the service-name label.
func TestServiceDiscovery_HandleEndpointSliceChange(t *testing.T) {
	sd := &K8sServiceDiscovery{
		logger: logging.NewLogger("debug"),
	}
	// No label: handler should bail out silently.
	sd.handleEndpointSliceChange(&discoveryv1.EndpointSlice{})

	ready := true
	notReady := false
	sd.handleEndpointSliceChange(&discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{discoveryv1.LabelServiceName: "memory"},
		},
		Endpoints: []discoveryv1.Endpoint{
			{Conditions: discoveryv1.EndpointConditions{Ready: &ready}},
			{Conditions: discoveryv1.EndpointConditions{Ready: &notReady}},
			{Conditions: discoveryv1.EndpointConditions{Ready: nil}},
		},
	})
}
