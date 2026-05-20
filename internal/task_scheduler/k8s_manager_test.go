package task_scheduler

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/logging"
)

func newTestK8sManager(t *testing.T, cfg *config.ComposeConfig, namespace string) *K8sManager {
	t.Helper()
	s := scheme.Scheme
	if err := crd.AddToScheme(s); err != nil {
		t.Fatalf("failed to add crd scheme: %v", err)
	}
	client := fake.NewClientBuilder().WithScheme(s).Build()
	if namespace == "" {
		namespace = constants.MateyNamespace
	}
	// NewK8sManager attempts to build a job manager which needs a real kube
	// config; that failure is logged and tolerated, so construct directly to
	// keep the test hermetic while still exercising the manager's own logic.
	return &K8sManager{
		client:    client,
		config:    cfg,
		logger:    logging.NewLogger("error"),
		namespace: namespace,
	}
}

func TestK8sManager_NamespaceDefaulting(t *testing.T) {
	s := scheme.Scheme
	if err := crd.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	client := fake.NewClientBuilder().WithScheme(s).Build()

	m := NewK8sManager(nil, client, "")
	if m.namespace != constants.MateyNamespace {
		t.Errorf("empty namespace should default to %q, got %q", constants.MateyNamespace, m.namespace)
	}

	m2 := NewK8sManager(nil, client, "explicit-ns")
	if m2.namespace != "explicit-ns" {
		t.Errorf("explicit namespace should be preserved, got %q", m2.namespace)
	}
}

func TestK8sManager_SetConfigFile(t *testing.T) {
	m := newTestK8sManager(t, nil, "")
	m.SetConfigFile("/path/to/matey.yaml")
	if m.configFile != "/path/to/matey.yaml" {
		t.Errorf("configFile = %q, want /path/to/matey.yaml", m.configFile)
	}
}

func TestCreateTaskSchedulerResource_Defaults(t *testing.T) {
	m := newTestK8sManager(t, nil, "matey")
	ts := m.createTaskSchedulerResource()

	if ts.Name != "task-scheduler" {
		t.Errorf("name = %q, want task-scheduler", ts.Name)
	}
	if ts.Namespace != "matey" {
		t.Errorf("namespace = %q, want matey", ts.Namespace)
	}
	if ts.Spec.Port != 8084 {
		t.Errorf("default port = %d, want 8084", ts.Spec.Port)
	}
	if ts.Spec.Host != "0.0.0.0" {
		t.Errorf("default host = %q, want 0.0.0.0", ts.Spec.Host)
	}
	if ts.Spec.LogLevel != "info" {
		t.Errorf("default log level = %q, want info", ts.Spec.LogLevel)
	}
	if ts.Spec.DatabasePath != "/app/data/scheduler.db" {
		t.Errorf("default db path = %q", ts.Spec.DatabasePath)
	}
	if ts.Spec.Replicas == nil || *ts.Spec.Replicas != 1 {
		t.Errorf("default replicas = %v, want 1", ts.Spec.Replicas)
	}
	if ts.Spec.SchedulerConfig.MaxConcurrentTasks != 10 {
		t.Errorf("default max concurrent = %d, want 10", ts.Spec.SchedulerConfig.MaxConcurrentTasks)
	}
	if ts.Spec.SchedulerConfig.RetryPolicy.MaxRetries != 3 {
		t.Errorf("default max retries = %d, want 3", ts.Spec.SchedulerConfig.RetryPolicy.MaxRetries)
	}
	if ts.Spec.Security == nil || !ts.Spec.Security.NoNewPrivileges {
		t.Errorf("expected security with NoNewPrivileges")
	}
	if ts.Spec.Security.RunAsUser == nil || *ts.Spec.Security.RunAsUser != 1000 {
		t.Errorf("expected RunAsUser 1000")
	}
	if ts.Labels["app.kubernetes.io/managed-by"] != "matey" {
		t.Errorf("managed-by label = %q", ts.Labels["app.kubernetes.io/managed-by"])
	}
}

