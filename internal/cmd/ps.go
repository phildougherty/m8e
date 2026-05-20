// internal/cmd/ps.go
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

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
			watch, _ := cmd.Flags().GetBool("watch")
			interval, _ := cmd.Flags().GetDuration("interval")

			if !watch {
				return compose.List(file)
			}

			return runPsWatch(cmd.Context(), file, interval)
		},
	}

	cmd.Flags().BoolP("watch", "w", false, "Watch for live updates until interrupted (Ctrl-C)")
	cmd.Flags().Duration("interval", 2*time.Second, "Refresh interval when --watch is set")
	cmd.Flags().StringP("format", "f", "table", "Output format (table, json, yaml)")
	cmd.Flags().StringP("filter", "", "", "Filter by status, namespace, or labels")

	return cmd
}

// runPsWatch re-renders the server status table on a fixed interval until the
// context is cancelled or the operator interrupts with Ctrl-C, emulating
// `kubectl get pods -w` / `watch`. A transient gather error is printed but does
// not abort the loop; only context cancellation ends it.
func runPsWatch(parent context.Context, file string, interval time.Duration) error {
	if interval <= 0 {
		interval = 2 * time.Second
	}

	// Be defensive: the inherited context may not be signal-aware, so wire our
	// own SIGINT/SIGTERM handler for clean Ctrl-C exit.
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	render := func() {
		// Clear screen + home cursor so the table redraws in place like `top`.
		// Stdout write errors are not actionable in a `top`-style watch loop;
		// SIGPIPE will tear the process down on a broken pipe.
		_, _ = fmt.Fprint(os.Stdout, "\033[2J\033[H")
		_, _ = fmt.Fprintf(os.Stdout, "Watching MCP servers (refresh: %s, press Ctrl-C to exit) - %s\n\n",
			interval, time.Now().Format("15:04:05"))

		status, err := compose.GatherStatus(file)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stdout, "error refreshing status: %v\n", err)
			return
		}
		compose.RenderStatus(os.Stdout, status)
	}

	// Render immediately so the operator sees output without waiting a full tick.
	render()

	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stdout)
			return nil
		case <-ticker.C:
			render()
		}
	}
}
