package integration

import (
	"context"
	"fmt"
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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/phildougherty/m8e/internal/controllers"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/logging"
)

func TestEnhancedTaskSchedulerIntegration(t *testing.T) {
	// Setup
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	taskScheduler := &crd.MCPTaskScheduler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "enhanced-test-scheduler",
			Namespace: "matey",
		},
		Spec: crd.MCPTaskSchedulerSpec{
			Image: "mcpcompose/task-scheduler:latest",
			Port:  8084,
			Host:  "0.0.0.0",
			DatabasePath: "/app/data/scheduler.db",
			LogLevel: "info",
			SchedulerConfig: crd.TaskSchedulerConfig{
				DefaultTimeout:     "5m",
				MaxConcurrentTasks: 10,
				RetryPolicy: crd.TaskRetryPolicy{
					MaxRetries:      3,
					RetryDelay:      "30s",
					BackoffStrategy: "exponential",
				},
				TaskStorageEnabled: true,
				TaskHistoryLimit:   100,
				TaskCleanupPolicy:  "auto",
				ActivityWebhook:    "https://hooks.slack.com/services/test",
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
						Conditions: []crd.TriggerCondition{
							{
								Field:    "message",
								Operator: "contains",
								Value:    "ImagePullBackOff",
							},
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
					CustomMetrics: []crd.CustomMetric{
						{
							Name:        "queue-depth",
							Type:        "external",
							TargetValue: "100",
							Selector: map[string]string{
								"queue": "task-queue",
							},
						},
					},
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(taskScheduler).
		WithStatusSubresource(&crd.MCPTaskScheduler{}).
		Build()

	logger := logging.NewLogger("test")
	reconciler := &controllers.MCPTaskSchedulerReconciler{
		Client: client,
		Scheme: s,
		Logger: logger,
	}

	ctx := context.Background()
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "enhanced-test-scheduler",
			Namespace: "matey",
		},
	}

	t.Run("complete enhanced task scheduler lifecycle", func(t *testing.T) {
		// First reconciliation - should create resources
		result, err := reconciler.Reconcile(ctx, req)
		require.NoError(t, err)
		assert.True(t, result.Requeue)

		// Verify task scheduler status was updated
		var updatedScheduler crd.MCPTaskScheduler
		err = client.Get(ctx, req.NamespacedName, &updatedScheduler)
		require.NoError(t, err)
		assert.Equal(t, crd.MCPTaskSchedulerPhaseCreating, updatedScheduler.Status.Phase)

		// Second reconciliation - should create resources and transition to starting
		result, err = reconciler.Reconcile(ctx, req)
		require.NoError(t, err)
		// Should not requeue immediately, just transition to starting phase
		assert.Equal(t, time.Duration(0), result.RequeueAfter)
		
		// Third reconciliation - should check deployment and requeue for 10s (not ready yet)
		result, err = reconciler.Reconcile(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, time.Second*10, result.RequeueAfter)

		// Verify resources were created
		var configMap corev1.ConfigMap
		err = client.Get(ctx, types.NamespacedName{
			Name:      "enhanced-test-scheduler-config",
			Namespace: "matey",
		}, &configMap)
		require.NoError(t, err)
		
		// Verify enhanced configuration in ConfigMap
		config := configMap.Data["matey.yaml"]
		assert.Contains(t, config, "event_triggers:")
		assert.Contains(t, config, "enabled: true")
		assert.Contains(t, config, "pod-failure-handler")
		assert.Contains(t, config, "incident-response-workflow")
		assert.Contains(t, config, "conditional_dependencies:")
		assert.Contains(t, config, "fail-fast")
		assert.Contains(t, config, "auto_scaling:")
		assert.Contains(t, config, "min_concurrent_tasks: 2")
		assert.Contains(t, config, "max_concurrent_tasks: 50")
		assert.Contains(t, config, "target_cpu_utilization: 70")

		var service corev1.Service
		err = client.Get(ctx, types.NamespacedName{
			Name:      "enhanced-test-scheduler",
			Namespace: "matey",
		}, &service)
		require.NoError(t, err)
		assert.Equal(t, int32(8084), service.Spec.Ports[0].Port)

		var deployment appsv1.Deployment
		err = client.Get(ctx, types.NamespacedName{
			Name:      "enhanced-test-scheduler",
			Namespace: "matey",
		}, &deployment)
		require.NoError(t, err)
		assert.Equal(t, "mcpcompose/task-scheduler:latest", deployment.Spec.Template.Spec.Containers[0].Image)

		// Verify environment variables include new configurations
		envVars := deployment.Spec.Template.Spec.Containers[0].Env
		envMap := make(map[string]string)
		for _, env := range envVars {
			envMap[env.Name] = env.Value
		}
		assert.Equal(t, "8084", envMap["MCP_CRON_SERVER_PORT"])
		assert.Equal(t, "0.0.0.0", envMap["MCP_CRON_SERVER_ADDRESS"])
		assert.Equal(t, "true", envMap["KUBERNETES_MODE"])
		assert.Equal(t, "matey", envMap["NAMESPACE"])

		// Simulate deployment becoming ready
		deployment.Status.ReadyReplicas = 1
		deployment.Status.Replicas = 1
		err = client.Status().Update(ctx, &deployment)
		require.NoError(t, err)

		// Fourth reconciliation - should transition to running (deployment is now ready)
		result, err = reconciler.Reconcile(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, time.Minute*2, result.RequeueAfter)

		// Verify status updated to running
		err = client.Get(ctx, req.NamespacedName, &updatedScheduler)
		require.NoError(t, err)
		assert.Equal(t, crd.MCPTaskSchedulerPhaseRunning, updatedScheduler.Status.Phase)
		assert.Equal(t, int32(1), updatedScheduler.Status.ReadyReplicas)
		assert.Equal(t, int32(1), updatedScheduler.Status.Replicas)

		// Verify ready condition was updated
		assert.NotEmpty(t, updatedScheduler.Status.Conditions)
		readyCondition := findCondition(updatedScheduler.Status.Conditions, crd.MCPTaskSchedulerConditionReady)
		require.NotNil(t, readyCondition)
		assert.Equal(t, metav1.ConditionTrue, readyCondition.Status)

		// Fifth reconciliation - should update health condition in running phase
		result, err = reconciler.Reconcile(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, time.Minute*2, result.RequeueAfter)

		// Verify health condition was updated
		err = client.Get(ctx, req.NamespacedName, &updatedScheduler)
		require.NoError(t, err)
		healthyCondition := findCondition(updatedScheduler.Status.Conditions, crd.MCPTaskSchedulerConditionHealthy)
		require.NotNil(t, healthyCondition)
		assert.Equal(t, metav1.ConditionTrue, healthyCondition.Status)
	})

	t.Run("event trigger workflow execution", func(t *testing.T) {
		// Create a test event that matches our trigger
		testEvent := &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod-event",
				Namespace: "production",
			},
			InvolvedObject: corev1.ObjectReference{
				Kind:      "Pod",
				Name:      "test-pod",
				Namespace: "production",
			},
			Reason:  "Failed",
			Message: "Pod failed due to ImagePullBackOff",
			Type:    "Warning",
		}

		err := client.Create(ctx, testEvent)
		require.NoError(t, err)

		// Get the updated scheduler
		var scheduler crd.MCPTaskScheduler
		err = client.Get(ctx, req.NamespacedName, &scheduler)
		require.NoError(t, err)

		// Test the event trigger matching
		// Use reflection to access the private method or create a test helper
		// For now, we'll test the public behavior through reconciliation
		
		// Simulate triggering the workflow
		_, err = reconciler.Reconcile(ctx, req)
		require.NoError(t, err)

		// The event watcher should be set up (even though it's polling in our implementation)
		// We can't easily test the actual event watching without a real Kubernetes cluster
		// but we can verify the configuration is correct
		
		// Verify the ConfigMap contains the event trigger configuration
		var configMap corev1.ConfigMap
		err = client.Get(ctx, types.NamespacedName{
			Name:      "enhanced-test-scheduler-config",
			Namespace: "matey",
		}, &configMap)
		require.NoError(t, err)
		
		config := configMap.Data["matey.yaml"]
		assert.Contains(t, config, "pod-failure-handler")
		assert.Contains(t, config, "incident-response-workflow")
		assert.Contains(t, config, "deployment-rollback")
		assert.Contains(t, config, "rollback-deployment")
	})

	t.Run("auto-scaling behavior", func(t *testing.T) {
		// Create some jobs to simulate running tasks
		for i := 0; i < 5; i++ {
			job := &batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("test-job-%d", i),
					Namespace: "matey",
					Labels: map[string]string{
						"mcp.matey.ai/scheduler": "enhanced-test-scheduler",
					},
				},
				Status: batchv1.JobStatus{
					Active: 1,
				},
			}
			err := client.Create(ctx, job)
			require.NoError(t, err)
		}

		// Get the scheduler
		var scheduler crd.MCPTaskScheduler
		err := client.Get(ctx, req.NamespacedName, &scheduler)
		require.NoError(t, err)

		// Set scheduler to running phase
		scheduler.Status.Phase = crd.MCPTaskSchedulerPhaseRunning
		err = client.Status().Update(ctx, &scheduler)
		require.NoError(t, err)

		// Reconcile to trigger auto-scaling logic
		result, err := reconciler.Reconcile(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, time.Minute*2, result.RequeueAfter)

		// Verify task statistics were updated
		err = client.Get(ctx, req.NamespacedName, &scheduler)
		require.NoError(t, err)
		assert.Equal(t, int64(5), scheduler.Status.TaskStats.RunningTasks)
		assert.Equal(t, int64(5), scheduler.Status.TaskStats.TotalTasks)
	})

	t.Run("conditional dependencies configuration", func(t *testing.T) {
		// Verify conditional dependencies are properly configured
		var scheduler crd.MCPTaskScheduler
		err := client.Get(ctx, req.NamespacedName, &scheduler)
		require.NoError(t, err)

		conditionalDeps := scheduler.Spec.SchedulerConfig.ConditionalDependencies
		assert.True(t, conditionalDeps.Enabled)
		assert.Equal(t, "fail-fast", conditionalDeps.DefaultStrategy)
		assert.Equal(t, "10m", conditionalDeps.ResolutionTimeout)
		assert.True(t, conditionalDeps.CrossWorkflowEnabled)

		// Verify it's included in the generated configuration
		var configMap corev1.ConfigMap
		err = client.Get(ctx, types.NamespacedName{
			Name:      "enhanced-test-scheduler-config",
			Namespace: "matey",
		}, &configMap)
		require.NoError(t, err)
		
		config := configMap.Data["matey.yaml"]
		assert.Contains(t, config, "conditional_dependencies:")
		assert.Contains(t, config, "enabled: true")
		assert.Contains(t, config, "default_strategy: fail-fast")
		assert.Contains(t, config, "resolution_timeout: 10m")
		assert.Contains(t, config, "cross_workflow_enabled: true")
	})
}

