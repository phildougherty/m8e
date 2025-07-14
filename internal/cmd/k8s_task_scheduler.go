// internal/cmd/k8s_task_scheduler.go
package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/crd"
	"github.com/phildougherty/m8e/internal/task_scheduler"
)

func NewK8sTaskSchedulerCommand() *cobra.Command {
	var (
		namespace     string
		image         string
		replicas      int32
		port          int32
		logLevel      string
		timeout       time.Duration
		cpuRequest    string
		memoryRequest string
		cpuLimit      string
		memoryLimit   string
	)

	cmd := &cobra.Command{
		Use:   "task-scheduler",
		Short: "Manage the Kubernetes-native task scheduler service",
		Long: `Manage the task scheduler service using Kubernetes-native resources.

The Kubernetes-native task scheduler provides:
- Automatic task execution using Kubernetes Jobs
- Built-in retry and timeout handling
- Resource management and scaling
- Integration with Kubernetes RBAC and networking
- Persistent task history and logging
- LLM integration for intelligent task automation

Examples:
  # Start the task scheduler service
  matey task-scheduler start

  # Stop the task scheduler service  
  matey task-scheduler stop

  # Check the status
  matey task-scheduler status

  # Execute a task
  matey task-scheduler exec --name "test-task" --image "busybox" --command "echo,hello,world"

  # List running tasks
  matey task-scheduler list

  # Get task logs
  matey task-scheduler logs <task-id>`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// Global flags
	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	cmd.PersistentFlags().StringVar(&image, "image", "ghcr.io/phildougherty/matey/task-scheduler:latest", "Task scheduler image")
	cmd.PersistentFlags().Int32Var(&replicas, "replicas", 1, "Number of replicas")
	cmd.PersistentFlags().Int32Var(&port, "port", 8084, "Service port")
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level")
	cmd.PersistentFlags().DurationVar(&timeout, "timeout", 5*time.Minute, "Operation timeout")

	// Resource flags
	cmd.PersistentFlags().StringVar(&cpuRequest, "cpu-request", "100m", "CPU request")
	cmd.PersistentFlags().StringVar(&memoryRequest, "memory-request", "128Mi", "Memory request")
	cmd.PersistentFlags().StringVar(&cpuLimit, "cpu-limit", "500m", "CPU limit")
	cmd.PersistentFlags().StringVar(&memoryLimit, "memory-limit", "512Mi", "Memory limit")

	// Add subcommands
	cmd.AddCommand(newTaskSchedulerStartCommand(namespace, image, replicas, port, logLevel, timeout, cpuRequest, memoryRequest, cpuLimit, memoryLimit))
	cmd.AddCommand(newTaskSchedulerStopCommand(namespace, timeout))
	cmd.AddCommand(newTaskSchedulerStatusCommand(namespace))
	cmd.AddCommand(newTaskSchedulerRestartCommand(namespace, timeout))
	cmd.AddCommand(newTaskSchedulerExecCommand(namespace))
	cmd.AddCommand(newTaskSchedulerListCommand(namespace))
	cmd.AddCommand(newTaskSchedulerLogsCommand(namespace))
	cmd.AddCommand(newTaskSchedulerCancelCommand(namespace))

	return cmd
}

func newTaskSchedulerStartCommand(namespace, image string, replicas, port int32, logLevel string, timeout time.Duration, cpuRequest, memoryRequest, cpuLimit, memoryLimit string) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the task scheduler service",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load configuration
			configFile, _ := cmd.Flags().GetString("file")
			cfg, err := config.LoadConfig(configFile)
			if err != nil {
				fmt.Printf("Warning: Failed to load config: %v\n", err)
				cfg = &config.ComposeConfig{}
			}

			// Create Kubernetes client
			k8sClient, err := createK8sClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			// Create manager
			manager := task_scheduler.NewK8sManager(cfg, k8sClient, namespace)
			manager.SetConfigFile(configFile)

			// Start the service
			fmt.Printf("Starting Kubernetes-native task scheduler in namespace: %s\n", namespace)
			if err := manager.Start(); err != nil {
				return fmt.Errorf("failed to start task scheduler: %w", err)
			}

			fmt.Println("Task scheduler service started successfully!")
			fmt.Printf("You can check the status with: matey task-scheduler status -n %s\n", namespace)

			return nil
		},
	}
}

