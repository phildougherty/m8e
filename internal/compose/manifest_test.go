// internal/compose/manifest_test.go
package compose

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/logging"
)

func TestCreateControllerManagerDeployment(t *testing.T) {
	c := &K8sComposer{
		config:    &config.ComposeConfig{},
		namespace: "team-ns",
		logger:    logging.NewLogger("error"),
	}
	dep := c.createControllerManagerDeployment()

	if dep.Name != "matey-controller-manager" {
		t.Errorf("Name = %q, want matey-controller-manager", dep.Name)
	}
	if dep.Namespace != "team-ns" {
		t.Errorf("Namespace = %q, want team-ns", dep.Namespace)
	}
	if dep.Spec.Replicas == nil || *dep.Spec.Replicas != 1 {
		t.Errorf("Replicas = %v, want 1", dep.Spec.Replicas)
	}

	// Selector must match the pod template labels, or the Deployment is invalid.
	sel := dep.Spec.Selector.MatchLabels["app"]
	tmplLabel := dep.Spec.Template.Labels["app"]
	if sel != "matey-controller-manager" || sel != tmplLabel {
		t.Errorf("selector/template label mismatch: selector=%q template=%q", sel, tmplLabel)
	}

	if len(dep.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(dep.Spec.Template.Spec.Containers))
	}
	ctr := dep.Spec.Template.Spec.Containers[0]
	if ctr.Image != "ghcr.io/phildougherty/matey:latest" {
		t.Errorf("Image = %q, want ghcr.io/phildougherty/matey:latest", ctr.Image)
	}

	// The --namespace arg must be threaded through from the composer namespace.
	foundNS := false
	for _, a := range ctr.Args {
		if a == "--namespace=team-ns" {
			foundNS = true
		}
	}
	if !foundNS {
		t.Errorf("container args %v missing --namespace=team-ns", ctr.Args)
	}

	// Ports.
	wantPorts := map[string]int32{"metrics": 8083, "health": 8082, "webhook": 9443}
	gotPorts := map[string]int32{}
	for _, p := range ctr.Ports {
		gotPorts[p.Name] = p.ContainerPort
	}
	for name, port := range wantPorts {
		if gotPorts[name] != port {
			t.Errorf("port %q = %d, want %d", name, gotPorts[name], port)
		}
	}

	// Probes target the health port.
	if ctr.LivenessProbe == nil || ctr.LivenessProbe.HTTPGet.Path != "/healthz" {
		t.Error("liveness probe should HTTP GET /healthz")
	}
	if ctr.ReadinessProbe == nil || ctr.ReadinessProbe.HTTPGet.Path != "/readyz" {
		t.Error("readiness probe should HTTP GET /readyz")
	}

	// Resource limits/requests.
	if ctr.Resources.Limits.Cpu().Cmp(resource.MustParse("500m")) != 0 {
		t.Errorf("cpu limit = %v, want 500m", ctr.Resources.Limits.Cpu())
	}
	if ctr.Resources.Requests.Memory().Cmp(resource.MustParse("128Mi")) != 0 {
		t.Errorf("memory request = %v, want 128Mi", ctr.Resources.Requests.Memory())
	}

	// Config volume mounted read-only from the matey-config ConfigMap.
	if len(ctr.VolumeMounts) != 1 || !ctr.VolumeMounts[0].ReadOnly {
		t.Errorf("expected 1 read-only volume mount, got %v", ctr.VolumeMounts)
	}
	if len(dep.Spec.Template.Spec.Volumes) != 1 ||
		dep.Spec.Template.Spec.Volumes[0].ConfigMap == nil ||
		dep.Spec.Template.Spec.Volumes[0].ConfigMap.Name != "matey-config" {
		t.Errorf("expected config volume sourced from matey-config ConfigMap, got %v",
			dep.Spec.Template.Spec.Volumes)
	}
	if dep.Spec.Template.Spec.ServiceAccountName != "matey-controller" {
		t.Errorf("ServiceAccountName = %q, want matey-controller", dep.Spec.Template.Spec.ServiceAccountName)
	}
}

