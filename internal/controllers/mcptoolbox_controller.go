// internal/controllers/mcptoolbox_controller.go
package controllers

import (
	"context"
	"fmt"
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/phildougherty/m8e/internal/config"
	mcpv1 "github.com/phildougherty/m8e/internal/crd"
)

// MCPToolboxReconciler reconciles MCPToolbox objects
type MCPToolboxReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Config *config.ComposeConfig
}

// +kubebuilder:rbac:groups=mcp.matey.ai,resources=mcptoolboxes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mcp.matey.ai,resources=mcptoolboxes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mcp.matey.ai,resources=mcptoolboxes/finalizers,verbs=update
// +kubebuilder:rbac:groups=mcp.matey.ai,resources=mcpservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles MCPToolbox resource reconciliation
func (r *MCPToolboxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the MCPToolbox instance
	var toolbox mcpv1.MCPToolbox
	if err := r.Get(ctx, req.NamespacedName, &toolbox); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("MCPToolbox resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get MCPToolbox")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if toolbox.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, &toolbox)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&toolbox, "mcp.matey.ai/toolbox-finalizer") {
		controllerutil.AddFinalizer(&toolbox, "mcp.matey.ai/toolbox-finalizer")
		return ctrl.Result{}, r.Update(ctx, &toolbox)
	}

	// Initialize status if needed
	if toolbox.Status.Phase == "" {
		toolbox.Status.Phase = mcpv1.MCPToolboxPhasePending
		toolbox.Status.ServerStatuses = make(map[string]mcpv1.ToolboxServerStatus)
		if err := r.Status().Update(ctx, &toolbox); err != nil {
			logger.Error(err, "Failed to initialize MCPToolbox status")
			return ctrl.Result{}, err
		}
	}

	// Apply template if specified
	if err := r.applyTemplate(ctx, &toolbox); err != nil {
		logger.Error(err, "Failed to apply toolbox template")
		return r.updateStatusWithError(ctx, &toolbox, err)
	}

	// Reconcile servers in dependency order
	if err := r.reconcileServersInOrder(ctx, &toolbox); err != nil {
		logger.Error(err, "Failed to reconcile servers")
		return r.updateStatusWithError(ctx, &toolbox, err)
	}

	// Reconcile toolbox-level resources
	if err := r.reconcileToolboxResources(ctx, &toolbox); err != nil {
		logger.Error(err, "Failed to reconcile toolbox resources")
		return r.updateStatusWithError(ctx, &toolbox, err)
	}

	// Update overall status
	if err := r.updateToolboxStatus(ctx, &toolbox); err != nil {
		logger.Error(err, "Failed to update toolbox status")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled MCPToolbox", "name", toolbox.Name, "phase", toolbox.Status.Phase)
	return ctrl.Result{RequeueAfter: time.Minute * 2}, nil
}

// handleDeletion handles MCPToolbox deletion
func (r *MCPToolboxReconciler) handleDeletion(ctx context.Context, toolbox *mcpv1.MCPToolbox) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling MCPToolbox deletion", "name", toolbox.Name)

	// Update status to Terminating
	if toolbox.Status.Phase != mcpv1.MCPToolboxPhaseTerminating {
		toolbox.Status.Phase = mcpv1.MCPToolboxPhaseTerminating
		if err := r.Status().Update(ctx, toolbox); err != nil {
			logger.Error(err, "Failed to update status to Terminating")
		}
	}

	// Clean up owned MCPServer resources
	serverList := &mcpv1.MCPServerList{}
	listOpts := []client.ListOption{
		client.InNamespace(toolbox.Namespace),
		client.MatchingLabels{
			"mcp.matey.ai/toolbox": toolbox.Name,
		},
	}
	
	if err := r.List(ctx, serverList, listOpts...); err != nil {
		logger.Error(err, "Failed to list MCPServers for cleanup")
		return ctrl.Result{}, err
	}

	// Delete all servers owned by this toolbox
	for _, server := range serverList.Items {
		if err := r.Delete(ctx, &server); err != nil && !errors.IsNotFound(err) {
			logger.Error(err, "Failed to delete MCPServer", "server", server.Name)
			return ctrl.Result{}, err
		}
	}

	// Wait for all servers to be deleted before removing finalizer
	if len(serverList.Items) > 0 {
		logger.Info("Waiting for MCPServers to be deleted", "count", len(serverList.Items))
		return ctrl.Result{RequeueAfter: time.Second * 10}, nil
	}

	// Remove finalizer to allow deletion
	controllerutil.RemoveFinalizer(toolbox, "mcp.matey.ai/toolbox-finalizer")
	if err := r.Update(ctx, toolbox); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// applyTemplate applies a pre-built template to the toolbox
