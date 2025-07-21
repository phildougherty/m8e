package controllers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/logging"
)

func TestMCPTaskSchedulerReconciler_EventTriggers(t *testing.T) {
	// Create a fake client
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))
	
	client := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects().
		Build()

	logger := logging.NewLogger("test")
	reconciler := &MCPTaskSchedulerReconciler{
		Client:            client,
		Scheme:            s,
		Logger:            logger,
		disableEventWatch: true, // Disable event watching for tests to avoid race conditions
	}

	t.Run("event triggers configuration", func(t *testing.T) {
		taskScheduler := &crd.MCPTaskScheduler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scheduler",
				Namespace: "default",
			},
			Spec: crd.MCPTaskSchedulerSpec{
				Image: "mcpcompose/task-scheduler:latest",
				Port:  8084,
				Host:  "0.0.0.0",
				SchedulerConfig: crd.TaskSchedulerConfig{
					EventTriggers: []crd.EventTrigger{
						{
							Name:     "pod-failure-handler",
							Type:     "k8s-event",
							Workflow: "incident-response-workflow",
							KubernetesEvent: &crd.KubernetesEventConfig{
								Kind:      "Pod",
								Reason:    "Failed",
								Namespace: "production",
							},
							CooldownDuration: "60s",
						},
					},
				},
			},
		}

		// Test event trigger configuration generation
		config := reconciler.generateTaskSchedulerConfig(taskScheduler)
		assert.Contains(t, config, "event_triggers:")
		assert.Contains(t, config, "enabled: true")
		assert.Contains(t, config, "pod-failure-handler")
		assert.Contains(t, config, "k8s-event")
		assert.Contains(t, config, "incident-response-workflow")
	})

	t.Run("event trigger matching", func(t *testing.T) {
		trigger := crd.EventTrigger{
			Name:     "pod-failure-handler",
			Type:     "k8s-event",
			Workflow: "incident-response-workflow",
			KubernetesEvent: &crd.KubernetesEventConfig{
				Kind:      "Pod",
				Reason:    "Failed",
				Namespace: "production",
			},
			Conditions: []crd.TriggerCondition{
				{
					Field:    "message",
					Operator: "contains",
					Value:    "ImagePullBackOff",
				},
			},
		}

		// Test matching event
		matchingEvent := &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-event",
				Namespace: "production",
			},
			InvolvedObject: corev1.ObjectReference{
				Kind: "Pod",
			},
			Reason:  "Failed",
			Message: "Pod failed due to ImagePullBackOff",
		}

		assert.True(t, reconciler.matchesEventTrigger(matchingEvent, trigger))

		// Test non-matching event
		nonMatchingEvent := &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-event",
				Namespace: "production",
			},
			InvolvedObject: corev1.ObjectReference{
				Kind: "Pod",
			},
			Reason:  "Failed",
			Message: "Pod failed due to OOMKilled",
		}

		assert.False(t, reconciler.matchesEventTrigger(nonMatchingEvent, trigger))
	})

	t.Run("condition evaluation", func(t *testing.T) {
		event := &corev1.Event{
			Reason:  "Failed",
			Message: "Pod failed due to ImagePullBackOff",
			Type:    "Warning",
		}

		tests := []struct {
			condition crd.TriggerCondition
			expected  bool
		}{
			{
				condition: crd.TriggerCondition{
					Field:    "reason",
					Operator: "equals",
					Value:    "Failed",
				},
				expected: true,
			},
			{
				condition: crd.TriggerCondition{
					Field:    "message",
					Operator: "contains",
					Value:    "ImagePullBackOff",
				},
				expected: true,
			},
			{
				condition: crd.TriggerCondition{
					Field:    "message",
					Operator: "startsWith",
					Value:    "Pod failed",
				},
				expected: true,
			},
			{
				condition: crd.TriggerCondition{
					Field:    "type",
					Operator: "equals",
					Value:    "Normal",
				},
				expected: false,
			},
		}

		for _, tt := range tests {
			result := reconciler.evaluateCondition(event, tt.condition)
			assert.Equal(t, tt.expected, result, "Condition evaluation failed for %+v", tt.condition)
		}
	})
}

