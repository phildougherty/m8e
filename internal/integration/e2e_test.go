package integration

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/controllers"
	mcpv1 "github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/discovery"
)

// TestEnvironment holds the test environment setup
type TestEnvironment struct {
	Client     client.Client
	Cfg        *rest.Config
	TestEnv    *envtest.Environment
	Reconciler *controllers.MCPServerReconciler
	Ctx        context.Context
	Cancel     context.CancelFunc
}

// SetupTestEnvironment creates a test environment for integration tests
func SetupTestEnvironment(t *testing.T) *TestEnvironment {
	t.Helper()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	// For integration tests, we'll use a fake client instead of envtest
	// In a real integration test, you would use envtest.Environment
	s := scheme.Scheme
	err := mcpv1.AddToScheme(s)
	if err != nil {
		t.Fatalf("Failed to add scheme: %v", err)
	}

	// Create fake client
	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithStatusSubresource(&mcpv1.MCPServer{}).
		Build()

	// Create test config
	cfg := &config.ComposeConfig{
		Version: "1",
		Servers: make(map[string]config.ServerConfig),
	}

	// Create reconciler
	reconciler := &controllers.MCPServerReconciler{
		Client:            fakeClient,
		Scheme:            s,
		ServiceDiscovery:  &discovery.K8sServiceDiscovery{},
		ConnectionManager: &discovery.DynamicConnectionManager{},
		Config:            cfg,
	}

	return &TestEnvironment{
		Client:     fakeClient,
		Cfg:        nil, // Not using real config for fake client
		TestEnv:    nil, // Not using envtest for this example
		Reconciler: reconciler,
		Ctx:        ctx,
		Cancel:     cancel,
	}
}

// TeardownTestEnvironment cleans up the test environment
func (te *TestEnvironment) TeardownTestEnvironment(t *testing.T) {
	t.Helper()
	te.Cancel()
	
	if te.TestEnv != nil {
		err := te.TestEnv.Stop()
		if err != nil {
			t.Errorf("Failed to stop test environment: %v", err)
		}
	}
}

