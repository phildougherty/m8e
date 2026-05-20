// internal/service/task_scheduler_test.go
package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/crd"
)

func tsScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := crd.AddToScheme(s); err != nil {
		t.Fatalf("crd scheme: %v", err)
	}

	return s
}

func newTaskSchedulerResource() *crd.MCPTaskScheduler {
	return &crd.MCPTaskScheduler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      taskSchedulerResourceName,
			Namespace: "matey",
		},
	}
}

func TestTaskScheduler_EnableConfig(t *testing.T) {
	// EnableConfig composes a default Postgres DSN when none is configured;
	// the password MUST come from POSTGRES_PASSWORD with no plaintext
	// fallback. Tests opt in explicitly via t.Setenv.
	t.Setenv("POSTGRES_PASSWORD", "test-password-do-not-commit")

	c := fake.NewClientBuilder().WithScheme(tsScheme(t)).Build()
	svc := NewTaskScheduler(c, "matey")

	cfg := &config.ComposeConfig{TaskScheduler: &config.TaskScheduler{}}

	ensureCalled := false
	warn := svc.EnableConfig(cfg, func() error {
		ensureCalled = true
		return nil
	})
	if warn != nil {
		t.Fatalf("EnableConfig returned warning: %v", warn)
	}
	if cfg.TaskScheduler.Workspace != defaultTaskSchedulerWorkspace {
		t.Errorf("expected workspace default %q, got %q", defaultTaskSchedulerWorkspace, cfg.TaskScheduler.Workspace)
	}
	if strings.Contains(cfg.TaskScheduler.DatabaseURL, ":password@") {
		t.Errorf("DatabaseURL must not contain the plaintext default password: %s", cfg.TaskScheduler.DatabaseURL)
	}
	if !strings.Contains(cfg.TaskScheduler.DatabaseURL, "test-password-do-not-commit") {
		t.Errorf("DatabaseURL should be composed from POSTGRES_PASSWORD env: %s", cfg.TaskScheduler.DatabaseURL)
	}
	if !ensureCalled {
		t.Error("expected ensurePostgres callback to be invoked")
	}

	if !cfg.TaskScheduler.Enabled {
		t.Error("expected TaskScheduler.Enabled to be true")
	}
	if cfg.TaskScheduler.Port != 8018 {
		t.Errorf("expected default port 8018, got %d", cfg.TaskScheduler.Port)
	}
	if !cfg.TaskScheduler.PostgresEnabled {
		t.Error("expected PostgresEnabled default true")
	}
	srv, ok := cfg.Servers["task-scheduler"]
	if !ok {
		t.Fatal("expected task-scheduler entry in Servers map")
	}
	if srv.Protocol != "http" {
		t.Errorf("expected server protocol http, got %q", srv.Protocol)
	}
	if srv.Env["POSTGRES_ENABLED"] != "true" {
		t.Errorf("expected POSTGRES_ENABLED=true, got %q", srv.Env["POSTGRES_ENABLED"])
	}
}

func TestTaskScheduler_EnableConfigPreservesExisting(t *testing.T) {
	t.Setenv("POSTGRES_PASSWORD", "test-password-do-not-commit")

	c := fake.NewClientBuilder().WithScheme(tsScheme(t)).Build()
	svc := NewTaskScheduler(c, "matey")

	cfg := &config.ComposeConfig{TaskScheduler: &config.TaskScheduler{
		Port:     9000,
		LogLevel: "debug",
	}}

	if warn := svc.EnableConfig(cfg, nil); warn != nil {
		t.Fatalf("unexpected warning: %v", warn)
	}
	if cfg.TaskScheduler.Port != 9000 {
		t.Errorf("expected existing port 9000 preserved, got %d", cfg.TaskScheduler.Port)
	}
	if cfg.TaskScheduler.LogLevel != "debug" {
		t.Errorf("expected existing log level preserved, got %q", cfg.TaskScheduler.LogLevel)
	}
}

