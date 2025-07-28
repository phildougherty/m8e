// internal/container/k8s.go
package container

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// KubernetesRuntime implements container runtime using Kubernetes
type KubernetesRuntime struct {
	client    kubernetes.Interface
	namespace string
	ctx       context.Context
}

// NewKubernetesRuntime creates a Kubernetes runtime
func NewKubernetesRuntime(namespace string) (Runtime, error) {
	if namespace == "" {
		namespace = "default"
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
		}
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return &KubernetesRuntime{
		client:    client,
		namespace: namespace,
		ctx:       context.Background(),
	}, nil
}

// GetRuntimeName returns the runtime name
func (k *KubernetesRuntime) GetRuntimeName() string {
	return "kubernetes"
}

// StartContainer creates and starts a Kubernetes deployment
func (k *KubernetesRuntime) StartContainer(opts *ContainerOptions) (string, error) {
	// Validate input
	if opts == nil {
		return "", fmt.Errorf("container options cannot be nil")
	}
	if opts.Name == "" {
		return "", fmt.Errorf("container name cannot be empty")
	}
	if opts.Image == "" {
		return "", fmt.Errorf("container image cannot be empty")
	}
	
	deployment := k.buildDeployment(opts)
	
	result, err := k.client.AppsV1().Deployments(k.namespace).Create(k.ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create deployment: %w", err)
	}

	// Create service for port exposure
	if len(opts.Ports) > 0 {
		service := k.buildService(opts)
		_, err = k.client.CoreV1().Services(k.namespace).Create(k.ctx, service, metav1.CreateOptions{})
		if err != nil {
			return "", fmt.Errorf("failed to create service: %w", err)
		}
	}

	// For fake clients, the UID might be empty, so generate one
	containerID := string(result.UID)
	if containerID == "" {
		containerID = fmt.Sprintf("fake-deployment-%s", opts.Name)
	}
	return containerID, nil
}

