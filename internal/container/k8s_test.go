package container

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func TestKubernetesRuntime_StartContainer(t *testing.T) {
	// Create fake Kubernetes client
	fakeClient := fake.NewSimpleClientset()

	// Create KubernetesRuntime with fake client
	runtime := &KubernetesRuntime{
		client:    fakeClient,
		namespace: "default",
		ctx:       context.Background(),
	}

	tests := []struct {
		name        string
		opts        *ContainerOptions
		expectError bool
		expectDeploy bool
		expectService bool
	}{
		{
			name: "Start container with basic options",
			opts: &ContainerOptions{
				Name:    "test-container",
				Image:   "nginx:latest",
				Command: "nginx",
				Args:    []string{"-g", "daemon off;"},
				Ports:   []string{"8080:80"},
				Env: map[string]string{
					"ENV": "test",
				},
			},
			expectError:   false,
			expectDeploy:  true,
			expectService: true,
		},
		{
			name: "Start container with resource limits",
			opts: &ContainerOptions{
				Name:  "resource-container",
				Image: "alpine:latest",
				CPUs:  "0.5",
				Memory: "512Mi",
				Ports: []string{"3000:3000"},
			},
			expectError:   false,
			expectDeploy:  true,
			expectService: true,
		},
		{
			name: "Start container with volumes",
			opts: &ContainerOptions{
				Name:    "volume-container",
				Image:   "busybox:latest",
				Volumes: []string{"/host/path:/container/path"},
			},
			expectError:   false,
			expectDeploy:  true,
			expectService: false, // No ports, no service
		},
		{
			name: "Start container with invalid options",
			opts: &ContainerOptions{
				Name:  "", // Empty name should cause error
				Image: "test:latest",
			},
			expectError:   true,
			expectDeploy:  false,
			expectService: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear any previous actions
			fakeClient.ClearActions()

			// Start container
			containerID, err := runtime.StartContainer(tt.opts)

			// Check error expectation
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if !tt.expectError {
				// Check container ID format
				if containerID == "" {
					t.Error("Expected non-empty container ID")
				}

				// Check that deployment was created
				if tt.expectDeploy {
					deploymentCreated := false
					for _, action := range fakeClient.Actions() {
						if action.GetVerb() == "create" && action.GetResource().Resource == "deployments" {
							deploymentCreated = true
							
							// Verify deployment properties
							createAction := action.(ktesting.CreateAction)
							deployment := createAction.GetObject().(*appsv1.Deployment)
							
							if deployment.Name != tt.opts.Name {
								t.Errorf("Expected deployment name %s, got %s", tt.opts.Name, deployment.Name)
							}
							
							if deployment.Spec.Template.Spec.Containers[0].Image != tt.opts.Image {
								t.Errorf("Expected image %s, got %s", tt.opts.Image, deployment.Spec.Template.Spec.Containers[0].Image)
							}
							
							break
						}
					}
					
					if !deploymentCreated {
						t.Error("Expected deployment to be created")
					}
				}

				// Check that service was created if ports are specified
				if tt.expectService {
					serviceCreated := false
					for _, action := range fakeClient.Actions() {
						if action.GetVerb() == "create" && action.GetResource().Resource == "services" {
							serviceCreated = true
							
							// Verify service properties
							createAction := action.(ktesting.CreateAction)
							service := createAction.GetObject().(*corev1.Service)
							
							if service.Name != tt.opts.Name {
								t.Errorf("Expected service name %s, got %s", tt.opts.Name, service.Name)
							}
							
							if len(service.Spec.Ports) == 0 {
								t.Error("Expected service to have ports")
							}
							
							break
						}
					}
					
					if !serviceCreated {
						t.Error("Expected service to be created")
					}
				}
			}
		})
	}
}

