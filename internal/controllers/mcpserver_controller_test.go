package controllers

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/phildougherty/m8e/internal/config"
	mcpv1 "github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/discovery"
)

func TestMCPServerReconciler_Reconcile(t *testing.T) {
	// Setup test scheme
	s := scheme.Scheme
	err := mcpv1.AddToScheme(s)
	if err != nil {
		t.Fatalf("Failed to add scheme: %v", err)
	}

	// Test cases
	tests := []struct {
		name           string
		mcpServer      *mcpv1.MCPServer
		expectError    bool
		expectDeploy   bool
		expectService  bool
		expectResult   ctrl.Result
	}{
		{
			name: "Create new MCPServer with basic config",
			mcpServer: &mcpv1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-server",
					Namespace: "default",
				},
				Spec: mcpv1.MCPServerSpec{
					Image:    "test-image:latest",
					Protocol: "http",
					Port:     8080,
					Command:  []string{"node", "server.js"},
					Env: []corev1.EnvVar{
						{Name: "NODE_ENV", Value: "production"},
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
					},
				},
			},
			expectError:   false,
			expectDeploy:  true,
			expectService: true,
			expectResult:  ctrl.Result{RequeueAfter: 30 * time.Second},
		},
		{
			name: "Create MCPServer with SSE protocol",
			mcpServer: &mcpv1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sse-server",
					Namespace: "default",
				},
				Spec: mcpv1.MCPServerSpec{
					Image:    "sse-image:latest",
					Protocol: "sse",
					Port:     8080,
					SSEPath:  "/events",
					Command:  []string{"python", "sse_server.py"},
				},
			},
			expectError:   false,
			expectDeploy:  true,
			expectService: true,
			expectResult:  ctrl.Result{RequeueAfter: 30 * time.Second},
		},
		{
			name: "Create MCPServer with custom replicas",
			mcpServer: &mcpv1.MCPServer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "scaled-server",
					Namespace: "default",
				},
				Spec: mcpv1.MCPServerSpec{
					Image:    "scaled-image:latest",
					Protocol: "http",
					Port:     8080,
					Replicas: 3,
				},
			},
			expectError:   false,
			expectDeploy:  true,
			expectService: true,
			expectResult:  ctrl.Result{RequeueAfter: 30 * time.Second},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.mcpServer).Build()

			// Create mock service discovery and connection manager
			serviceDiscovery := &discovery.K8sServiceDiscovery{}
			connectionManager := &discovery.DynamicConnectionManager{}

			// Create test config
			cfg := &config.ComposeConfig{
				Version: "1",
				Servers: make(map[string]config.ServerConfig),
			}

			// Create reconciler
			reconciler := &MCPServerReconciler{
				Client:            fakeClient,
				Scheme:            s,
				ServiceDiscovery:  serviceDiscovery,
				ConnectionManager: connectionManager,
				Config:            cfg,
			}

			// Create reconcile request
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.mcpServer.Name,
					Namespace: tt.mcpServer.Namespace,
				},
			}

			// Run reconcile
			result, err := reconciler.Reconcile(context.TODO(), req)

			// Check error expectation
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			// Check result
			if result.RequeueAfter != tt.expectResult.RequeueAfter {
				t.Errorf("Expected RequeueAfter %v, got %v", tt.expectResult.RequeueAfter, result.RequeueAfter)
			}

			// Check if deployment was created
			if tt.expectDeploy {
				deployment := &appsv1.Deployment{}
				deploymentName := types.NamespacedName{
					Name:      tt.mcpServer.Name,
					Namespace: tt.mcpServer.Namespace,
				}
				err := fakeClient.Get(context.TODO(), deploymentName, deployment)
				if err != nil {
					t.Errorf("Expected deployment to be created but got error: %v", err)
				} else {
					// Verify deployment properties
					if deployment.Spec.Template.Spec.Containers[0].Image != tt.mcpServer.Spec.Image {
						t.Errorf("Expected image %s, got %s", tt.mcpServer.Spec.Image, deployment.Spec.Template.Spec.Containers[0].Image)
					}
					
					if tt.mcpServer.Spec.Replicas > 0 && *deployment.Spec.Replicas != tt.mcpServer.Spec.Replicas {
						t.Errorf("Expected replicas %d, got %d", tt.mcpServer.Spec.Replicas, *deployment.Spec.Replicas)
					}
				}
			}

			// Check if service was created
			if tt.expectService {
				service := &corev1.Service{}
				serviceName := types.NamespacedName{
					Name:      tt.mcpServer.Name,
					Namespace: tt.mcpServer.Namespace,
				}
				err := fakeClient.Get(context.TODO(), serviceName, service)
				if err != nil {
					t.Errorf("Expected service to be created but got error: %v", err)
				} else {
					// Verify service properties
					if len(service.Spec.Ports) == 0 {
						t.Error("Expected service to have ports")
					} else if service.Spec.Ports[0].Port != tt.mcpServer.Spec.Port {
						t.Errorf("Expected port %d, got %d", tt.mcpServer.Spec.Port, service.Spec.Ports[0].Port)
					}
				}
			}
		})
	}
}