func TestTaskScheduler_AddWorkflow(t *testing.T) {
	c := fake.NewClientBuilder().
		WithScheme(tsScheme(t)).
		WithObjects(newTaskSchedulerResource()).
		Build()
	svc := NewTaskScheduler(c, "matey")

	wf := crd.WorkflowDefinition{Name: "nightly", Schedule: "0 0 * * *"}
	if err := svc.AddWorkflow(context.Background(), wf); err != nil {
		t.Fatalf("AddWorkflow: %v", err)
	}

	got := &crd.MCPTaskScheduler{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: taskSchedulerResourceName, Namespace: "matey"}, got); err != nil {
		t.Fatalf("get task scheduler: %v", err)
	}
	if len(got.Spec.Workflows) != 1 || got.Spec.Workflows[0].Name != "nightly" {
		t.Errorf("expected workflow nightly to be appended, got %+v", got.Spec.Workflows)
	}
}

func TestTaskScheduler_AddWorkflowMissingResource(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(tsScheme(t)).Build()
	svc := NewTaskScheduler(c, "matey")

	err := svc.AddWorkflow(context.Background(), crd.WorkflowDefinition{Name: "x"})
	if err == nil {
		t.Fatal("expected error when MCPTaskScheduler resource is absent")
	}
}

func TestTaskScheduler_ListWorkflows(t *testing.T) {
	ts := newTaskSchedulerResource()
	ts.Spec.Workflows = []crd.WorkflowDefinition{
		{Name: "a"}, {Name: "b"},
	}
	c := fake.NewClientBuilder().WithScheme(tsScheme(t)).WithObjects(ts).Build()
	svc := NewTaskScheduler(c, "matey")

	workflows, err := svc.ListWorkflows(context.Background(), false)
	if err != nil {
		t.Fatalf("ListWorkflows: %v", err)
	}
	if len(workflows) != 2 {
		t.Errorf("expected 2 workflows, got %d", len(workflows))
	}
}

func TestTaskScheduler_GetAndDeleteWorkflow(t *testing.T) {
	ts := newTaskSchedulerResource()
	ts.Spec.Workflows = []crd.WorkflowDefinition{
		{Name: "keep"}, {Name: "remove"},
	}
	c := fake.NewClientBuilder().WithScheme(tsScheme(t)).WithObjects(ts).Build()
	svc := NewTaskScheduler(c, "matey")

	wf, owner, err := svc.GetWorkflow(context.Background(), "remove")
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if wf.Name != "remove" || owner == nil {
		t.Errorf("GetWorkflow returned wrong data: %+v owner=%v", wf, owner)
	}

	if _, _, err := svc.GetWorkflow(context.Background(), "missing"); err == nil {
		t.Error("expected error for missing workflow")
	}

	if err := svc.DeleteWorkflow(context.Background(), "remove"); err != nil {
		t.Fatalf("DeleteWorkflow: %v", err)
	}

	got := &crd.MCPTaskScheduler{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: taskSchedulerResourceName, Namespace: "matey"}, got); err != nil {
		t.Fatalf("get after delete: %v", err)
	}
	if len(got.Spec.Workflows) != 1 || got.Spec.Workflows[0].Name != "keep" {
		t.Errorf("expected only 'keep' workflow remaining, got %+v", got.Spec.Workflows)
	}

	if err := svc.DeleteWorkflow(context.Background(), "missing"); err == nil {
		t.Error("expected error deleting missing workflow")
	}
}

func TestTaskScheduler_SetWorkflowEnabled(t *testing.T) {
	ts := newTaskSchedulerResource()
	ts.Spec.Workflows = []crd.WorkflowDefinition{{Name: "wf", Enabled: true}}
	c := fake.NewClientBuilder().WithScheme(tsScheme(t)).WithObjects(ts).Build()
	svc := NewTaskScheduler(c, "matey")

	// Pause: enabled -> false.
	if err := svc.SetWorkflowEnabled(context.Background(), "wf", false); err != nil {
		t.Fatalf("SetWorkflowEnabled: %v", err)
	}
	got := &crd.MCPTaskScheduler{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: taskSchedulerResourceName, Namespace: "matey"}, got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Spec.Workflows[0].Enabled {
		t.Error("expected workflow to be disabled after pause")
	}

	if err := svc.SetWorkflowEnabled(context.Background(), "missing", true); err == nil {
		t.Error("expected error for missing workflow")
	}
}