func (r *MCPToolboxReconciler) applyTemplate(ctx context.Context, toolbox *mcpv1.MCPToolbox) error {
	if toolbox.Spec.Template == "" {
		return nil // No template specified
	}

	// Check if template has already been applied
	if toolbox.Annotations != nil && toolbox.Annotations["mcp.matey.ai/template-applied"] == toolbox.Spec.Template {
		return nil // Template already applied
	}

	logger := log.FromContext(ctx)
	logger.Info("Applying toolbox template", "template", toolbox.Spec.Template)

	template, err := r.getToolboxTemplate(toolbox.Spec.Template)
	if err != nil {
		return fmt.Errorf("failed to get template %s: %w", toolbox.Spec.Template, err)
	}

	// Apply template to toolbox spec
	if err := r.mergeTemplate(toolbox, template); err != nil {
		return fmt.Errorf("failed to merge template: %w", err)
	}

	// Mark template as applied
	if toolbox.Annotations == nil {
		toolbox.Annotations = make(map[string]string)
	}
	toolbox.Annotations["mcp.matey.ai/template-applied"] = toolbox.Spec.Template

	// Update the toolbox with template applied
	if err := r.Update(ctx, toolbox); err != nil {
		return fmt.Errorf("failed to update toolbox with template: %w", err)
	}

	return nil
}

// reconcileServersInOrder reconciles servers based on dependencies and priority
func (r *MCPToolboxReconciler) reconcileServersInOrder(ctx context.Context, toolbox *mcpv1.MCPToolbox) error {
	logger := log.FromContext(ctx)

	// Get dependency-ordered list of servers
	orderedServers, err := r.getServerOrderWithDependencies(toolbox)
	if err != nil {
		return fmt.Errorf("failed to determine server order: %w", err)
	}

	logger.Info("Reconciling servers in order", "servers", len(orderedServers))

	// Reconcile each server in order
	for _, serverName := range orderedServers {
		serverSpec, exists := toolbox.Spec.Servers[serverName]
		if !exists {
			continue
		}

		// Skip disabled servers
		if serverSpec.Enabled != nil && !*serverSpec.Enabled {
			continue
		}

		// Check if dependencies are satisfied
		if !r.areServerDependenciesSatisfied(ctx, toolbox, serverName) {
			logger.Info("Server dependencies not satisfied, skipping", "server", serverName)
			continue
		}

		// Reconcile the individual server
		if err := r.reconcileServer(ctx, toolbox, serverName, serverSpec); err != nil {
			logger.Error(err, "Failed to reconcile server", "server", serverName)
			return err
		}
	}

	return nil
}

// reconcileServer reconciles an individual MCPServer within the toolbox
func (r *MCPToolboxReconciler) reconcileServer(ctx context.Context, toolbox *mcpv1.MCPToolbox, serverName string, serverSpec mcpv1.ToolboxServerSpec) error {
	logger := log.FromContext(ctx)

	// Create MCPServer resource
	mcpServer := &mcpv1.MCPServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", toolbox.Name, serverName),
			Namespace: toolbox.Namespace,
			Labels: map[string]string{
				"mcp.matey.ai/toolbox":     toolbox.Name,
				"mcp.matey.ai/server-name": serverName,
				"mcp.matey.ai/role":        serverSpec.ToolboxRole,
			},
		},
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(toolbox, mcpServer, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, mcpServer, func() error {
		// Apply server template if specified
		if serverSpec.ServerTemplate != "" {
			if err := r.applyServerTemplate(mcpServer, serverSpec.ServerTemplate); err != nil {
				return fmt.Errorf("failed to apply server template: %w", err)
			}
		}

		// Merge the embedded MCPServerSpec
		mcpServer.Spec = serverSpec.MCPServerSpec

		// Apply toolbox-level defaults
		r.applyToolboxDefaults(toolbox, &mcpServer.Spec)

		// Set toolbox-specific labels
		if mcpServer.Spec.Labels == nil {
			mcpServer.Spec.Labels = make(map[string]string)
		}
		mcpServer.Spec.Labels["mcp.matey.ai/toolbox"] = toolbox.Name
		mcpServer.Spec.Labels["mcp.matey.ai/server-name"] = serverName

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create or update MCPServer %s: %w", serverName, err)
	}

	logger.Info("MCPServer reconciled", "operation", op, "server", serverName)
	return nil
}

