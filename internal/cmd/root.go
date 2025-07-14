// internal/cmd/root.go
package cmd

import (
	"github.com/spf13/cobra"
)

func NewRootCommand(version string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "matey",
		Short:   "Kubernetes-native MCP server orchestrator",
		Long:    `Matey (m8e) is a Kubernetes-native tool for defining and running multi-server Model Context Protocol applications using Helm charts.`,
		Version: version, // ← Add this line to enable --version flag
	}

	rootCmd.PersistentFlags().StringP("file", "c", "matey.yaml", "Specify matey configuration file")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")

	// Add subcommands
	rootCmd.AddCommand(NewUpCommand())
	rootCmd.AddCommand(NewDownCommand())
	rootCmd.AddCommand(NewStartCommand())
	rootCmd.AddCommand(NewStopCommand())
	rootCmd.AddCommand(NewRestartCommand())
	rootCmd.AddCommand(NewPsCommand())
	rootCmd.AddCommand(NewTopCommand())
	rootCmd.AddCommand(NewLogsCommand())
	rootCmd.AddCommand(NewValidateCommand())
	rootCmd.AddCommand(NewCompletionCommand())
	rootCmd.AddCommand(NewCreateConfigCommand())
	rootCmd.AddCommand(NewProxyCommand())
	rootCmd.AddCommand(NewReloadCommand())
	rootCmd.AddCommand(NewDashboardCommand())
	rootCmd.AddCommand(NewTaskSchedulerCommand())
	rootCmd.AddCommand(NewMemoryCommand())

	return rootCmd
}