func TestMCPTaskSchedulerReconciler_AutoScaling(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects().
		Build()

	logger := logging.NewLogger("test")
	reconciler := &MCPTaskSchedulerReconciler{
		Client:            client,
		Scheme:            s,
		Logger:            logger,
		disableEventWatch: true, // Disable event watching for tests to avoid race conditions
	}

	t.Run("auto-scaling configuration", func(t *testing.T) {
		taskScheduler := &crd.MCPTaskScheduler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scheduler",
				Namespace: "default",
			},
			Spec: crd.MCPTaskSchedulerSpec{
				Image: "mcpcompose/task-scheduler:latest",
				Port:  8084,
				Host:  "0.0.0.0",
				SchedulerConfig: crd.TaskSchedulerConfig{
					AutoScaling: crd.AutoScalingConfig{
						Enabled:                   true,
						MinConcurrentTasks:        2,
						MaxConcurrentTasks:        50,
						TargetCPUUtilization:      70,
						TargetMemoryUtilization:   80,
						ScaleUpCooldown:          "30s",
						ScaleDownCooldown:        "300s",
						MetricsInterval:          "15s",
					},
				},
			},
		}

		config := reconciler.generateTaskSchedulerConfig(taskScheduler)
		assert.Contains(t, config, "auto_scaling:")
		assert.Contains(t, config, "enabled: true")
		assert.Contains(t, config, "min_concurrent_tasks: 2")
		assert.Contains(t, config, "max_concurrent_tasks: 50")
		assert.Contains(t, config, "target_cpu_utilization: 70")
	})

	t.Run("desired concurrency calculation", func(t *testing.T) {
		taskScheduler := &crd.MCPTaskScheduler{
			Spec: crd.MCPTaskSchedulerSpec{
				SchedulerConfig: crd.TaskSchedulerConfig{
					MaxConcurrentTasks: 10,
					AutoScaling: crd.AutoScalingConfig{
						MinConcurrentTasks:   2,
						MaxConcurrentTasks:   50,
						TargetCPUUtilization: 70,
					},
				},
			},
		}

		tests := []struct {
			name         string
			currentTasks int32
			expected     int32
		}{
			{
				name:         "high utilization - scale up",
				currentTasks: 9, // 90% utilization
				expected:     12, // 10 + 25% = 12.5 -> 12
			},
			{
				name:         "low utilization - scale down",
				currentTasks: 2, // 20% utilization (< 35%)
				expected:     7, // 10 - 25% = 7.5 -> 7
			},
			{
				name:         "normal utilization - no change",
				currentTasks: 6, // 60% utilization
				expected:     10, // no change
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result := reconciler.calculateDesiredConcurrency(taskScheduler, tt.currentTasks)
				assert.Equal(t, tt.expected, result)
			})
		}
	})

	t.Run("auto-scaling bounds", func(t *testing.T) {
		taskScheduler := &crd.MCPTaskScheduler{
			Spec: crd.MCPTaskSchedulerSpec{
				SchedulerConfig: crd.TaskSchedulerConfig{
					MaxConcurrentTasks: 4,
					AutoScaling: crd.AutoScalingConfig{
						MinConcurrentTasks:   2,
						MaxConcurrentTasks:   6,
						TargetCPUUtilization: 70,
					},
				},
			},
		}

		// Test minimum bound
		result := reconciler.calculateDesiredConcurrency(taskScheduler, 0)
		assert.Equal(t, int32(2), result, "Should respect minimum bound")

		// Test maximum bound
		taskScheduler.Spec.SchedulerConfig.MaxConcurrentTasks = 10
		result = reconciler.calculateDesiredConcurrency(taskScheduler, 10)
		assert.Equal(t, int32(6), result, "Should respect maximum bound")
	})
}