// reconcileToolboxResources reconciles toolbox-level resources like networking, monitoring, etc.
func (r *MCPToolboxReconciler) reconcileToolboxResources(ctx context.Context, toolbox *mcpv1.MCPToolbox) error {
	// Reconcile network policies
	if err := r.reconcileNetworkPolicies(ctx, toolbox); err != nil {
		return fmt.Errorf("failed to reconcile network policies: %w", err)
	}

	// Reconcile ingress
	if err := r.reconcileIngress(ctx, toolbox); err != nil {
		return fmt.Errorf("failed to reconcile ingress: %w", err)
	}

	// Reconcile shared volumes
	if err := r.reconcileSharedVolumes(ctx, toolbox); err != nil {
		return fmt.Errorf("failed to reconcile shared volumes: %w", err)
	}

	return nil
}

// reconcileNetworkPolicies creates network policies based on toolbox security settings
func (r *MCPToolboxReconciler) reconcileNetworkPolicies(ctx context.Context, toolbox *mcpv1.MCPToolbox) error {
	if toolbox.Spec.Security == nil || toolbox.Spec.Security.NetworkPolicy == nil || !toolbox.Spec.Security.NetworkPolicy.Enabled {
		return nil // Network policies not enabled
	}

	logger := log.FromContext(ctx)
	logger.Info("Reconciling network policies for toolbox", "toolbox", toolbox.Name)

	networkPolicy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-network-policy", toolbox.Name),
			Namespace: toolbox.Namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, networkPolicy, func() error {
		// Set owner reference
		if err := controllerutil.SetControllerReference(toolbox, networkPolicy, r.Scheme); err != nil {
			return err
		}

		// Build network policy spec based on toolbox configuration
		networkPolicy.Spec = r.buildNetworkPolicySpec(toolbox)
		return nil
	})

	if err != nil {
		return err
	}

	logger.Info("Network policy reconciled", "operation", op)
	return nil
}

// reconcileIngress creates an ingress for external access if enabled
func (r *MCPToolboxReconciler) reconcileIngress(ctx context.Context, toolbox *mcpv1.MCPToolbox) error {
	if toolbox.Spec.Networking == nil || !toolbox.Spec.Networking.IngressEnabled {
		return nil // Ingress not enabled
	}

	logger := log.FromContext(ctx)
	logger.Info("Reconciling ingress for toolbox", "toolbox", toolbox.Name)

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-ingress", toolbox.Name),
			Namespace: toolbox.Namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, ingress, func() error {
		// Set owner reference
		if err := controllerutil.SetControllerReference(toolbox, ingress, r.Scheme); err != nil {
			return err
		}

		// Build ingress spec
		ingress.Spec = r.buildIngressSpec(toolbox)
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to reconcile ingress: %w", err)
	}

	logger.Info("Ingress reconciled", "operation", op)
	return nil
}

// reconcileSharedVolumes creates shared persistent volumes for the toolbox
func (r *MCPToolboxReconciler) reconcileSharedVolumes(ctx context.Context, toolbox *mcpv1.MCPToolbox) error {
	if toolbox.Spec.Persistence == nil || len(toolbox.Spec.Persistence.SharedVolumes) == 0 {
		return nil // No shared volumes specified
	}

	logger := log.FromContext(ctx)
	logger.Info("Reconciling shared volumes for toolbox", "toolbox", toolbox.Name)

	// Create PersistentVolumeClaims for each shared volume
	for _, volumeSpec := range toolbox.Spec.Persistence.SharedVolumes {
		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%s", toolbox.Name, volumeSpec.Name),
				Namespace: toolbox.Namespace,
			},
		}

		op, err := controllerutil.CreateOrUpdate(ctx, r.Client, pvc, func() error {
			// Set owner reference
			if err := controllerutil.SetControllerReference(toolbox, pvc, r.Scheme); err != nil {
				return err
			}

			// Build PVC spec
			pvc.Spec = r.buildPVCSpec(toolbox, volumeSpec)
			return nil
		})

		if err != nil {
			return fmt.Errorf("failed to reconcile PVC %s: %w", volumeSpec.Name, err)
		}

		logger.Info("PVC reconciled", "operation", op, "pvc", volumeSpec.Name)
	}
	
	return nil
}

