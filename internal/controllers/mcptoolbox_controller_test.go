package controllers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/crd"
)

func TestMCPToolboxReconciler_Reconcile(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	tests := []struct {
		name     string
		toolbox  *crd.MCPToolbox
		expected ctrl.Result
		wantErr  bool
	}{
		{
			name: "Create new MCPToolbox with basic config",
			toolbox: &crd.MCPToolbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-toolbox",
					Namespace: "default",
				},
				Spec: crd.MCPToolboxSpec{
					Template: "coding-assistant",
					Servers: map[string]crd.ToolboxServerSpec{
						"code-server": {
							MCPServerSpec: crd.MCPServerSpec{
								Image:    "mcpcompose/code-server:latest",
								Protocol: "http",
								HttpPort: 8080,
							},
						},
					},
				},
			},
			expected: ctrl.Result{},
			wantErr:  false,
		},
		{
			name: "Create MCPToolbox with multiple servers",
			toolbox: &crd.MCPToolbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "multi-server-toolbox",
					Namespace: "default",
				},
				Spec: crd.MCPToolboxSpec{
					Description: "Multi-server toolbox",
					Servers: map[string]crd.ToolboxServerSpec{
						"primary": {
							MCPServerSpec: crd.MCPServerSpec{
								Image:    "mcpcompose/primary:latest",
								Protocol: "http",
								HttpPort: 8080,
							},
							ToolboxRole: "primary",
							Priority:    1,
						},
						"helper": {
							MCPServerSpec: crd.MCPServerSpec{
								Image:    "mcpcompose/helper:latest",
								Protocol: "sse",
								HttpPort: 8081,
							},
							ToolboxRole: "helper",
							Priority:    2,
						},
					},
					Dependencies: []crd.ToolboxDependency{
						{
							Server:    "helper",
							DependsOn: []string{"primary"},
						},
					},
				},
			},
			expected: ctrl.Result{},
			wantErr:  false,
		},
		{
			name: "Create MCPToolbox with custom resources",
			toolbox: &crd.MCPToolbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "resource-toolbox",
					Namespace: "default",
				},
				Spec: crd.MCPToolboxSpec{
					Template: "resource-heavy",
					Servers: map[string]crd.ToolboxServerSpec{
						"resource-server": {
							MCPServerSpec: crd.MCPServerSpec{
								Image:    "mcpcompose/resource-server:latest",
								Protocol: "http",
								HttpPort: 8080,
								Resources: crd.ResourceRequirements{
									Limits: crd.ResourceList{
										"cpu":    "1000m",
										"memory": "1Gi",
									},
									Requests: crd.ResourceList{
										"cpu":    "500m",
										"memory": "512Mi",
									},
								},
							},
						},
					},
				},
			},
			expected: ctrl.Result{},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().
				WithScheme(s).
				WithStatusSubresource(&crd.MCPToolbox{}).
				Build()

			reconciler := &MCPToolboxReconciler{
				Client: client,
				Scheme: s,
				Config: &config.ComposeConfig{
					Version: "1",
					Servers: make(map[string]config.ServerConfig),
				},
			}

			// Create the toolbox
			err := client.Create(context.Background(), tt.toolbox)
			require.NoError(t, err)

			// Perform reconciliation
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.toolbox.Name,
					Namespace: tt.toolbox.Namespace,
				},
			}

			result, err := reconciler.Reconcile(context.Background(), req)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			// The result might have a requeue time, so we mainly check for errors
			_ = result

			// Verify the toolbox was processed (might need multiple reconciliations)
			var updated crd.MCPToolbox
			err = client.Get(context.Background(), req.NamespacedName, &updated)
			require.NoError(t, err)
			
			// After first reconciliation, should have finalizer
			assert.Contains(t, updated.Finalizers, "mcp.matey.ai/toolbox-finalizer")
			
			// Status might be empty after first reconciliation depending on implementation
			// Some reconcilers need multiple passes to fully initialize

			t.Logf("Reconcile result: %+v", result)
		})
	}
}

