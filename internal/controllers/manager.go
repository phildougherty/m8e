// internal/controllers/manager.go
package controllers

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/logging"
)

// ControllerManager manages all MCP controllers
type ControllerManager struct {
	manager ctrl.Manager
	logger  *logging.Logger
	cancel  context.CancelFunc
	config  *config.ComposeConfig
}

// NewControllerManager creates a new controller manager
func NewControllerManager(namespace string, cfg *config.ComposeConfig) (*ControllerManager, error) {
	logger := logging.NewLogger("info")
	
	// Set up controller-runtime logging
	ctrl.SetLogger(logger.GetLogr())
	
	// Setup Kubernetes config
	config, err := createK8sConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes config: %w", err)
	}

	// Create scheme with our CRDs and core Kubernetes types
	scheme := runtime.NewScheme()
	
	// Add core Kubernetes types
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add core v1 scheme: %w", err)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add apps v1 scheme: %w", err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add batch v1 scheme: %w", err)
	}
	
	// Add our CRDs
	if err := crd.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add CRD scheme: %w", err)
	}

	// Setup controller manager options
	opts := ctrl.Options{
		Scheme:                 scheme,
		Cache:                  cache.Options{DefaultNamespaces: map[string]cache.Config{namespace: {}}},
		Metrics:                server.Options{BindAddress: ":8083"},
		WebhookServer:          webhook.NewServer(webhook.Options{Port: 9443}),
		HealthProbeBindAddress: ":8082",
		LeaderElection:         false, // Disable for now, can enable later for HA
		LeaderElectionID:       "matey-controller-leader",
	}

	// Create the manager
	mgr, err := ctrl.NewManager(config, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create controller manager: %w", err)
	}

	// Setup health checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return nil, fmt.Errorf("failed to set up health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return nil, fmt.Errorf("failed to set up ready check: %w", err)
	}

	// Register all our controllers
	if err := setupControllers(mgr, logger, cfg); err != nil {
		return nil, fmt.Errorf("failed to setup controllers: %w", err)
	}

	return &ControllerManager{
		manager: mgr,
		logger:  logger,
		config:  cfg,
	}, nil
}

// Start starts the controller manager
func (cm *ControllerManager) Start(ctx context.Context) error {
	cm.logger.Info("Starting Matey controller manager")

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	cm.cancel = cancel

	// Start the manager
	if err := cm.manager.Start(ctx); err != nil {
		return fmt.Errorf("failed to start controller manager: %w", err)
	}

	return nil
}

// Stop stops the controller manager
func (cm *ControllerManager) Stop() error {
	if cm.cancel != nil {
		cm.logger.Info("Stopping Matey controller manager")
		cm.cancel()
	}
	return nil
}

// IsReady checks if the controller manager is ready
func (cm *ControllerManager) IsReady() bool {
	// Simple check - in production you might want more sophisticated health checking
	return cm.manager != nil
}

// setupControllers registers all our controllers with the manager
func setupControllers(mgr ctrl.Manager, logger *logging.Logger, cfg *config.ComposeConfig) error {
	// Setup MCPServer controller
	if err := (&MCPServerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Config: cfg,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("failed to setup MCPServer controller: %w", err)
	}

	// Setup MCPMemory controller
	if err := (&MCPMemoryReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Config: cfg,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("failed to setup MCPMemory controller: %w", err)
	}

	// Setup MCPTaskScheduler controller
	if err := (&MCPTaskSchedulerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Config: cfg,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("failed to setup MCPTaskScheduler controller: %w", err)
	}

	return nil
}

// createK8sConfig creates a Kubernetes configuration
func createK8sConfig() (*rest.Config, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
		}
	}
	return config, nil
}

// StartControllerManagerInBackground starts the controller manager in a separate goroutine
func StartControllerManagerInBackground(namespace string, cfg *config.ComposeConfig) (*ControllerManager, error) {
	cm, err := NewControllerManager(namespace, cfg)
	if err != nil {
		return nil, err
	}

	// Start in background
	go func() {
		ctx := ctrl.SetupSignalHandler()
		
		if err := cm.Start(ctx); err != nil {
			cm.logger.Error(fmt.Sprintf("Controller manager failed: %v", err))
		}
	}()

	// Give it a moment to start
	time.Sleep(2 * time.Second)

	return cm, nil
}