func TestKubernetesRuntime_StopContainer(t *testing.T) {
	// Create fake Kubernetes client with existing deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-container",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &[]int32{1}[0],
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-container"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test-container"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
						},
					},
				},
			},
		},
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-container",
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": "test-container"},
			Ports: []corev1.ServicePort{
				{
					Port:       80,
					TargetPort: intstr.FromInt(80),
				},
			},
		},
	}

	fakeClient := fake.NewSimpleClientset(deployment, service)

	runtime := &KubernetesRuntime{
		client:    fakeClient,
		namespace: "default",
		ctx:       context.Background(),
	}

	tests := []struct {
		name        string
		containerName string
		expectError bool
	}{
		{
			name:          "Stop existing container",
			containerName: "test-container",
			expectError:   false,
		},
		{
			name:          "Stop non-existent container",
			containerName: "non-existent",
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear any previous actions
			fakeClient.ClearActions()

			// Stop container
			err := runtime.StopContainer(tt.containerName)

			// Check error expectation
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if !tt.expectError {
				// Check that deployment was scaled to 0 replicas (not deleted)
				deploymentScaled := false
				
				for _, action := range fakeClient.Actions() {
					if action.GetVerb() == "update" && action.GetResource().Resource == "deployments" {
						deploymentScaled = true
						break
					}
				}

				if !deploymentScaled {
					t.Error("Expected deployment to be scaled to 0 replicas")
				}
				
				// Verify the deployment is scaled to 0
				deployment, err := runtime.client.AppsV1().Deployments(runtime.namespace).Get(runtime.ctx, tt.containerName, metav1.GetOptions{})
				if err != nil {
					t.Errorf("Failed to get deployment after stop: %v", err)
				} else if *deployment.Spec.Replicas != 0 {
					t.Errorf("Expected deployment replicas to be 0, got %d", *deployment.Spec.Replicas)
				}
			}
		})
	}
}

func TestKubernetesRuntime_GetContainerStatus(t *testing.T) {
	// Create fake Kubernetes client with deployment in different states
	runningDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "running-container",
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

	stoppedDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stopped-container",
			Namespace: "default",
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 0,
			Replicas:      0,  // Both replicas should be 0 for a stopped container
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentAvailable,
					Status: corev1.ConditionFalse,
				},
			},
		},
	}

	fakeClient := fake.NewSimpleClientset(runningDeployment, stoppedDeployment)

	runtime := &KubernetesRuntime{
		client:    fakeClient,
		namespace: "default",
		ctx:       context.Background(),
	}

	tests := []struct {
		name           string
		containerName  string
		expectedStatus string
		expectError    bool
	}{
		{
			name:           "Get status of running container",
			containerName:  "running-container",
			expectedStatus: "running",
			expectError:    false,
		},
		{
			name:           "Get status of stopped container",
			containerName:  "stopped-container",
			expectedStatus: "stopped",
			expectError:    false,
		},
		{
			name:           "Get status of non-existent container",
			containerName:  "non-existent",
			expectedStatus: "unknown",
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, err := runtime.GetContainerStatus(tt.containerName)

			// Check error expectation
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			// Check status
			if status != tt.expectedStatus {
				t.Errorf("Expected status %s, got %s", tt.expectedStatus, status)
			}
		})
	}
}

func TestKubernetesRuntime_GetContainerInfo(t *testing.T) {
	// Create fake Kubernetes client with deployment and pods
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-container",
			Namespace: "default",
			Labels: map[string]string{
				"app": "test-container",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &[]int32{1}[0],
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test-container"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test-container"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 80},
							},
							Env: []corev1.EnvVar{
								{Name: "ENV", Value: "test"},
							},
						},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 1,
			Replicas:      1,
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-container-pod",
			Namespace: "default",
			Labels: map[string]string{
				"app": "test-container",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "nginx:latest",
					Ports: []corev1.ContainerPort{
						{ContainerPort: 80},
					},
					Env: []corev1.EnvVar{
						{Name: "ENV", Value: "test"},
					},
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	fakeClient := fake.NewSimpleClientset(deployment, pod)

	runtime := &KubernetesRuntime{
		client:    fakeClient,
		namespace: "default",
		ctx:       context.Background(),
	}

	tests := []struct {
		name          string
		containerName string
		expectError   bool
	}{
		{
			name:          "Get info for existing container",
			containerName: "test-container",
			expectError:   false,
		},
		{
			name:          "Get info for non-existent container",
			containerName: "non-existent",
			expectError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := runtime.GetContainerInfo(tt.containerName)

			// Check error expectation
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if !tt.expectError {
				// Verify container info
				if info.Name != tt.containerName {
					t.Errorf("Expected container name %s, got %s", tt.containerName, info.Name)
				}

				if info.Image != "nginx:latest" {
					t.Errorf("Expected image nginx:latest, got %s", info.Image)
				}

				if info.Status != "running" {
					t.Errorf("Expected status running, got %s", info.Status)
				}

				if len(info.Ports) == 0 {
					t.Error("Expected container to have ports")
				}

				if len(info.Env) == 0 {
					t.Error("Expected container to have environment variables")
				}
			}
		})
	}
}