func TestCreateControllerManagerService(t *testing.T) {
	c := &K8sComposer{namespace: "team-ns", logger: logging.NewLogger("error")}
	svc := c.createControllerManagerService()

	if svc.Name != "matey-controller-manager-metrics" {
		t.Errorf("Name = %q, want matey-controller-manager-metrics", svc.Name)
	}
	if svc.Namespace != "team-ns" {
		t.Errorf("Namespace = %q, want team-ns", svc.Namespace)
	}
	if svc.Spec.Selector["app"] != "matey-controller-manager" {
		t.Errorf("selector app = %q, want matey-controller-manager", svc.Spec.Selector["app"])
	}
	if len(svc.Spec.Ports) != 1 {
		t.Fatalf("expected 1 port, got %d", len(svc.Spec.Ports))
	}
	if svc.Spec.Ports[0].Port != 8083 || svc.Spec.Ports[0].Name != "metrics" {
		t.Errorf("port = %+v, want metrics/8083", svc.Spec.Ports[0])
	}
}

func TestBuildOAuthConfig(t *testing.T) {
	t.Run("nil oauth config returns nil", func(t *testing.T) {
		c := &K8sComposer{config: &config.ComposeConfig{}}
		if got := c.buildOAuthConfig(); got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})

	t.Run("disabled oauth returns nil", func(t *testing.T) {
		c := &K8sComposer{config: &config.ComposeConfig{
			OAuth: &config.OAuthConfig{Enabled: false, Issuer: "https://issuer"},
		}}
		if got := c.buildOAuthConfig(); got != nil {
			t.Errorf("expected nil for disabled oauth, got %+v", got)
		}
	})

	t.Run("enabled oauth is fully translated", func(t *testing.T) {
		cfg := &config.ComposeConfig{
			OAuth: &config.OAuthConfig{
				Enabled: true,
				Issuer:  "https://issuer.example.com",
				Endpoints: config.OAuthEndpoints{
					Authorization: "/auth",
					Token:         "/token",
					UserInfo:      "/userinfo",
					Revoke:        "/revoke",
					Discovery:     "/.well-known/openid-configuration",
				},
				Tokens: config.TokenConfig{
					AccessTokenTTL:  "1h",
					RefreshTokenTTL: "24h",
					CodeTTL:         "10m",
					Algorithm:       "RS256",
				},
				Security:        config.OAuthSecurityConfig{RequirePKCE: true},
				GrantTypes:      []string{"authorization_code", "refresh_token"},
				ResponseTypes:   []string{"code"},
				ScopesSupported: []string{"openid", "mcp:read"},
			},
		}
		c := &K8sComposer{config: cfg}
		got := c.buildOAuthConfig()
		if got == nil {
			t.Fatal("expected non-nil OAuthConfig")
		}
		if !got.Enabled || got.Issuer != "https://issuer.example.com" {
			t.Errorf("Enabled/Issuer = %v/%q", got.Enabled, got.Issuer)
		}
		if got.Endpoints.Token != "/token" || got.Endpoints.Authorization != "/auth" {
			t.Errorf("Endpoints not translated: %+v", got.Endpoints)
		}
		if got.Tokens.Algorithm != "RS256" || got.Tokens.AccessTokenTTL != "1h" {
			t.Errorf("Tokens not translated: %+v", got.Tokens)
		}
		if !got.Security.RequirePKCE {
			t.Error("Security.RequirePKCE should be true")
		}
		if len(got.GrantTypes) != 2 || len(got.ScopesSupported) != 2 {
			t.Errorf("array fields not translated: grants=%v scopes=%v", got.GrantTypes, got.ScopesSupported)
		}
	})
}

// --- fake-client backed tests -----------------------------------------