// updateToolboxStatus updates the overall status of the toolbox
func (r *MCPToolboxReconciler) updateToolboxStatus(ctx context.Context, toolbox *mcpv1.MCPToolbox) error {
	// Get all MCPServers owned by this toolbox
	serverList := &mcpv1.MCPServerList{}
	listOpts := []client.ListOption{
		client.InNamespace(toolbox.Namespace),
		client.MatchingLabels{
			"mcp.matey.ai/toolbox": toolbox.Name,
		},
	}
	
	if err := r.List(ctx, serverList, listOpts...); err != nil {
		return fmt.Errorf("failed to list MCPServers: %w", err)
	}

	// Count servers and their status
	totalServers := int32(len(serverList.Items))
	readyServers := int32(0)
	serverStatuses := make(map[string]mcpv1.ToolboxServerStatus)

	for _, server := range serverList.Items {
		serverName := server.Labels["mcp.matey.ai/server-name"]
		if serverName == "" {
			continue
		}

		status := mcpv1.ToolboxServerStatus{
			Phase:         server.Status.Phase,
			Ready:         server.Status.Phase == mcpv1.MCPServerPhaseRunning,
			Health:        server.Status.HealthStatus,
			Replicas:      server.Status.Replicas,
			ReadyReplicas: server.Status.ReadyReplicas,
		}

		if status.Ready {
			readyServers++
		}

		serverStatuses[serverName] = status
	}

	// Determine overall toolbox phase
	phase := r.determineToolboxPhase(toolbox, totalServers, readyServers)

	// Update status
	toolbox.Status.Phase = phase
	toolbox.Status.ServerCount = totalServers
	toolbox.Status.ReadyServers = readyServers
	toolbox.Status.ServerStatuses = serverStatuses
	toolbox.Status.LastReconcileTime = metav1.Now()
	toolbox.Status.ObservedGeneration = toolbox.Generation

	// Update conditions
	r.updateToolboxConditions(toolbox)

	return r.Status().Update(ctx, toolbox)
}

// determineToolboxPhase determines the overall phase based on server statuses
func (r *MCPToolboxReconciler) determineToolboxPhase(toolbox *mcpv1.MCPToolbox, totalServers, readyServers int32) mcpv1.MCPToolboxPhase {
	if totalServers == 0 {
		return mcpv1.MCPToolboxPhasePending
	}

	if readyServers == 0 {
		return mcpv1.MCPToolboxPhaseStarting
	}

	if readyServers == totalServers {
		return mcpv1.MCPToolboxPhaseRunning
	}

	// Some servers ready, some not
	return mcpv1.MCPToolboxPhaseDegraded
}

// updateToolboxConditions updates the toolbox conditions
func (r *MCPToolboxReconciler) updateToolboxConditions(toolbox *mcpv1.MCPToolbox) {
	now := metav1.Now()

	// Ready condition
	readyCondition := mcpv1.MCPToolboxCondition{
		Type:               mcpv1.MCPToolboxConditionReady,
		LastTransitionTime: now,
	}

	if toolbox.Status.Phase == mcpv1.MCPToolboxPhaseRunning {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "AllServersReady"
		readyCondition.Message = "All servers in the toolbox are ready"
	} else {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "ServersNotReady"
		readyCondition.Message = fmt.Sprintf("%d of %d servers are ready", toolbox.Status.ReadyServers, toolbox.Status.ServerCount)
	}

	// Update or add the condition
	toolbox.Status.Conditions = r.setCondition(toolbox.Status.Conditions, readyCondition)
}

// setCondition sets a condition in the conditions list
func (r *MCPToolboxReconciler) setCondition(conditions []mcpv1.MCPToolboxCondition, newCondition mcpv1.MCPToolboxCondition) []mcpv1.MCPToolboxCondition {
	for i, condition := range conditions {
		if condition.Type == newCondition.Type {
			if condition.Status != newCondition.Status {
				newCondition.LastTransitionTime = metav1.Now()
			} else {
				newCondition.LastTransitionTime = condition.LastTransitionTime
			}
			conditions[i] = newCondition
			return conditions
		}
	}
	return append(conditions, newCondition)
}