// TestMCPServerLifecycle tests the complete lifecycle of an MCP server
func TestMCPServerLifecycle(t *testing.T) {
	env := SetupTestEnvironment(t)
	defer env.TeardownTestEnvironment(t)

	t.Run("complete_mcp_server_lifecycle", func(t *testing.T) {
		// Phase 1: Create MCPServer
		mcpServer := &mcpv1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-mcp-server",
				Namespace: "default",
			},
			Spec: mcpv1.MCPServerSpec{
				Image:    "test-mcp-server:latest",
				Protocol: "http",
				HttpPort: 8080,
				Command:  []string{"node", "server.js"},
				Env: map[string]string{
					"NODE_ENV": "production",
					"PORT":     "8080",
				},
				Resources: mcpv1.ResourceRequirements{
					Limits: mcpv1.ResourceList{
						"cpu":    "100m",
						"memory": "128Mi",
					},
					Requests: mcpv1.ResourceList{
						"cpu":    "50m",
						"memory": "64Mi",
					},
				},
			},
		}

		// Create the MCPServer
		err := env.Client.Create(env.Ctx, mcpServer)
		if err != nil {
			t.Fatalf("Failed to create MCPServer: %v", err)
		}

		// Phase 2: First reconciliation - should add finalizer
		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      mcpServer.Name,
				Namespace: mcpServer.Namespace,
			},
		}

		result, err := env.Reconciler.Reconcile(env.Ctx, req)
		if err != nil {
			t.Fatalf("First reconciliation failed: %v", err)
		}

		// Should return immediately to add finalizer
		if result.RequeueAfter != 0 {
			t.Errorf("Expected immediate requeue after adding finalizer, got %v", result.RequeueAfter)
		}

		// Verify finalizer was added
		updated := &mcpv1.MCPServer{}
		err = env.Client.Get(env.Ctx, req.NamespacedName, updated)
		if err != nil {
			t.Fatalf("Failed to get updated MCPServer: %v", err)
		}

		if len(updated.Finalizers) == 0 {
			t.Error("Expected finalizer to be added")
		}

		// Phase 3: Second reconciliation - should create resources
		result, err = env.Reconciler.Reconcile(env.Ctx, req)
		if err != nil {
			t.Fatalf("Second reconciliation failed: %v", err)
		}

		// Should requeue after 30 seconds for status monitoring
		if result.RequeueAfter != 30*time.Second {
			t.Errorf("Expected requeue after 30s, got %v", result.RequeueAfter)
		}

		// Phase 4: Verify Deployment was created
		deployment := &appsv1.Deployment{}
		err = env.Client.Get(env.Ctx, req.NamespacedName, deployment)
		if err != nil {
			t.Fatalf("Failed to get deployment: %v", err)
		}

		// Verify deployment properties
		if deployment.Name != mcpServer.Name {
			t.Errorf("Expected deployment name %s, got %s", mcpServer.Name, deployment.Name)
		}

		if len(deployment.Spec.Template.Spec.Containers) == 0 {
			t.Fatal("Expected at least one container in deployment")
		}

		container := deployment.Spec.Template.Spec.Containers[0]
		if container.Image != mcpServer.Spec.Image {
			t.Errorf("Expected image %s, got %s", mcpServer.Spec.Image, container.Image)
		}

		// Verify environment variables
		if len(container.Env) == 0 {
			t.Error("Expected environment variables to be set")
		}

		// Verify ports
		if len(container.Ports) == 0 {
			t.Error("Expected ports to be set")
		} else if container.Ports[0].ContainerPort != mcpServer.Spec.HttpPort {
			t.Errorf("Expected port %d, got %d", mcpServer.Spec.HttpPort, container.Ports[0].ContainerPort)
		}

		// Phase 5: Verify Service was created
		service := &corev1.Service{}
		err = env.Client.Get(env.Ctx, req.NamespacedName, service)
		if err != nil {
			t.Fatalf("Failed to get service: %v", err)
		}

		// Verify service properties
		if service.Name != mcpServer.Name {
			t.Errorf("Expected service name %s, got %s", mcpServer.Name, service.Name)
		}

		if len(service.Spec.Ports) == 0 {
			t.Error("Expected service ports to be set")
		} else if service.Spec.Ports[0].Port != mcpServer.Spec.HttpPort {
			t.Errorf("Expected service port %d, got %d", mcpServer.Spec.HttpPort, service.Spec.Ports[0].Port)
		}

		// Verify service discovery labels
		expectedLabels := map[string]string{
			"mcp.matey.ai/role":        "server",
			"mcp.matey.ai/protocol":    "http",
			"mcp.matey.ai/server-name": mcpServer.Name,
		}

		for key, expectedValue := range expectedLabels {
			if actualValue, exists := service.Labels[key]; !exists {
				t.Errorf("Expected label %s to be set", key)
			} else if actualValue != expectedValue {
				t.Errorf("Expected label %s=%s, got %s", key, expectedValue, actualValue)
			}
		}

		// Phase 6: Simulate deployment becoming ready
		deployment.Status.ReadyReplicas = 1
		deployment.Status.Replicas = 1
		deployment.Status.Conditions = []appsv1.DeploymentCondition{
			{
				Type:   appsv1.DeploymentAvailable,
				Status: corev1.ConditionTrue,
			},
		}
		err = env.Client.Status().Update(env.Ctx, deployment)
		if err != nil {
			t.Fatalf("Failed to update deployment status: %v", err)
		}

		// Phase 7: Third reconciliation - should update status
		result, err = env.Reconciler.Reconcile(env.Ctx, req)
		if err != nil {
			t.Fatalf("Third reconciliation failed: %v", err)
		}

		// Verify MCPServer status was updated
		err = env.Client.Get(env.Ctx, req.NamespacedName, updated)
		if err != nil {
			t.Fatalf("Failed to get updated MCPServer: %v", err)
		}

		if updated.Status.Phase != mcpv1.MCPServerPhaseRunning {
			t.Errorf("Expected phase %s, got %s", mcpv1.MCPServerPhaseRunning, updated.Status.Phase)
		}

		if updated.Status.ReadyReplicas != 1 {
			t.Errorf("Expected ReadyReplicas 1, got %d", updated.Status.ReadyReplicas)
		}

		// Phase 8: Test deletion
		// Delete the MCPServer to trigger deletion reconciliation
		err = env.Client.Delete(env.Ctx, updated)
		if err != nil {
			t.Fatalf("Failed to delete MCPServer: %v", err)
		}

		// Final reconciliation - should handle deletion
		result, err = env.Reconciler.Reconcile(env.Ctx, req)
		if err != nil {
			t.Fatalf("Deletion reconciliation failed: %v", err)
		}

		// Should not requeue after deletion
		if result.RequeueAfter != 0 {
			t.Errorf("Expected no requeue after deletion, got %v", result.RequeueAfter)
		}

		// Verify finalizer was removed (object may or may not exist depending on fake client behavior)
		finalUpdated := &mcpv1.MCPServer{}
		err = env.Client.Get(env.Ctx, req.NamespacedName, finalUpdated)
		if err == nil {
			// If object still exists, finalizer should be removed
			if len(finalUpdated.Finalizers) > 0 {
				t.Error("Expected finalizer to be removed after deletion")
			}
		}
		// If object doesn't exist, that's also acceptable as it means deletion completed
	})
}