func newTaskSchedulerStopCommand(namespace string, timeout time.Duration) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the task scheduler service",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create Kubernetes client
			k8sClient, err := createK8sClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			// Create manager
			manager := task_scheduler.NewK8sManager(&config.ComposeConfig{}, k8sClient, namespace)

			// Stop the service
			fmt.Printf("Stopping task scheduler in namespace: %s\n", namespace)
			if err := manager.Stop(); err != nil {
				return fmt.Errorf("failed to stop task scheduler: %w", err)
			}

			fmt.Println("Task scheduler service stopped successfully!")
			return nil
		},
	}
}

func newTaskSchedulerStatusCommand(namespace string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check the status of the task scheduler service",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create Kubernetes client
			k8sClient, err := createK8sClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			// Create manager
			manager := task_scheduler.NewK8sManager(&config.ComposeConfig{}, k8sClient, namespace)

			// Get status
			status, err := manager.GetStatus()
			if err != nil {
				return fmt.Errorf("failed to get status: %w", err)
			}

			fmt.Printf("Task Scheduler Status: %s\n", status)

			// Get task statistics
			stats, err := manager.GetTaskStatistics()
			if err != nil {
				fmt.Printf("Warning: Failed to get task statistics: %v\n", err)
			} else {
				fmt.Printf("\nTask Statistics:\n")
				fmt.Printf("  Total Tasks: %d\n", stats.TotalTasks)
				fmt.Printf("  Running Tasks: %d\n", stats.RunningTasks)
				fmt.Printf("  Completed Tasks: %d\n", stats.CompletedTasks)
				fmt.Printf("  Failed Tasks: %d\n", stats.FailedTasks)
				fmt.Printf("  Scheduled Tasks: %d\n", stats.ScheduledTasks)
				if stats.LastTaskTime != "" {
					fmt.Printf("  Last Task Time: %s\n", stats.LastTaskTime)
				}
			}

			return nil
		},
	}
}

func newTaskSchedulerRestartCommand(namespace string, timeout time.Duration) *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the task scheduler service",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create Kubernetes client
			k8sClient, err := createK8sClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			// Create manager
			manager := task_scheduler.NewK8sManager(&config.ComposeConfig{}, k8sClient, namespace)

			// Restart the service
			fmt.Printf("Restarting task scheduler in namespace: %s\n", namespace)
			if err := manager.Restart(); err != nil {
				return fmt.Errorf("failed to restart task scheduler: %w", err)
			}

			fmt.Println("Task scheduler service restarted successfully!")
			return nil
		},
	}
}