// updateStatusWithError updates the toolbox status with an error condition
func (r *MCPToolboxReconciler) updateStatusWithError(ctx context.Context, toolbox *mcpv1.MCPToolbox, err error) (ctrl.Result, error) {
	toolbox.Status.Phase = mcpv1.MCPToolboxPhaseFailed
	
	// Add error condition
	errorCondition := mcpv1.MCPToolboxCondition{
		Type:               mcpv1.MCPToolboxConditionReady,
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             "ReconcileError",
		Message:            err.Error(),
	}
	toolbox.Status.Conditions = r.setCondition(toolbox.Status.Conditions, errorCondition)

	if statusErr := r.Status().Update(ctx, toolbox); statusErr != nil {
		log.FromContext(ctx).Error(statusErr, "Failed to update error status")
	}

	return ctrl.Result{RequeueAfter: time.Minute}, err
}

// Helper functions for template and dependency management

// getToolboxTemplate returns a pre-built toolbox template
func (r *MCPToolboxReconciler) getToolboxTemplate(templateName string) (*mcpv1.MCPToolboxSpec, error) {
	templates := map[string]*mcpv1.MCPToolboxSpec{
		"coding-assistant": {
			Description: "Complete coding assistant with filesystem, git, web search, and memory",
			Servers: map[string]mcpv1.ToolboxServerSpec{
				"filesystem": {
					MCPServerSpec: mcpv1.MCPServerSpec{
						Image:        "mcp/filesystem:latest",
						Protocol:     "http",
						HttpPort:     3000,
						Capabilities: []string{"tools", "resources"},
					},
					ToolboxRole: "file-management",
					Priority:    1,
				},
				"git": {
					MCPServerSpec: mcpv1.MCPServerSpec{
						Image:        "mcp/git:latest",
						Protocol:     "http",
						HttpPort:     3001,
						Capabilities: []string{"tools"},
					},
					ToolboxRole: "version-control",
					Priority:    2,
				},
				"web-search": {
					MCPServerSpec: mcpv1.MCPServerSpec{
						Image:        "mcp/web-search:latest",
						Protocol:     "http",
						HttpPort:     3002,
						Capabilities: []string{"tools"},
					},
					ToolboxRole: "information-retrieval",
					Priority:    2,
				},
				"memory": {
					MCPServerSpec: mcpv1.MCPServerSpec{
						Image:        "mcp/memory:latest",
						Protocol:     "http",
						HttpPort:     3003,
						Capabilities: []string{"tools", "resources"},
					},
					ToolboxRole: "memory-management",
					Priority:    1,
				},
			},
			Dependencies: []mcpv1.ToolboxDependency{
				{
					Server:    "git",
					DependsOn: []string{"filesystem"},
				},
				{
					Server:    "web-search",
					DependsOn: []string{"memory"},
				},
			},
		},
		"rag-stack": {
			Description: "Retrieval Augmented Generation stack with memory, search, and document processing",
			Servers: map[string]mcpv1.ToolboxServerSpec{
				"memory": {
					MCPServerSpec: mcpv1.MCPServerSpec{
						Image:        "mcp/memory:latest",
						Protocol:     "http",
						HttpPort:     3000,
						Capabilities: []string{"tools", "resources"},
					},
					ToolboxRole: "vector-store",
					Priority:    1,
				},
				"web-search": {
					MCPServerSpec: mcpv1.MCPServerSpec{
						Image:        "mcp/web-search:latest",
						Protocol:     "http",
						HttpPort:     3001,
						Capabilities: []string{"tools"},
					},
					ToolboxRole: "search-engine",
					Priority:    2,
				},
				"document-processor": {
					MCPServerSpec: mcpv1.MCPServerSpec{
						Image:        "mcp/document-processor:latest",
						Protocol:     "http",
						HttpPort:     3002,
						Capabilities: []string{"tools"},
					},
					ToolboxRole: "document-processing",
					Priority:    2,
				},
			},
			Dependencies: []mcpv1.ToolboxDependency{
				{
					Server:    "web-search",
					DependsOn: []string{"memory"},
				},
				{
					Server:    "document-processor",
					DependsOn: []string{"memory"},
				},
			},
		},
	}

	template, exists := templates[templateName]
	if !exists {
		return nil, fmt.Errorf("unknown template: %s", templateName)
	}

	return template, nil
}