// StopContainer scales deployment to 0 replicas
func (k *KubernetesRuntime) StopContainer(name string) error {
	deployment, err := k.client.AppsV1().Deployments(k.namespace).Get(k.ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	replicas := int32(0)
	deployment.Spec.Replicas = &replicas

	_, err = k.client.AppsV1().Deployments(k.namespace).Update(k.ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to scale deployment to 0: %w", err)
	}

	return nil
}

// RestartContainer restarts the deployment
func (k *KubernetesRuntime) RestartContainer(name string) error {
	deployment, err := k.client.AppsV1().Deployments(k.namespace).Get(k.ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	// Add restart annotation to trigger rolling update
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = make(map[string]string)
	}
	deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	_, err = k.client.AppsV1().Deployments(k.namespace).Update(k.ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to restart deployment: %w", err)
	}

	return nil
}

// PauseContainer - not directly supported in Kubernetes, scale to 0 instead
func (k *KubernetesRuntime) PauseContainer(name string) error {
	return k.StopContainer(name)
}

// UnpauseContainer - scale back to 1 replica
func (k *KubernetesRuntime) UnpauseContainer(name string) error {
	deployment, err := k.client.AppsV1().Deployments(k.namespace).Get(k.ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	replicas := int32(1)
	deployment.Spec.Replicas = &replicas

	_, err = k.client.AppsV1().Deployments(k.namespace).Update(k.ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to scale deployment to 1: %w", err)
	}

	return nil
}

// GetContainerStatus returns the deployment status
func (k *KubernetesRuntime) GetContainerStatus(name string) (string, error) {
	deployment, err := k.client.AppsV1().Deployments(k.namespace).Get(k.ctx, name, metav1.GetOptions{})
	if err != nil {
		return "unknown", fmt.Errorf("failed to get deployment: %w", err)
	}

	if deployment.Status.ReadyReplicas > 0 {
		return "running", nil
	}
	if deployment.Status.Replicas > 0 {
		return "starting", nil
	}
	return "stopped", nil
}

// GetContainerInfo returns detailed container information
func (k *KubernetesRuntime) GetContainerInfo(name string) (*ContainerInfo, error) {
	deployment, err := k.client.AppsV1().Deployments(k.namespace).Get(k.ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	// Get pods for this deployment (would be used for more detailed info)
	// pods, err := k.client.CoreV1().Pods(k.namespace).List(k.ctx, metav1.ListOptions{
	//	LabelSelector: metav1.FormatLabelSelector(deployment.Spec.Selector),
	// })
	// if err != nil {
	//	return nil, fmt.Errorf("failed to get pods: %w", err)
	// }

	status := "stopped"
	if deployment.Status.ReadyReplicas > 0 {
		status = "running"
	} else if deployment.Status.Replicas > 0 {
		status = "starting"
	}

	var ports []PortBinding
	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		for _, port := range deployment.Spec.Template.Spec.Containers[0].Ports {
			ports = append(ports, PortBinding{
				PrivatePort: int(port.ContainerPort),
				PublicPort:  int(port.ContainerPort), // In k8s, this would be service port
				Type:        string(port.Protocol),
			})
		}
	}

	info := &ContainerInfo{
		ID:           string(deployment.UID),
		Name:         deployment.Name,
		Status:       status,
		State:        status,
		Created:      deployment.CreationTimestamp.Format(time.RFC3339),
		Ports:        ports,
		Labels:       deployment.Labels,
		RestartCount: 0, // Would need to aggregate from pods
	}

	if len(deployment.Spec.Template.Spec.Containers) > 0 {
		container := deployment.Spec.Template.Spec.Containers[0]
		info.Image = container.Image
		info.Command = container.Command
		
		// Extract environment variables
		for _, env := range container.Env {
			info.Env = append(info.Env, fmt.Sprintf("%s=%s", env.Name, env.Value))
		}
	}

	return info, nil
}

// ListContainers returns list of deployments
func (k *KubernetesRuntime) ListContainers(filters map[string]string) ([]ContainerInfo, error) {
	deployments, err := k.client.AppsV1().Deployments(k.namespace).List(k.ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	var containers []ContainerInfo
	for _, deployment := range deployments.Items {
		info, err := k.GetContainerInfo(deployment.Name)
		if err != nil {
			continue // Skip deployments we can't get info for
		}
		containers = append(containers, *info)
	}

	return containers, nil
}

// GetContainerStats returns container statistics (from metrics server if available)
func (k *KubernetesRuntime) GetContainerStats(name string) (*ContainerStats, error) {
	// This would require metrics server integration
	return &ContainerStats{
		CPUUsage:    0,
		MemoryUsage: 0,
		MemoryLimit: 0,
	}, nil
}

// WaitForContainer waits for deployment to reach desired state
func (k *KubernetesRuntime) WaitForContainer(name string, condition string) error {
	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for container %s to reach condition %s", name, condition)
		case <-ticker.C:
			status, err := k.GetContainerStatus(name)
			if err != nil {
				continue
			}
			if status == condition {
				return nil
			}
		}
	}
}

// ShowContainerLogs shows logs from pods
func (k *KubernetesRuntime) ShowContainerLogs(name string, follow bool) error {
	deployment, err := k.client.AppsV1().Deployments(k.namespace).Get(k.ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	// Get pods for this deployment
	pods, err := k.client.CoreV1().Pods(k.namespace).List(k.ctx, metav1.ListOptions{
		LabelSelector: metav1.FormatLabelSelector(deployment.Spec.Selector),
	})
	if err != nil {
		return fmt.Errorf("failed to get pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return fmt.Errorf("no pods found for deployment %s", name)
	}

	// Show logs from first pod
	pod := pods.Items[0]
	req := k.client.CoreV1().Pods(k.namespace).GetLogs(pod.Name, &corev1.PodLogOptions{
		Follow: follow,
	})

	logs, err := req.Stream(k.ctx)
	if err != nil {
		return fmt.Errorf("failed to get logs: %w", err)
	}
	defer logs.Close()

	// Copy logs to stdout
	_, err = io.Copy(os.Stdout, logs)
	return err
}

// ExecContainer executes command in pod
func (k *KubernetesRuntime) ExecContainer(containerName string, command []string, interactive bool) (*exec.Cmd, io.Writer, io.Reader, error) {
	// This would require implementing kubectl exec equivalent
	// For now, return error as this is complex to implement
	return nil, nil, nil, fmt.Errorf("exec not implemented for Kubernetes runtime")
}

// buildDeployment creates a Kubernetes deployment from container options
func (k *KubernetesRuntime) buildDeployment(opts *ContainerOptions) *appsv1.Deployment {
	labels := map[string]string{
		"app":     opts.Name,
		"runtime": "matey",
	}

	// Add custom labels
	for key, value := range opts.Labels {
		labels[key] = value
	}

	replicas := int32(1)
	
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:   opts.Name,
			Labels: labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  opts.Name,
							Image: opts.Image,
						},
					},
				},
			},
		},
	}

	container := &deployment.Spec.Template.Spec.Containers[0]

	// Set command and args
	if opts.Command != "" {
		container.Command = []string{opts.Command}
	}
	if len(opts.Args) > 0 {
		container.Args = opts.Args
	}

	// Set environment variables
	for key, value := range opts.Env {
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  key,
			Value: value,
		})
	}

	// Set ports
	for _, portStr := range opts.Ports {
		if port, err := k.parsePort(portStr); err == nil {
			container.Ports = append(container.Ports, corev1.ContainerPort{
				ContainerPort: int32(port),
				Protocol:      corev1.ProtocolTCP,
			})
		}
	}

	// Set resource limits
	if opts.Memory != "" || opts.CPUs != "" {
		resources := corev1.ResourceRequirements{
			Limits: corev1.ResourceList{},
		}
		
		if opts.Memory != "" {
			if memory, err := resource.ParseQuantity(opts.Memory); err == nil {
				resources.Limits[corev1.ResourceMemory] = memory
			}
		}
		
		if opts.CPUs != "" {
			if cpu, err := resource.ParseQuantity(opts.CPUs); err == nil {
				resources.Limits[corev1.ResourceCPU] = cpu
			}
		}
		
		container.Resources = resources
	}

	// Set security context
	if opts.User != "" || opts.ReadOnly || !opts.Privileged {
		securityContext := &corev1.SecurityContext{}
		
		if opts.User != "" {
			if uid, err := strconv.ParseInt(opts.User, 10, 64); err == nil {
				securityContext.RunAsUser = &uid
			}
		}
		
		securityContext.ReadOnlyRootFilesystem = &opts.ReadOnly
		securityContext.Privileged = &opts.Privileged
		
		container.SecurityContext = securityContext
	}

	// Set working directory
	if opts.WorkDir != "" {
		container.WorkingDir = opts.WorkDir
	}

	return deployment
}

