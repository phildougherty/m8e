// internal/cmd/k8s_memory.go
package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/memory"
)

func NewK8sMemoryCommand() *cobra.Command {
	var (
		namespace        string
		port             int32
		host             string
		postgresEnabled  bool
		postgresPort     int32
		postgresDB       string
		postgresUser     string
		postgresPassword string
		cpus             string
		memoryLimit      string
		postgresCPUs     string
		postgresMemory   string
		timeout          time.Duration
	)

	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Manage the Kubernetes-native memory service",
		Long: `Manage the memory service using Kubernetes-native resources.

The Kubernetes-native memory service provides:
- PostgreSQL-backed persistent storage
- Graph-based knowledge storage
- Entity and relationship management  
- Integration with Kubernetes RBAC and networking
- Automatic scaling and health monitoring
- Persistent data with PVC storage

Examples:
  # Start the memory service
  matey memory start

  # Stop the memory service  
  matey memory stop

  # Check the status
  matey memory status

  # Restart the service
  matey memory restart

  # Get detailed information
  matey memory info`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// Global flags
	cmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	cmd.PersistentFlags().Int32Var(&port, "port", 3001, "Memory service port")
	cmd.PersistentFlags().StringVar(&host, "host", "0.0.0.0", "Memory service host")
	cmd.PersistentFlags().DurationVar(&timeout, "timeout", 5*time.Minute, "Operation timeout")

	// PostgreSQL flags
	cmd.PersistentFlags().BoolVar(&postgresEnabled, "postgres-enabled", true, "Enable PostgreSQL backend")
	cmd.PersistentFlags().Int32Var(&postgresPort, "postgres-port", 5432, "PostgreSQL port")
	cmd.PersistentFlags().StringVar(&postgresDB, "postgres-db", "memory_graph", "PostgreSQL database name")
	cmd.PersistentFlags().StringVar(&postgresUser, "postgres-user", "postgres", "PostgreSQL user")
	cmd.PersistentFlags().StringVar(&postgresPassword, "postgres-password", "password", "PostgreSQL password")

	// Resource flags
	cmd.PersistentFlags().StringVar(&cpus, "cpus", "1.0", "CPU limit for memory service")
	cmd.PersistentFlags().StringVar(&memoryLimit, "memory", "1Gi", "Memory limit for memory service")
	cmd.PersistentFlags().StringVar(&postgresCPUs, "postgres-cpus", "2.0", "CPU limit for PostgreSQL")
	cmd.PersistentFlags().StringVar(&postgresMemory, "postgres-memory", "2Gi", "Memory limit for PostgreSQL")

	// Add subcommands
	cmd.AddCommand(newMemoryStartCommand(namespace, port, host, postgresEnabled, postgresPort, postgresDB, postgresUser, postgresPassword, cpus, memoryLimit, postgresCPUs, postgresMemory, timeout))
	cmd.AddCommand(newMemoryStopCommand(namespace, timeout))
	cmd.AddCommand(newMemoryStatusCommand(namespace))
	cmd.AddCommand(newMemoryRestartCommand(namespace, timeout))
	cmd.AddCommand(newMemoryInfoCommand(namespace))
	cmd.AddCommand(newMemoryLogsCommand(namespace))

	return cmd
}

func newMemoryStartCommand(namespace string, port int32, host string, postgresEnabled bool, postgresPort int32, postgresDB, postgresUser, postgresPassword, cpus, memoryLimit, postgresCPUs, postgresMemory string, timeout time.Duration) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the memory service",
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
			manager := memory.NewK8sManager(cfg, k8sClient, namespace)
			manager.SetConfigFile(configFile)

			// Start the service
			fmt.Printf("Starting Kubernetes-native memory service in namespace: %s\n", namespace)
			if err := manager.Start(); err != nil {
				return fmt.Errorf("failed to start memory service: %w", err)
			}

			fmt.Println("Memory service started successfully!")
			fmt.Printf("You can check the status with: matey memory status -n %s\n", namespace)

			return nil
		},
	}
}

func newMemoryStopCommand(namespace string, timeout time.Duration) *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the memory service",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create Kubernetes client
			k8sClient, err := createK8sClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			// Create manager
			manager := memory.NewK8sManager(&config.ComposeConfig{}, k8sClient, namespace)

			// Stop the service
			fmt.Printf("Stopping memory service in namespace: %s\n", namespace)
			if err := manager.Stop(); err != nil {
				return fmt.Errorf("failed to stop memory service: %w", err)
			}

			fmt.Println("Memory service stopped successfully!")
			return nil
		},
	}
}

func newMemoryStatusCommand(namespace string) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check the status of the memory service",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create Kubernetes client
			k8sClient, err := createK8sClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			// Create manager
			manager := memory.NewK8sManager(&config.ComposeConfig{}, k8sClient, namespace)

			// Get status
			status, err := manager.Status()
			if err != nil {
				return fmt.Errorf("failed to get status: %w", err)
			}

			fmt.Printf("Memory Service Status: %s\n", status)
			return nil
		},
	}
}

func newMemoryRestartCommand(namespace string, timeout time.Duration) *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the memory service",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create Kubernetes client
			k8sClient, err := createK8sClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			// Create manager
			manager := memory.NewK8sManager(&config.ComposeConfig{}, k8sClient, namespace)

			// Restart the service
			fmt.Printf("Restarting memory service in namespace: %s\n", namespace)
			if err := manager.Restart(); err != nil {
				return fmt.Errorf("failed to restart memory service: %w", err)
			}

			fmt.Println("Memory service restarted successfully!")
			return nil
		},
	}
}

func newMemoryInfoCommand(namespace string) *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Get detailed information about the memory service",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create Kubernetes client
			k8sClient, err := createK8sClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			// Create manager
			manager := memory.NewK8sManager(&config.ComposeConfig{}, k8sClient, namespace)

			// Get detailed information
			info, err := manager.GetMemoryInfo()
			if err != nil {
				return fmt.Errorf("failed to get memory info: %w", err)
			}

			fmt.Printf("Memory Service Information:\n")
			fmt.Printf("  Status: %s\n", info.Status)
			fmt.Printf("  Ready Replicas: %d/%d\n", info.ReadyReplicas, info.TotalReplicas)
			if info.PostgresStatus != "" {
				fmt.Printf("  PostgreSQL Status: %s\n", info.PostgresStatus)
			}
			if len(info.Conditions) > 0 {
				fmt.Printf("  Conditions: %s\n", strings.Join(info.Conditions, ", "))
			}
			if info.Endpoint != "" {
				fmt.Printf("  Endpoint: %s\n", info.Endpoint)
			}

			return nil
		},
	}
}

func newMemoryLogsCommand(namespace string) *cobra.Command {
	return &cobra.Command{
		Use:   "logs",
		Short: "Get logs from the memory service",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create Kubernetes client
			k8sClient, err := createK8sClient()
			if err != nil {
				return fmt.Errorf("failed to create Kubernetes client: %w", err)
			}

			// Create manager
			manager := memory.NewK8sManager(&config.ComposeConfig{}, k8sClient, namespace)

			// Get logs
			logs, err := manager.GetLogs()
			if err != nil {
				return fmt.Errorf("failed to get logs: %w", err)
			}

			fmt.Printf("Memory Service Logs:\n")
			fmt.Println(strings.Repeat("-", 40))
			fmt.Print(logs)

			return nil
		},
	}
}

// Note: createK8sClient is defined in k8s_task_scheduler.go and reused here