// mergeTemplate merges a template into the toolbox spec
func (r *MCPToolboxReconciler) mergeTemplate(toolbox *mcpv1.MCPToolbox, template *mcpv1.MCPToolboxSpec) error {
	// Only merge if the field is not already set
	if toolbox.Spec.Description == "" {
		toolbox.Spec.Description = template.Description
	}

	if toolbox.Spec.Servers == nil {
		toolbox.Spec.Servers = make(map[string]mcpv1.ToolboxServerSpec)
	}

	// Merge servers (only add if not already present)
	for name, serverSpec := range template.Servers {
		if _, exists := toolbox.Spec.Servers[name]; !exists {
			toolbox.Spec.Servers[name] = serverSpec
		}
	}

	// Merge dependencies (only add if not already present)
	if len(toolbox.Spec.Dependencies) == 0 {
		toolbox.Spec.Dependencies = template.Dependencies
	}

	return nil
}

// applyServerTemplate applies a server template to an MCPServer
func (r *MCPToolboxReconciler) applyServerTemplate(server *mcpv1.MCPServer, templateName string) error {
	templates := map[string]*mcpv1.MCPServerSpec{
		"filesystem-server": {
			Image:        "mcp/filesystem:latest",
			Protocol:     "http",
			HttpPort:     3000,
			Capabilities: []string{"tools", "resources"},
			Resources: mcpv1.ResourceRequirements{
				Limits: mcpv1.ResourceList{
					"cpu":    "500m",
					"memory": "512Mi",
				},
				Requests: mcpv1.ResourceList{
					"cpu":    "100m",
					"memory": "128Mi",
				},
			},
			Security: &mcpv1.SecurityConfig{
				ReadOnlyRootFS:     true,
				AllowPrivilegedOps: false,
			},
		},
		"git-server": {
			Image:        "mcp/git:latest",
			Protocol:     "http",
			HttpPort:     3001,
			Capabilities: []string{"tools"},
			Resources: mcpv1.ResourceRequirements{
				Limits: mcpv1.ResourceList{
					"cpu":    "200m",
					"memory": "256Mi",
				},
				Requests: mcpv1.ResourceList{
					"cpu":    "50m",
					"memory": "64Mi",
				},
			},
			Security: &mcpv1.SecurityConfig{
				ReadOnlyRootFS:     true,
				AllowPrivilegedOps: false,
			},
		},
		"web-search-server": {
			Image:        "mcp/web-search:latest",
			Protocol:     "http",
			HttpPort:     3002,
			Capabilities: []string{"tools"},
			Resources: mcpv1.ResourceRequirements{
				Limits: mcpv1.ResourceList{
					"cpu":    "300m",
					"memory": "384Mi",
				},
				Requests: mcpv1.ResourceList{
					"cpu":    "75m",
					"memory": "96Mi",
				},
			},
			Security: &mcpv1.SecurityConfig{
				ReadOnlyRootFS:     true,
				AllowPrivilegedOps: false,
			},
		},
		"memory-server": {
			Image:        "mcp/memory:latest",
			Protocol:     "http",
			HttpPort:     3003,
			Capabilities: []string{"tools", "resources"},
			Resources: mcpv1.ResourceRequirements{
				Limits: mcpv1.ResourceList{
					"cpu":    "400m",
					"memory": "1Gi",
				},
				Requests: mcpv1.ResourceList{
					"cpu":    "100m",
					"memory": "256Mi",
				},
			},
			Security: &mcpv1.SecurityConfig{
				ReadOnlyRootFS:     true,
				AllowPrivilegedOps: false,
			},
		},
		"document-processor-server": {
			Image:        "mcp/document-processor:latest",
			Protocol:     "http",
			HttpPort:     3004,
			Capabilities: []string{"tools"},
			Resources: mcpv1.ResourceRequirements{
				Limits: mcpv1.ResourceList{
					"cpu":    "500m",
					"memory": "768Mi",
				},
				Requests: mcpv1.ResourceList{
					"cpu":    "150m",
					"memory": "192Mi",
				},
			},
			Security: &mcpv1.SecurityConfig{
				ReadOnlyRootFS:     true,
				AllowPrivilegedOps: false,
			},
		},
	}

	template, exists := templates[templateName]
	if !exists {
		return fmt.Errorf("unknown server template: %s", templateName)
	}

	// Apply template to server spec (only set fields that are not already set)
	if server.Spec.Image == "" {
		server.Spec.Image = template.Image
	}
	if server.Spec.Protocol == "" {
		server.Spec.Protocol = template.Protocol
	}
	if server.Spec.HttpPort == 0 {
		server.Spec.HttpPort = template.HttpPort
	}
	if len(server.Spec.Capabilities) == 0 {
		server.Spec.Capabilities = template.Capabilities
	}
	if server.Spec.Resources.Limits == nil && template.Resources.Limits != nil {
		server.Spec.Resources.Limits = template.Resources.Limits
	}
	if server.Spec.Resources.Requests == nil && template.Resources.Requests != nil {
		server.Spec.Resources.Requests = template.Resources.Requests
	}
	if server.Spec.Security == nil && template.Security != nil {
		server.Spec.Security = template.Security
	}

	return nil
}