func TestCreateControllerManagerConfigMap(t *testing.T) {
	cfg := &config.ComposeConfig{Version: "1"}
	cfg.Servers = map[string]config.ServerConfig{
		"alpha": {Image: "alpha:latest"},
	}
	c := newTestComposer(t, cfg, "matey")

	if err := c.createControllerManagerConfigMap(); err != nil {
		t.Fatalf("createControllerManagerConfigMap() error: %v", err)
	}

	cm := &corev1.ConfigMap{}
	if err := c.k8sClient.Get(context.Background(),
		client.ObjectKey{Name: "matey-config", Namespace: "matey"}, cm); err != nil {
		t.Fatalf("expected ConfigMap to be created: %v", err)
	}
	yaml, ok := cm.Data["matey.yaml"]
	if !ok || yaml == "" {
		t.Fatal("ConfigMap should contain a non-empty matey.yaml key")
	}
	if cm.Labels["app.kubernetes.io/managed-by"] != "matey" {
		t.Errorf("managed-by label = %q, want matey", cm.Labels["app.kubernetes.io/managed-by"])
	}

	// Second call should update (not error) the existing ConfigMap.
	cfg.Servers["beta"] = config.ServerConfig{Image: "beta:latest"}
	if err := c.createControllerManagerConfigMap(); err != nil {
		t.Fatalf("second createControllerManagerConfigMap() error: %v", err)
	}
	cm2 := &corev1.ConfigMap{}
	if err := c.k8sClient.Get(context.Background(),
		client.ObjectKey{Name: "matey-config", Namespace: "matey"}, cm2); err != nil {
		t.Fatalf("expected ConfigMap to still exist: %v", err)
	}
	if cm2.Data["matey.yaml"] == yaml {
		t.Error("expected ConfigMap data to be updated on second call")
	}
}

func TestWaitForDeploymentDeleted(t *testing.T) {
	t.Run("returns promptly when deployment is already absent", func(t *testing.T) {
		c := newTestComposer(t, nil, "matey")
		start := time.Now()
		c.waitForDeploymentDeleted("ghost", 2*time.Second)
		if elapsed := time.Since(start); elapsed > 1*time.Second {
			t.Errorf("expected fast return for absent deployment, took %v", elapsed)
		}
	})

	t.Run("times out while deployment still exists", func(t *testing.T) {
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "stubborn", Namespace: "matey"},
		}
		c := newTestComposer(t, nil, "matey", dep)
		start := time.Now()
		c.waitForDeploymentDeleted("stubborn", 600*time.Millisecond)
		if elapsed := time.Since(start); elapsed < 500*time.Millisecond {
			t.Errorf("expected to block until timeout, returned after %v", elapsed)
		}
		// Deployment must still be present (function is best-effort, not destructive).
		got := &appsv1.Deployment{}
		if err := c.k8sClient.Get(context.Background(),
			client.ObjectKey{Name: "stubborn", Namespace: "matey"}, got); err != nil {
			t.Errorf("waitForDeploymentDeleted must not delete the deployment: %v", err)
		}
	})
}

func TestWaitForMateyDeploymentsDeleted(t *testing.T) {
	t.Run("returns promptly when no matey deployments exist", func(t *testing.T) {
		// A deployment without the app=matey label must be ignored.
		other := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "unrelated",
				Namespace: "matey",
				Labels:    map[string]string{"app": "something-else"},
			},
		}
		c := newTestComposer(t, nil, "matey", other)
		start := time.Now()
		c.waitForMateyDeploymentsDeleted(2 * time.Second)
		if elapsed := time.Since(start); elapsed > 1*time.Second {
			t.Errorf("expected fast return, took %v", elapsed)
		}
	})

	t.Run("times out while matey deployment still exists", func(t *testing.T) {
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "matey-proxy",
				Namespace: "matey",
				Labels:    map[string]string{"app": "matey"},
			},
		}
		c := newTestComposer(t, nil, "matey", dep)
		start := time.Now()
		c.waitForMateyDeploymentsDeleted(600 * time.Millisecond)
		if elapsed := time.Since(start); elapsed < 500*time.Millisecond {
			t.Errorf("expected to block until timeout, returned after %v", elapsed)
		}
	})
}

