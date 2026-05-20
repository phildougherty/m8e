// internal/service/task_scheduler.go
package service

import (
	"context"
	"errors"
	"fmt"
	"os"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/task_scheduler"
)

// ErrMissingPostgresPassword is returned when a default Postgres DSN is
// required but the POSTGRES_PASSWORD environment variable is unset. Hardcoding
// a plaintext password fallback would silently mask deploys that forgot to
// wire a Kubernetes Secret as the env var via valueFrom.secretKeyRef, so the
// task scheduler refuses to start in that condition.
var ErrMissingPostgresPassword = errors.New("POSTGRES_PASSWORD env var is required to compose a default Postgres DSN; set it directly or via valueFrom.secretKeyRef from a Kubernetes Secret")

// defaultTaskSchedulerWorkspace is the in-container path the task scheduler
// pod uses when no explicit workspace is configured by the operator. A
// container-friendly default avoids leaking host user paths (previously
// "/home/phil") into manifests that ship to other environments.
const defaultTaskSchedulerWorkspace = "/workspace"

// taskSchedulerResourceName is the fixed name of the MCPTaskScheduler resource
// and the workflow-owning task scheduler in a namespace.
const taskSchedulerResourceName = "task-scheduler"

// TaskScheduler owns orchestration of the task scheduler service and its
// workflows: enabling/disabling it in config, creating the Kubernetes
// resource, and managing workflows on the MCPTaskScheduler resource. It holds
// no cobra/stdout concerns.
type TaskScheduler struct {
	client    client.Client
	namespace string
}

// NewTaskScheduler builds a TaskScheduler service bound to a namespace.
func NewTaskScheduler(c client.Client, namespace string) *TaskScheduler {
	return &TaskScheduler{client: c, namespace: namespace}
}

// EnableConfig mutates cfg in place so the task scheduler is enabled with sane
// defaults, mirroring the prior `--enable` behavior. It does not persist cfg;
// the caller saves it. ensurePostgres, when non-nil, is invoked to guarantee
// the shared MCPPostgres resource exists (a warning-only step).
func (t *TaskScheduler) EnableConfig(cfg *config.ComposeConfig, ensurePostgres func() error) (warning error) {
	cfg.TaskScheduler.Enabled = true
	if cfg.TaskScheduler.Port == 0 {
		cfg.TaskScheduler.Port = 8018
	}
	if cfg.TaskScheduler.Host == "" {
		cfg.TaskScheduler.Host = "0.0.0.0"
	}
	if !cfg.TaskScheduler.PostgresEnabled {
		cfg.TaskScheduler.PostgresEnabled = true
	}
	if cfg.TaskScheduler.DatabaseURL == "" {
		dsn, err := BuildDefaultPostgresDSN("matey")
		if err != nil {
			return fmt.Errorf("task scheduler: %w", err)
		}
		cfg.TaskScheduler.DatabaseURL = dsn
	}
	if cfg.TaskScheduler.DatabasePath == "" {
		cfg.TaskScheduler.DatabasePath = "/data/task-scheduler.db"
	}
	if cfg.TaskScheduler.LogLevel == "" {
		cfg.TaskScheduler.LogLevel = "info"
	}
	if cfg.TaskScheduler.Workspace == "" {
		// Default to a container-friendly path; operators override via the
		// task_scheduler.workspace config key when they need a host mount.
		cfg.TaskScheduler.Workspace = defaultTaskSchedulerWorkspace
	}
	if cfg.TaskScheduler.CPUs == "" {
		cfg.TaskScheduler.CPUs = "2.0"
	}
	if cfg.TaskScheduler.Memory == "" {
		cfg.TaskScheduler.Memory = "1g"
	}
	if len(cfg.TaskScheduler.Volumes) == 0 {
		// Default to mounting only /tmp; host workspaces should be configured
		// explicitly rather than baking a host user's home directory into the
		// scheduler manifest.
		cfg.TaskScheduler.Volumes = []string{"/tmp:/tmp:rw"}
	}

	if cfg.Servers == nil {
		cfg.Servers = make(map[string]config.ServerConfig)
	}

	cfg.Servers["task-scheduler"] = config.ServerConfig{
		Build: config.BuildConfig{
			Context:    "github.com/phildougherty/m8e-task-scheduler.git",
			Dockerfile: "Dockerfile",
		},
		Command:      "./matey-task-scheduler",
		Args:         []string{"--host", "0.0.0.0", "--port", "8018"},
		Protocol:     "http",
		HttpPort:     constants.TaskSchedulerDefaultPort,
		User:         "root",
		ReadOnly:     false,
		Privileged:   false,
		SecurityOpt:  []string{"no-new-privileges:true"},
		Capabilities: []string{"tools"},
		Env: map[string]string{
			"NODE_ENV":           "production",
			"DATABASE_PATH":      cfg.TaskScheduler.DatabasePath,
			"DATABASE_URL":       cfg.TaskScheduler.DatabaseURL,
			"POSTGRES_ENABLED":   fmt.Sprintf("%t", cfg.TaskScheduler.PostgresEnabled),
			"MCP_PROXY_URL":      cfg.TaskScheduler.MCPProxyURL,
			"MCP_PROXY_API_KEY":  cfg.TaskScheduler.MCPProxyAPIKey,
			"OPENROUTER_API_KEY": cfg.TaskScheduler.OpenRouterAPIKey,
			"OPENROUTER_MODEL":   cfg.TaskScheduler.OpenRouterModel,
			"OLLAMA_URL":         cfg.TaskScheduler.OllamaURL,
			"OLLAMA_MODEL":       cfg.TaskScheduler.OllamaModel,
		},
		Networks: []string{"mcp-net"},
		Authentication: &config.ServerAuthConfig{
			Enabled:       true,
			RequiredScope: "mcp:tools",
			OptionalAuth:  false,
			AllowAPIKey:   &[]bool{true}[0],
		},
		Volumes: cfg.TaskScheduler.Volumes,
	}

	if ensurePostgres != nil {
		if err := ensurePostgres(); err != nil {
			return fmt.Errorf("failed to ensure postgres resource: %w", err)
		}
	}

	return nil
}