func TestMCPServerReconciler_Delete(t *testing.T) {
	// Setup test scheme
	s := scheme.Scheme
	err := mcpv1.AddToScheme(s)
	if err != nil {
		t.Fatalf("Failed to add scheme: %v", err)
	}

	// Create MCPServer with finalizer
	mcpServer := &mcpv1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "default",
			Finalizers: []string{
				"mcpserver.mcp.matey.ai/finalizer",
			},
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
		},
		Spec: mcpv1.MCPServerSpec{
			Image:    "test-image:latest",
			Protocol: "http",
			Port:     8080,
		},
	}

	// Create fake client with existing resources
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &[]int32{1}[0],
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-server"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test-server"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-server",
							Image: "test-image:latest",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
							},
						},
					},
				},
			},
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "test-server"},
			Ports: []corev1.ServicePort{
				{
					Port:       8080,
					TargetPort: intstr.FromInt(8080),
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(mcpServer, deployment, service).Build()

	// Create reconciler
	reconciler := &MCPServerReconciler{
		Client:            fakeClient,
		Scheme:            s,
		ServiceDiscovery:  &discovery.K8sServiceDiscovery{},
		ConnectionManager: &discovery.DynamicConnectionManager{},
		Config:            &config.ComposeConfig{Version: "1"},
	}

	// Create reconcile request
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-server",
			Namespace: "default",
		},
	}

	// Run reconcile
	result, err := reconciler.Reconcile(context.TODO(), req)
	if err != nil {
		t.Errorf("Expected no error during deletion but got: %v", err)
	}

	// Check that deployment was deleted
	deletedDeploy := &appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "test-server", Namespace: "default"}, deletedDeploy)
	if !errors.IsNotFound(err) {
		t.Errorf("Expected deployment to be deleted but it still exists")
	}

	// Check that service was deleted
	deletedService := &corev1.Service{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "test-server", Namespace: "default"}, deletedService)
	if !errors.IsNotFound(err) {
		t.Errorf("Expected service to be deleted but it still exists")
	}

	// Check that finalizer was removed
	updatedMCPServer := &mcpv1.MCPServer{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "test-server", Namespace: "default"}, updatedMCPServer)
	if err != nil {
		t.Errorf("Expected MCPServer to exist but got error: %v", err)
	} else if len(updatedMCPServer.Finalizers) > 0 {
		t.Error("Expected finalizer to be removed")
	}

	// Should not requeue since deletion is complete
	if result.RequeueAfter != 0 {
		t.Errorf("Expected no requeue after deletion, got %v", result.RequeueAfter)
	}
}

func TestMCPServerReconciler_CreateDeployment(t *testing.T) {
	// Setup test scheme
	s := scheme.Scheme
	err := mcpv1.AddToScheme(s)
	if err != nil {
		t.Fatalf("Failed to add scheme: %v", err)
	}

	mcpServer := &mcpv1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "default",
		},
		Spec: mcpv1.MCPServerSpec{
			Image:    "test-image:latest",
			Protocol: "http",
			Port:     8080,
			Command:  []string{"node", "server.js"},
			Args:     []string{"--port", "8080"},
			Env: []corev1.EnvVar{
				{Name: "NODE_ENV", Value: "production"},
				{Name: "PORT", Value: "8080"},
			},
			Resources: corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("200m"),
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				},
			},
			Replicas: 2,
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(mcpServer).Build()

	reconciler := &MCPServerReconciler{
		Client:            fakeClient,
		Scheme:            s,
		ServiceDiscovery:  &discovery.K8sServiceDiscovery{},
		ConnectionManager: &discovery.DynamicConnectionManager{},
		Config:            &config.ComposeConfig{Version: "1"},
	}

	// Create deployment using internal method (we need to make it public for testing)
	deployment, err := reconciler.createDeployment(mcpServer)
	if err != nil {
		t.Fatalf("Failed to create deployment: %v", err)
	}

	// Verify deployment structure
	if deployment.Name != mcpServer.Name {
		t.Errorf("Expected deployment name %s, got %s", mcpServer.Name, deployment.Name)
	}

	if deployment.Namespace != mcpServer.Namespace {
		t.Errorf("Expected deployment namespace %s, got %s", mcpServer.Namespace, deployment.Namespace)
	}

	if *deployment.Spec.Replicas != mcpServer.Spec.Replicas {
		t.Errorf("Expected replicas %d, got %d", mcpServer.Spec.Replicas, *deployment.Spec.Replicas)
	}

	container := deployment.Spec.Template.Spec.Containers[0]
	if container.Image != mcpServer.Spec.Image {
		t.Errorf("Expected image %s, got %s", mcpServer.Spec.Image, container.Image)
	}

	if len(container.Command) != len(mcpServer.Spec.Command) {
		t.Errorf("Expected command length %d, got %d", len(mcpServer.Spec.Command), len(container.Command))
	}

	if len(container.Args) != len(mcpServer.Spec.Args) {
		t.Errorf("Expected args length %d, got %d", len(mcpServer.Spec.Args), len(container.Args))
	}

	if len(container.Env) != len(mcpServer.Spec.Env) {
		t.Errorf("Expected env length %d, got %d", len(mcpServer.Spec.Env), len(container.Env))
	}

	if len(container.Ports) == 0 {
		t.Error("Expected container to have ports")
	} else if container.Ports[0].ContainerPort != mcpServer.Spec.Port {
		t.Errorf("Expected port %d, got %d", mcpServer.Spec.Port, container.Ports[0].ContainerPort)
	}

	// Verify resource requirements
	if container.Resources.Limits.Cpu().Cmp(mcpServer.Spec.Resources.Limits.Cpu()) != 0 {
		t.Errorf("Expected CPU limit %v, got %v", mcpServer.Spec.Resources.Limits.Cpu(), container.Resources.Limits.Cpu())
	}

	if container.Resources.Limits.Memory().Cmp(mcpServer.Spec.Resources.Limits.Memory()) != 0 {
		t.Errorf("Expected memory limit %v, got %v", mcpServer.Spec.Resources.Limits.Memory(), container.Resources.Limits.Memory())
	}
}