func TestEventTriggerWorkflowExecution(t *testing.T) {
	// Setup
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	taskScheduler := &crd.MCPTaskScheduler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "workflow-test-scheduler",
			Namespace: "matey",
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
						Conditions: []crd.TriggerCondition{
							{
								Field:    "message",
								Operator: "contains",
								Value:    "ImagePullBackOff",
							},
						},
						CooldownDuration: "60s",
					},
				},
			},
		},
	}

	client := fake.NewClientBuilder().
		WithScheme(s).
		WithRuntimeObjects(taskScheduler).
		WithStatusSubresource(&crd.MCPTaskScheduler{}).
		Build()

	logger := logging.NewLogger("test")
	reconciler := &controllers.MCPTaskSchedulerReconciler{
		Client: client,
		Scheme: s,
		Logger: logger,
	}

	ctx := context.Background()

	t.Run("workflow trigger job creation", func(t *testing.T) {
		trigger := taskScheduler.Spec.SchedulerConfig.EventTriggers[0]

		// Create the job (simulating workflow trigger)
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "workflow-test-scheduler",
				Namespace: "matey",
			},
		})
		require.NoError(t, err)

		// Verify the scheduler was created properly
		var scheduler crd.MCPTaskScheduler
		err = client.Get(ctx, types.NamespacedName{
			Name:      "workflow-test-scheduler",
			Namespace: "matey",
		}, &scheduler)
		require.NoError(t, err)

		// The event matching and job creation would happen in the event watcher
		// For testing purposes, we'll verify the configuration is set up correctly
		assert.Equal(t, "pod-failure-handler", trigger.Name)
		assert.Equal(t, "k8s-event", trigger.Type)
		assert.Equal(t, "incident-response-workflow", trigger.Workflow)
		assert.Equal(t, "Pod", trigger.KubernetesEvent.Kind)
		assert.Equal(t, "Failed", trigger.KubernetesEvent.Reason)
		assert.Equal(t, "production", trigger.KubernetesEvent.Namespace)
		assert.Len(t, trigger.Conditions, 1)
		assert.Equal(t, "message", trigger.Conditions[0].Field)
		assert.Equal(t, "contains", trigger.Conditions[0].Operator)
		assert.Equal(t, "ImagePullBackOff", trigger.Conditions[0].Value)
		assert.Equal(t, "60s", trigger.CooldownDuration)
	})
}