func TestCreateTaskSchedulerResource_ConfigOverrides(t *testing.T) {
	cfg := &config.ComposeConfig{
		TaskScheduler: &config.TaskScheduler{
			Port:             9090,
			Host:             "127.0.0.1",
			LogLevel:         "debug",
			DatabasePath:     "/custom/db.sqlite",
			OpenRouterAPIKey: "or-key",
			OpenRouterModel:  "anthropic/claude",
			OllamaURL:        "http://ollama:11434",
			OllamaModel:      "llama3",
			PostgresEnabled:  true,
			DatabaseURL:      "postgres://localhost/db",
			Workspace:        "/workspace",
			Env:              map[string]string{"KEY": "VAL"},
			Volumes:          []string{"/host:/container"},
		},
	}
	m := newTestK8sManager(t, cfg, "matey")
	ts := m.createTaskSchedulerResource()

	if ts.Spec.Port != 9090 {
		t.Errorf("port = %d, want 9090", ts.Spec.Port)
	}
	if ts.Spec.Host != "127.0.0.1" {
		t.Errorf("host = %q, want 127.0.0.1", ts.Spec.Host)
	}
	if ts.Spec.LogLevel != "debug" {
		t.Errorf("log level = %q, want debug", ts.Spec.LogLevel)
	}
	if ts.Spec.DatabasePath != "/custom/db.sqlite" {
		t.Errorf("db path = %q", ts.Spec.DatabasePath)
	}
	if ts.Spec.OpenRouterAPIKey != "or-key" {
		t.Errorf("openrouter key not propagated")
	}
	if ts.Spec.OllamaModel != "llama3" {
		t.Errorf("ollama model not propagated")
	}
	if !ts.Spec.PostgresEnabled || ts.Spec.DatabaseURL != "postgres://localhost/db" {
		t.Errorf("postgres config not propagated")
	}
	if ts.Spec.Workspace != "/workspace" {
		t.Errorf("workspace not propagated")
	}
	if ts.Spec.Env["KEY"] != "VAL" {
		t.Errorf("env not propagated")
	}
	if len(ts.Spec.Volumes) != 1 {
		t.Errorf("volumes not propagated")
	}
}

func TestCreateTaskSchedulerResource_PostgresRequiresURL(t *testing.T) {
	// PostgresEnabled true but no DatabaseURL -> should NOT enable postgres.
	cfg := &config.ComposeConfig{
		TaskScheduler: &config.TaskScheduler{PostgresEnabled: true},
	}
	m := newTestK8sManager(t, cfg, "matey")
	ts := m.createTaskSchedulerResource()
	if ts.Spec.PostgresEnabled {
		t.Errorf("postgres should not be enabled without a DatabaseURL")
	}
}

func TestK8sManager_StartCreatesResource(t *testing.T) {
	m := newTestK8sManager(t, nil, "matey")
	if err := m.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	ts := &crd.MCPTaskScheduler{}
	err := m.client.Get(context.Background(), types.NamespacedName{Name: "task-scheduler", Namespace: "matey"}, ts)
	if err != nil {
		t.Fatalf("expected MCPTaskScheduler resource to be created: %v", err)
	}
	if ts.Spec.Port != 8084 {
		t.Errorf("created resource port = %d, want 8084", ts.Spec.Port)
	}
}

func TestK8sManager_StartIdempotentWhenExists(t *testing.T) {
	m := newTestK8sManager(t, nil, "matey")
	// Pre-create the resource in Running phase.
	existing := &crd.MCPTaskScheduler{
		ObjectMeta: metav1.ObjectMeta{Name: "task-scheduler", Namespace: "matey"},
	}
	existing.Status.Phase = crd.MCPTaskSchedulerPhaseRunning
	if err := m.client.Create(context.Background(), existing); err != nil {
		t.Fatalf("seed create failed: %v", err)
	}

	if err := m.Start(); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	// Should still be exactly one and untouched.
	list := &crd.MCPTaskSchedulerList{}
	if err := m.client.List(context.Background(), list); err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list.Items) != 1 {
		t.Errorf("expected 1 task scheduler, got %d", len(list.Items))
	}
}

func TestK8sManager_StopDeletesResource(t *testing.T) {
	m := newTestK8sManager(t, nil, "matey")
	existing := &crd.MCPTaskScheduler{
		ObjectMeta: metav1.ObjectMeta{Name: "task-scheduler", Namespace: "matey"},
	}
	if err := m.client.Create(context.Background(), existing); err != nil {
		t.Fatalf("seed create failed: %v", err)
	}

	if err := m.Stop(); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}

	ts := &crd.MCPTaskScheduler{}
	err := m.client.Get(context.Background(), types.NamespacedName{Name: "task-scheduler", Namespace: "matey"}, ts)
	if err == nil {
		t.Errorf("expected resource to be deleted")
	}
}

