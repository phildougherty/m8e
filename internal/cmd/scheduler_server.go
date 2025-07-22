// internal/cmd/scheduler_server.go
package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/logging"
	"github.com/phildougherty/m8e/internal/scheduler"
	"github.com/spf13/cobra"
)

func NewSchedulerServerCommand() *cobra.Command {
	var port string
	var host string

	cmd := &cobra.Command{
		Use:   "scheduler-server",
		Short: "Run the built-in task scheduler MCP server",
		Long: `Run the built-in task scheduler MCP server that provides intelligent task automation.

The scheduler server provides MCP tools for:
- Creating and managing scheduled tasks
- AI-powered cron expression generation  
- Workflow orchestration and dependency management
- Task execution with Kubernetes Jobs
- OpenRouter and Ollama integration for LLM-powered workflows

This server runs as an HTTP service with MCP protocol support including SSE and WebSocket endpoints.

Examples:
  matey scheduler-server                    # Run with default settings
  matey scheduler-server --port 8084       # Run on specific port
  matey scheduler-server --host 0.0.0.0    # Bind to all interfaces`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("file")
			
			// Load configuration
			cfg, err := config.LoadConfig(configFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Use config values if not overridden by flags
			if !cmd.Flags().Changed("port") && cfg.TaskScheduler != nil && cfg.TaskScheduler.Port > 0 {
				port = fmt.Sprintf("%d", cfg.TaskScheduler.Port)
			}
			if !cmd.Flags().Changed("host") && cfg.TaskScheduler != nil && cfg.TaskScheduler.Host != "" {
				host = cfg.TaskScheduler.Host
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
					namespace = "default"
				}
				// Use Kubernetes service DNS for proxy discovery
				mcpProxyURL = fmt.Sprintf("http://matey-proxy.%s.svc.cluster.local:9876", namespace)
				logger.Info("Using Kubernetes service discovery for MCP proxy: %s", mcpProxyURL)
			}
			
			workflowEngine := scheduler.NewWorkflowEngine(mcpProxyURL, mcpProxyAPIKey, logr.WithName("workflow"))
			
			// Create the MCP tool server
			toolServer := scheduler.NewMCPToolServer(cronEngine, workflowEngine, logr.WithName("scheduler"))

			// Set up Kubernetes client for CRD access
			namespace, _ := cmd.Flags().GetString("namespace")
			if namespace == "" {
				namespace = "default"
			}

			k8sClient, err := createK8sClientWithScheme()
			var workflowScheduler *scheduler.WorkflowScheduler
			if err != nil {
				logger.Warning("Failed to create Kubernetes client, workflow CRD management will be disabled: %v", err)
			} else {
				toolServer.SetK8sClient(k8sClient, namespace)
				logger.Info("Kubernetes client configured for workflow management")
				
				// Create and start workflow scheduler for automated workflow execution with K8s Job execution
				workflowScheduler = scheduler.NewWorkflowScheduler(cronEngine, workflowEngine, k8sClient, namespace, cfg, logr.WithName("workflow-scheduler"))
				if err := workflowScheduler.Start(); err != nil {
					logger.Error("Failed to start workflow scheduler: %v", err)
				} else {
					logger.Info("Workflow scheduler started successfully")
				}
				
				// Connect the toolServer to the workflowScheduler for synchronization
				toolServer.SetWorkflowScheduler(workflowScheduler)
				logger.Info("Connected MCP tool server to workflow scheduler for live sync")
			}
			
			// Create the MCP server
			mcpServer := scheduler.NewMCPServer(toolServer, logr.WithName("mcp"))
			
			// Set up HTTP server
			mux := http.NewServeMux()
			mcpServer.SetupRoutes(mux)
			
			server := &http.Server{
				Addr:    fmt.Sprintf("%s:%s", host, port),
				Handler: mux,
			}
			
			// Start server in goroutine
			go func() {
				logger.Info("Starting task scheduler MCP server")
				logger.Info("Server address: %s", server.Addr)
				if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logger.Error("Failed to start server: %v", err)
					os.Exit(1)
				}
			}()
			
			// Wait for interrupt signal
			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
			<-quit
			
			logger.Info("Shutting down task scheduler MCP server...")
			
			// Graceful shutdown
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			
			// Stop workflow scheduler if it was started
			if workflowScheduler != nil {
				logger.Info("Stopping workflow scheduler...")
				workflowScheduler.Stop()
			}
			
			if err := server.Shutdown(ctx); err != nil {
				logger.Error("Failed to gracefully shutdown server: %v", err)
				return err
			}
			
			logger.Info("Task scheduler MCP server stopped")
			return nil
		},
	}

	cmd.Flags().StringVarP(&port, "port", "p", "8084", "Port to run the scheduler server on")
	cmd.Flags().StringVarP(&host, "host", "H", "0.0.0.0", "Host to bind the scheduler server to")

	return cmd
}