// getServerOrderWithDependencies returns servers ordered by dependencies and priority
func (r *MCPToolboxReconciler) getServerOrderWithDependencies(toolbox *mcpv1.MCPToolbox) ([]string, error) {
	// Create a map of server dependencies
	deps := make(map[string][]string)
	priorities := make(map[string]int32)

	for name, spec := range toolbox.Spec.Servers {
		deps[name] = []string{}
		priorities[name] = spec.Priority
	}

	// Add dependencies from the dependency list
	for _, dep := range toolbox.Spec.Dependencies {
		if _, exists := deps[dep.Server]; exists {
			deps[dep.Server] = append(deps[dep.Server], dep.DependsOn...)
		}
	}

	// Topological sort with priority consideration
	ordered, err := r.topologicalSort(deps, priorities)
	if err != nil {
		return nil, fmt.Errorf("circular dependency detected: %w", err)
	}

	return ordered, nil
}

// topologicalSort performs topological sorting with priority consideration
func (r *MCPToolboxReconciler) topologicalSort(deps map[string][]string, priorities map[string]int32) ([]string, error) {
	// Simple topological sort implementation
	visited := make(map[string]bool)
	visiting := make(map[string]bool)
	result := []string{}

	var visit func(string) error
	visit = func(node string) error {
		if visiting[node] {
			return fmt.Errorf("circular dependency involving %s", node)
		}
		if visited[node] {
			return nil
		}

		visiting[node] = true
		for _, dep := range deps[node] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visiting[node] = false
		visited[node] = true
		result = append(result, node)
		return nil
	}

	// Get all nodes sorted by priority
	var nodes []string
	for node := range deps {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return priorities[nodes[i]] < priorities[nodes[j]]
	})

	// Visit all nodes
	for _, node := range nodes {
		if err := visit(node); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// areServerDependenciesSatisfied checks if all dependencies for a server are satisfied
func (r *MCPToolboxReconciler) areServerDependenciesSatisfied(ctx context.Context, toolbox *mcpv1.MCPToolbox, serverName string) bool {
	// Find dependencies for this server
	var dependencies []string
	for _, dep := range toolbox.Spec.Dependencies {
		if dep.Server == serverName {
			dependencies = append(dependencies, dep.DependsOn...)
		}
	}

	if len(dependencies) == 0 {
		return true // No dependencies
	}

	// Check if all dependencies are ready
	for _, depName := range dependencies {
		depServerName := fmt.Sprintf("%s-%s", toolbox.Name, depName)
		var depServer mcpv1.MCPServer
		
		err := r.Get(ctx, types.NamespacedName{
			Name:      depServerName,
			Namespace: toolbox.Namespace,
		}, &depServer)
		
		if err != nil || depServer.Status.Phase != mcpv1.MCPServerPhaseRunning {
			return false
		}
	}

	return true
}

// applyToolboxDefaults applies toolbox-level defaults to a server spec
func (r *MCPToolboxReconciler) applyToolboxDefaults(toolbox *mcpv1.MCPToolbox, serverSpec *mcpv1.MCPServerSpec) {
	// Apply resource defaults
	if toolbox.Spec.Resources != nil && toolbox.Spec.Resources.DefaultServerLimits != nil {
		if serverSpec.Resources.Limits == nil && len(toolbox.Spec.Resources.DefaultServerLimits) > 0 {
			serverSpec.Resources.Limits = toolbox.Spec.Resources.DefaultServerLimits
		}
	}

	// Apply security defaults
	if toolbox.Spec.Security != nil {
		if serverSpec.Security == nil {
			serverSpec.Security = &mcpv1.SecurityConfig{}
		}
		
		if toolbox.Spec.Security.ImagePullPolicy != "" {
			// Apply image pull policy (would need to be implemented in MCPServerSpec)
		}
	}
}

// buildNetworkPolicySpec builds a network policy spec based on toolbox configuration
func (r *MCPToolboxReconciler) buildNetworkPolicySpec(toolbox *mcpv1.MCPToolbox) networkingv1.NetworkPolicySpec {
	// This is a simplified implementation
	podSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			"mcp.matey.ai/toolbox": toolbox.Name,
		},
	}

	spec := networkingv1.NetworkPolicySpec{
		PodSelector: podSelector,
		PolicyTypes: []networkingv1.PolicyType{
			networkingv1.PolicyTypeIngress,
			networkingv1.PolicyTypeEgress,
		},
	}

	// Allow intra-toolbox communication
	spec.Ingress = []networkingv1.NetworkPolicyIngressRule{
		{
			From: []networkingv1.NetworkPolicyPeer{
				{
					PodSelector: &podSelector,
				},
			},
		},
	}

	spec.Egress = []networkingv1.NetworkPolicyEgressRule{
		{
			To: []networkingv1.NetworkPolicyPeer{
				{
					PodSelector: &podSelector,
				},
			},
		},
	}

	return spec
}