// Start creates the MCPTaskScheduler resource so the controller deploys the
// service. It is non-blocking.
func (t *TaskScheduler) Start(cfg *config.ComposeConfig) error {
	mgr := task_scheduler.NewK8sManager(cfg, t.client, t.namespace)
	if err := mgr.Start(); err != nil {
		return fmt.Errorf("failed to create MCPTaskScheduler resource: %w", err)
	}

	return nil
}

// Stop tears down the task scheduler Kubernetes resources. It returns any
// error from the manager so the caller can decide whether it is fatal.
func (t *TaskScheduler) Stop(cfg *config.ComposeConfig) error {
	mgr := task_scheduler.NewK8sManager(cfg, t.client, t.namespace)

	return mgr.Stop()
}

// getTaskScheduler fetches the namespace's MCPTaskScheduler resource.
func (t *TaskScheduler) getTaskScheduler(ctx context.Context) (*crd.MCPTaskScheduler, error) {
	ts := &crd.MCPTaskScheduler{}
	err := t.client.Get(ctx, types.NamespacedName{
		Name: taskSchedulerResourceName, Namespace: t.namespace,
	}, ts)
	if err != nil {
		return nil, fmt.Errorf("failed to get MCPTaskScheduler: %w", err)
	}

	return ts, nil
}

// AddWorkflow appends a workflow definition to the namespace's task scheduler.
func (t *TaskScheduler) AddWorkflow(ctx context.Context, workflowDef crd.WorkflowDefinition) error {
	ts, err := t.getTaskScheduler(ctx)
	if err != nil {
		return err
	}

	ts.Spec.Workflows = append(ts.Spec.Workflows, workflowDef)

	if err := t.client.Update(ctx, ts); err != nil {
		return fmt.Errorf("failed to update MCPTaskScheduler: %w", err)
	}

	return nil
}

// ListWorkflows returns all workflows across the service's namespace, or
// across all namespaces when allNamespaces is true.
func (t *TaskScheduler) ListWorkflows(ctx context.Context, allNamespaces bool) ([]crd.WorkflowDefinition, error) {
	var list crd.MCPTaskSchedulerList
	if allNamespaces {
		if err := t.client.List(ctx, &list); err != nil {
			return nil, fmt.Errorf("failed to list task schedulers: %w", err)
		}
	} else {
		if err := t.client.List(ctx, &list, client.InNamespace(t.namespace)); err != nil {
			return nil, fmt.Errorf("failed to list task schedulers: %w", err)
		}
	}

	var workflows []crd.WorkflowDefinition
	for _, ts := range list.Items {
		workflows = append(workflows, ts.Spec.Workflows...)
	}

	return workflows, nil
}

// GetWorkflow returns a single workflow plus its owning task scheduler.
func (t *TaskScheduler) GetWorkflow(ctx context.Context, name string) (*crd.WorkflowDefinition, *crd.MCPTaskScheduler, error) {
	ts, err := t.getTaskScheduler(ctx)
	if err != nil {
		return nil, nil, err
	}

	for i := range ts.Spec.Workflows {
		if ts.Spec.Workflows[i].Name == name {
			return &ts.Spec.Workflows[i], ts, nil
		}
	}

	return nil, nil, fmt.Errorf("workflow %s not found in task scheduler", name)
}