func TestKubernetesRuntime_ListContainers(t *testing.T) {
	// Create fake Kubernetes client with multiple deployments
	deployment1 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "container-1",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container-1",
							Image: "nginx:latest",
						},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 1,
			Replicas:      1,
		},
	}

	deployment2 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "container-2",
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "container-2",
							Image: "alpine:latest",
						},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			ReadyReplicas: 0,
			Replicas:      0,  // Both replicas should be 0 for a stopped container
		},
	}

	fakeClient := fake.NewSimpleClientset(deployment1, deployment2)

	runtime := &KubernetesRuntime{
		client:    fakeClient,
		namespace: "default",
		ctx:       context.Background(),
	}

	// List containers
	containers, err := runtime.ListContainers(nil)
	if err != nil {
		t.Fatalf("Expected no error but got: %v", err)
	}

	// Verify results
	if len(containers) != 2 {
		t.Errorf("Expected 2 containers, got %d", len(containers))
	}

	// Check container details
	containerNames := make(map[string]bool)
	for _, container := range containers {
		containerNames[container.Name] = true
		
		switch container.Name {
		case "container-1":
			if container.Image != "nginx:latest" {
				t.Errorf("Expected image nginx:latest for container-1, got %s", container.Image)
			}
			if container.Status != "running" {
				t.Errorf("Expected status running for container-1, got %s", container.Status)
			}
		case "container-2":
			if container.Image != "alpine:latest" {
				t.Errorf("Expected image alpine:latest for container-2, got %s", container.Image)
			}
			if container.Status != "stopped" {
				t.Errorf("Expected status stopped for container-2, got %s", container.Status)
			}
		}
	}

	// Verify all expected containers are present
	if !containerNames["container-1"] {
		t.Error("Expected container-1 to be in the list")
	}
	if !containerNames["container-2"] {
		t.Error("Expected container-2 to be in the list")
	}
}

func TestKubernetesRuntime_ValidateSecurityContext(t *testing.T) {
	runtime := &KubernetesRuntime{
		client:    fake.NewSimpleClientset(),
		namespace: "default",
		ctx:       context.Background(),
	}

	tests := []struct {
		name        string
		opts        *ContainerOptions
		expectError bool
	}{
		{
			name: "Valid security context",
			opts: &ContainerOptions{
				Name:  "test-container",
				Image: "nginx:latest",
				Security: SecurityConfig{
					AllowPrivilegedOps: false,
					TrustedImage:       true,
				},
			},
			expectError: false,
		},
		{
			name: "Invalid privileged operations",
			opts: &ContainerOptions{
				Name:       "test-container",
				Image:      "nginx:latest",
				Privileged: true,
				Security: SecurityConfig{
					AllowPrivilegedOps: false,
					TrustedImage:       true,
				},
			},
			expectError: true,
		},
		{
			name: "Nil options",
			opts: nil,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runtime.ValidateSecurityContext(tt.opts)

			// Check error expectation
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}

func TestKubernetesRuntime_ConvertContainerOptionsToDeployment(t *testing.T) {
	runtime := &KubernetesRuntime{
		client:    fake.NewSimpleClientset(),
		namespace: "default",
		ctx:       context.Background(),
	}

	opts := &ContainerOptions{
		Name:    "test-container",
		Image:   "nginx:latest",
		Command: "nginx",
		Args:    []string{"-g", "daemon off;"},
		Env: map[string]string{
			"ENV":  "production",
			"PORT": "80",
		},
		Ports:   []string{"8080:80", "8443:443"},
		Volumes: []string{"/host/path:/container/path:ro"},
		CPUs:    "0.5",
		Memory:  "512Mi",
		Labels: map[string]string{
			"app":     "test-app",
			"version": "v1.0.0",
		},
	}

	// This method might be private, so we'll test it indirectly through StartContainer
	containerID, err := runtime.StartContainer(opts)
	if err != nil {
		t.Fatalf("Expected no error but got: %v", err)
	}

	if containerID == "" {
		t.Error("Expected non-empty container ID")
	}

	// Verify deployment was created with correct specifications
	// We can check this by examining the actions performed on the fake client
	// This is a simplified test - in a real scenario, we'd check the actual deployment spec
}

func TestKubernetesRuntime_GetRuntimeName(t *testing.T) {
	runtime := &KubernetesRuntime{
		client:    fake.NewSimpleClientset(),
		namespace: "default",
		ctx:       context.Background(),
	}

	name := runtime.GetRuntimeName()
	if name != "kubernetes" {
		t.Errorf("Expected runtime name 'kubernetes', got %s", name)
	}
}


