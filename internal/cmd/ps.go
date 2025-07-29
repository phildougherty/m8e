// internal/cmd/ps.go
package cmd

import (
	"github.com/phildougherty/m8e/internal/compose"

	"github.com/spf13/cobra"
)

func NewPsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ps",
		Short: "Show running MCP servers with detailed process information",
		Long: `Show running MCP servers with detailed information including:
- Pod status, resource usage, and restart counts
- Network endpoints and port mappings
- Volume mounts and persistent storage
- Health check status and readiness
- Labels, annotations, and metadata`,
		RunE: func(cmd *cobra.Command, args []string) error {
			file, _ := cmd.Flags().GetString("file")
			// TODO: Implement watch mode for live updates
			// For now, just ignore the watch flag and call the regular list function

			return compose.List(file)
		},
	}

	cmd.Flags().BoolP("watch", "w", false, "Watch for live updates")
	cmd.Flags().StringP("format", "f", "table", "Output format (table, json, yaml)")
	cmd.Flags().StringP("filter", "", "", "Filter by status, namespace, or labels")

	return cmd
}
