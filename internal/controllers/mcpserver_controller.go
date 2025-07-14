// internal/controllers/mcpserver_controller.go
package controllers

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/phildougherty/m8e/internal/config"
	mcpv1 "github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/discovery"
)

// MCPServerReconciler reconciles MCPServer objects
type MCPServerReconciler struct {
	client.Client
	Scheme               *runtime.Scheme
	ServiceDiscovery     *discovery.K8sServiceDiscovery
	ConnectionManager    *discovery.DynamicConnectionManager
	Config               *config.ComposeConfig
}

// +kubebuilder:rbac:groups=mcp.matey.ai,resources=mcpservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=mcp.matey.ai,resources=mcpservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mcp.matey.ai,resources=mcpservers/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles MCPServer resource reconciliation
func (r *MCPServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the MCPServer instance
	var mcpServer mcpv1.MCPServer
	if err := r.Get(ctx, req.NamespacedName, &mcpServer); err != nil {
		if errors.IsNotFound(err) {
			// Object not found, could have been deleted after reconcile request
			logger.Info("MCPServer resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get MCPServer")
		return ctrl.Result{}, err
	}

	// Handle deletion
	if mcpServer.DeletionTimestamp != nil {
		return r.handleDeletion(ctx, &mcpServer)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&mcpServer, "mcp.matey.ai/finalizer") {
		controllerutil.AddFinalizer(&mcpServer, "mcp.matey.ai/finalizer")
		return ctrl.Result{}, r.Update(ctx, &mcpServer)
	}

	// Update status phase
	if mcpServer.Status.Phase == "" {
		mcpServer.Status.Phase = mcpv1.MCPServerPhasePending
		if err := r.Status().Update(ctx, &mcpServer); err != nil {
			logger.Error(err, "Failed to update MCPServer status")
			return ctrl.Result{}, err
		}
	}

	// Reconcile Deployment
	deployment, err := r.reconcileDeployment(ctx, &mcpServer)
	if err != nil {
		logger.Error(err, "Failed to reconcile Deployment")
		return ctrl.Result{}, err
	}

	// Reconcile Service
	service, err := r.reconcileService(ctx, &mcpServer)
	if err != nil {
		logger.Error(err, "Failed to reconcile Service")
		return ctrl.Result{}, err
	}

	// Update status based on deployment and service state
	if err := r.updateStatus(ctx, &mcpServer, deployment, service); err != nil {
		logger.Error(err, "Failed to update MCPServer status")
		return ctrl.Result{}, err
	}

	logger.Info("Successfully reconciled MCPServer", "name", mcpServer.Name)
	return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
}

// handleDeletion handles MCPServer deletion
func (r *MCPServerReconciler) handleDeletion(ctx context.Context, mcpServer *mcpv1.MCPServer) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Perform cleanup if necessary
	logger.Info("Handling MCPServer deletion", "name", mcpServer.Name)

	// Remove finalizer to allow deletion
	controllerutil.RemoveFinalizer(mcpServer, "mcp.matey.ai/finalizer")
	if err := r.Update(ctx, mcpServer); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcileDeployment creates or updates the Deployment for the MCPServer
func (r *MCPServerReconciler) reconcileDeployment(ctx context.Context, mcpServer *mcpv1.MCPServer) (*appsv1.Deployment, error) {
	logger := log.FromContext(ctx)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
		// Set owner reference
		if err := controllerutil.SetControllerReference(mcpServer, deployment, r.Scheme); err != nil {
			return err
		}

		// Build deployment spec
		deployment.Spec = r.buildDeploymentSpec(mcpServer)
		return nil
	})

	if err != nil {
		logger.Error(err, "Failed to create or update Deployment")
		return nil, err
	}

	logger.Info("Deployment reconciled", "operation", op, "name", deployment.Name)
	return deployment, nil
}

