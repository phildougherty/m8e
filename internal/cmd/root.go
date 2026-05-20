// internal/cmd/root.go
package cmd

import (
	"github.com/spf13/cobra"

	"github.com/phildougherty/m8e/internal/constants"
)

// Command group IDs. Cobra renders each group as its own section in --help,
// which keeps operator-facing commands from being buried among the internal
// plumbing commands that only run inside containers.
const (
	groupLifecycle  = "lifecycle"
	groupInspection = "inspection"
	groupConfig     = "configuration"
	groupServices   = "services"
)

func NewRootCommand(version string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "matey",
		Short:   "Kubernetes-native orchestrator for Model Context Protocol servers",
		Long:    `Matey (m8e) defines and runs multi-server Model Context Protocol applications on Kubernetes.`,
		Version: version,
	}

	rootCmd.PersistentFlags().StringP("file", "c", "matey.yaml", "Specify matey configuration file")
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().StringP("namespace", "n", constants.MateyNamespace, "Kubernetes namespace")

	rootCmd.AddGroup(
		&cobra.Group{ID: groupLifecycle, Title: "Lifecycle Commands:"},
		&cobra.Group{ID: groupInspection, Title: "Inspection Commands:"},
		&cobra.Group{ID: groupConfig, Title: "Configuration Commands:"},
		&cobra.Group{ID: groupServices, Title: "Service Commands:"},
	)

	// Lifecycle: create, start, stop, and remove MCP services.
	addToGroup(rootCmd, groupLifecycle,
		NewUpCommand(),
		NewDownCommand(),
		NewStartCommand(),
		NewStopCommand(),
		NewRestartCommand(),
	)

	// Inspection: observe what is running and what happened to it.
	addToGroup(rootCmd, groupInspection,
		NewPsCommand(),
		NewTopCommand(),
		NewLogsCommand(),
		NewEventsCommand(),
		NewInspectCommand(),
	)

	// Configuration: validate, generate, and install matey configuration.
	addToGroup(rootCmd, groupConfig,
		NewValidateCommand(),
		NewCreateConfigCommand(),
		NewInstallCommand(),
		NewReloadCommand(),
	)

	// Services: long-lived components an operator manages directly.
	addToGroup(rootCmd, groupServices,
		NewProxyCommand(),
		NewMemoryCommand(),
		NewTaskSchedulerCommand(),
	)

	// Shell completion stays in the default (ungrouped) section alongside help.
	rootCmd.AddCommand(NewCompletionCommand())

	// Internal plumbing commands: invoked by matey-managed pods, never by an
	// operator at a shell. They remain runnable but are hidden from --help so
	// the command surface stays small and obvious.
	for _, c := range []*cobra.Command{
		NewControllerManagerCommand(),
		NewMCPServerCommand(),
		NewServeProxyCommand(),
		NewSchedulerServerCommand(),
		schedulerExecuteWorkflowCmd,
		NewPostgresCommand(),
	} {
		c.Hidden = true
		rootCmd.AddCommand(c)
	}

	return rootCmd
}

// addToGroup assigns each command to a cobra group and registers it on the root.
func addToGroup(root *cobra.Command, groupID string, cmds ...*cobra.Command) {
	for _, c := range cmds {
		c.GroupID = groupID
		root.AddCommand(c)
	}
}
