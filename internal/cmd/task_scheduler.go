// internal/cmd/task_scheduler.go
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/phildougherty/m8e/internal/config"

	"github.com/spf13/cobra"
)

func NewTaskSchedulerCommand() *cobra.Command {
	var (
		port             int
		host             string
		enable           bool
		disable          bool
		dbPath           string
		workspace        string
		logLevel         string
		mcpProxyURL      string
		mcpProxyAPIKey   string
		ollamaURL        string
		ollamaModel      string
		openrouterAPIKey string
		openrouterModel  string
		debug            bool
	)

	cmd := &cobra.Command{
		Use:   "task-scheduler",
		Short: "Manage the task scheduler service (native mode)",
		Long: `Start the MCP task scheduler service in native mode.
        
The task scheduler provides intelligent task automation with:
- AI-powered cron expression generation
- Kubernetes-native deployment (use 'matey k8s task-scheduler' for K8s mode)
- OpenRouter and Ollama integration
- Task history and monitoring`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configFile, _ := cmd.Flags().GetString("file")
			
			cfg, err := config.LoadConfig(configFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if enable {
				if cfg.TaskScheduler == nil {
					cfg.TaskScheduler = &config.TaskScheduler{}
				}
				cfg.TaskScheduler.Enabled = true
				return config.SaveConfig(configFile, cfg)
			}

			if disable {
				if cfg.TaskScheduler != nil {
					cfg.TaskScheduler.Enabled = false
				}
				return config.SaveConfig(configFile, cfg)
			}

			// Apply config values if not provided via flags
			if cfg.TaskScheduler != nil {
				if port == 0 && cfg.TaskScheduler.Port != 0 {
					port = cfg.TaskScheduler.Port
				}
				if host == "" && cfg.TaskScheduler.Host != "" {
					host = cfg.TaskScheduler.Host
				}
				if dbPath == "" && cfg.TaskScheduler.DatabasePath != "" {
					dbPath = cfg.TaskScheduler.DatabasePath
				}
				if workspace == "" && cfg.TaskScheduler.Workspace != "" {
					workspace = cfg.TaskScheduler.Workspace
				}
				if logLevel == "" && cfg.TaskScheduler.LogLevel != "" {
					logLevel = cfg.TaskScheduler.LogLevel
				}
				if mcpProxyURL == "" && cfg.TaskScheduler.MCPProxyURL != "" {
					mcpProxyURL = cfg.TaskScheduler.MCPProxyURL
				}
				if mcpProxyAPIKey == "" && cfg.TaskScheduler.MCPProxyAPIKey != "" {
					mcpProxyAPIKey = cfg.TaskScheduler.MCPProxyAPIKey
				}
				if ollamaURL == "" && cfg.TaskScheduler.OllamaURL != "" {
					ollamaURL = cfg.TaskScheduler.OllamaURL
				}
				if ollamaModel == "" && cfg.TaskScheduler.OllamaModel != "" {
					ollamaModel = cfg.TaskScheduler.OllamaModel
				}
				if openrouterAPIKey == "" && cfg.TaskScheduler.OpenRouterAPIKey != "" {
					openrouterAPIKey = cfg.TaskScheduler.OpenRouterAPIKey
				}
				if openrouterModel == "" && cfg.TaskScheduler.OpenRouterModel != "" {
					openrouterModel = cfg.TaskScheduler.OpenRouterModel
				}
			}

			// Apply server config values if available (as fallback)
			if serverConfig, exists := cfg.Servers["task-scheduler"]; exists {
				if port == 0 && serverConfig.HttpPort != 0 {
					port = serverConfig.HttpPort
				}
				// Use environment variables from config
				if mcpProxyURL == "" {
					if url, ok := serverConfig.Env["MCP_PROXY_URL"]; ok {
						mcpProxyURL = url
					}
				}
				if mcpProxyAPIKey == "" {
					if key, ok := serverConfig.Env["MCP_PROXY_API_KEY"]; ok {
						mcpProxyAPIKey = key
					}
				}
				if ollamaURL == "" {
					if url, ok := serverConfig.Env["MCP_CRON_OLLAMA_BASE_URL"]; ok {
						ollamaURL = url
					}
				}
				if ollamaModel == "" {
					if model, ok := serverConfig.Env["MCP_CRON_OLLAMA_DEFAULT_MODEL"]; ok {
						ollamaModel = model
					}
				}
				if openrouterAPIKey == "" {
					if key, ok := serverConfig.Env["OPENROUTER_API_KEY"]; ok {
						openrouterAPIKey = key
					}
				}
				if openrouterModel == "" {
					if model, ok := serverConfig.Env["OPENROUTER_MODEL"]; ok {
						openrouterModel = model
					}
				}
			}

			// Set final defaults
			if port == 0 {
				port = 8080
			}
			if host == "" {
				host = "0.0.0.0"
			}
			if workspace == "" {
				workspace = "/home/phil" // Default workspace
			}
			if dbPath == "" {
				dbPath = "/data/task-scheduler.db"
			}

			fmt.Printf("Starting native task scheduler with port: %d\n", port)

			return runNativeTaskScheduler(cfg, port, host, dbPath, workspace, logLevel, debug)
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 0, "Task scheduler port (default: from config or 8080)")
	cmd.Flags().StringVar(&host, "host", "", "Task scheduler host interface (default: 0.0.0.0)")
	cmd.Flags().BoolVar(&enable, "enable", false, "Enable the task scheduler in config")
	cmd.Flags().BoolVar(&disable, "disable", false, "Disable the task scheduler")
	cmd.Flags().StringVar(&dbPath, "db-path", "", "Database path (default: /data/task-scheduler.db)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace directory (default: /home/phil)")
	cmd.Flags().StringVar(&logLevel, "log-level", "", "Log level (debug, info, warn, error)")
	cmd.Flags().StringVar(&mcpProxyURL, "mcp-proxy-url", "", "MCP Proxy URL")
	cmd.Flags().StringVar(&mcpProxyAPIKey, "mcp-proxy-api-key", "", "MCP Proxy API key")
	cmd.Flags().StringVar(&ollamaURL, "ollama-url", "", "Ollama base URL")
	cmd.Flags().StringVar(&ollamaModel, "ollama-model", "", "Ollama model")
	cmd.Flags().StringVar(&openrouterAPIKey, "openrouter-api-key", "", "OpenRouter API key")
	cmd.Flags().StringVar(&openrouterModel, "openrouter-model", "", "OpenRouter model")
	cmd.Flags().BoolVar(&debug, "debug", false, "Enable debug mode")

	return cmd
}

func runNativeTaskScheduler(cfg *config.ComposeConfig, port int, host, dbPath, workspace, logLevel string, debug bool) error {
	fmt.Printf("Starting native task scheduler on %s:%d...\n", host, port)
	fmt.Printf("Using workspace: %s\n", workspace)
	fmt.Printf("Using database: %s\n", dbPath)

	// Check if we're in the mcp-cron-persistent directory or if it exists as a subdirectory
	mcpCronPaths := []string{
		"./mcp-cron",                                   // If built in current directory
		"../mcp-cron-persistent/mcp-cron",              // If in subdirectory
		"../mcp-cron-persistent/cmd/mcp-cron/mcp-cron", // Alternative path
	}

	var mcpCronPath string
	for _, path := range mcpCronPaths {
		if _, err := os.Stat(path); err == nil {
			mcpCronPath = path
			break
		}
	}

	if mcpCronPath == "" {
		return fmt.Errorf("mcp-cron binary not found. Please build it first:\n" +
			"cd ../mcp-cron-persistent && go build -o mcp-cron ./cmd/mcp-cron")
	}

	// Set up environment from config and parameters
	env := os.Environ()
	env = append(env, fmt.Sprintf("MCP_CRON_SERVER_ADDRESS=%s", host))
	env = append(env, fmt.Sprintf("MCP_CRON_SERVER_PORT=%d", port))
	env = append(env, fmt.Sprintf("MCP_CRON_DATABASE_PATH=%s", dbPath))
	env = append(env, "MCP_CRON_SERVER_TRANSPORT=sse")
	env = append(env, "MCP_CRON_DATABASE_ENABLED=true")

	// Add additional environment from config if available
	if serverConfig, exists := cfg.Servers["task-scheduler"]; exists {
		for key, value := range serverConfig.Env {
			env = append(env, fmt.Sprintf("%s=%s", key, value))
		}
	}

	if logLevel != "" {
		env = append(env, fmt.Sprintf("MCP_CRON_LOGGING_LEVEL=%s", logLevel))
	}

	if debug {
		env = append(env, "MCP_CRON_DEBUG=true")
		env = append(env, "MCP_CRON_LOGGING_LEVEL=debug")
	}

	// Create the command
	args := []string{"--transport", "sse", "--address", host, "--port", fmt.Sprintf("%d", port), "--db-path", dbPath}
	if debug {
		args = append(args, "--debug-config")
	}

	cmd := exec.Command(mcpCronPath, args...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		fmt.Println("\nShutting down task scheduler...")
		cancel()
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
		}
	}()

	fmt.Printf("Task scheduler running at http://%s:%d\n", host, port)
	fmt.Printf("Available endpoints:\n")
	fmt.Printf("  Health Check:  http://%s:%d/health\n", host, port)
	fmt.Printf("  SSE Endpoint:  http://%s:%d/sse\n", host, port)

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start task scheduler: %w", err)
	}

	// Wait for completion or cancellation
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-done:
		if err != nil {
			return fmt.Errorf("task scheduler exited with error: %w", err)
		}
		return nil
	}
}