func TestIsControllerManagerDeploymentRunning(t *testing.T) {
	t.Run("false when deployment is absent", func(t *testing.T) {
		c := newTestComposer(t, nil, "matey")
		if c.isControllerManagerDeploymentRunning() {
			t.Error("expected false when controller-manager deployment absent")
		}
	})

	t.Run("false when deployment has zero ready replicas", func(t *testing.T) {
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "matey-controller-manager", Namespace: "matey"},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 0},
		}
		c := newTestComposer(t, nil, "matey", dep)
		if c.isControllerManagerDeploymentRunning() {
			t.Error("expected false when ready replicas is 0")
		}
	})

	t.Run("true when deployment has ready replicas", func(t *testing.T) {
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "matey-controller-manager", Namespace: "matey"},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 1},
		}
		c := newTestComposer(t, nil, "matey", dep)
		if !c.isControllerManagerDeploymentRunning() {
			t.Error("expected true when ready replicas > 0")
		}
	})
}

func TestGetSystemServiceStatus(t *testing.T) {
	tests := []struct {
		name   string
		status appsv1.DeploymentStatus
		want   string
	}{
		{
			name:   "ready replicas equal desired is running",
			status: appsv1.DeploymentStatus{Replicas: 2, ReadyReplicas: 2},
			want:   "running",
		},
		{
			name:   "zero replicas is stopped",
			status: appsv1.DeploymentStatus{Replicas: 0, ReadyReplicas: 0},
			want:   "stopped",
		},
		{
			name:   "replicas desired but unavailable is starting",
			status: appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 0, UnavailableReplicas: 1},
			want:   "starting",
		},
		{
			name:   "replicas desired none ready none unavailable is pending",
			status: appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 0},
			want:   "pending",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "matey-proxy", Namespace: "matey"},
				Status:     tt.status,
			}
			c := newTestComposer(t, nil, "matey", dep)
			if got := c.getSystemServiceStatus("matey-proxy"); got != tt.want {
				t.Errorf("getSystemServiceStatus() = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("absent deployment is not-found", func(t *testing.T) {
		c := newTestComposer(t, nil, "matey")
		if got := c.getSystemServiceStatus("matey-proxy"); got != "not-found" {
			t.Errorf("getSystemServiceStatus() = %q, want not-found", got)
		}
	})
}

func TestGetServiceType(t *testing.T) {
	t.Run("MCPPostgres resource yields database", func(t *testing.T) {
		pg := &crd.MCPPostgres{
			ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "matey"},
		}
		c := newTestComposer(t, nil, "matey", pg)
		if got := c.getServiceType("db"); got != "database" {
			t.Errorf("getServiceType(db) = %q, want database", got)
		}
	})

	t.Run("non-postgres resource defaults to mcp-server", func(t *testing.T) {
		c := newTestComposer(t, nil, "matey")
		if got := c.getServiceType("some-server"); got != "mcp-server" {
			t.Errorf("getServiceType() = %q, want mcp-server", got)
		}
	})
}