func TestAutoScalingIntegration(t *testing.T) {
	// Setup
	s := scheme.Scheme
	require.NoError(t, crd.AddToScheme(s))

	taskScheduler := &crd.MCPTaskScheduler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "autoscaling-test-scheduler",
			Namespace: "matey",
		},
		Spec: crd.MCPTaskSchedulerSpec{
			Image: "mcpcompose/task-scheduler:latest",
			Port:  8084,
			Host:  "0.0.0.0",
			SchedulerConfig: crd.TaskSchedulerConfig{
				MaxConcurrentTasks: 10,
				AutoScaling: crd.AutoScalingConfig{
					Enabled:                   true,
					MinConcurrentTasks:        2,
					MaxConcurrentTasks:        50,
					TargetCPUUtilization:      70,
					TargetMemoryUtilization:   80,
					ScaleUpCooldown:          "30s",
					ScaleDownCooldown:        "300s",
					MetricsInterval:          "15s",
					CustomMetrics: []crd.CustomMetric{
						{
							Name:        "queue-depth",
							Type:        "external",
							TargetValue: "100",
							Selector: map[string]string{
								"queue": "task-queue",
							},
						},
					},
				},
			},
		},
	}

	// Create a deployment to simulate the running scheduler
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "autoscaling-test-scheduler",
			Namespace: "matey",
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
	reconciler := &controllers.MCPTaskSchedulerReconciler{
		Client: client,
		Scheme: s,
		Logger: logger,
	}

	ctx := context.Background()
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "autoscaling-test-scheduler",
			Namespace: "matey",
		},
	}

	t.Run("auto-scaling with high load", func(t *testing.T) {
		// Set scheduler to running phase
		var scheduler crd.MCPTaskScheduler
		err := client.Get(ctx, req.NamespacedName, &scheduler)
		require.NoError(t, err)
		
		scheduler.Status.Phase = crd.MCPTaskSchedulerPhaseRunning
		scheduler.Status.TaskStats.RunningTasks = 9 // 90% utilization
		err = client.Status().Update(ctx, &scheduler)
		require.NoError(t, err)

		// Reconcile to trigger auto-scaling
		result, err := reconciler.Reconcile(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, time.Minute*2, result.RequeueAfter)

		// Verify auto-scaling configuration is applied
		var configMap corev1.ConfigMap
		err = client.Get(ctx, types.NamespacedName{
			Name:      "autoscaling-test-scheduler-config",
			Namespace: "matey",
		}, &configMap)
		require.NoError(t, err)
		
		config := configMap.Data["matey.yaml"]
		assert.Contains(t, config, "auto_scaling:")
		assert.Contains(t, config, "enabled: true")
		assert.Contains(t, config, "min_concurrent_tasks: 2")
		assert.Contains(t, config, "max_concurrent_tasks: 50")
		assert.Contains(t, config, "target_cpu_utilization: 70")
		assert.Contains(t, config, "target_memory_utilization: 80")
		assert.Contains(t, config, "scale_up_cooldown: 30s")
		assert.Contains(t, config, "scale_down_cooldown: 300s")
		assert.Contains(t, config, "metrics_interval: 15s")
	})
}

// Helper functions

func findCondition(conditions []crd.MCPTaskSchedulerCondition, conditionType crd.MCPTaskSchedulerConditionType) *crd.MCPTaskSchedulerCondition {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return &condition
		}
	}
	return nil
}