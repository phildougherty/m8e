// internal/cmd/root.go
package cmd

import (
	"github.com/spf13/cobra"
)

func NewRootCommand(version string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "matey",
		Short:   "MCP server orchestrator",
		Long:    `Matey (m8e) is a tool for defining and running multi-server Model Context Protocol applications.`,
		Version: version,
	}

	rootCmd.PersistentFlags().StringP("file", "c", "matey.yaml", "Specify matey configuration file")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().StringP("namespace", "n", "matey", "Kubernetes namespace")

	// Add installation command
	rootCmd.AddCommand(NewInstallCommand())
	
	// Add core orchestration commands
	rootCmd.AddCommand(NewUpCommand())
	rootCmd.AddCommand(NewDownCommand())
	rootCmd.AddCommand(NewStartCommand())
	rootCmd.AddCommand(NewStopCommand())
	rootCmd.AddCommand(NewRestartCommand())
	rootCmd.AddCommand(NewPsCommand())
	rootCmd.AddCommand(NewTopCommand())
	rootCmd.AddCommand(NewChatCommand()) // AI chat interface
	rootCmd.AddCommand(NewLogsCommand())
	
	// Add toolbox management commands
	rootCmd.AddCommand(NewToolboxCommand())
	
	// Add utility commands
	rootCmd.AddCommand(NewValidateCommand())
	rootCmd.AddCommand(NewCompletionCommand())
	rootCmd.AddCommand(NewCreateConfigCommand())
	rootCmd.AddCommand(NewProxyCommand())
	rootCmd.AddCommand(NewServeProxyCommand()) // Internal command for containers
	rootCmd.AddCommand(NewReloadCommand())
	
	// Add service-specific commands
	rootCmd.AddCommand(NewTaskSchedulerCommand())
	rootCmd.AddCommand(NewSchedulerServerCommand()) // Built-in scheduler MCP server
	rootCmd.AddCommand(schedulerExecuteWorkflowCmd) // Internal workflow execution command
	rootCmd.AddCommand(NewMemoryCommand())
	rootCmd.AddCommand(NewPostgresCommand()) // Built-in PostgreSQL service
	// TODO: Workflow commands migrated to Task Scheduler
	// rootCmd.AddCommand(NewWorkflowCommand())
	rootCmd.AddCommand(NewInspectCommand())
	
	// Add file editing and analysis commands
	rootCmd.AddCommand(NewEditCommand())
	rootCmd.AddCommand(NewSearchCommand())
	rootCmd.AddCommand(NewContextCommand())
	rootCmd.AddCommand(NewParseCommand())
	
	// Add MCP server command
	rootCmd.AddCommand(NewMCPServerCommand())
	
	// Add controller manager command
	rootCmd.AddCommand(NewControllerManagerCommand())

	return rootCmd
}
