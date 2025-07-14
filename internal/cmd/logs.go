// internal/cmd/logs.go
package cmd

import (
	"github.com/phildougherty/m8e/internal/compose"

	"github.com/spf13/cobra"
)

func NewLogsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs [SERVER...]",
		Short: "View logs from MCP servers",
		Long: `View logs from MCP servers using Kubernetes pod logs.
Special services:
  proxy          - Shows logs from proxy pods
  task-scheduler - Shows logs from task-scheduler pods
  memory         - Shows logs from memory server pods

Examples:
  matey logs                    # Show logs from all servers
  matey logs proxy -f           # Follow proxy logs
  matey logs task-scheduler -f  # Follow task scheduler logs
  matey logs memory -f          # Follow memory server logs
  matey logs filesystem -f      # Follow filesystem server logs`,
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("file")
			follow, _ := cmd.Flags().GetBool("follow")
			namespace, _ := cmd.Flags().GetString("namespace")

			return runLogsCommand(file, args, follow, namespace)
		},
	}
	cmd.Flags().BoolP("follow", "f", false, "Follow log output")

	return cmd
}

func runLogsCommand(configFile string, serverNames []string, follow bool, namespace string) error {
	// Use Kubernetes-native compose logs
	return compose.Logs(configFile, serverNames, follow)
}