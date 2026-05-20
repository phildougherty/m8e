// internal/memory/k8s_manager_timeout_test.go
package memory

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/phildougherty/m8e/internal/crd"
)

// deadlineRecorder wraps a controller-runtime client.Client and records the
// context.Deadline observed on every call. Tests use this to assert that
// K8sManager passes a bounded context (rather than a bare context.Background)
// to API calls — otherwise a slow API server could hang lifecycle ops.
type deadlineRecorder struct {
	client.Client
	deadlines []time.Time
	hasDL     []bool
}

func (d *deadlineRecorder) record(ctx context.Context) {
	dl, ok := ctx.Deadline()
	d.deadlines = append(d.deadlines, dl)
	d.hasDL = append(d.hasDL, ok)
}

func (d *deadlineRecorder) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	d.record(ctx)
	return d.Client.Get(ctx, key, obj, opts...)
}
func (d *deadlineRecorder) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	d.record(ctx)
	return d.Client.List(ctx, list, opts...)
}
func (d *deadlineRecorder) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	d.record(ctx)
	return d.Client.Create(ctx, obj, opts...)
}
func (d *deadlineRecorder) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	d.record(ctx)
	return d.Client.Delete(ctx, obj, opts...)
}

func newRecorder(t *testing.T, objs ...client.Object) *deadlineRecorder {
	t.Helper()
	s := runtime.NewScheme()
	if err := crd.AddToScheme(s); err != nil {
		t.Fatalf("crd.AddToScheme: %v", err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("corev1.AddToScheme: %v", err)
	}
	fc := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
	return &deadlineRecorder{Client: fc}
}

// assertBoundedDeadlines fails the test if any recorded API call lacked a
// context deadline. We don't pin the exact deadline because it depends on
// wall-clock time; bounded is what matters.
func (d *deadlineRecorder) assertBoundedDeadlines(t *testing.T, name string) {
	t.Helper()
	if len(d.hasDL) == 0 {
		t.Fatalf("%s: no API calls were recorded", name)
	}
	for i, ok := range d.hasDL {
		if !ok {
			t.Errorf("%s: API call #%d used a context without a deadline (got context.Background)", name, i)
		}
	}
}

func TestK8sManager_Start_UsesBoundedContext(t *testing.T) {
	rec := newRecorder(t)
	m := NewK8sManager(nil, rec, "matey")
	if err := m.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	rec.assertBoundedDeadlines(t, "Start")
}

func TestK8sManager_Stop_UsesBoundedContext(t *testing.T) {
	// Pre-seed an MCPMemory so Stop has something to delete.
	mem := &crd.MCPMemory{}
	mem.Name = "memory"
	mem.Namespace = "matey"
	rec := newRecorder(t, mem)
	m := NewK8sManager(nil, rec, "matey")
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	rec.assertBoundedDeadlines(t, "Stop")
}

func TestK8sManager_Status_UsesBoundedContext(t *testing.T) {
	rec := newRecorder(t)
	m := NewK8sManager(nil, rec, "matey")
	if _, err := m.Status(); err == nil {
		// Status returns ("not-found", non-nil err) when MCPMemory is absent;
		// either path exercises the API call we want to observe.
		t.Log("Status returned no error; that's fine, we only care about the context")
	}
	rec.assertBoundedDeadlines(t, "Status")
}

func TestK8sManager_GetMemoryInfo_UsesBoundedContext(t *testing.T) {
	rec := newRecorder(t)
	m := NewK8sManager(nil, rec, "matey")
	_, _ = m.GetMemoryInfo()
	rec.assertBoundedDeadlines(t, "GetMemoryInfo")
}
