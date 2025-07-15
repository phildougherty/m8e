// internal/cmd/root.go
package cmd

import (
	"github.com/spf13/cobra"
)

func NewRootCommand(version string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "matey",
		Short:   "Kubernetes-native MCP server orchestrator",
		Long:    `Matey (m8e) is a Kubernetes-native tool for defining and running multi-server Model Context Protocol applications using Kubernetes resources.`,
		Version: version,
	}

	rootCmd.PersistentFlags().StringP("file", "c", "matey.yaml", "Specify matey configuration file")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().StringP("namespace", "n", "default", "Kubernetes namespace")

	// Add installation command
	rootCmd.AddCommand(NewInstallCommand())
	
	// Add core orchestration commands (Kubernetes-native by default)
	rootCmd.AddCommand(NewUpCommand())
	rootCmd.AddCommand(NewDownCommand())
	rootCmd.AddCommand(NewStartCommand())
	rootCmd.AddCommand(NewStopCommand())
	rootCmd.AddCommand(NewRestartCommand())
	rootCmd.AddCommand(NewPsCommand())
	rootCmd.AddCommand(NewTopCommand())
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
	
	// Add service-specific commands (both legacy and Kubernetes-native)
	rootCmd.AddCommand(NewTaskSchedulerCommand())
	rootCmd.AddCommand(NewMemoryCommand())
	rootCmd.AddCommand(NewWorkflowCommand())
	
	// Add Kubernetes-native service commands
	k8sCmd := &cobra.Command{
		Use:   "k8s",
		Short: "Kubernetes-native service management commands",
		Long:  `Direct Kubernetes-native commands for managing individual services with full control over Kubernetes resources.`,
	}
	k8sCmd.AddCommand(NewK8sTaskSchedulerCommand())
	k8sCmd.AddCommand(NewK8sMemoryCommand())
	rootCmd.AddCommand(k8sCmd)

	return rootCmd
}