// reconcileService creates or updates the Service for the MCPServer
func (r *MCPServerReconciler) reconcileService(ctx context.Context, mcpServer *mcpv1.MCPServer) (*corev1.Service, error) {
	logger := log.FromContext(ctx)

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcpServer.Name,
			Namespace: mcpServer.Namespace,
		},
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
		// Set owner reference
		if err := controllerutil.SetControllerReference(mcpServer, service, r.Scheme); err != nil {
			return err
		}

		// Build service spec
		service.Spec = r.buildServiceSpec(mcpServer)
		
		// Add service discovery labels
		if service.Labels == nil {
			service.Labels = make(map[string]string)
		}
		
		// Standard MCP service discovery labels
		service.Labels["mcp.matey.ai/role"] = "server"
		service.Labels["mcp.matey.ai/protocol"] = mcpServer.Spec.Protocol
		service.Labels["mcp.matey.ai/server-name"] = mcpServer.Name
		
		if len(mcpServer.Spec.Capabilities) > 0 {
			// Join capabilities with dots for discovery (commas are invalid in Kubernetes labels)
			caps := ""
			for i, cap := range mcpServer.Spec.Capabilities {
				if i > 0 {
					caps += "."
				}
				caps += cap
			}
			service.Labels["mcp.matey.ai/capabilities"] = caps
		}
		
		// Add custom discovery labels
		if mcpServer.Spec.ServiceDiscovery.DiscoveryLabels != nil {
			for k, v := range mcpServer.Spec.ServiceDiscovery.DiscoveryLabels {
				service.Labels[k] = v
			}
		}

		return nil
	})

	if err != nil {
		logger.Error(err, "Failed to create or update Service")
		return nil, err
	}

	logger.Info("Service reconciled", "operation", op, "name", service.Name)
	return service, nil
}

// buildDeploymentSpec builds the deployment specification
func (r *MCPServerReconciler) buildDeploymentSpec(mcpServer *mcpv1.MCPServer) appsv1.DeploymentSpec {
	labels := map[string]string{
		"app":                    mcpServer.Name,
		"mcp.matey.ai/server":   mcpServer.Name,
		"mcp.matey.ai/protocol": mcpServer.Spec.Protocol,
	}

	// Add custom labels
	for k, v := range mcpServer.Spec.Labels {
		labels[k] = v
	}

	replicas := int32(1)
	if mcpServer.Spec.Replicas != nil {
		replicas = *mcpServer.Spec.Replicas
	}

	container := corev1.Container{
		Name:            "mcp-server",
		Image:           mcpServer.Spec.Image,
		ImagePullPolicy: corev1.PullIfNotPresent,
	}

	// Set command and args
	if len(mcpServer.Spec.Command) > 0 {
		container.Command = mcpServer.Spec.Command
	}
	if len(mcpServer.Spec.Args) > 0 {
		container.Args = mcpServer.Spec.Args
	}

	// Set environment variables
	if len(mcpServer.Spec.Env) > 0 {
		for key, value := range mcpServer.Spec.Env {
			container.Env = append(container.Env, corev1.EnvVar{
				Name:  key,
				Value: value,
			})
		}
	}

	// Set working directory
	if mcpServer.Spec.WorkingDir != "" {
		container.WorkingDir = mcpServer.Spec.WorkingDir
	}

	// Set ports
	if mcpServer.Spec.HttpPort > 0 {
		container.Ports = append(container.Ports, corev1.ContainerPort{
			Name:          "http",
			ContainerPort: mcpServer.Spec.HttpPort,
			Protocol:      corev1.ProtocolTCP,
		})
	}
	if mcpServer.Spec.SSEPort > 0 {
		container.Ports = append(container.Ports, corev1.ContainerPort{
			Name:          "sse",
			ContainerPort: mcpServer.Spec.SSEPort,
			Protocol:      corev1.ProtocolTCP,
		})
	}

	// Set resource requirements
	if mcpServer.Spec.Resources.Limits != nil || mcpServer.Spec.Resources.Requests != nil {
		container.Resources = corev1.ResourceRequirements{}
		
		if mcpServer.Spec.Resources.Limits != nil {
			container.Resources.Limits = convertResourceList(mcpServer.Spec.Resources.Limits)
		}
		if mcpServer.Spec.Resources.Requests != nil {
			container.Resources.Requests = convertResourceList(mcpServer.Spec.Resources.Requests)
		}
	}

	// Set security context
	if mcpServer.Spec.Security != nil {
		container.SecurityContext = &corev1.SecurityContext{
			ReadOnlyRootFilesystem: &mcpServer.Spec.Security.ReadOnlyRootFS,
		}

		// Only set RunAsNonRoot if not allowing privileged operations and no specific user is set
		if !mcpServer.Spec.Security.AllowPrivilegedOps {
			if mcpServer.Spec.Security.RunAsUser == nil || *mcpServer.Spec.Security.RunAsUser != 0 {
				container.SecurityContext.RunAsNonRoot = &[]bool{true}[0]
				container.SecurityContext.AllowPrivilegeEscalation = &[]bool{false}[0]
			}
		}

		if mcpServer.Spec.Security.RunAsUser != nil {
			container.SecurityContext.RunAsUser = mcpServer.Spec.Security.RunAsUser
		}
		if mcpServer.Spec.Security.RunAsGroup != nil {
			container.SecurityContext.RunAsGroup = mcpServer.Spec.Security.RunAsGroup
		}

		// Set capabilities
		if len(mcpServer.Spec.Security.CapAdd) > 0 || len(mcpServer.Spec.Security.CapDrop) > 0 {
			container.SecurityContext.Capabilities = &corev1.Capabilities{}
			
			for _, cap := range mcpServer.Spec.Security.CapAdd {
				container.SecurityContext.Capabilities.Add = append(
					container.SecurityContext.Capabilities.Add, 
					corev1.Capability(cap),
				)
			}
			for _, cap := range mcpServer.Spec.Security.CapDrop {
				container.SecurityContext.Capabilities.Drop = append(
					container.SecurityContext.Capabilities.Drop, 
					corev1.Capability(cap),
				)
			}
		}
	}

	// Volume mounts
	for _, vol := range mcpServer.Spec.Volumes {
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      vol.Name,
			MountPath: vol.MountPath,
			SubPath:   vol.SubPath,
		})
	}

	podSpec := corev1.PodSpec{
		Containers: []corev1.Container{container},
	}

	// Add volumes to pod spec
	for _, vol := range mcpServer.Spec.Volumes {
		// Create host path volumes for the volume mounts
		// Use the hostPath from the volume spec, or fall back to the mountPath if not specified
		hostPath := vol.HostPath
		if hostPath == "" {
			hostPath = vol.MountPath
		}
		
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: vol.Name,
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: hostPath,
					Type: &[]corev1.HostPathType{corev1.HostPathDirectoryOrCreate}[0],
				},
			},
		})
	}

	// Set service account
	if mcpServer.Spec.ServiceAccount != "" {
		podSpec.ServiceAccountName = mcpServer.Spec.ServiceAccount
	}

	// Set image pull secrets
	if len(mcpServer.Spec.ImagePullSecrets) > 0 {
		for _, secret := range mcpServer.Spec.ImagePullSecrets {
			podSpec.ImagePullSecrets = append(podSpec.ImagePullSecrets, corev1.LocalObjectReference{
				Name: secret,
			})
		}
	}

	// Set node selector
	if len(mcpServer.Spec.NodeSelector) > 0 {
		podSpec.NodeSelector = mcpServer.Spec.NodeSelector
	}

	// Add pod annotations
	podAnnotations := make(map[string]string)
	for k, v := range mcpServer.Spec.PodAnnotations {
		podAnnotations[k] = v
	}

	return appsv1.DeploymentSpec{
		Replicas: &replicas,
		Selector: &metav1.LabelSelector{
			MatchLabels: labels,
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      labels,
				Annotations: podAnnotations,
			},
			Spec: podSpec,
		},
	}
}

