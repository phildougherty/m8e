// internal/service/cluster_stats.go
package service

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/phildougherty/m8e/internal/compose"
)

// ServerInfo is the per-server resource and status snapshot gathered for the
// `top` view. It carries data only; the cmd layer owns rendering.
type ServerInfo struct {
	Name        string
	Status      string
	Type        string
	Restarts    int32
	Age         time.Duration
	CPU         string
	Memory      string
	Image       string
	Protocol    string
	Port        int32
	Replicas    string
	Ready       bool
	LastUpdated time.Time
}

// statusReader is the slice of compose.K8sComposer that ClusterStats needs.
// Narrowing to an interface keeps the service unit-testable with a fake.
type statusReader interface {
	Status() (*compose.ComposeStatus, error)
}

// ClusterStats gathers resource-usage and status information about MCP servers
// in a namespace by correlating the composer's view with live Kubernetes
// deployments and pods. It holds no cobra/TUI concerns.
type ClusterStats struct {
	composer  statusReader
	k8sClient kubernetes.Interface
	namespace string
}

// NewClusterStats builds a ClusterStats service.
func NewClusterStats(composer statusReader, k8sClient kubernetes.Interface, namespace string) *ClusterStats {
	return &ClusterStats{composer: composer, k8sClient: k8sClient, namespace: namespace}
}

// ServerInfos returns a snapshot of every server known to the composer,
// enriched with deployment and pod detail from Kubernetes.
func (c *ClusterStats) ServerInfos(ctx context.Context) ([]ServerInfo, error) {
	status, err := c.composer.Status()
	if err != nil {
		return nil, fmt.Errorf("failed to get status: %w", err)
	}

	deployments, err := c.k8sClient.AppsV1().Deployments(c.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	pods, err := c.k8sClient.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	deploymentMap := make(map[string]*appsv1.Deployment)
	for i := range deployments.Items {
		dep := &deployments.Items[i]
		deploymentMap[dep.Name] = dep
	}

	podMap := make(map[string][]*corev1.Pod)
	for i := range pods.Items {
		pod := &pods.Items[i]
		if appLabel, ok := pod.Labels["app"]; ok {
			podMap[appLabel] = append(podMap[appLabel], pod)
		}
	}

	var servers []ServerInfo
	for name, svcStatus := range status.Services {
		server := ServerInfo{
			Name:        name,
			Status:      svcStatus.Status,
			Type:        svcStatus.Type,
			LastUpdated: time.Now(),
		}

		if dep, exists := deploymentMap[name]; exists {
			server.Age = time.Since(dep.CreationTimestamp.Time)
			server.Replicas = fmt.Sprintf("%d/%d", dep.Status.ReadyReplicas, dep.Status.Replicas)
			server.Ready = dep.Status.ReadyReplicas > 0

			if len(dep.Spec.Template.Spec.Containers) > 0 {
				container := dep.Spec.Template.Spec.Containers[0]
				server.Image = container.Image

				if container.Resources.Requests != nil {
					if cpu, ok := container.Resources.Requests[corev1.ResourceCPU]; ok {
						server.CPU = cpu.String()
					}
					if memory, ok := container.Resources.Requests[corev1.ResourceMemory]; ok {
						server.Memory = memory.String()
					}
				}

				if len(container.Ports) > 0 {
					server.Port = container.Ports[0].ContainerPort
				}
			}

			if protocol, ok := dep.Labels["mcp.matey.ai/protocol"]; ok {
				server.Protocol = protocol
			}
		}

		if podList, exists := podMap[name]; exists && len(podList) > 0 {
			var totalRestarts int32
			for _, pod := range podList {
				for _, containerStatus := range pod.Status.ContainerStatuses {
					totalRestarts += containerStatus.RestartCount
				}
			}
			server.Restarts = totalRestarts
		}

		servers = append(servers, server)
	}

	return servers, nil
}
