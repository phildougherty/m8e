// internal/cmd/top.go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func NewTopCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "top",
		Short: "Real-time monitoring dashboard for MCP servers",
		Long: `Real-time TUI dashboard showing live metrics like htop/nvtop:
- Live CPU, memory, network I/O per pod
- Resource utilization graphs and charts
- MCP proxy request/response metrics
- Registry usage and image pull statistics
- Active connections and session counts
- Error rates and health status
- Interactive sorting and filtering
- Drill-down into individual pod details
- Cluster-wide resource overview`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: Implement TUI dashboard using tview
			// For now, return an error indicating it's not yet implemented
			return fmt.Errorf("top command not yet implemented - coming soon with Kubernetes native features")
		},
	}

	cmd.Flags().StringP("sort", "s", "cpu", "Sort by (cpu, memory, name, status)")
	cmd.Flags().IntP("refresh", "r", 2, "Refresh interval in seconds")
	cmd.Flags().Bool("compact", false, "Compact view")

	return cmd
}