// buildServiceSpec builds the service specification
func (r *MCPServerReconciler) buildServiceSpec(mcpServer *mcpv1.MCPServer) corev1.ServiceSpec {
	labels := map[string]string{
		"app":                    mcpServer.Name,
		"mcp.matey.ai/server":   mcpServer.Name,
		"mcp.matey.ai/protocol": mcpServer.Spec.Protocol,
	}

	var ports []corev1.ServicePort

	// Add HTTP port
	if mcpServer.Spec.HttpPort > 0 {
		ports = append(ports, corev1.ServicePort{
			Name:       "http",
			Port:       mcpServer.Spec.HttpPort,
			TargetPort: intstr.FromInt(int(mcpServer.Spec.HttpPort)),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	// Add SSE port
	if mcpServer.Spec.SSEPort > 0 {
		ports = append(ports, corev1.ServicePort{
			Name:       "sse",
			Port:       mcpServer.Spec.SSEPort,
			TargetPort: intstr.FromInt(int(mcpServer.Spec.SSEPort)),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	// For stdio services without ports, add a dummy port to satisfy Kubernetes service requirements
	if len(ports) == 0 && mcpServer.Spec.Protocol == "stdio" {
		ports = append(ports, corev1.ServicePort{
			Name:       "dummy",
			Port:       80,
			TargetPort: intstr.FromInt(80),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	serviceType := corev1.ServiceTypeClusterIP
	if mcpServer.Spec.ServiceType != "" {
		serviceType = corev1.ServiceType(mcpServer.Spec.ServiceType)
	}

	return corev1.ServiceSpec{
		Selector: labels,
		Ports:    ports,
		Type:     serviceType,
	}
}

// updateStatus updates the MCPServer status based on deployment and service state
func (r *MCPServerReconciler) updateStatus(ctx context.Context, mcpServer *mcpv1.MCPServer, deployment *appsv1.Deployment, service *corev1.Service) error {
	// Update status based on deployment status
	newStatus := mcpServer.Status.DeepCopy()

	if deployment.Status.ReadyReplicas > 0 {
		newStatus.Phase = mcpv1.MCPServerPhaseRunning
	} else if deployment.Status.Replicas > 0 {
		newStatus.Phase = mcpv1.MCPServerPhaseStarting
	} else {
		newStatus.Phase = mcpv1.MCPServerPhaseCreating
	}

	newStatus.Replicas = deployment.Status.Replicas
	newStatus.ReadyReplicas = deployment.Status.ReadyReplicas
	newStatus.ObservedGeneration = mcpServer.Generation

	// Update connection info
	if service != nil {
		connectionInfo := &mcpv1.ConnectionInfo{
			Protocol: mcpServer.Spec.Protocol,
		}

		if len(service.Spec.Ports) > 0 {
			port := service.Spec.Ports[0]
			connectionInfo.Port = port.Port
			connectionInfo.Endpoint = fmt.Sprintf("%s.%s.svc.cluster.local:%d", 
				service.Name, service.Namespace, port.Port)
		}

		if mcpServer.Spec.HttpPath != "" {
			connectionInfo.Path = mcpServer.Spec.HttpPath
		}
		if mcpServer.Spec.SSEPath != "" {
			connectionInfo.SSEEndpoint = fmt.Sprintf("%s%s", connectionInfo.Endpoint, mcpServer.Spec.SSEPath)
		}

		newStatus.ConnectionInfo = connectionInfo
	}

	// Update service endpoints
	newStatus.ServiceEndpoints = []mcpv1.ServiceEndpoint{
		{
			Name: service.Name,
			Port: service.Spec.Ports[0].Port,
			URL:  fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", service.Name, service.Namespace, service.Spec.Ports[0].Port),
		},
	}

	// Update conditions
	r.updateConditions(newStatus, deployment)

	mcpServer.Status = *newStatus
	return r.Status().Update(ctx, mcpServer)
}

// updateConditions updates the MCPServer conditions
func (r *MCPServerReconciler) updateConditions(status *mcpv1.MCPServerStatus, deployment *appsv1.Deployment) {
	now := metav1.Now()

	// Ready condition
	readyCondition := mcpv1.MCPServerCondition{
		Type:               mcpv1.MCPServerConditionReady,
		LastTransitionTime: now,
	}

	if deployment.Status.ReadyReplicas > 0 {
		readyCondition.Status = metav1.ConditionTrue
		readyCondition.Reason = "DeploymentReady"
		readyCondition.Message = "Deployment has ready replicas"
	} else {
		readyCondition.Status = metav1.ConditionFalse
		readyCondition.Reason = "DeploymentNotReady"
		readyCondition.Message = "Deployment has no ready replicas"
	}

	// Update or add condition
	r.setCondition(status, readyCondition)
}

// setCondition sets or updates a condition in the status
func (r *MCPServerReconciler) setCondition(status *mcpv1.MCPServerStatus, newCondition mcpv1.MCPServerCondition) {
	for i, condition := range status.Conditions {
		if condition.Type == newCondition.Type {
			if condition.Status != newCondition.Status {
				status.Conditions[i] = newCondition
			}
			return
		}
	}
	status.Conditions = append(status.Conditions, newCondition)
}

// convertResourceList converts custom ResourceList to Kubernetes ResourceList
func convertResourceList(rl mcpv1.ResourceList) corev1.ResourceList {
	result := make(corev1.ResourceList)
	for key, value := range rl {
		// Convert common Docker-style memory formats to Kubernetes format
		convertedValue := value
		if key == "memory" {
			// Convert Docker-style memory (512m, 1g) to Kubernetes format (512Mi, 1Gi)
			if strings.HasSuffix(value, "m") && !strings.HasSuffix(value, "Mi") {
				convertedValue = strings.TrimSuffix(value, "m") + "Mi"
			} else if strings.HasSuffix(value, "g") && !strings.HasSuffix(value, "Gi") {
				convertedValue = strings.TrimSuffix(value, "g") + "Gi"
			}
		}
		
		quantity, err := resource.ParseQuantity(convertedValue)
		if err == nil {
			result[corev1.ResourceName(key)] = quantity
		}
	}
	return result
}

// SetupWithManager sets up the controller with the Manager
func (r *MCPServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mcpv1.MCPServer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}