// TestMCPServerServiceDiscovery tests service discovery functionality
func TestMCPServerServiceDiscovery(t *testing.T) {
	env := SetupTestEnvironment(t)
	defer env.TeardownTestEnvironment(t)

	t.Run("service_discovery_labels", func(t *testing.T) {
		// Create MCPServer with capabilities
		mcpServer := &mcpv1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "discovery-test-server",
				Namespace: "default",
			},
			Spec: mcpv1.MCPServerSpec{
				Image:        "test-server:latest",
				Protocol:     "http",
				HttpPort:     8080,
				Capabilities: []string{"tools", "resources"},
				ServiceDiscovery: mcpv1.ServiceDiscoveryConfig{
					DiscoveryLabels: map[string]string{
						"custom-label":     "custom-value",
						"service-type":     "mcp-server",
						"feature-enabled":  "true",
					},
				},
			},
		}

		// Create and reconcile
		err := env.Client.Create(env.Ctx, mcpServer)
		if err != nil {
			t.Fatalf("Failed to create MCPServer: %v", err)
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      mcpServer.Name,
				Namespace: mcpServer.Namespace,
			},
		}

		// First reconciliation - add finalizer
		_, err = env.Reconciler.Reconcile(env.Ctx, req)
		if err != nil {
			t.Fatalf("First reconciliation failed: %v", err)
		}

		// Second reconciliation - create resources
		_, err = env.Reconciler.Reconcile(env.Ctx, req)
		if err != nil {
			t.Fatalf("Second reconciliation failed: %v", err)
		}

		// Verify service has correct discovery labels
		service := &corev1.Service{}
		err = env.Client.Get(env.Ctx, req.NamespacedName, service)
		if err != nil {
			t.Fatalf("Failed to get service: %v", err)
		}

		// Check standard MCP labels
		expectedLabels := map[string]string{
			"mcp.matey.ai/role":         "server",
			"mcp.matey.ai/protocol":     "http",
			"mcp.matey.ai/server-name":  mcpServer.Name,
			"mcp.matey.ai/capabilities": "tools.resources", // capabilities joined with dots
		}

		for key, expectedValue := range expectedLabels {
			if actualValue, exists := service.Labels[key]; !exists {
				t.Errorf("Expected standard label %s to be set", key)
			} else if actualValue != expectedValue {
				t.Errorf("Expected standard label %s=%s, got %s", key, expectedValue, actualValue)
			}
		}

		// Check custom discovery labels
		customLabels := map[string]string{
			"custom-label":    "custom-value",
			"service-type":    "mcp-server",
			"feature-enabled": "true",
		}

		for key, expectedValue := range customLabels {
			if actualValue, exists := service.Labels[key]; !exists {
				t.Errorf("Expected custom label %s to be set", key)
			} else if actualValue != expectedValue {
				t.Errorf("Expected custom label %s=%s, got %s", key, expectedValue, actualValue)
			}
		}
	})
}