func TestK8sManager_StopWhenNotExists(t *testing.T) {
	m := newTestK8sManager(t, nil, "matey")
	// Stopping when nothing exists should be a no-op, not an error.
	if err := m.Stop(); err != nil {
		t.Errorf("Stop on missing resource should not error, got: %v", err)
	}
}

func TestK8sManager_GetStatusNotFound(t *testing.T) {
	m := newTestK8sManager(t, nil, "matey")
	status, err := m.GetStatus()
	if err == nil {
		t.Errorf("expected error when resource not found")
	}
	if status != "not-found" {
		t.Errorf("status = %q, want not-found", status)
	}
}

func TestK8sManager_GetStatusFromCRDPhase(t *testing.T) {
	tests := []struct {
		name       string
		phase      crd.MCPTaskSchedulerPhase
		ready      int32
		wantStatus string
	}{
		{"running with ready replicas", crd.MCPTaskSchedulerPhaseRunning, 1, "running"},
		{"running without ready replicas", crd.MCPTaskSchedulerPhaseRunning, 0, "starting"},
		{"failed", crd.MCPTaskSchedulerPhaseFailed, 0, "failed"},
		{"terminating", crd.MCPTaskSchedulerPhaseTerminating, 0, "stopping"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestK8sManager(t, nil, "matey")
			ts := &crd.MCPTaskScheduler{
				ObjectMeta: metav1.ObjectMeta{Name: "task-scheduler", Namespace: "matey"},
			}
			ts.Status.Phase = tt.phase
			ts.Status.ReadyReplicas = tt.ready
			if err := m.client.Create(context.Background(), ts); err != nil {
				t.Fatalf("seed create failed: %v", err)
			}

			status, err := m.GetStatus()
			if err != nil {
				t.Fatalf("GetStatus returned error: %v", err)
			}
			if status != tt.wantStatus {
				t.Errorf("status = %q, want %q", status, tt.wantStatus)
			}
		})
	}
}

func TestK8sManager_GetStatusFromPod(t *testing.T) {
	m := newTestK8sManager(t, nil, "matey")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-scheduler-abc",
			Namespace: "matey",
			Labels:    map[string]string{"app.kubernetes.io/name": "task-scheduler"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}
	if err := m.client.Create(context.Background(), pod); err != nil {
		t.Fatalf("seed pod failed: %v", err)
	}

	status, err := m.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus returned error: %v", err)
	}
	if status != "running" {
		t.Errorf("status = %q, want running", status)
	}
}

func TestK8sManager_GetStatusCrashLoop(t *testing.T) {
	m := newTestK8sManager(t, nil, "matey")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "task-scheduler-crash",
			Namespace: "matey",
			Labels:    map[string]string{"app.kubernetes.io/name": "task-scheduler"},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					},
				},
			},
		},
	}
	if err := m.client.Create(context.Background(), pod); err != nil {
		t.Fatalf("seed pod failed: %v", err)
	}

	status, err := m.GetStatus()
	if err != nil {
		t.Fatalf("GetStatus returned error: %v", err)
	}
	if status != "failed" {
		t.Errorf("status = %q, want failed (CrashLoopBackOff)", status)
	}
}

func TestK8sManager_JobManagerNilGuards(t *testing.T) {
	// When jobManager is nil, the delegating methods must return errors
	// rather than panicking.
	m := newTestK8sManager(t, nil, "matey")
	m.jobManager = nil

	if _, err := m.ExecuteTask(&TaskRequest{ID: "x"}); err == nil {
		t.Errorf("ExecuteTask should error when job manager is nil")
	}
	if _, err := m.GetTaskStatus("x"); err == nil {
		t.Errorf("GetTaskStatus should error when job manager is nil")
	}
	if _, err := m.ListTasks(); err == nil {
		t.Errorf("ListTasks should error when job manager is nil")
	}
	if err := m.CancelTask("x"); err == nil {
		t.Errorf("CancelTask should error when job manager is nil")
	}
	if _, err := m.GetTaskLogs("x"); err == nil {
		t.Errorf("GetTaskLogs should error when job manager is nil")
	}
	if _, err := m.GetTaskStatistics(); err == nil {
		t.Errorf("GetTaskStatistics should error when job manager is nil")
	}
	if err := m.CleanupOldTasks(0); err == nil {
		t.Errorf("CleanupOldTasks should error when job manager is nil")
	}
	if _, err := m.GetLogs(); err == nil {
		t.Errorf("GetLogs should error when job manager is nil")
	}
}