func TestTaskScheduler_EnsurePostgresResource(t *testing.T) {
	t.Setenv("POSTGRES_PASSWORD", "test-password-do-not-commit")

	c := fake.NewClientBuilder().WithScheme(tsScheme(t)).Build()
	svc := NewTaskScheduler(c, "matey")

	if err := svc.EnsurePostgresResource(context.Background()); err != nil {
		t.Fatalf("EnsurePostgresResource: %v", err)
	}
	pg := &crd.MCPPostgres{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "matey-postgres", Namespace: "matey"}, pg); err != nil {
		t.Fatalf("expected matey-postgres to exist: %v", err)
	}
	if pg.Spec.Password == "password" {
		t.Error("EnsurePostgresResource must not write the hardcoded plaintext default password into the spec")
	}
	if pg.Spec.Password != "test-password-do-not-commit" {
		t.Errorf("expected spec password sourced from POSTGRES_PASSWORD, got %q", pg.Spec.Password)
	}

	// Second call must be idempotent (AlreadyExists is swallowed).
	if err := svc.EnsurePostgresResource(context.Background()); err != nil {
		t.Fatalf("EnsurePostgresResource not idempotent: %v", err)
	}
}

// TestTaskScheduler_RefusesDefaultPasswordFallback is the security regression
// test: without POSTGRES_PASSWORD set, both EnableConfig (when no DatabaseURL
// is configured) and EnsurePostgresResource must return ErrMissingPostgresPassword
// rather than silently composing a "postgres:password" default.
func TestTaskScheduler_RefusesDefaultPasswordFallback(t *testing.T) {
	// Ensure no inherited password leaks into the test.
	t.Setenv("POSTGRES_PASSWORD", "")

	c := fake.NewClientBuilder().WithScheme(tsScheme(t)).Build()
	svc := NewTaskScheduler(c, "matey")

	cfg := &config.ComposeConfig{TaskScheduler: &config.TaskScheduler{}}
	err := svc.EnableConfig(cfg, nil)
	if err == nil {
		t.Fatal("EnableConfig should reject default DSN construction without POSTGRES_PASSWORD")
	}
	if !errors.Is(err, ErrMissingPostgresPassword) {
		t.Errorf("expected ErrMissingPostgresPassword, got %v", err)
	}
	if strings.Contains(cfg.TaskScheduler.DatabaseURL, ":password@") {
		t.Errorf("DatabaseURL must not be populated with a plaintext default on the error path: %s", cfg.TaskScheduler.DatabaseURL)
	}

	ensureErr := svc.EnsurePostgresResource(context.Background())
	if ensureErr == nil {
		t.Fatal("EnsurePostgresResource should reject creating a default-password postgres resource")
	}
	if !errors.Is(ensureErr, ErrMissingPostgresPassword) {
		t.Errorf("expected ErrMissingPostgresPassword from EnsurePostgresResource, got %v", ensureErr)
	}

	// And the resource must NOT have been created with a default password.
	pg := &crd.MCPPostgres{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: "matey-postgres", Namespace: "matey"}, pg); err == nil {
		t.Errorf("matey-postgres resource was created despite missing POSTGRES_PASSWORD: spec=%+v", pg.Spec)
	}
}

// TestBuildDefaultPostgresDSN_FromEnv verifies the DSN builder pulls each
// component from its env var with sensible cluster defaults.
func TestBuildDefaultPostgresDSN_FromEnv(t *testing.T) {
	t.Setenv("POSTGRES_PASSWORD", "secret123")
	t.Setenv("POSTGRES_HOST", "pg.example.svc")
	t.Setenv("POSTGRES_PORT", "5433")
	t.Setenv("POSTGRES_USER", "matey")

	dsn, err := BuildDefaultPostgresDSN("memory_graph")
	if err != nil {
		t.Fatalf("BuildDefaultPostgresDSN: %v", err)
	}
	want := "postgresql://matey:secret123@pg.example.svc:5433/memory_graph?sslmode=disable"
	if dsn != want {
		t.Errorf("dsn = %q, want %q", dsn, want)
	}
}