func TestMCPTaskSchedulerReconciler_ConditionalDependencies(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects().
		Build()

	logger := logging.NewLogger("test")
	reconciler := &MCPTaskSchedulerReconciler{
		Client:            client,
		Scheme:            s,
		Logger:            logger,
		disableEventWatch: true, // Disable event watching for tests to avoid race conditions
	}

	t.Run("conditional dependencies configuration", func(t *testing.T) {
		taskScheduler := &crd.MCPTaskScheduler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scheduler",
				Namespace: "default",
			},
			Spec: crd.MCPTaskSchedulerSpec{
				Image: "mcpcompose/task-scheduler:latest",
				Port:  8084,
				Host:  "0.0.0.0",
				SchedulerConfig: crd.TaskSchedulerConfig{
					ConditionalDependencies: crd.ConditionalDependencyConfig{
						Enabled:              true,
						DefaultStrategy:      "fail-fast",
						ResolutionTimeout:    "10m",
						CrossWorkflowEnabled: true,
					},
				},
			},
		}

		config := reconciler.generateTaskSchedulerConfig(taskScheduler)
		assert.Contains(t, config, "conditional_dependencies:")
		assert.Contains(t, config, "enabled: true")
		assert.Contains(t, config, "default_strategy: fail-fast")
		assert.Contains(t, config, "resolution_timeout: 10m")
		assert.Contains(t, config, "cross_workflow_enabled: true")
	})
}

func TestMCPTaskSchedulerReconciler_WorkflowTrigger(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects().
		Build()

	logger := logging.NewLogger("test")
	reconciler := &MCPTaskSchedulerReconciler{
		Client:            client,
		Scheme:            s,
		Logger:            logger,
		disableEventWatch: true, // Disable event watching for tests to avoid race conditions
	}

	t.Run("workflow trigger job creation", func(t *testing.T) {
		ctx := context.Background()
		
		taskScheduler := &crd.MCPTaskScheduler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scheduler",
				Namespace: "default",
			},
			Spec: crd.MCPTaskSchedulerSpec{
				Image: "mcpcompose/task-scheduler:latest",
				Port:  8084,
				Host:  "0.0.0.0",
			},
		}

		trigger := crd.EventTrigger{
			Name:     "test-trigger",
			Type:     "k8s-event",
			Workflow: "test-workflow",
		}

		event := &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-event",
				Namespace: "default",
			},
			Reason: "Failed",
		}

		// Create the task scheduler in the fake client
		require.NoError(t, client.Create(ctx, taskScheduler))

		// Trigger the workflow
		err := reconciler.triggerWorkflow(ctx, taskScheduler, trigger, event)
		require.NoError(t, err)

		// Verify that a job was created
		jobs := &batchv1.JobList{}
		err = client.List(ctx, jobs)
		require.NoError(t, err)
		assert.Len(t, jobs.Items, 1)

		job := jobs.Items[0]
		assert.Contains(t, job.Name, "test-scheduler-test-trigger")
		assert.Equal(t, "default", job.Namespace)
		assert.Equal(t, "test-scheduler", job.Labels["mcp.matey.ai/scheduler"])
		assert.Equal(t, "test-trigger", job.Labels["mcp.matey.ai/trigger"])
		assert.Equal(t, "test-workflow", job.Labels["mcp.matey.ai/workflow"])

		// Verify job command
		container := job.Spec.Template.Spec.Containers[0]
		assert.Equal(t, "workflow-trigger", container.Name)
		assert.Contains(t, container.Command, "/app/trigger-workflow")
		assert.Contains(t, container.Command, "--workflow")
		assert.Contains(t, container.Command, "test-workflow")
	})
}

