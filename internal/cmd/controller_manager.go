// internal/cmd/controller_manager.go
package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/phildougherty/m8e/internal/audit"
	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/controllers"
	"github.com/phildougherty/m8e/internal/logging"
	"github.com/phildougherty/m8e/internal/observability"
)

func NewControllerManagerCommand() *cobra.Command {
	var (
		configFile  string
		namespace   string
		logLevel    string
		metricsPort int
	)

	cmd := &cobra.Command{
		Use:   "controller-manager",
		Short: "Run the Matey controller manager",
		Long: `Run the Matey controller manager as a standalone process.
This command is typically used when deploying the controller manager as a Kubernetes pod.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := logging.NewLogger(logLevel)

			logger.Info("Starting Matey controller manager")

			// Load configuration
			cfg, err := config.LoadConfig(configFile)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Initialize matey's own metrics and register them into
			// controller-runtime's registry. The manager already serves that
			// registry over /metrics on :8083 (see controllers.NewControllerManager),
			// so matey_* series land on the same endpoint Prometheus already
			// scrapes for controller-runtime's built-in controller metrics.
			metrics := observability.New()
			if err := registerMateyMetrics(metrics); err != nil {
				return fmt.Errorf("failed to register metrics: %w", err)
			}

			// Construct the process-wide audit logger and register it as the
			// audit package global. Controllers and any other code path that
			// needs to emit audit events reaches it via audit.Global() /
			// audit.SafeLog. The logger defaults to a file backend at
			// /var/log/matey/audit.log (configurable via M8E_AUDIT_FILE_PATH),
			// falling back to a bounded in-memory ring if the file backend
			// cannot be initialized.
			auditLogger := audit.NewLoggerForProcess(cfg.Audit, logger)
			if auditLogger != nil {
				defer func() {
					if shutdownErr := auditLogger.Shutdown(); shutdownErr != nil {
						logger.Warning("audit logger shutdown returned error: %v", shutdownErr)
					}
				}()

				// Emit a startup marker so operators can confirm audit
				// logging is wired in a freshly-deployed pod by tailing the
				// audit log.
				auditLogger.Log(
					"process.startup",
					"", "", "", "",
					true,
					map[string]interface{}{
						"process":   "controller-manager",
						"namespace": namespace,
						"hostname":  hostnameForAudit(),
					},
					nil,
				)
			}

			// Create controller manager, threading in the same *Metrics instance
			// whose collectors were just registered above so the reconcilers
			// populate the matey_controller_* series.
			cm, err := controllers.NewControllerManager(namespace, cfg, metrics)
			if err != nil {
				return fmt.Errorf("failed to create controller manager: %w", err)
			}

			// Setup signal handling
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			// Serve a matey-only /metrics endpoint on a dedicated port. This is
			// independent of controller-runtime's :8083 endpoint and exposes
			// only matey_* series, which is convenient for dashboards or
			// scrape configs that want matey's metrics in isolation.
			metricsSrv := startMetricsServer(ctx, logger, metrics, metricsPort)

			// Start controller manager
			logger.Info("Controller manager starting...")
			if err := cm.Start(ctx); err != nil {
				return fmt.Errorf("failed to start controller manager: %w", err)
			}

			// Drain the metrics server on shutdown.
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			if err := metricsSrv.Shutdown(shutdownCtx); err != nil {
				logger.Warning("Metrics server shutdown returned error: %v", err)
			}

			if auditLogger != nil {
				auditLogger.Log(
					"process.shutdown",
					"", "", "", "",
					true,
					map[string]interface{}{
						"process":   "controller-manager",
						"namespace": namespace,
					},
					nil,
				)
			}

			logger.Info("Controller manager stopped")
			return nil
		},
	}

	cmd.Flags().StringVar(&configFile, "config", "matey.yaml", "Path to configuration file")
	cmd.Flags().StringVar(&namespace, "namespace", "matey", "Kubernetes namespace to operate in")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level (debug, info, warn, error)")
	cmd.Flags().IntVar(&metricsPort, "metrics-port", 8080, "Port for the matey-only Prometheus /metrics endpoint (controller-runtime metrics remain on :8083)")

	return cmd
}

// registerMateyMetrics registers matey's collectors into controller-runtime's
// shared metrics registry so they are exposed alongside the built-in controller
// metrics on the manager's metrics endpoint.
func registerMateyMetrics(m *observability.Metrics) error {
	for _, c := range m.Collectors() {
		if err := ctrlmetrics.Registry.Register(c); err != nil {
			// AlreadyRegisteredError is benign (e.g. if the command is invoked
			// twice within one process, as in tests); anything else is a real
			// failure.
			var are prometheus.AlreadyRegisteredError
			if errors.As(err, &are) {
				continue
			}

			return err
		}
	}

	return nil
}

// hostnameForAudit returns the local hostname for audit detail enrichment,
// falling back to "unknown" if the OS call fails. The value is informational
// only; audit correctness does not depend on it.
func hostnameForAudit() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "unknown"
	}

	return h
}

// startMetricsServer starts an HTTP server serving the matey metrics handler on
// the given port and returns it so the caller can shut it down. The server runs
// in its own goroutine; a bind failure is logged but does not abort the manager.
func startMetricsServer(ctx context.Context, logger *logging.Logger, m *observability.Metrics, port int) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", m.Handler())

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("Serving matey metrics on :%d/metrics", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Warning("Matey metrics server error: %v", err)
		}
	}()

	// Also shut the server down if the parent context is cancelled before the
	// manager returns (covers signal-driven shutdown).
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	return srv
}
