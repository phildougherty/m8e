// internal/cmd/restart.go
package cmd

import (
	"github.com/phildougherty/m8e/internal/compose"

	"github.com/spf13/cobra"
)

func NewRestartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restart [SERVICE...]",
		Short: "Restart MCP services using Kubernetes resources",
		Long: `Restart MCP services using system resources.

This command stops and then starts the specified services by updating their
Kubernetes Deployments and related resources.

Examples:
  matey restart                    # Restart all enabled services
  matey restart memory             # Restart only memory service
  matey restart memory task-scheduler  # Restart memory and task scheduler`,
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("file")
			namespace, _ := cmd.Flags().GetString("namespace")

			composer, err := compose.NewK8sComposer(file, namespace)
			if err != nil {
				return err
			}

			// If no args provided, restart all enabled services
			if len(args) == 0 {
				return composer.Restart([]string{})
			}

			return composer.Restart(args)
		},
	}

	return cmd
}