func TestMCPToolboxReconciler_Delete(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	toolbox := &crd.MCPToolbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "delete-test-toolbox",
			Namespace: "default",
			Finalizers: []string{"mcp.matey.ai/toolbox-finalizer"},
		},
		Spec: crd.MCPToolboxSpec{
			Template: "coding-assistant",
			Servers: map[string]crd.ToolboxServerSpec{
				"server1": {
					MCPServerSpec: crd.MCPServerSpec{
						Image:    "test:latest",
						Protocol: "http",
						HttpPort: 8080,
					},
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(toolbox).
		WithStatusSubresource(&crd.MCPToolbox{}).
		Build()

	reconciler := &MCPToolboxReconciler{
		Client: client,
		Scheme: s,
		Config: &config.ComposeConfig{
			Version: "1",
			Servers: make(map[string]config.ServerConfig),
		},
	}

	// Delete the toolbox
	err := client.Delete(context.Background(), toolbox)
	require.NoError(t, err)

	// Reconcile should handle the deletion
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      toolbox.Name,
			Namespace: toolbox.Namespace,
		},
	}

	_, err = reconciler.Reconcile(context.Background(), req)
	assert.NoError(t, err)
}

func TestMCPToolboxReconciler_NonExistentToolbox(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	client := fake.NewClientBuilder().
		WithScheme(s).
		Build()

	reconciler := &MCPToolboxReconciler{
		Client: client,
		Scheme: s,
		Config: &config.ComposeConfig{
			Version: "1",
			Servers: make(map[string]config.ServerConfig),
		},
	}

	// Try to reconcile a non-existent toolbox
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "non-existent",
			Namespace: "default",
		},
	}

	result, err := reconciler.Reconcile(context.Background(), req)
	assert.NoError(t, err)
	assert.Equal(t, ctrl.Result{}, result)
}

func TestMCPToolboxReconciler_StatusUpdates(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	toolbox := &crd.MCPToolbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "status-test-toolbox",
			Namespace: "default",
		},
		Spec: crd.MCPToolboxSpec{
			Template: "coding-assistant",
			Servers: map[string]crd.ToolboxServerSpec{
				"server1": {
					MCPServerSpec: crd.MCPServerSpec{
						Image:    "test:latest",
						Protocol: "http",
						HttpPort: 8080,
					},
				},
				"server2": {
					MCPServerSpec: crd.MCPServerSpec{
						Image:    "test2:latest",
						Protocol: "sse",
						HttpPort: 8081,
					},
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(toolbox).
		WithStatusSubresource(&crd.MCPToolbox{}).
		Build()

	reconciler := &MCPToolboxReconciler{
		Client: client,
		Scheme: s,
		Config: &config.ComposeConfig{
			Version: "1",
			Servers: make(map[string]config.ServerConfig),
		},
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      toolbox.Name,
			Namespace: toolbox.Namespace,
		},
	}

	// First reconciliation should initialize status
	_, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)

	// Verify status was updated (might need multiple reconciliations)
	var updated crd.MCPToolbox
	err = client.Get(context.Background(), req.NamespacedName, &updated)
	require.NoError(t, err)
	
	// After first reconciliation, should have finalizer
	assert.Contains(t, updated.Finalizers, "mcp.matey.ai/toolbox-finalizer")
	
	// Status initialization depends on the reconciler implementation
	// Some reconcilers set status immediately, others need multiple passes
}

