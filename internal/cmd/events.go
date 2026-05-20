// internal/cmd/events.go
package cmd

import (
	"context"
	"fmt"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/phildougherty/m8e/internal/constants"
)

// mateyEventKinds are the involved-object kinds an operator cares about when
// debugging matey. Events on the matey CRDs are always relevant; events on
// their child workloads (Deployments/Pods/Jobs/etc.) are relevant because
// matey installs into its own namespace, so everything there is matey's.
var mateyEventKinds = map[string]bool{
	"MCPServer":        true,
	"MCPProxy":         true,
	"MCPMemory":        true,
	"MCPPostgres":      true,
	"MCPTaskScheduler": true,
	"Deployment":       true,
	"ReplicaSet":       true,
	"StatefulSet":      true,
	"Pod":              true,
	"Job":              true,
	"Service":          true,
}

// NewEventsCommand returns the `matey events` command. It surfaces Kubernetes
// Events for matey-managed resources — the thing an operator reaches for when
// an MCPServer is stuck in CrashLoopBackOff and `matey ps` only says "not
// ready". Previously this required dropping to raw kubectl.
func NewEventsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events [resource-name]",
		Short: "Show Kubernetes events for matey-managed resources",
		Long: `Show recent Kubernetes events for matey-managed resources.

With no argument, shows events for every matey resource in the namespace.
With a resource name, scopes to events whose involved object matches that name
(useful for "why is my-server not starting?").

Events are ordered oldest-first so the most recent activity is at the bottom,
matching kubectl's convention.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			namespace, _ := cmd.Flags().GetString("namespace")
			if namespace == "" {
				namespace = constants.MateyNamespace
			}
			watch, _ := cmd.Flags().GetBool("watch")

			var resourceName string
			if len(args) == 1 {
				resourceName = args[0]
			}

			kc, err := newKubeClient()
			if err != nil {
				return err
			}

			if !watch {
				return printEvents(cmd.Context(), kc, namespace, resourceName, cmd.OutOrStdout())
			}

			return watchEvents(cmd.Context(), kc, namespace, resourceName, cmd.OutOrStdout())
		},
	}

	cmd.Flags().BoolP("watch", "w", false, "Re-render events on an interval until interrupted")

	return cmd
}

// fetchMateyEvents lists events in the namespace and filters them to matey's
// involved-object kinds, optionally scoped to one resource name.
func fetchMateyEvents(ctx context.Context, kc client.Client, namespace, resourceName string) ([]corev1.Event, error) {
	var list corev1.EventList
	if err := kc.List(ctx, &list, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("failed to list events in namespace %q: %w", namespace, err)
	}

	filtered := make([]corev1.Event, 0, len(list.Items))
	for _, ev := range list.Items {
		if !mateyEventKinds[ev.InvolvedObject.Kind] {
			continue
		}
		if resourceName != "" && !strings.Contains(ev.InvolvedObject.Name, resourceName) {
			continue
		}
		filtered = append(filtered, ev)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return eventTime(filtered[i]).Before(eventTime(filtered[j]))
	})

	return filtered, nil
}

// eventTime returns the most recent timestamp on an event, preferring the
// series/last-observed time and falling back to the creation time.
func eventTime(ev corev1.Event) time.Time {
	if ev.Series != nil && !ev.Series.LastObservedTime.IsZero() {
		return ev.Series.LastObservedTime.Time
	}
	if !ev.LastTimestamp.IsZero() {
		return ev.LastTimestamp.Time
	}
	if !ev.EventTime.IsZero() {
		return ev.EventTime.Time
	}

	return ev.CreationTimestamp.Time
}

func printEvents(ctx context.Context, kc client.Client, namespace, resourceName string, out interface{ Write([]byte) (int, error) }) error {
	events, err := fetchMateyEvents(ctx, kc, namespace, resourceName)
	if err != nil {
		return err
	}
	renderEvents(events, namespace, resourceName, out)

	return nil
}

// watchEvents re-renders the event table on an interval until the context is
// cancelled or the operator interrupts with Ctrl-C. It uses a ticker + select
// on ctx.Done(); there is deliberately no bare time.Sleep here.
func watchEvents(parent context.Context, kc client.Client, namespace, resourceName string, out interface{ Write([]byte) (int, error) }) error {
	ctx, stop := signal.NotifyContext(parent, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	const interval = 2 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	render := func() error {
		events, err := fetchMateyEvents(ctx, kc, namespace, resourceName)
		if err != nil {
			return err
		}
		// Clear screen + home cursor so the table redraws in place.
		_, _ = out.Write([]byte("\033[2J\033[H"))
		_, _ = fmt.Fprintf(out, "matey events (watching, refresh %s, Ctrl-C to exit)\n\n", interval)
		renderEvents(events, namespace, resourceName, out)

		return nil
	}

	if err := render(); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := render(); err != nil {
				return err
			}
		}
	}
}

func renderEvents(events []corev1.Event, namespace, resourceName string, out interface{ Write([]byte) (int, error) }) {
	if len(events) == 0 {
		scope := fmt.Sprintf("namespace %q", namespace)
		if resourceName != "" {
			scope = fmt.Sprintf("resource %q in namespace %q", resourceName, namespace)
		}
		_, _ = fmt.Fprintf(out, "No matey events found for %s.\n", scope)

		return
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "LAST SEEN\tTYPE\tREASON\tOBJECT\tCOUNT\tMESSAGE")
	now := time.Now()
	for _, ev := range events {
		object := ev.InvolvedObject.Kind + "/" + ev.InvolvedObject.Name
		count := ev.Count
		if ev.Series != nil && ev.Series.Count > count {
			count = ev.Series.Count
		}
		if count == 0 {
			count = 1
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%s\n",
			humanizeAge(now.Sub(eventTime(ev))),
			ev.Type,
			ev.Reason,
			object,
			count,
			singleLine(ev.Message),
		)
	}
	_ = tw.Flush()
}

// humanizeAge renders a duration the way kubectl does: 3s, 5m, 2h, 4d.
func humanizeAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// singleLine collapses a multi-line event message so the table stays aligned.
func singleLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")

	return strings.TrimSpace(s)
}