// DeleteWorkflow removes a workflow from the namespace's task scheduler.
func (t *TaskScheduler) DeleteWorkflow(ctx context.Context, name string) error {
	ts, err := t.getTaskScheduler(ctx)
	if err != nil {
		return err
	}

	idx := -1
	for i := range ts.Spec.Workflows {
		if ts.Spec.Workflows[i].Name == name {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("workflow %s not found in task scheduler", name)
	}

	ts.Spec.Workflows = append(ts.Spec.Workflows[:idx], ts.Spec.Workflows[idx+1:]...)

	if err := t.client.Update(ctx, ts); err != nil {
		return fmt.Errorf("failed to update MCPTaskScheduler: %w", err)
	}

	return nil
}

// SetWorkflowEnabled pauses (enabled=false) or resumes (enabled=true) a
// workflow. WorkflowDefinition has no Suspend field, so Enabled carries the
// suspend state.
func (t *TaskScheduler) SetWorkflowEnabled(ctx context.Context, name string, enabled bool) error {
	ts, err := t.getTaskScheduler(ctx)
	if err != nil {
		return err
	}

	found := false
	for i := range ts.Spec.Workflows {
		if ts.Spec.Workflows[i].Name == name {
			ts.Spec.Workflows[i].Enabled = enabled
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("workflow %s not found in task scheduler", name)
	}

	if err := t.client.Update(ctx, ts); err != nil {
		return fmt.Errorf("failed to update MCPTaskScheduler: %w", err)
	}

	return nil
}

// EnsurePostgresResource creates the shared matey-postgres MCPPostgres resource
// if it does not already exist. This mirrors the cmd-package helper of the same
// name, but operates on the service's injected client so it is testable.
//
// The password is sourced from the POSTGRES_PASSWORD environment variable; if
// it is unset, EnsurePostgresResource refuses to create the resource rather
// than baking a plaintext default into the cluster spec. Operators are
// expected to provide POSTGRES_PASSWORD via a Kubernetes Secret mounted as
// an env var (valueFrom.secretKeyRef).
func (t *TaskScheduler) EnsurePostgresResource(ctx context.Context) error {
	password := os.Getenv("POSTGRES_PASSWORD")
	if password == "" {
		return fmt.Errorf("EnsurePostgresResource: %w", ErrMissingPostgresPassword)
	}

	postgres := &crd.MCPPostgres{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matey-postgres",
			Namespace: t.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/component":  "postgres",
				"app.kubernetes.io/managed-by": "matey",
				"app.kubernetes.io/name":       "postgres",
				"mcp.matey.ai/role":            "database",
			},
		},
		Spec: crd.MCPPostgresSpec{
			Database:         "matey",
			User:             "postgres",
			Password:         password,
			Port:             5432,
			Version:          "15",
			StorageSize:      "10Gi",
			StorageClassName: "",
			Resources: &crd.ResourceRequirements{
				Requests: map[string]string{
					"cpu":    "100m",
					"memory": "256Mi",
				},
				Limits: map[string]string{
					"cpu":    "500m",
					"memory": "512Mi",
				},
			},
			SecurityContext: &crd.SecurityConfig{
				ReadOnlyRootFS:     false,
				AllowPrivilegedOps: false,
				TrustedImage:       true,
			},
		},
	}

	if err := t.client.Create(ctx, postgres); err != nil && !k8serrors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create postgres resource: %w", err)
	}

	return nil
}

// BuildDefaultPostgresDSN composes the in-cluster Postgres connection string
// the task scheduler / memory server use when no explicit DatabaseURL is
// configured. The password MUST come from the POSTGRES_PASSWORD environment
// variable — there is no plaintext fallback because a deploy that forgot to
// wire the Secret would otherwise silently authenticate (or fail mysteriously)
// against a dev DB. Other env vars (POSTGRES_HOST, POSTGRES_PORT, POSTGRES_USER)
// are optional with cluster-friendly defaults.
func BuildDefaultPostgresDSN(database string) (string, error) {
	password := os.Getenv("POSTGRES_PASSWORD")
	if password == "" {
		return "", ErrMissingPostgresPassword
	}

	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "matey-postgres.matey.svc.cluster.local"
	}
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5432"
	}
	user := os.Getenv("POSTGRES_USER")
	if user == "" {
		user = "postgres"
	}

	return fmt.Sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=disable",
		user, password, host, port, database), nil
}