// buildIngressSpec builds an ingress spec based on toolbox configuration
func (r *MCPToolboxReconciler) buildIngressSpec(toolbox *mcpv1.MCPToolbox) networkingv1.IngressSpec {
	pathType := networkingv1.PathTypePrefix
	
	// Determine the host
	host := ""
	if toolbox.Spec.Networking != nil && toolbox.Spec.Networking.CustomDomain != "" {
		host = toolbox.Spec.Networking.CustomDomain
	} else {
		host = fmt.Sprintf("%s.%s.local", toolbox.Name, toolbox.Namespace)
	}

	// Build ingress rules
	rules := []networkingv1.IngressRule{
		{
			Host: host,
			IngressRuleValue: networkingv1.IngressRuleValue{
				HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: fmt.Sprintf("%s-gateway", toolbox.Name),
									Port: networkingv1.ServiceBackendPort{
										Number: 80,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	spec := networkingv1.IngressSpec{
		Rules: rules,
	}

	// Add TLS if enabled
	if toolbox.Spec.Networking != nil && toolbox.Spec.Networking.TLSEnabled {
		spec.TLS = []networkingv1.IngressTLS{
			{
				Hosts:      []string{host},
				SecretName: fmt.Sprintf("%s-tls", toolbox.Name),
			},
		}
	}

	return spec
}

// buildPVCSpec builds a PersistentVolumeClaim spec based on volume configuration
func (r *MCPToolboxReconciler) buildPVCSpec(toolbox *mcpv1.MCPToolbox, volumeSpec mcpv1.SharedVolumeSpec) corev1.PersistentVolumeClaimSpec {
	// Default access mode
	accessModes := []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	if len(volumeSpec.AccessModes) > 0 {
		accessModes = []corev1.PersistentVolumeAccessMode{}
		for _, mode := range volumeSpec.AccessModes {
			accessModes = append(accessModes, corev1.PersistentVolumeAccessMode(mode))
		}
	}

	// Default size
	size := "1Gi"
	if volumeSpec.Size != "" {
		size = volumeSpec.Size
	}

	spec := corev1.PersistentVolumeClaimSpec{
		AccessModes: accessModes,
		Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse(size),
			},
		},
	}

	// Set storage class if specified
	if volumeSpec.StorageClass != "" {
		spec.StorageClassName = &volumeSpec.StorageClass
	}

	return spec
}

// SetupWithManager sets up the controller with the Manager
func (r *MCPToolboxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcpv1.MCPToolbox{}).
		Owns(&mcpv1.MCPServer{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&networkingv1.NetworkPolicy{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}