func TestMCPTaskSchedulerReconciler_EventTriggersConfig(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects().
		Build()

	logger := logging.NewLogger("test")
	reconciler := &MCPTaskSchedulerReconciler{
		Client:            client,
		Scheme:            s,
		Logger:            logger,
		disableEventWatch: true, // Disable event watching for tests to avoid race conditions
	}

	t.Run("event triggers config generation", func(t *testing.T) {
		triggers := []crd.EventTrigger{
			{
				Name:     "pod-failure-handler",
				Type:     "k8s-event",
				Workflow: "incident-response-workflow",
				KubernetesEvent: &crd.KubernetesEventConfig{
					Kind:          "Pod",
					Reason:        "Failed",
					Namespace:     "production",
					LabelSelector: "app=critical",
					FieldSelector: "involvedObject.name=webapp",
				},
				CooldownDuration: "60s",
			},
			{
				Name:     "deployment-rollback",
				Type:     "k8s-event",
				Workflow: "rollback-deployment",
				KubernetesEvent: &crd.KubernetesEventConfig{
					Kind:      "Deployment",
					Reason:    "ProgressDeadlineExceeded",
					Namespace: "production",
				},
				CooldownDuration: "120s",
			},
		}

		config := reconciler.generateEventTriggersConfig(triggers)
		
		// Verify the generated config contains expected elements
		assert.Contains(t, config, "pod-failure-handler")
		assert.Contains(t, config, "k8s-event")
		assert.Contains(t, config, "incident-response-workflow")
		assert.Contains(t, config, "60s")
		assert.Contains(t, config, "Pod")
		assert.Contains(t, config, "Failed")
		assert.Contains(t, config, "production")
		assert.Contains(t, config, "app=critical")
		assert.Contains(t, config, "involvedObject.name=webapp")
		
		assert.Contains(t, config, "deployment-rollback")
		assert.Contains(t, config, "rollback-deployment")
		assert.Contains(t, config, "120s")
		assert.Contains(t, config, "Deployment")
		assert.Contains(t, config, "ProgressDeadlineExceeded")
	})

	t.Run("empty event triggers config", func(t *testing.T) {
		config := reconciler.generateEventTriggersConfig([]crd.EventTrigger{})
		assert.Equal(t, "[]", config)
	})
}

func TestMCPTaskSchedulerReconciler_EnhancedReconciliation(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	taskScheduler := &crd.MCPTaskScheduler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "enhanced-scheduler",
			Namespace: "default",
		},
		Spec: crd.MCPTaskSchedulerSpec{
			Image: "mcpcompose/task-scheduler:latest",
			Port:  8084,
			Host:  "0.0.0.0",
			SchedulerConfig: crd.TaskSchedulerConfig{
				DefaultTimeout:     "5m",
				MaxConcurrentTasks: 10,
				EventTriggers: []crd.EventTrigger{
					{
						Name:     "pod-failure-handler",
						Type:     "k8s-event",
						Workflow: "incident-response-workflow",
						KubernetesEvent: &crd.KubernetesEventConfig{
							Kind:      "Pod",
							Reason:    "Failed",
							Namespace: "production",
						},
						CooldownDuration: "60s",
					},
				},
				ConditionalDependencies: crd.ConditionalDependencyConfig{
					Enabled:              true,
					DefaultStrategy:      "fail-fast",
					ResolutionTimeout:    "10m",
					CrossWorkflowEnabled: true,
				},
				AutoScaling: crd.AutoScalingConfig{
					Enabled:                   true,
					MinConcurrentTasks:        2,
					MaxConcurrentTasks:        50,
					TargetCPUUtilization:      70,
					TargetMemoryUtilization:   80,
					ScaleUpCooldown:          "30s",
					ScaleDownCooldown:        "300s",
					MetricsInterval:          "15s",
				},
			},
		},
	}

	// Create deployment to simulate running scheduler
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "enhanced-scheduler",
			Namespace: "default",
		},
		Status: appsv1.DeploymentStatus{
			Replicas:      1,
			ReadyReplicas: 1,
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(taskScheduler, deployment).
		WithStatusSubresource(&crd.MCPTaskScheduler{}).
		Build()

	logger := logging.NewLogger("test")
	reconciler := &MCPTaskSchedulerReconciler{
		Client:            client,
		Scheme:            s,
		Logger:            logger,
		disableEventWatch: true, // Disable event watching for tests to avoid race conditions
	}

	t.Run("enhanced reconciliation with all features", func(t *testing.T) {
		ctx := context.Background()
		
		// Set the task scheduler to running phase
		taskScheduler.Status.Phase = crd.MCPTaskSchedulerPhaseRunning
		require.NoError(t, client.Status().Update(ctx, taskScheduler))

		// Perform reconciliation
		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      "enhanced-scheduler",
				Namespace: "default",
			},
		}

		result, err := reconciler.Reconcile(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, time.Minute*2, result.RequeueAfter)

		// Verify that the ConfigMap was created/updated with enhanced config
		configMap := &corev1.ConfigMap{}
		err = client.Get(ctx, types.NamespacedName{
			Name:      "enhanced-scheduler-config",
			Namespace: "default",
		}, configMap)
		require.NoError(t, err)

		config := configMap.Data["matey.yaml"]
		assert.Contains(t, config, "event_triggers:")
		assert.Contains(t, config, "conditional_dependencies:")
		assert.Contains(t, config, "auto_scaling:")
		assert.Contains(t, config, "enabled: true")
	})
}

