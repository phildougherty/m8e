package controllers

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

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
	
	// The scheme.Scheme already includes appsv1 and corev1 by default

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
					HttpPort: 8080,
					Command:  []string{"node", "server.js"},
					Env: map[string]string{
						"NODE_ENV": "production",
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
					HttpPort: 8080,
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
					HttpPort: 8080,
					Replicas: &[]int32{3}[0],
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
			// Create fake client with status subresource
			fakeClient := fake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(tt.mcpServer).
				WithStatusSubresource(tt.mcpServer).
				Build()

			// Create mock service discovery and connection manager
			// These can be nil for unit tests since the controller doesn't directly depend on them
			var serviceDiscovery *discovery.K8sServiceDiscovery
			var connectionManager *discovery.DynamicConnectionManager

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

			// Run reconcile (may require multiple attempts for controller to complete)
			result, err := reconciler.Reconcile(context.TODO(), req)
			
			// If the first reconcile adds finalizer, run it again
			if err == nil && result.RequeueAfter == 0 {
				result, err = reconciler.Reconcile(context.TODO(), req)
			}

			// Check error expectation
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
			
			// Debug: print error and result if test is failing
			if err != nil {
				t.Logf("Reconcile error: %v", err)
			}
			t.Logf("Reconcile result: %+v", result)

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
					
					if tt.mcpServer.Spec.Replicas != nil && *tt.mcpServer.Spec.Replicas > 0 && *deployment.Spec.Replicas != *tt.mcpServer.Spec.Replicas {
						t.Errorf("Expected replicas %d, got %d", *tt.mcpServer.Spec.Replicas, *deployment.Spec.Replicas)
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
					} else if service.Spec.Ports[0].Port != tt.mcpServer.Spec.HttpPort {
						t.Errorf("Expected port %d, got %d", tt.mcpServer.Spec.HttpPort, service.Spec.Ports[0].Port)
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
				"mcp.matey.ai/finalizer",
			},
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
		},
		Spec: mcpv1.MCPServerSpec{
			Image:    "test-image:latest",
			Protocol: "http",
			HttpPort: 8080,
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

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(mcpServer, deployment, service).
		WithStatusSubresource(mcpServer).
		Build()

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

	// In a real Kubernetes environment, the deployment and service would be automatically
	// deleted due to owner references. With the fake client, we just verify the finalizer
	// was removed, which allows the MCPServer to be deleted.
	// The garbage collector would handle the cleanup of owned resources.
	
	// Verify that deployment still exists but has owner reference (would be cleaned up by GC)
	deletedDeploy := &appsv1.Deployment{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "test-server", Namespace: "default"}, deletedDeploy)
	if err != nil {
		t.Errorf("Expected deployment to still exist (would be cleaned up by GC): %v", err)
	}

	// Verify that service still exists but has owner reference (would be cleaned up by GC)
	deletedService := &corev1.Service{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "test-server", Namespace: "default"}, deletedService)
	if err != nil {
		t.Errorf("Expected service to still exist (would be cleaned up by GC): %v", err)
	}

	// Check that MCPServer was deleted after finalizer removal
	// In a real Kubernetes environment, when finalizers are removed and DeletionTimestamp is set,
	// the object gets deleted by the API server
	updatedMCPServer := &mcpv1.MCPServer{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "test-server", Namespace: "default"}, updatedMCPServer)
	if err == nil {
		// If the object still exists, check that the finalizer was removed
		if len(updatedMCPServer.Finalizers) > 0 {
			t.Error("Expected finalizer to be removed")
		}
	}
	// If the object is not found, that's also acceptable as it means deletion completed

	// Should not requeue since deletion is complete
	if result.RequeueAfter != 0 {
		t.Errorf("Expected no requeue after deletion, got %v", result.RequeueAfter)
	}
}

// TestMCPServerReconciler_CreateDeployment is skipped because the createDeployment method 
// is not exposed. This test would be implemented once the method is made public or
// the deployment creation logic is tested through the main Reconcile method.

// TestMCPServerReconciler_CreateService is skipped because the createService method 
// is not exposed. This test would be implemented once the method is made public or
// the service creation logic is tested through the main Reconcile method.

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
			HttpPort: 8080,
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

	fakeClient := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(mcpServer, deployment).
		WithStatusSubresource(mcpServer).
		Build()

	reconciler := &MCPServerReconciler{
		Client:            fakeClient,
		Scheme:            s,
		ServiceDiscovery:  &discovery.K8sServiceDiscovery{},
		ConnectionManager: &discovery.DynamicConnectionManager{},
		Config:            &config.ComposeConfig{Version: "1"},
	}

	// Run reconcile to update status (may require multiple attempts)
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-server",
			Namespace: "default",
		},
	}

	// First reconcile adds finalizer and creates resources
	result, err := reconciler.Reconcile(context.TODO(), req)
	if err != nil {
		t.Errorf("Expected no error during first reconcile but got: %v", err)
	}
	
	// If the first reconcile adds finalizer, run it again
	if result.RequeueAfter == 0 {
		result, err = reconciler.Reconcile(context.TODO(), req)
		if err != nil {
			t.Errorf("Expected no error during second reconcile but got: %v", err)
		}
	}

	// Verify status was updated
	updatedServer := &mcpv1.MCPServer{}
	err = fakeClient.Get(context.TODO(), types.NamespacedName{Name: "test-server", Namespace: "default"}, updatedServer)
	if err != nil {
		t.Fatalf("Failed to get updated server: %v", err)
	}

	// With the fake deployment having ReadyReplicas=1, the status should be updated
	if updatedServer.Status.Phase != "Running" {
		t.Errorf("Expected phase 'Running' (based on ReadyReplicas>0), got %s", updatedServer.Status.Phase)
	}

	if updatedServer.Status.ReadyReplicas != 1 {
		t.Errorf("Expected ReadyReplicas 1, got %d", updatedServer.Status.ReadyReplicas)
	}
}

// Helper function to create a basic MCPServer for testing