func newTaskSchedulerExecCommand(namespace string) *cobra.Command {
	var (
		taskName    string
		taskImage   string
		command     []string
		args        []string
		env         map[string]string
		cpuLimit    string
		memoryLimit string
		timeout     time.Duration
		maxRetries  int
	)

	cmd := &cobra.Command{
		Use:   "exec",
		Short: "Execute a task using Kubernetes Jobs",
		Example: `  # Execute a simple command
  matey task-scheduler exec --name "hello-world" --image "busybox" --command "echo" --args "hello,world"
  
  # Execute with resource limits
  matey task-scheduler exec --name "cpu-task" --image "busybox" --cpu-limit "500m" --memory-limit "256Mi"
  
  # Execute with environment variables  
  matey task-scheduler exec --name "env-task" --image "busybox" --env "KEY1=value1,KEY2=value2"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if taskName == "" {
				return fmt.Errorf("task name is required")
			}
			if taskImage == "" {
				return fmt.Errorf("task image is required")
			}

			// Create Kubernetes client
			k8sClient, err := createK8sClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			// Create manager
			manager := task_scheduler.NewK8sManager(&config.ComposeConfig{}, k8sClient, namespace)

			// Create task request
			taskReq := &task_scheduler.TaskRequest{
				ID:          fmt.Sprintf("%s-%d", taskName, time.Now().Unix()),
				Name:        taskName,
				Description: fmt.Sprintf("Task executed via CLI: %s", taskName),
				Image:       taskImage,
				Command:     command,
				Args:        args,
				Env:         env,
				Timeout:     timeout,
				Retry: task_scheduler.TaskRetryConfig{
					MaxRetries: maxRetries,
				},
				Resources: task_scheduler.TaskResourceConfig{
					CPU:    cpuLimit,
					Memory: memoryLimit,
				},
			}

			// Execute task
			fmt.Printf("Executing task: %s\n", taskName)
			status, err := manager.ExecuteTask(taskReq)
			if err != nil {
				return fmt.Errorf("failed to execute task: %w", err)
			}

			fmt.Printf("Task submitted successfully!\n")
			fmt.Printf("Task ID: %s\n", status.ID)
			fmt.Printf("Job Name: %s\n", status.JobName)
			fmt.Printf("Status: %s\n", status.Phase)
			fmt.Printf("\nYou can check the status with: matey task-scheduler status -n %s\n", namespace)
			fmt.Printf("You can view logs with: matey task-scheduler logs %s -n %s\n", status.ID, namespace)

			return nil
		},
	}

	cmd.Flags().StringVar(&taskName, "name", "", "Task name (required)")
	cmd.Flags().StringVar(&taskImage, "image", "", "Container image (required)")
	cmd.Flags().StringSliceVar(&command, "command", []string{}, "Command to execute")
	cmd.Flags().StringSliceVar(&args, "args", []string{}, "Arguments for the command")
	cmd.Flags().StringToStringVar(&env, "env", map[string]string{}, "Environment variables (key=value)")
	cmd.Flags().StringVar(&cpuLimit, "cpu-limit", "100m", "CPU limit")
	cmd.Flags().StringVar(&memoryLimit, "memory-limit", "128Mi", "Memory limit")
	cmd.Flags().DurationVar(&timeout, "timeout", 10*time.Minute, "Task timeout")
	cmd.Flags().IntVar(&maxRetries, "max-retries", 3, "Maximum number of retries")

	return cmd
}

func newTaskSchedulerListCommand(namespace string) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create Kubernetes client
			k8sClient, err := createK8sClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			// Create manager
			manager := task_scheduler.NewK8sManager(&config.ComposeConfig{}, k8sClient, namespace)

			// List tasks
			tasks, err := manager.ListTasks()
			if err != nil {
				return fmt.Errorf("failed to list tasks: %w", err)
			}

			if len(tasks) == 0 {
				fmt.Println("No tasks found.")
				return nil
			}

			fmt.Printf("%-20s %-15s %-10s %-20s %s\n", "TASK ID", "PHASE", "EXIT CODE", "JOB NAME", "MESSAGE")
			fmt.Println(strings.Repeat("-", 80))

			for _, task := range tasks {
				exitCode := "N/A"
				if task.ExitCode != nil {
					exitCode = fmt.Sprintf("%d", *task.ExitCode)
				}
				
				fmt.Printf("%-20s %-15s %-10s %-20s %s\n",
					truncate(task.ID, 20),
					task.Phase,
					exitCode,
					truncate(task.JobName, 20),
					truncate(task.Message, 30))
			}

			return nil
		},
	}
}

func newTaskSchedulerLogsCommand(namespace string) *cobra.Command {
	return &cobra.Command{
		Use:   "logs <task-id>",
		Short: "Get logs from a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]

			// Create Kubernetes client
			k8sClient, err := createK8sClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			// Create manager
			manager := task_scheduler.NewK8sManager(&config.ComposeConfig{}, k8sClient, namespace)

			// Get logs
			logs, err := manager.GetTaskLogs(taskID)
			if err != nil {
				return fmt.Errorf("failed to get task logs: %w", err)
			}

			if logs == "" {
				fmt.Println("No logs available for this task.")
				return nil
			}

			fmt.Printf("Logs for task %s:\n", taskID)
			fmt.Println(strings.Repeat("-", 40))
			fmt.Print(logs)

			return nil
		},
	}
}

func newTaskSchedulerCancelCommand(namespace string) *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <task-id>",
		Short: "Cancel a running task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]

			// Create Kubernetes client
			k8sClient, err := createK8sClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			// Create manager
			manager := task_scheduler.NewK8sManager(&config.ComposeConfig{}, k8sClient, namespace)

			// Cancel task
			fmt.Printf("Cancelling task: %s\n", taskID)
			if err := manager.CancelTask(taskID); err != nil {
				return fmt.Errorf("failed to cancel task: %w", err)
			}

			fmt.Println("Task cancelled successfully!")
			return nil
		},
	}
}

// createK8sClient creates a Kubernetes client
func createK8sClient() (client.Client, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		config, err = clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
		if err != nil {
			return nil, fmt.Errorf("failed to create kubernetes config: %w", err)
		}
	}

	// Create the scheme
	scheme := runtime.NewScheme()
	if err := crd.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add CRD scheme: %w", err)
	}

	// Create the client
	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return k8sClient, nil
}

// Helper functions
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}