func TestMCPServerReconciler_CreateService(t *testing.T) {
	// Setup test scheme
	s := scheme.Scheme
	err := mcpv1.AddToScheme(s)
	if err != nil {
		t.Fatalf("Failed to add scheme: %v", err)
	}

	mcpServer := &mcpv1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "default",
		},
		Spec: mcpv1.MCPServerSpec{
			Image:    "test-image:latest",
			Protocol: "http",
			Port:     8080,
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(mcpServer).Build()

	reconciler := &MCPServerReconciler{
		Client:            fakeClient,
		Scheme:            s,
		ServiceDiscovery:  &discovery.K8sServiceDiscovery{},
		ConnectionManager: &discovery.DynamicConnectionManager{},
		Config:            &config.ComposeConfig{Version: "1"},
	}

	// Create service using internal method
	service, err := reconciler.createService(mcpServer)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Verify service structure
	if service.Name != mcpServer.Name {
		t.Errorf("Expected service name %s, got %s", mcpServer.Name, service.Name)
	}

	if service.Namespace != mcpServer.Namespace {
		t.Errorf("Expected service namespace %s, got %s", mcpServer.Namespace, service.Namespace)
	}

	if len(service.Spec.Ports) == 0 {
		t.Error("Expected service to have ports")
	} else {
		port := service.Spec.Ports[0]
		if port.Port != mcpServer.Spec.Port {
			t.Errorf("Expected port %d, got %d", mcpServer.Spec.Port, port.Port)
		}
		if port.TargetPort != intstr.FromInt(int(mcpServer.Spec.Port)) {
			t.Errorf("Expected target port %d, got %v", mcpServer.Spec.Port, port.TargetPort)
		}
	}

	// Verify selector
	expectedSelector := map[string]string{
		"app": mcpServer.Name,
	}
	for k, v := range expectedSelector {
		if service.Spec.Selector[k] != v {
			t.Errorf("Expected selector %s=%s, got %s=%s", k, v, k, service.Spec.Selector[k])
		}
	}
}

func TestMCPServerReconciler_UpdateStatus(t *testing.T) {
	// Setup test scheme
	s := scheme.Scheme
	err := mcpv1.AddToScheme(s)
	if err != nil {
		t.Fatalf("Failed to add scheme: %v", err)
	}

	mcpServer := &mcpv1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "default",
		},
		Spec: mcpv1.MCPServerSpec{
			Image:    "test-image:latest",
			Protocol: "http",
			Port:     8080,
		},
	}

	// Create deployment with ready status
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-server",
			Namespace: "default",
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 1,
			Replicas:      1,
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(mcpServer, deployment).Build()

	reconciler := &MCPServerReconciler{
		Client:            fakeClient,
		Scheme:            s,
		ServiceDiscovery:  &discovery.K8sServiceDiscovery{},
		ConnectionManager: &discovery.DynamicConnectionManager{},
		Config:            &config.ComposeConfig{Version: "1"},
	}

	// Run reconcile to update status
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-server",
			Namespace: "default",
		},
	}

	_, err = reconciler.Reconcile(context.TODO(), req)
	if err != nil {
		t.Errorf("Expected no error during reconcile but got: %v", err)
	}

	// Verify status was updated
	updatedServer := &mcpv1.MCPServer{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "test-server", Namespace: "default"}, updatedServer)
	if err != nil {
		t.Fatalf("Failed to get updated server: %v", err)
	}

	if updatedServer.Status.Phase != "Ready" {
		t.Errorf("Expected phase 'Ready', got %s", updatedServer.Status.Phase)
	}

	if updatedServer.Status.ReadyReplicas != 1 {
		t.Errorf("Expected ReadyReplicas 1, got %d", updatedServer.Status.ReadyReplicas)
	}
}

// Helper function to create a basic MCPServer for testing
func createTestMCPServer(name, namespace string) *mcpv1.MCPServer {
	return &mcpv1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: mcpv1.MCPServerSpec{
			Image:    "test-image:latest",
			Protocol: "http",
			Port:     8080,
		},
	}
}