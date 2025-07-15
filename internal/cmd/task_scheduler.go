// internal/cmd/task_scheduler.go
package cmd

import (
	"fmt"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/constants"
	"github.com/phildougherty/m8e/internal/task_scheduler"
	"github.com/spf13/cobra"
)

func NewTaskSchedulerCommand() *cobra.Command {
	var enable bool
	var disable bool

	cmd := &cobra.Command{
		Use:   "task-scheduler",
		Short: "Manage the task scheduler service (Kubernetes-native)",
		Long: `Start, stop, enable, or disable the task scheduler service using Kubernetes.
The task scheduler provides intelligent task automation with:
- Built-in cron scheduling with AI-powered expression generation
- 14 MCP tools for workflow and task management
- Kubernetes Jobs for reliable task execution
- OpenRouter and Ollama integration for LLM-powered workflows
- Workflow templates and dependency management

This is a Kubernetes-native implementation that uses CRDs and controllers.

Examples:
  matey task-scheduler               # Start task scheduler via Kubernetes
  matey task-scheduler --enable      # Enable in config
  matey task-scheduler --disable     # Disable service`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("file")
			namespace, _ := cmd.Flags().GetString("namespace")
			
			cfg, err := config.LoadConfig(configFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if enable {
				return enableTaskScheduler(configFile, cfg)
			}

			if disable {
				return disableTaskScheduler(configFile, cfg, namespace)
			}

			// Check if task scheduler is enabled in config
			if !cfg.TaskScheduler.Enabled {
				fmt.Println("Task scheduler is not enabled in configuration.")
				fmt.Println("Use --enable flag to enable it first.")
				return nil
			}

			// Always use Kubernetes mode (this is a Kubernetes-native system)
			return startK8sTaskScheduler(cfg, namespace)
		},
	}

	cmd.Flags().BoolVar(&enable, "enable", false, "Enable the task scheduler in config")
	cmd.Flags().BoolVar(&disable, "disable", false, "Disable the task scheduler")

	return cmd
}

func enableTaskScheduler(configFile string, cfg *config.ComposeConfig) error {
	fmt.Println("Enabling task scheduler...")

	// Enable in the built-in task scheduler section
	cfg.TaskScheduler.Enabled = true
	if cfg.TaskScheduler.Port == 0 {
		cfg.TaskScheduler.Port = 8018
	}
	if cfg.TaskScheduler.Host == "" {
		cfg.TaskScheduler.Host = "0.0.0.0"
	}
	if cfg.TaskScheduler.DatabasePath == "" {
		cfg.TaskScheduler.DatabasePath = "/data/task-scheduler.db"
	}
	if cfg.TaskScheduler.LogLevel == "" {
		cfg.TaskScheduler.LogLevel = "info"
	}
	if cfg.TaskScheduler.Workspace == "" {
		cfg.TaskScheduler.Workspace = "/home/phil"
	}
	if cfg.TaskScheduler.CPUs == "" {
		cfg.TaskScheduler.CPUs = "2.0"
	}
	if cfg.TaskScheduler.Memory == "" {
		cfg.TaskScheduler.Memory = "1g"
	}
	if len(cfg.TaskScheduler.Volumes) == 0 {
		cfg.TaskScheduler.Volumes = []string{"/home/phil:/workspace:rw", "/tmp:/tmp:rw"}
	}

	// Also add to servers section for proxy discovery
	if cfg.Servers == nil {
		cfg.Servers = make(map[string]config.ServerConfig)
	}

	// Add task scheduler server to servers config
	cfg.Servers["task-scheduler"] = config.ServerConfig{
		Build: config.BuildConfig{
			Context:    "github.com/phildougherty/m8e-task-scheduler.git",
			Dockerfile: "Dockerfile",
		},
		Command:      "./matey-task-scheduler",
		Args:         []string{"--host", "0.0.0.0", "--port", "8018"},
		Protocol:     "http",
		HttpPort:     constants.TaskSchedulerDefaultPort,
		User:         "root",
		ReadOnly:     false,
		Privileged:   false,
		SecurityOpt:  []string{"no-new-privileges:true"},
		Capabilities: []string{"tools"},
		Env: map[string]string{
			"NODE_ENV":           "production",
			"DATABASE_PATH":      cfg.TaskScheduler.DatabasePath,
			"MCP_PROXY_URL":      cfg.TaskScheduler.MCPProxyURL,
			"MCP_PROXY_API_KEY":  cfg.TaskScheduler.MCPProxyAPIKey,
			"OPENROUTER_API_KEY": cfg.TaskScheduler.OpenRouterAPIKey,
			"OPENROUTER_MODEL":   cfg.TaskScheduler.OpenRouterModel,
			"OLLAMA_URL":         cfg.TaskScheduler.OllamaURL,
			"OLLAMA_MODEL":       cfg.TaskScheduler.OllamaModel,
		},
		Networks: []string{"mcp-net"},
		Authentication: &config.ServerAuthConfig{
			Enabled:       true,
			RequiredScope: "mcp:tools",
			OptionalAuth:  false,
			AllowAPIKey:   &[]bool{true}[0],
		},
		Volumes: cfg.TaskScheduler.Volumes,
	}

	fmt.Printf("Task scheduler enabled in both built-in config and servers list (port: %d).\n", cfg.TaskScheduler.Port)

	return config.SaveConfig(configFile, cfg)
}

func disableTaskScheduler(configFile string, cfg *config.ComposeConfig, namespace string) error {
	fmt.Println("Disabling task scheduler...")

	// Create Kubernetes client
	k8sClient, err := createK8sClientWithScheme()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Stop the Kubernetes resources
	taskSchedulerManager := task_scheduler.NewK8sManager(cfg, k8sClient, namespace)
	if err := taskSchedulerManager.Stop(); err != nil {
		fmt.Printf("Warning: %v\n", err)
	}

	// Disable in config
	cfg.TaskScheduler.Enabled = false

	fmt.Println("Task scheduler disabled.")

	return config.SaveConfig(configFile, cfg)
}

// startK8sTaskScheduler starts the task scheduler using Kubernetes
func startK8sTaskScheduler(cfg *config.ComposeConfig, namespace string) error {
	fmt.Println("Creating Kubernetes-native MCP task scheduler...")
	fmt.Printf("Namespace: %s\n", namespace)
	
	// Create Kubernetes client
	k8sClient, err := createK8sClientWithScheme()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Create task scheduler manager and start (non-blocking)
	taskSchedulerManager := task_scheduler.NewK8sManager(cfg, k8sClient, namespace)
	if err := taskSchedulerManager.Start(); err != nil {
		return fmt.Errorf("failed to create MCPTaskScheduler resource: %w", err)
	}

	fmt.Println("MCPTaskScheduler resource created successfully")
	fmt.Println("The controller will deploy the task scheduler service automatically")
	fmt.Printf("Check deployment status with: kubectl get mcptaskscheduler -n %s\n", namespace)
	
	return nil
}