// buildService creates a Kubernetes service for port exposure
func (k *KubernetesRuntime) buildService(opts *ContainerOptions) *corev1.Service {
	labels := map[string]string{
		"app":     opts.Name,
		"runtime": "matey",
	}

	var ports []corev1.ServicePort
	for _, portStr := range opts.Ports {
		if port, err := k.parsePort(portStr); err == nil {
			ports = append(ports, corev1.ServicePort{
				Port:       int32(port),
				TargetPort: intstr.FromInt(port),
				Protocol:   corev1.ProtocolTCP,
			})
		}
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:   opts.Name,
			Labels: labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports:    ports,
			Type:     corev1.ServiceTypeClusterIP,
		},
	}
}

// parsePort extracts port number from port mapping string
func (k *KubernetesRuntime) parsePort(portStr string) (int, error) {
	// Handle various port formats: "8080", "8080:8080", "0.0.0.0:8080:8080"
	parts := strings.Split(portStr, ":")
	if len(parts) == 1 {
		return strconv.Atoi(parts[0])
	}
	if len(parts) == 2 {
		return strconv.Atoi(parts[1])
	}
	if len(parts) == 3 {
		return strconv.Atoi(parts[2])
	}
	return 0, fmt.Errorf("invalid port format: %s", portStr)
}