// TestMCPServerResourceManagement tests resource limits and requests
func TestMCPServerResourceManagement(t *testing.T) {
	env := SetupTestEnvironment(t)
	defer env.TeardownTestEnvironment(t)

	t.Run("resource_limits_and_requests", func(t *testing.T) {
		mcpServer := &mcpv1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "resource-test-server",
				Namespace: "default",
			},
			Spec: mcpv1.MCPServerSpec{
				Image:    "test-server:latest",
				Protocol: "http",
				HttpPort: 8080,
				Resources: mcpv1.ResourceRequirements{
					Limits: mcpv1.ResourceList{
						"cpu":    "200m",
						"memory": "256Mi",
					},
					Requests: mcpv1.ResourceList{
						"cpu":    "100m",
						"memory": "128Mi",
					},
				},
			},
		}

		// Create and reconcile
		err := env.Client.Create(env.Ctx, mcpServer)
		if err != nil {
			t.Fatalf("Failed to create MCPServer: %v", err)
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      mcpServer.Name,
				Namespace: mcpServer.Namespace,
			},
		}

		// Reconcile to create resources
		_, err = env.Reconciler.Reconcile(env.Ctx, req)
		if err != nil {
			t.Fatalf("First reconciliation failed: %v", err)
		}

		_, err = env.Reconciler.Reconcile(env.Ctx, req)
		if err != nil {
			t.Fatalf("Second reconciliation failed: %v", err)
		}

		// Verify deployment has correct resource requirements
		deployment := &appsv1.Deployment{}
		err = env.Client.Get(env.Ctx, req.NamespacedName, deployment)
		if err != nil {
			t.Fatalf("Failed to get deployment: %v", err)
		}

		if len(deployment.Spec.Template.Spec.Containers) == 0 {
			t.Fatal("Expected at least one container in deployment")
		}

		container := deployment.Spec.Template.Spec.Containers[0]
		
		// Check resource limits
		if container.Resources.Limits == nil {
			t.Fatal("Expected resource limits to be set")
		}

		cpuLimit := container.Resources.Limits[corev1.ResourceCPU]
		if cpuLimit.String() != "200m" {
			t.Errorf("Expected CPU limit 200m, got %s", cpuLimit.String())
		}

		memoryLimit := container.Resources.Limits[corev1.ResourceMemory]
		if memoryLimit.String() != "256Mi" {
			t.Errorf("Expected memory limit 256Mi, got %s", memoryLimit.String())
		}

		// Check resource requests
		if container.Resources.Requests == nil {
			t.Fatal("Expected resource requests to be set")
		}

		cpuRequest := container.Resources.Requests[corev1.ResourceCPU]
		if cpuRequest.String() != "100m" {
			t.Errorf("Expected CPU request 100m, got %s", cpuRequest.String())
		}

		memoryRequest := container.Resources.Requests[corev1.ResourceMemory]
		if memoryRequest.String() != "128Mi" {
			t.Errorf("Expected memory request 128Mi, got %s", memoryRequest.String())
		}
	})
}

