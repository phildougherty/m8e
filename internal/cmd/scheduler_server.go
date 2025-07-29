// internal/cmd/scheduler_server.go
package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/logging"
	"github.com/phildougherty/m8e/internal/scheduler"
	"github.com/spf13/cobra"
)

func NewSchedulerServerCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scheduler-server",
		Short: "Run the built-in task scheduler engine",
		Long: `Run the built-in task scheduler engine that provides intelligent task automation.

The scheduler engine provides:
- Automated workflow execution based on CRD schedules
- AI-powered workflow orchestration and dependency management
- Task execution with Kubernetes Jobs
- OpenRouter and Ollama integration for LLM-powered workflows
- 2-way sync between Kubernetes CRDs and PostgreSQL

MCP tools for scheduler management are now provided by the MateyMCPServer.

Examples:
  matey scheduler-server                    # Run scheduler engine
  matey scheduler-server --namespace prod   # Run in specific namespace`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("file")
			
			// Load configuration
			cfg, err := config.LoadConfig(configFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}


			// Set up logging
			logLevel := "info"
			if cfg.TaskScheduler != nil && cfg.TaskScheduler.LogLevel != "" {
				logLevel = cfg.TaskScheduler.LogLevel
			}
			
			logger := logging.NewLogger(logLevel)
			logr := logger.GetLogr()
			
			// Create the cron and workflow engines
			cronEngine := scheduler.NewCronEngine(logr.WithName("cron"))
			
			// Get MCP proxy configuration with Kubernetes service discovery
			var mcpProxyURL, mcpProxyAPIKey string
			
			if cfg.TaskScheduler != nil {
				mcpProxyURL = cfg.TaskScheduler.MCPProxyURL
				mcpProxyAPIKey = cfg.TaskScheduler.MCPProxyAPIKey
			}
			
			// If no proxy URL configured, try Kubernetes service discovery
			if mcpProxyURL == "" {
				namespace, _ := cmd.Flags().GetString("namespace")
				if namespace == "" {
					namespace = "matey"
				}
				// Use Kubernetes service DNS for proxy discovery
				mcpProxyURL = fmt.Sprintf("http://matey-proxy.%s.svc.cluster.local:9876", namespace)
				logger.Info("Using Kubernetes service discovery for MCP proxy: %s", mcpProxyURL)
			}
			
			workflowEngine := scheduler.NewWorkflowEngine(mcpProxyURL, mcpProxyAPIKey, logr.WithName("workflow"))
			
			// Set up PostgreSQL workflow store if database is available
			var workflowStore *scheduler.WorkflowStore
			if cfg != nil && cfg.TaskScheduler != nil && cfg.TaskScheduler.DatabaseURL != "" {
				store, err := scheduler.NewWorkflowStore(cfg.TaskScheduler.DatabaseURL, logr.WithName("workflow-store"))
				if err != nil {
					logger.Warning("Failed to create workflow store, falling back to CRD storage: %v", err)
				} else {
					workflowStore = store
					logger.Info("PostgreSQL workflow store initialized successfully")
				}
			} else {
				// Try default PostgreSQL connection
				defaultDatabaseURL := "postgresql://postgres:password@matey-postgres:5432/matey?sslmode=disable"
				store, err := scheduler.NewWorkflowStore(defaultDatabaseURL, logr.WithName("workflow-store"))
				if err != nil {
					logger.Warning("Failed to create default workflow store, falling back to CRD storage: %v", err)
				} else {
					workflowStore = store
					logger.Info("Default PostgreSQL workflow store initialized successfully")
				}
			}

			// Set up Kubernetes client for CRD access
			namespace, _ := cmd.Flags().GetString("namespace")
			if namespace == "" {
				namespace = "matey"
			}

			k8sClient, err := createK8sClientWithScheme()
			var workflowScheduler *scheduler.WorkflowScheduler
			if err != nil {
				logger.Warning("Failed to create Kubernetes client, workflow CRD management will be disabled: %v", err)
			} else {
				logger.Info("Kubernetes client configured for workflow management")
				
				// Create and start workflow scheduler for automated workflow execution with K8s Job execution
				workflowScheduler = scheduler.NewWorkflowScheduler(cronEngine, workflowEngine, k8sClient, namespace, cfg, logr.WithName("workflow-scheduler"))
				
				// Set workflow store for PostgreSQL sync if available
				if workflowStore != nil {
					workflowScheduler.SetWorkflowStore(workflowStore)
					workflowScheduler.EnableDeletionSync() // Enable safe deletion sync
					logger.Info("Workflow scheduler connected to PostgreSQL workflow store with deletion sync enabled")
				}
				
				if err := workflowScheduler.Start(); err != nil {
					logger.Error("Failed to start workflow scheduler: %v", err)
				} else {
					logger.Info("Workflow scheduler started successfully")
				}
			}
			
			logger.Info("Task scheduler engine running (MCP tools now provided by MateyMCPServer)")
			logger.Info("Workflow scheduler will automatically execute scheduled workflows")
			
			// Wait for interrupt signal
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
			<-quit
			
			logger.Info("Shutting down task scheduler engine...")
			
			// Stop workflow scheduler if it was started
			if workflowScheduler != nil {
				logger.Info("Stopping workflow scheduler...")
				workflowScheduler.Stop()
			}
			
			logger.Info("Task scheduler engine stopped")
			return nil
		},
	}

	return cmd
}