// Placeholder implementations for other interface methods
func (k *KubernetesRuntime) PullImage(image string, auth *ImageAuth) error {
	// Images are pulled automatically by Kubernetes
	return nil
}

func (k *KubernetesRuntime) BuildImage(opts *BuildOptions) error {
	return fmt.Errorf("image building not supported in Kubernetes runtime")
}

func (k *KubernetesRuntime) RemoveImage(image string, force bool) error {
	return fmt.Errorf("image removal not supported in Kubernetes runtime")
}

func (k *KubernetesRuntime) ListImages() ([]ImageInfo, error) {
	return nil, fmt.Errorf("image listing not supported in Kubernetes runtime")
}

func (k *KubernetesRuntime) CreateVolume(name string, opts *VolumeOptions) error {
	// Would create PersistentVolume/PersistentVolumeClaim
	return fmt.Errorf("volume creation not yet implemented")
}

func (k *KubernetesRuntime) RemoveVolume(name string, force bool) error {
	return fmt.Errorf("volume removal not yet implemented")
}

func (k *KubernetesRuntime) ListVolumes() ([]VolumeInfo, error) {
	return nil, fmt.Errorf("volume listing not yet implemented")
}

func (k *KubernetesRuntime) NetworkExists(name string) (bool, error) {
	// Kubernetes handles networking differently
	return true, nil
}

func (k *KubernetesRuntime) CreateNetwork(name string) error {
	// Networks are managed by Kubernetes CNI
	return nil
}

func (k *KubernetesRuntime) RemoveNetwork(name string) error {
	return nil
}

func (k *KubernetesRuntime) ListNetworks() ([]NetworkInfo, error) {
	return nil, fmt.Errorf("network listing not supported in Kubernetes runtime")
}

func (k *KubernetesRuntime) GetNetworkInfo(name string) (*NetworkInfo, error) {
	return nil, fmt.Errorf("network info not supported in Kubernetes runtime")
}

func (k *KubernetesRuntime) ConnectToNetwork(containerName, networkName string) error {
	return nil // Handled by Kubernetes networking
}

func (k *KubernetesRuntime) DisconnectFromNetwork(containerName, networkName string) error {
	return nil // Handled by Kubernetes networking
}

func (k *KubernetesRuntime) UpdateContainerResources(name string, resources *ResourceLimits) error {
	return fmt.Errorf("resource updates not yet implemented")
}

func (k *KubernetesRuntime) ValidateSecurityContext(opts *ContainerOptions) error {
	// Validate security context for Kubernetes deployment
	if opts == nil {
		return fmt.Errorf("container options cannot be nil")
	}
	
	// Check if privileged operations are allowed
	if opts.Privileged && !opts.Security.AllowPrivilegedOps {
		return fmt.Errorf("privileged mode is not allowed by security policy")
	}
	
	// Validate host mounts
	if len(opts.Security.AllowHostMounts) > 0 {
		// Check if host mounts are allowed by security policy
		// This would typically be validated against PodSecurityPolicy or similar
		for _, mount := range opts.Security.AllowHostMounts {
			if mount == "/" {
				return fmt.Errorf("mounting root filesystem is not allowed")
			}
		}
	}
	
	// Validate user context
	if opts.User != "" {
		// Validate user specification format
		if strings.Contains(opts.User, ":") {
			parts := strings.Split(opts.User, ":")
			if len(parts) != 2 {
				return fmt.Errorf("invalid user specification format: %s", opts.User)
			}
			// Validate UID and GID are numeric
			if _, err := strconv.ParseInt(parts[0], 10, 64); err != nil {
				return fmt.Errorf("invalid UID: %s", parts[0])
			}
			if _, err := strconv.ParseInt(parts[1], 10, 64); err != nil {
				return fmt.Errorf("invalid GID: %s", parts[1])
			}
		} else if opts.User != "root" {
			// Single value should be numeric UID
			if _, err := strconv.ParseInt(opts.User, 10, 64); err != nil {
				return fmt.Errorf("invalid user specification: %s", opts.User)
			}
		}
	}
	
	return nil
}