// TestMCPServerScaling tests scaling functionality
func TestMCPServerScaling(t *testing.T) {
	env := SetupTestEnvironment(t)
	defer env.TeardownTestEnvironment(t)

	t.Run("custom_replica_count", func(t *testing.T) {
		replicas := int32(3)
		mcpServer := &mcpv1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "scaling-test-server",
				Namespace: "default",
			},
			Spec: mcpv1.MCPServerSpec{
				Image:    "test-server:latest",
				Protocol: "http",
				HttpPort: 8080,
				Replicas: &replicas,
			},
		}

		// Create and reconcile
		err := env.Client.Create(env.Ctx, mcpServer)
		if err != nil {
			t.Fatalf("Failed to create MCPServer: %v", err)
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      mcpServer.Name,
				Namespace: mcpServer.Namespace,
			},
		}

		// Reconcile to create resources
		_, err = env.Reconciler.Reconcile(env.Ctx, req)
		if err != nil {
			t.Fatalf("First reconciliation failed: %v", err)
		}

		_, err = env.Reconciler.Reconcile(env.Ctx, req)
		if err != nil {
			t.Fatalf("Second reconciliation failed: %v", err)
		}

		// Verify deployment has correct replica count
		deployment := &appsv1.Deployment{}
		err = env.Client.Get(env.Ctx, req.NamespacedName, deployment)
		if err != nil {
			t.Fatalf("Failed to get deployment: %v", err)
		}

		if deployment.Spec.Replicas == nil {
			t.Fatal("Expected replicas to be set")
		}

		if *deployment.Spec.Replicas != replicas {
			t.Errorf("Expected %d replicas, got %d", replicas, *deployment.Spec.Replicas)
		}
	})
}

// TestMCPServerErrorHandling tests error scenarios
func TestMCPServerErrorHandling(t *testing.T) {
	env := SetupTestEnvironment(t)
	defer env.TeardownTestEnvironment(t)

	t.Run("reconcile_nonexistent_server", func(t *testing.T) {
		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      "nonexistent-server",
				Namespace: "default",
			},
		}

		// Try to reconcile non-existent server
		result, err := env.Reconciler.Reconcile(env.Ctx, req)
		if err != nil {
			t.Fatalf("Expected no error for non-existent server, got: %v", err)
		}

		// Should return immediately without requeue
		if result.RequeueAfter != 0 {
			t.Errorf("Expected no requeue for non-existent server, got %v", result.RequeueAfter)
		}
	})

	t.Run("server_with_missing_required_fields", func(t *testing.T) {
		// Create server with missing required fields
		mcpServer := &mcpv1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "invalid-server",
				Namespace: "default",
			},
			Spec: mcpv1.MCPServerSpec{
				// Missing required Image field
				Protocol: "http",
				HttpPort: 8080,
			},
		}

		err := env.Client.Create(env.Ctx, mcpServer)
		if err != nil {
			t.Fatalf("Failed to create MCPServer: %v", err)
		}

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      mcpServer.Name,
				Namespace: mcpServer.Namespace,
			},
		}

		// First reconciliation should still succeed (adds finalizer)
		_, err = env.Reconciler.Reconcile(env.Ctx, req)
		if err != nil {
			t.Fatalf("First reconciliation failed: %v", err)
		}

		// Second reconciliation should create deployment with empty image
		// (Kubernetes will handle the validation)
		_, err = env.Reconciler.Reconcile(env.Ctx, req)
		if err != nil {
			t.Fatalf("Second reconciliation failed: %v", err)
		}

		// Verify deployment was created even with empty image
		deployment := &appsv1.Deployment{}
		err = env.Client.Get(env.Ctx, req.NamespacedName, deployment)
		if err != nil {
			t.Fatalf("Failed to get deployment: %v", err)
		}

		// The deployment should have empty image (Kubernetes will reject it)
		if len(deployment.Spec.Template.Spec.Containers) == 0 {
			t.Fatal("Expected at least one container in deployment")
		}

		container := deployment.Spec.Template.Spec.Containers[0]
		if container.Image != "" {
			t.Errorf("Expected empty image for invalid server, got %s", container.Image)
		}
	})
}