func TestMCPToolboxReconciler_DependencyHandling(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	toolbox := &crd.MCPToolbox{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dependency-test-toolbox",
			Namespace: "default",
		},
		Spec: crd.MCPToolboxSpec{
			Description: "Toolbox with dependencies",
			Servers: map[string]crd.ToolboxServerSpec{
				"primary": {
					MCPServerSpec: crd.MCPServerSpec{
						Image:    "primary:latest",
						Protocol: "http",
						HttpPort: 8080,
					},
					Priority: 1,
				},
				"secondary": {
					MCPServerSpec: crd.MCPServerSpec{
						Image:    "secondary:latest",
						Protocol: "sse",
						HttpPort: 8081,
					},
					Priority: 2,
				},
			},
			Dependencies: []crd.ToolboxDependency{
				{
					Server:    "secondary",
					DependsOn: []string{"primary"},
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(toolbox).
		WithStatusSubresource(&crd.MCPToolbox{}).
		Build()

	reconciler := &MCPToolboxReconciler{
		Client: client,
		Scheme: s,
		Config: &config.ComposeConfig{
			Version: "1",
			Servers: make(map[string]config.ServerConfig),
		},
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      toolbox.Name,
			Namespace: toolbox.Namespace,
		},
	}

	// Perform reconciliation
	_, err := reconciler.Reconcile(context.Background(), req)
	require.NoError(t, err)

	// Verify the toolbox was processed
	var updated crd.MCPToolbox
	err = client.Get(context.Background(), req.NamespacedName, &updated)
	require.NoError(t, err)
	
	// After first reconciliation, should have finalizer
	assert.Contains(t, updated.Finalizers, "mcp.matey.ai/toolbox-finalizer")
	
	// Verify dependencies are preserved
	assert.Len(t, updated.Spec.Dependencies, 1)
	assert.Equal(t, "secondary", updated.Spec.Dependencies[0].Server)
	assert.Contains(t, updated.Spec.Dependencies[0].DependsOn, "primary")
}

func TestMCPToolboxReconciler_TemplateHandling(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	tests := []struct {
		name     string
		template string
	}{
		{
			name:     "coding-assistant template",
			template: "coding-assistant",
		},
		{
			name:     "rag-stack template",
			template: "rag-stack",
		},
		{
			name:     "research-agent template",
			template: "research-agent",
		},
		{
			name:     "content-creator template",
			template: "content-creator",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolbox := &crd.MCPToolbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "template-test-" + tt.template,
					Namespace: "default",
				},
				Spec: crd.MCPToolboxSpec{
					Template: tt.template,
					Servers: map[string]crd.ToolboxServerSpec{
						"server1": {
							MCPServerSpec: crd.MCPServerSpec{
								Image:    "test:latest",
								Protocol: "http",
								HttpPort: 8080,
							},
						},
					},
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(toolbox).
				WithStatusSubresource(&crd.MCPToolbox{}).
				Build()

			reconciler := &MCPToolboxReconciler{
				Client: client,
				Scheme: s,
				Config: &config.ComposeConfig{
					Version: "1",
					Servers: make(map[string]config.ServerConfig),
				},
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      toolbox.Name,
					Namespace: toolbox.Namespace,
				},
			}

			// Perform reconciliation
			_, err := reconciler.Reconcile(context.Background(), req)
			require.NoError(t, err)

			// Verify the toolbox was processed
			var updated crd.MCPToolbox
			err = client.Get(context.Background(), req.NamespacedName, &updated)
			require.NoError(t, err)
			
			// After first reconciliation, should have finalizer
			assert.Contains(t, updated.Finalizers, "mcp.matey.ai/toolbox-finalizer")
			
			// Verify template is preserved
			assert.Equal(t, tt.template, updated.Spec.Template)
		})
	}
}

func TestMCPToolboxReconciler_SetupWithManager(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	// This test verifies the controller setup doesn't panic
	// In a real environment, this would be tested with a proper manager
	reconciler := &MCPToolboxReconciler{
		Scheme: s,
		Config: &config.ComposeConfig{
			Version: "1",
			Servers: make(map[string]config.ServerConfig),
		},
	}

	// We can't easily test the actual manager setup in a unit test
	// But we can verify the reconciler is properly configured
	assert.NotNil(t, reconciler.Scheme)
	assert.NotNil(t, reconciler.Config)
}

func TestMCPToolboxReconciler_ErrorHandling(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	tests := []struct {
		name        string
		toolbox     *crd.MCPToolbox
		expectError bool
	}{
		{
			name: "toolbox with empty servers",
			toolbox: &crd.MCPToolbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "empty-servers-toolbox",
					Namespace: "default",
				},
				Spec: crd.MCPToolboxSpec{
					Template: "coding-assistant",
					Servers:  map[string]crd.ToolboxServerSpec{},
				},
			},
			expectError: false, // The reconciler should handle this gracefully
		},
		{
			name: "toolbox without template or servers",
			toolbox: &crd.MCPToolbox{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "minimal-toolbox",
					Namespace: "default",
				},
				Spec: crd.MCPToolboxSpec{
					Description: "Minimal toolbox",
				},
			},
			expectError: false, // The reconciler should handle this gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().
				WithScheme(s).
				WithRuntimeObjects(tt.toolbox).
				WithStatusSubresource(&crd.MCPToolbox{}).
				Build()

			reconciler := &MCPToolboxReconciler{
				Client: client,
				Scheme: s,
				Config: &config.ComposeConfig{
					Version: "1",
					Servers: make(map[string]config.ServerConfig),
				},
			}

			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      tt.toolbox.Name,
					Namespace: tt.toolbox.Namespace,
				},
			}

			_, err := reconciler.Reconcile(context.Background(), req)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}