func TestGetMCPServerStatus(t *testing.T) {
	t.Run("no deployment and no CRD is stopped", func(t *testing.T) {
		c := newTestComposer(t, nil, "matey")
		if got := c.getMCPServerStatus("nonexistent"); got != "stopped" {
			t.Errorf("getMCPServerStatus() = %q, want stopped", got)
		}
	})

	t.Run("MCPServer CRD without deployment uses CRD phase", func(t *testing.T) {
		srv := &crd.MCPServer{
			ObjectMeta: metav1.ObjectMeta{Name: "crd-only", Namespace: "matey"},
			Status:     crd.MCPServerStatus{Phase: crd.MCPServerPhaseRunning},
		}
		c := newTestComposer(t, nil, "matey", srv)
		if got := c.getMCPServerStatus("crd-only"); got != "running" {
			t.Errorf("getMCPServerStatus() = %q, want running", got)
		}
	})

	t.Run("MCPPostgres CRD takes precedence and maps phase", func(t *testing.T) {
		pg := &crd.MCPPostgres{
			ObjectMeta: metav1.ObjectMeta{Name: "pgsvc", Namespace: "matey"},
			Status:     crd.MCPPostgresStatus{Phase: crd.PostgresPhaseRunning},
		}
		c := newTestComposer(t, nil, "matey", pg)
		if got := c.getMCPServerStatus("pgsvc"); got != "running" {
			t.Errorf("getMCPServerStatus() = %q, want running", got)
		}
	})

	t.Run("deployment with no pods is pending", func(t *testing.T) {
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "dep-server", Namespace: "matey"},
			Status:     appsv1.DeploymentStatus{Replicas: 1},
		}
		c := newTestComposer(t, nil, "matey", dep)
		if got := c.getMCPServerStatus("dep-server"); got != "pending" {
			t.Errorf("getMCPServerStatus() = %q, want pending", got)
		}
	})

	t.Run("deployment with running ready pod is running", func(t *testing.T) {
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "dep-server", Namespace: "matey"},
			Status:     appsv1.DeploymentStatus{Replicas: 1, ReadyReplicas: 1},
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dep-server-abc",
				Namespace: "matey",
				Labels:    map[string]string{"app": "dep-server"},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		}
		c := newTestComposer(t, nil, "matey", dep, pod)
		if got := c.getMCPServerStatus("dep-server"); got != "running" {
			t.Errorf("getMCPServerStatus() = %q, want running", got)
		}
	})

	t.Run("pod in CrashLoopBackOff is failed", func(t *testing.T) {
		dep := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "dep-server", Namespace: "matey"},
			Status:     appsv1.DeploymentStatus{Replicas: 1},
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dep-server-abc",
				Namespace: "matey",
				Labels:    map[string]string{"app": "dep-server"},
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				ContainerStatuses: []corev1.ContainerStatus{
					{State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
					}},
				},
			},
		}
		c := newTestComposer(t, nil, "matey", dep, pod)
		if got := c.getMCPServerStatus("dep-server"); got != "failed" {
			t.Errorf("getMCPServerStatus() = %q, want failed", got)
		}
	})
}

func TestStartStopSystemService(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "matey-proxy", Namespace: "matey"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(0)},
	}
	c := newTestComposer(t, nil, "matey", dep)

	if err := c.startSystemService("matey-proxy"); err != nil {
		t.Fatalf("startSystemService error: %v", err)
	}
	got := &appsv1.Deployment{}
	if err := c.k8sClient.Get(context.Background(),
		client.ObjectKey{Name: "matey-proxy", Namespace: "matey"}, got); err != nil {
		t.Fatalf("get after start: %v", err)
	}
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 1 {
		t.Errorf("after start, replicas = %v, want 1", got.Spec.Replicas)
	}

	if err := c.stopSystemService("matey-proxy"); err != nil {
		t.Fatalf("stopSystemService error: %v", err)
	}
	if err := c.k8sClient.Get(context.Background(),
		client.ObjectKey{Name: "matey-proxy", Namespace: "matey"}, got); err != nil {
		t.Fatalf("get after stop: %v", err)
	}
	if got.Spec.Replicas == nil || *got.Spec.Replicas != 0 {
		t.Errorf("after stop, replicas = %v, want 0", got.Spec.Replicas)
	}

	// Missing deployment must error rather than panic.
	if err := c.startSystemService("does-not-exist"); err == nil {
		t.Error("expected error starting nonexistent system service")
	}
}

func TestRestartSystemService(t *testing.T) {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "matey-proxy", Namespace: "matey"},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(1)},
	}
	c := newTestComposer(t, nil, "matey", dep)

	if err := c.restartSystemService("matey-proxy"); err != nil {
		t.Fatalf("restartSystemService error: %v", err)
	}
	got := &appsv1.Deployment{}
	if err := c.k8sClient.Get(context.Background(),
		client.ObjectKey{Name: "matey-proxy", Namespace: "matey"}, got); err != nil {
		t.Fatalf("get after restart: %v", err)
	}
	if _, ok := got.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"]; !ok {
		t.Error("restartSystemService should set the restartedAt annotation to trigger a rollout")
	}
}

func int32Ptr(v int32) *int32 { return &v }
