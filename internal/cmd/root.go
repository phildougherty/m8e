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
	rootCmd.PersistentFlags().StringP("namespace", "n", "default", "Kubernetes namespace")

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
	rootCmd.AddCommand(NewDashboardCommand())
	rootCmd.AddCommand(NewTViewDashboardCommand()) // AI chat interface
	rootCmd.AddCommand(NewChatCommand()) // Alternative terminal chat
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
	rootCmd.AddCommand(NewMemoryCommand())
	rootCmd.AddCommand(NewWorkflowCommand())
	rootCmd.AddCommand(NewInspectCommand())
	
	// Add MCP server command
	rootCmd.AddCommand(NewMCPServerCommand())

	return rootCmd
}
