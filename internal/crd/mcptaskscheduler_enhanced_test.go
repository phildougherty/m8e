package crd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMCPTaskSchedulerEnhancedFields(t *testing.T) {
	t.Run("event triggers serialization", func(t *testing.T) {
		eventTrigger := EventTrigger{
			Name:     "pod-failure-handler",
			Type:     "k8s-event",
			Workflow: "incident-response-workflow",
			KubernetesEvent: &KubernetesEventConfig{
				Kind:          "Pod",
				Reason:        "Failed",
				Namespace:     "production",
				LabelSelector: "app=critical",
				FieldSelector: "involvedObject.name=webapp",
			},
			Webhook: &WebhookConfig{
				Endpoint:       "/webhook/alerts",
				Authentication: "bearer-token",
				Method:         "POST",
				Headers: map[string]string{
					"Content-Type": "application/json",
				},
			},
			FileWatch: &FileWatchConfig{
				Path:      "/data/input",
				Pattern:   "*.json",
				Events:    []string{"create", "modify"},
				Recursive: true,
			},
			Conditions: []TriggerCondition{
				{
					Field:    "message",
					Operator: "contains",
					Value:    "ImagePullBackOff",
				},
			},
			CooldownDuration: "60s",
		}

		// Test JSON marshaling
		data, err := json.Marshal(eventTrigger)
		require.NoError(t, err)
		assert.Contains(t, string(data), "pod-failure-handler")
		assert.Contains(t, string(data), "k8s-event")
		assert.Contains(t, string(data), "incident-response-workflow")
		assert.Contains(t, string(data), "Pod")
		assert.Contains(t, string(data), "Failed")
		assert.Contains(t, string(data), "production")
		assert.Contains(t, string(data), "app=critical")
		assert.Contains(t, string(data), "involvedObject.name=webapp")
		assert.Contains(t, string(data), "/webhook/alerts")
		assert.Contains(t, string(data), "bearer-token")
		assert.Contains(t, string(data), "/data/input")
		assert.Contains(t, string(data), "*.json")
		assert.Contains(t, string(data), "create")
		assert.Contains(t, string(data), "modify")
		assert.Contains(t, string(data), "ImagePullBackOff")
		assert.Contains(t, string(data), "60s")

		// Test JSON unmarshaling
		var unmarshaled EventTrigger
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)
		assert.Equal(t, eventTrigger.Name, unmarshaled.Name)
		assert.Equal(t, eventTrigger.Type, unmarshaled.Type)
		assert.Equal(t, eventTrigger.Workflow, unmarshaled.Workflow)
		assert.Equal(t, eventTrigger.KubernetesEvent.Kind, unmarshaled.KubernetesEvent.Kind)
		assert.Equal(t, eventTrigger.KubernetesEvent.Reason, unmarshaled.KubernetesEvent.Reason)
		assert.Equal(t, eventTrigger.KubernetesEvent.Namespace, unmarshaled.KubernetesEvent.Namespace)
		assert.Equal(t, eventTrigger.KubernetesEvent.LabelSelector, unmarshaled.KubernetesEvent.LabelSelector)
		assert.Equal(t, eventTrigger.KubernetesEvent.FieldSelector, unmarshaled.KubernetesEvent.FieldSelector)
		assert.Equal(t, eventTrigger.Webhook.Endpoint, unmarshaled.Webhook.Endpoint)
		assert.Equal(t, eventTrigger.Webhook.Authentication, unmarshaled.Webhook.Authentication)
		assert.Equal(t, eventTrigger.Webhook.Method, unmarshaled.Webhook.Method)
		assert.Equal(t, eventTrigger.Webhook.Headers, unmarshaled.Webhook.Headers)
		assert.Equal(t, eventTrigger.FileWatch.Path, unmarshaled.FileWatch.Path)
		assert.Equal(t, eventTrigger.FileWatch.Pattern, unmarshaled.FileWatch.Pattern)
		assert.Equal(t, eventTrigger.FileWatch.Events, unmarshaled.FileWatch.Events)
		assert.Equal(t, eventTrigger.FileWatch.Recursive, unmarshaled.FileWatch.Recursive)
		assert.Equal(t, len(eventTrigger.Conditions), len(unmarshaled.Conditions))
		assert.Equal(t, eventTrigger.Conditions[0].Field, unmarshaled.Conditions[0].Field)
		assert.Equal(t, eventTrigger.Conditions[0].Operator, unmarshaled.Conditions[0].Operator)
		assert.Equal(t, eventTrigger.Conditions[0].Value, unmarshaled.Conditions[0].Value)
		assert.Equal(t, eventTrigger.CooldownDuration, unmarshaled.CooldownDuration)
	})

	t.Run("conditional dependencies serialization", func(t *testing.T) {
		conditionalDeps := ConditionalDependencyConfig{
			Enabled:              true,
			DefaultStrategy:      "fail-fast",
			ResolutionTimeout:    "10m",
			CrossWorkflowEnabled: true,
		}

		// Test JSON marshaling
		data, err := json.Marshal(conditionalDeps)
		require.NoError(t, err)
		assert.Contains(t, string(data), "fail-fast")
		assert.Contains(t, string(data), "10m")
		assert.Contains(t, string(data), "true")

		// Test JSON unmarshaling
		var unmarshaled ConditionalDependencyConfig
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)
		assert.Equal(t, conditionalDeps.Enabled, unmarshaled.Enabled)
		assert.Equal(t, conditionalDeps.DefaultStrategy, unmarshaled.DefaultStrategy)
		assert.Equal(t, conditionalDeps.ResolutionTimeout, unmarshaled.ResolutionTimeout)
		assert.Equal(t, conditionalDeps.CrossWorkflowEnabled, unmarshaled.CrossWorkflowEnabled)
	})

	t.Run("auto scaling serialization", func(t *testing.T) {
		autoScaling := AutoScalingConfig{
			Enabled:                   true,
			MinConcurrentTasks:        2,
			MaxConcurrentTasks:        50,
			TargetCPUUtilization:      70,
			TargetMemoryUtilization:   80,
			ScaleUpCooldown:          "30s",
			ScaleDownCooldown:        "300s",
			MetricsInterval:          "15s",
			CustomMetrics: []CustomMetric{
				{
					Name:        "queue-depth",
					Type:        "external",
					TargetValue: "100",
					Selector: map[string]string{
						"queue": "task-queue",
					},
				},
				{
					Name:        "active-connections",
					Type:        "resource",
					TargetValue: "50",
					Selector: map[string]string{
						"resource": "connections",
					},
				},
			},
		}

		// Test JSON marshaling
		data, err := json.Marshal(autoScaling)
		require.NoError(t, err)
		assert.Contains(t, string(data), "queue-depth")
		assert.Contains(t, string(data), "external")
		assert.Contains(t, string(data), "100")
		assert.Contains(t, string(data), "task-queue")
		assert.Contains(t, string(data), "active-connections")
		assert.Contains(t, string(data), "resource")
		assert.Contains(t, string(data), "50")
		assert.Contains(t, string(data), "connections")

		// Test JSON unmarshaling
		var unmarshaled AutoScalingConfig
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)
		assert.Equal(t, autoScaling.Enabled, unmarshaled.Enabled)
		assert.Equal(t, autoScaling.MinConcurrentTasks, unmarshaled.MinConcurrentTasks)
		assert.Equal(t, autoScaling.MaxConcurrentTasks, unmarshaled.MaxConcurrentTasks)
		assert.Equal(t, autoScaling.TargetCPUUtilization, unmarshaled.TargetCPUUtilization)
		assert.Equal(t, autoScaling.TargetMemoryUtilization, unmarshaled.TargetMemoryUtilization)
		assert.Equal(t, autoScaling.ScaleUpCooldown, unmarshaled.ScaleUpCooldown)
		assert.Equal(t, autoScaling.ScaleDownCooldown, unmarshaled.ScaleDownCooldown)
		assert.Equal(t, autoScaling.MetricsInterval, unmarshaled.MetricsInterval)
		assert.Equal(t, len(autoScaling.CustomMetrics), len(unmarshaled.CustomMetrics))
		
		for i, metric := range autoScaling.CustomMetrics {
			assert.Equal(t, metric.Name, unmarshaled.CustomMetrics[i].Name)
			assert.Equal(t, metric.Type, unmarshaled.CustomMetrics[i].Type)
			assert.Equal(t, metric.TargetValue, unmarshaled.CustomMetrics[i].TargetValue)
			assert.Equal(t, metric.Selector, unmarshaled.CustomMetrics[i].Selector)
		}
	})

	t.Run("full MCPTaskScheduler with enhanced fields", func(t *testing.T) {
		taskScheduler := MCPTaskScheduler{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "mcp.matey.ai/v1",
				Kind:       "MCPTaskScheduler",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "enhanced-scheduler",
				Namespace: "default",
			},
			Spec: MCPTaskSchedulerSpec{
				Image: "mcpcompose/task-scheduler:latest",
				Port:  8084,
				Host:  "0.0.0.0",
				SchedulerConfig: TaskSchedulerConfig{
					DefaultTimeout:     "5m",
					MaxConcurrentTasks: 10,
					RetryPolicy: TaskRetryPolicy{
						MaxRetries:      3,
						RetryDelay:      "30s",
						BackoffStrategy: "exponential",
					},
					TaskStorageEnabled: true,
					TaskHistoryLimit:   100,
					TaskCleanupPolicy:  "auto",
					ActivityWebhook:    "https://hooks.slack.com/services/...",
					EventTriggers: []EventTrigger{
						{
							Name:     "pod-failure-handler",
							Type:     "k8s-event",
							Workflow: "incident-response-workflow",
							KubernetesEvent: &KubernetesEventConfig{
								Kind:      "Pod",
								Reason:    "Failed",
								Namespace: "production",
							},
							Conditions: []TriggerCondition{
								{
									Field:    "message",
									Operator: "contains",
									Value:    "ImagePullBackOff",
								},
							},
							CooldownDuration: "60s",
						},
					},
					ConditionalDependencies: ConditionalDependencyConfig{
						Enabled:              true,
						DefaultStrategy:      "fail-fast",
						ResolutionTimeout:    "10m",
						CrossWorkflowEnabled: true,
					},
					AutoScaling: AutoScalingConfig{
						Enabled:                   true,
						MinConcurrentTasks:        2,
						MaxConcurrentTasks:        50,
						TargetCPUUtilization:      70,
						TargetMemoryUtilization:   80,
						ScaleUpCooldown:          "30s",
						ScaleDownCooldown:        "300s",
						MetricsInterval:          "15s",
						CustomMetrics: []CustomMetric{
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

		// Test JSON marshaling
		data, err := json.Marshal(taskScheduler)
		require.NoError(t, err)
		
		// Verify key fields are present
		assert.Contains(t, string(data), "enhanced-scheduler")
		assert.Contains(t, string(data), "mcpcompose/task-scheduler:latest")
		assert.Contains(t, string(data), "pod-failure-handler")
		assert.Contains(t, string(data), "incident-response-workflow")
		assert.Contains(t, string(data), "fail-fast")
		assert.Contains(t, string(data), "queue-depth")
		assert.Contains(t, string(data), "external")

		// Test JSON unmarshaling
		var unmarshaled MCPTaskScheduler
		err = json.Unmarshal(data, &unmarshaled)
		require.NoError(t, err)
		
		assert.Equal(t, taskScheduler.Name, unmarshaled.Name)
		assert.Equal(t, taskScheduler.Namespace, unmarshaled.Namespace)
		assert.Equal(t, taskScheduler.Spec.Image, unmarshaled.Spec.Image)
		assert.Equal(t, taskScheduler.Spec.Port, unmarshaled.Spec.Port)
		assert.Equal(t, taskScheduler.Spec.Host, unmarshaled.Spec.Host)
		assert.Equal(t, taskScheduler.Spec.SchedulerConfig.DefaultTimeout, unmarshaled.Spec.SchedulerConfig.DefaultTimeout)
		assert.Equal(t, taskScheduler.Spec.SchedulerConfig.MaxConcurrentTasks, unmarshaled.Spec.SchedulerConfig.MaxConcurrentTasks)
		assert.Equal(t, len(taskScheduler.Spec.SchedulerConfig.EventTriggers), len(unmarshaled.Spec.SchedulerConfig.EventTriggers))
		assert.Equal(t, taskScheduler.Spec.SchedulerConfig.ConditionalDependencies.Enabled, unmarshaled.Spec.SchedulerConfig.ConditionalDependencies.Enabled)
		assert.Equal(t, taskScheduler.Spec.SchedulerConfig.AutoScaling.Enabled, unmarshaled.Spec.SchedulerConfig.AutoScaling.Enabled)
		assert.Equal(t, len(taskScheduler.Spec.SchedulerConfig.AutoScaling.CustomMetrics), len(unmarshaled.Spec.SchedulerConfig.AutoScaling.CustomMetrics))
	})
}

func TestMCPTaskSchedulerDeepCopy(t *testing.T) {
	t.Run("deep copy with enhanced fields", func(t *testing.T) {
		original := &MCPTaskScheduler{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scheduler",
				Namespace: "default",
			},
			Spec: MCPTaskSchedulerSpec{
				Image: "mcpcompose/task-scheduler:latest",
				Port:  8084,
				Host:  "0.0.0.0",
				SchedulerConfig: TaskSchedulerConfig{
					EventTriggers: []EventTrigger{
						{
							Name:     "pod-failure-handler",
							Type:     "k8s-event",
							Workflow: "incident-response-workflow",
							KubernetesEvent: &KubernetesEventConfig{
								Kind:      "Pod",
								Reason:    "Failed",
								Namespace: "production",
							},
							Conditions: []TriggerCondition{
								{
									Field:    "message",
									Operator: "contains",
									Value:    "ImagePullBackOff",
								},
							},
							CooldownDuration: "60s",
						},
					},
					ConditionalDependencies: ConditionalDependencyConfig{
						Enabled:              true,
						DefaultStrategy:      "fail-fast",
						ResolutionTimeout:    "10m",
						CrossWorkflowEnabled: true,
					},
					AutoScaling: AutoScalingConfig{
						Enabled:                   true,
						MinConcurrentTasks:        2,
						MaxConcurrentTasks:        50,
						TargetCPUUtilization:      70,
						TargetMemoryUtilization:   80,
						ScaleUpCooldown:          "30s",
						ScaleDownCooldown:        "300s",
						MetricsInterval:          "15s",
						CustomMetrics: []CustomMetric{
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

		// Perform deep copy
		copied := original.DeepCopy()
		
		// Verify that the copy is independent
		assert.Equal(t, original.Name, copied.Name)
		assert.Equal(t, original.Namespace, copied.Namespace)
		assert.Equal(t, original.Spec.Image, copied.Spec.Image)
		assert.Equal(t, original.Spec.Port, copied.Spec.Port)
		assert.Equal(t, original.Spec.Host, copied.Spec.Host)
		
		// Verify event triggers deep copy
		assert.Equal(t, len(original.Spec.SchedulerConfig.EventTriggers), len(copied.Spec.SchedulerConfig.EventTriggers))
		assert.Equal(t, original.Spec.SchedulerConfig.EventTriggers[0].Name, copied.Spec.SchedulerConfig.EventTriggers[0].Name)
		assert.Equal(t, original.Spec.SchedulerConfig.EventTriggers[0].Type, copied.Spec.SchedulerConfig.EventTriggers[0].Type)
		assert.Equal(t, original.Spec.SchedulerConfig.EventTriggers[0].Workflow, copied.Spec.SchedulerConfig.EventTriggers[0].Workflow)
		assert.Equal(t, original.Spec.SchedulerConfig.EventTriggers[0].KubernetesEvent.Kind, copied.Spec.SchedulerConfig.EventTriggers[0].KubernetesEvent.Kind)
		assert.Equal(t, original.Spec.SchedulerConfig.EventTriggers[0].KubernetesEvent.Reason, copied.Spec.SchedulerConfig.EventTriggers[0].KubernetesEvent.Reason)
		assert.Equal(t, original.Spec.SchedulerConfig.EventTriggers[0].KubernetesEvent.Namespace, copied.Spec.SchedulerConfig.EventTriggers[0].KubernetesEvent.Namespace)
		assert.Equal(t, len(original.Spec.SchedulerConfig.EventTriggers[0].Conditions), len(copied.Spec.SchedulerConfig.EventTriggers[0].Conditions))
		assert.Equal(t, original.Spec.SchedulerConfig.EventTriggers[0].Conditions[0].Field, copied.Spec.SchedulerConfig.EventTriggers[0].Conditions[0].Field)
		assert.Equal(t, original.Spec.SchedulerConfig.EventTriggers[0].Conditions[0].Operator, copied.Spec.SchedulerConfig.EventTriggers[0].Conditions[0].Operator)
		assert.Equal(t, original.Spec.SchedulerConfig.EventTriggers[0].Conditions[0].Value, copied.Spec.SchedulerConfig.EventTriggers[0].Conditions[0].Value)
		assert.Equal(t, original.Spec.SchedulerConfig.EventTriggers[0].CooldownDuration, copied.Spec.SchedulerConfig.EventTriggers[0].CooldownDuration)
		
		// Verify conditional dependencies deep copy
		assert.Equal(t, original.Spec.SchedulerConfig.ConditionalDependencies.Enabled, copied.Spec.SchedulerConfig.ConditionalDependencies.Enabled)
		assert.Equal(t, original.Spec.SchedulerConfig.ConditionalDependencies.DefaultStrategy, copied.Spec.SchedulerConfig.ConditionalDependencies.DefaultStrategy)
		assert.Equal(t, original.Spec.SchedulerConfig.ConditionalDependencies.ResolutionTimeout, copied.Spec.SchedulerConfig.ConditionalDependencies.ResolutionTimeout)
		assert.Equal(t, original.Spec.SchedulerConfig.ConditionalDependencies.CrossWorkflowEnabled, copied.Spec.SchedulerConfig.ConditionalDependencies.CrossWorkflowEnabled)
		
		// Verify auto scaling deep copy
		assert.Equal(t, original.Spec.SchedulerConfig.AutoScaling.Enabled, copied.Spec.SchedulerConfig.AutoScaling.Enabled)
		assert.Equal(t, original.Spec.SchedulerConfig.AutoScaling.MinConcurrentTasks, copied.Spec.SchedulerConfig.AutoScaling.MinConcurrentTasks)
		assert.Equal(t, original.Spec.SchedulerConfig.AutoScaling.MaxConcurrentTasks, copied.Spec.SchedulerConfig.AutoScaling.MaxConcurrentTasks)
		assert.Equal(t, original.Spec.SchedulerConfig.AutoScaling.TargetCPUUtilization, copied.Spec.SchedulerConfig.AutoScaling.TargetCPUUtilization)
		assert.Equal(t, original.Spec.SchedulerConfig.AutoScaling.TargetMemoryUtilization, copied.Spec.SchedulerConfig.AutoScaling.TargetMemoryUtilization)
		assert.Equal(t, original.Spec.SchedulerConfig.AutoScaling.ScaleUpCooldown, copied.Spec.SchedulerConfig.AutoScaling.ScaleUpCooldown)
		assert.Equal(t, original.Spec.SchedulerConfig.AutoScaling.ScaleDownCooldown, copied.Spec.SchedulerConfig.AutoScaling.ScaleDownCooldown)
		assert.Equal(t, original.Spec.SchedulerConfig.AutoScaling.MetricsInterval, copied.Spec.SchedulerConfig.AutoScaling.MetricsInterval)
		assert.Equal(t, len(original.Spec.SchedulerConfig.AutoScaling.CustomMetrics), len(copied.Spec.SchedulerConfig.AutoScaling.CustomMetrics))
		assert.Equal(t, original.Spec.SchedulerConfig.AutoScaling.CustomMetrics[0].Name, copied.Spec.SchedulerConfig.AutoScaling.CustomMetrics[0].Name)
		assert.Equal(t, original.Spec.SchedulerConfig.AutoScaling.CustomMetrics[0].Type, copied.Spec.SchedulerConfig.AutoScaling.CustomMetrics[0].Type)
		assert.Equal(t, original.Spec.SchedulerConfig.AutoScaling.CustomMetrics[0].TargetValue, copied.Spec.SchedulerConfig.AutoScaling.CustomMetrics[0].TargetValue)
		assert.Equal(t, original.Spec.SchedulerConfig.AutoScaling.CustomMetrics[0].Selector, copied.Spec.SchedulerConfig.AutoScaling.CustomMetrics[0].Selector)
		
		// Verify independence - modifying copy doesn't affect original
		copied.Spec.SchedulerConfig.EventTriggers[0].Name = "modified-trigger"
		assert.NotEqual(t, original.Spec.SchedulerConfig.EventTriggers[0].Name, copied.Spec.SchedulerConfig.EventTriggers[0].Name)
		
		copied.Spec.SchedulerConfig.AutoScaling.CustomMetrics[0].Selector["queue"] = "modified-queue"
		assert.NotEqual(t, original.Spec.SchedulerConfig.AutoScaling.CustomMetrics[0].Selector["queue"], copied.Spec.SchedulerConfig.AutoScaling.CustomMetrics[0].Selector["queue"])
	})
}

func TestEventTriggerValidation(t *testing.T) {
	t.Run("kubernetes event config validation", func(t *testing.T) {
		config := KubernetesEventConfig{
			Kind:          "Pod",
			Reason:        "Failed",
			Namespace:     "production",
			LabelSelector: "app=critical",
			FieldSelector: "involvedObject.name=webapp",
		}

		assert.NotEmpty(t, config.Kind)
		assert.NotEmpty(t, config.Reason)
		assert.NotEmpty(t, config.Namespace)
		assert.NotEmpty(t, config.LabelSelector)
		assert.NotEmpty(t, config.FieldSelector)
	})

	t.Run("webhook config validation", func(t *testing.T) {
		config := WebhookConfig{
			Endpoint:       "/webhook/alerts",
			Authentication: "bearer-token",
			Method:         "POST",
			Headers: map[string]string{
				"Content-Type": "application/json",
				"User-Agent":   "matey-task-scheduler",
			},
		}

		assert.NotEmpty(t, config.Endpoint)
		assert.NotEmpty(t, config.Authentication)
		assert.NotEmpty(t, config.Method)
		assert.NotEmpty(t, config.Headers)
		assert.Equal(t, "application/json", config.Headers["Content-Type"])
		assert.Equal(t, "matey-task-scheduler", config.Headers["User-Agent"])
	})

	t.Run("file watch config validation", func(t *testing.T) {
		config := FileWatchConfig{
			Path:      "/data/input",
			Pattern:   "*.json",
			Events:    []string{"create", "modify", "delete"},
			Recursive: true,
		}

		assert.NotEmpty(t, config.Path)
		assert.NotEmpty(t, config.Pattern)
		assert.NotEmpty(t, config.Events)
		assert.True(t, config.Recursive)
		assert.Contains(t, config.Events, "create")
		assert.Contains(t, config.Events, "modify")
		assert.Contains(t, config.Events, "delete")
	})

	t.Run("trigger condition validation", func(t *testing.T) {
		condition := TriggerCondition{
			Field:    "message",
			Operator: "contains",
			Value:    "ImagePullBackOff",
		}

		assert.NotEmpty(t, condition.Field)
		assert.NotEmpty(t, condition.Operator)
		assert.NotEmpty(t, condition.Value)
		assert.Equal(t, "message", condition.Field)
		assert.Equal(t, "contains", condition.Operator)
		assert.Equal(t, "ImagePullBackOff", condition.Value)
	})
}

func TestCustomMetricValidation(t *testing.T) {
	t.Run("custom metric validation", func(t *testing.T) {
		metric := CustomMetric{
			Name:        "queue-depth",
			Type:        "external",
			TargetValue: "100",
			Selector: map[string]string{
				"queue":   "task-queue",
				"region":  "us-west-2",
				"service": "task-processor",
			},
		}

		assert.NotEmpty(t, metric.Name)
		assert.NotEmpty(t, metric.Type)
		assert.NotEmpty(t, metric.TargetValue)
		assert.NotEmpty(t, metric.Selector)
		assert.Equal(t, "queue-depth", metric.Name)
		assert.Equal(t, "external", metric.Type)
		assert.Equal(t, "100", metric.TargetValue)
		assert.Equal(t, "task-queue", metric.Selector["queue"])
		assert.Equal(t, "us-west-2", metric.Selector["region"])
		assert.Equal(t, "task-processor", metric.Selector["service"])
	})

	t.Run("multiple custom metrics", func(t *testing.T) {
		metrics := []CustomMetric{
			{
				Name:        "queue-depth",
				Type:        "external",
				TargetValue: "100",
				Selector: map[string]string{
					"queue": "task-queue",
				},
			},
			{
				Name:        "active-connections",
				Type:        "resource",
				TargetValue: "50",
				Selector: map[string]string{
					"resource": "connections",
				},
			},
			{
				Name:        "pod-cpu-utilization",
				Type:        "pods",
				TargetValue: "80",
				Selector: map[string]string{
					"app": "task-scheduler",
				},
			},
		}

		assert.Len(t, metrics, 3)
		assert.Equal(t, "queue-depth", metrics[0].Name)
		assert.Equal(t, "external", metrics[0].Type)
		assert.Equal(t, "active-connections", metrics[1].Name)
		assert.Equal(t, "resource", metrics[1].Type)
		assert.Equal(t, "pod-cpu-utilization", metrics[2].Name)
		assert.Equal(t, "pods", metrics[2].Type)
	})
}