func TestMCPTaskSchedulerReconciler_EventWatcherManagement(t *testing.T) {
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects().
		Build()

	logger := logging.NewLogger("test")
	reconciler := &MCPTaskSchedulerReconciler{
		Client:            client,
		Scheme:            s,
		Logger:            logger,
		disableEventWatch: true, // Disable event watching for tests to avoid race conditions
	}

	t.Run("event watcher setup and cleanup", func(t *testing.T) {
		// Create a separate reconciler with event watching enabled for this test
		eventReconciler := &MCPTaskSchedulerReconciler{
			Client:            client,
			Scheme:            s,
			Logger:            logger,
			disableEventWatch: false, // Enable event watching for this specific test
		}
		ctx := context.Background()
		
		taskScheduler := &crd.MCPTaskScheduler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scheduler",
				Namespace: "default",
			},
			Spec: crd.MCPTaskSchedulerSpec{
				SchedulerConfig: crd.TaskSchedulerConfig{
					EventTriggers: []crd.EventTrigger{
						{
							Name:     "pod-failure-handler",
							Type:     "k8s-event",
							Workflow: "incident-response-workflow",
							KubernetesEvent: &crd.KubernetesEventConfig{
								Kind:      "Pod",
								Reason:    "Failed",
								Namespace: "production",
							},
							CooldownDuration: "60s",
						},
					},
				},
			},
		}

		// Setup event watching
		err := eventReconciler.setupEventWatching(ctx, taskScheduler)
		require.NoError(t, err)

		// Verify that the event watcher was created
		key := "default/test-scheduler"
		assert.Contains(t, eventReconciler.eventWatchers, key)

		// Test cleanup during termination
		_, err = eventReconciler.reconcileTerminating(ctx, taskScheduler)
		require.NoError(t, err)

		// Verify that the event watcher was cleaned up
		assert.NotContains(t, eventReconciler.eventWatchers, key)
	})

	t.Run("event watcher with no triggers", func(t *testing.T) {
		ctx := context.Background()
		
		taskScheduler := &crd.MCPTaskScheduler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scheduler",
				Namespace: "default",
			},
			Spec: crd.MCPTaskSchedulerSpec{
				SchedulerConfig: crd.TaskSchedulerConfig{
					EventTriggers: []crd.EventTrigger{},
				},
			},
		}

		// Setup event watching with no triggers
		err := reconciler.setupEventWatching(ctx, taskScheduler)
		require.NoError(t, err)

		// Verify that no event watcher was created
		key := "default/test-scheduler"
		assert.NotContains(t, reconciler.eventWatchers, key)
	})
}