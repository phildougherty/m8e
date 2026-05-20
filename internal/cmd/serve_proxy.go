// internal/cmd/serve_proxy.go
package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/phildougherty/m8e/internal/audit"
	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/logging"
	"github.com/phildougherty/m8e/internal/observability"
	"github.com/phildougherty/m8e/internal/server"
)

func NewServeProxyCommand() *cobra.Command {
	var port int
	var namespace string
	var apiKey string

	cmd := &cobra.Command{
		Use:   "serve-proxy",
		Short: "Run the actual MCP proxy server (used internally by deployments)",
		Long: `Run the actual MCP proxy server HTTP service. This command is used internally
by Kubernetes deployments and should not be called directly by users.

For creating proxy deployments, use 'matey proxy' instead.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServeProxy(cmd, port, namespace, apiKey)
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 8080, "Port to run the proxy server on")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "matey", "Kubernetes namespace to discover services in")
	cmd.Flags().StringVarP(&apiKey, "api-key", "k", "", "API key for proxy authentication (optional)")

	return cmd
}

func runServeProxy(cmd *cobra.Command, port int, namespace, apiKey string) error {
	// Load configuration if available
	file, _ := cmd.Flags().GetString("file")
	var cfg *config.ComposeConfig
	var err error

	if file != "" {
		cfg, err = config.LoadConfig(file)
		if err != nil {
			fmt.Printf("Warning: Failed to load config file %s: %v\n", file, err)
			fmt.Println("Continuing with default configuration...")
			cfg = &config.ComposeConfig{}
		}
	} else {
		cfg = &config.ComposeConfig{}
	}

	// Get API key from environment if not provided
	if apiKey == "" {
		apiKey = os.Getenv("MCP_API_KEY")
	}

	fmt.Printf("Starting system MCP proxy server...\n")
	fmt.Printf("Namespace: %s\n", namespace)
	fmt.Printf("Port: %d\n", port)
	if apiKey != "" {
		fmt.Printf("Authentication: Enabled\n")
	} else {
		fmt.Printf("Authentication: Disabled\n")
	}

	// Construct matey's observability metrics. This is the process that
	// actually serves proxied traffic (the 'matey proxy' command only creates
	// the MCPProxy resource and runs the controller), so the proxy request,
	// latency and connection metrics must be wired and exposed from here.
	metrics := observability.New()

	// Construct the process-wide audit logger. The proxy is the primary
	// authentication chokepoint, so every 401/403 produced by the underlying
	// handler is forwarded to the audit log via the wrapping HTTP handler
	// below. Audit events are also reachable from any code path that calls
	// audit.SafeLog (e.g. execute_bash, oauth issuance) via the package-level
	// global registered here.
	auditLogLevel := "info"
	if cfg != nil && cfg.Logging.Level != "" {
		auditLogLevel = cfg.Logging.Level
	}
	auditCmdLogger := logging.NewLogger(auditLogLevel)
	auditLogger := audit.NewLoggerForProcess(cfg.Audit, auditCmdLogger)
	if auditLogger != nil {
		defer func() {
			if shutdownErr := auditLogger.Shutdown(); shutdownErr != nil {
				fmt.Printf("audit logger shutdown error: %v\n", shutdownErr)
			}
		}()

		// Startup marker so operators can confirm audit logging is wired in
		// a freshly-deployed proxy pod.
		auditLogger.Log(
			"process.startup",
			"", "", "", "",
			true,
			map[string]interface{}{
				"process":   "serve-proxy",
				"namespace": namespace,
				"port":      port,
				"hostname":  hostnameForAudit(),
				"auth":      apiKey != "",
			},
			nil,
		)
	}

	// Create system proxy handler
	proxyHandler, err := server.NewProxyHandler(cfg, namespace, apiKey, metrics)
	if err != nil {
		return fmt.Errorf("failed to create proxy handler: %w", err)
	}

	// Start the proxy handler
	if err := proxyHandler.Start(); err != nil {
		return fmt.Errorf("failed to start proxy handler: %w", err)
	}
	defer proxyHandler.Stop()

	// Use the unified proxy HTTP router
	mux := http.NewServeMux()

	// Expose matey's metrics registry. /metrics is served on the same port as
	// the proxy; the proxy router treats unknown single-segment paths as tool
	// or server names, so registering an explicit /metrics route keeps it from
	// being routed into the proxy.
	mux.Handle("/metrics", metrics.Handler())

	// Use the unified proxy handler's HTTP router for all requests, wrapped
	// so any 401/403 response (the proxy's own authentication chokepoint) is
	// captured as an audit event. We don't have access to the proxy's
	// internal request context here, so we record best-effort metadata
	// (method, path, remote address, user-agent) — enough to investigate a
	// suspicious access pattern after the fact.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ari := newAuditRecordingWriter(w)
		proxyHandler.ServeHTTP(ari, r)
		emitProxyAuthAudit(r, ari.status)
	})

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  25 * time.Minute, // Extended for execute_agent
		WriteTimeout: 25 * time.Minute, // Extended for execute_agent
		IdleTimeout:  25 * time.Minute, // Extended for execute_agent
	}

	// Start server in a goroutine
	go func() {
		fmt.Printf("system MCP proxy server listening on :%d\n", port)
		fmt.Printf("Service discovery endpoint: http://localhost:%d/api/discovery\n", port)
		fmt.Printf("Health check endpoint: http://localhost:%d/health\n", port)
		fmt.Printf("API endpoint: http://localhost:%d/api/\n", port)

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("Server error: %v\n", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("\nShutting down proxy server...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		fmt.Printf("Server forced to shutdown: %v\n", err)
	}

	fmt.Println("Proxy server stopped")
	return nil
}

// auditRecordingWriter wraps http.ResponseWriter to capture the status code
// the underlying handler writes, so the proxy can decide post-hoc whether the
// request should produce a proxy.auth.success or proxy.auth.failure audit
// event. WriteHeader may be called at most once; if the handler skips it, the
// default response is 200 (matching net/http behavior).
type auditRecordingWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func newAuditRecordingWriter(w http.ResponseWriter) *auditRecordingWriter {
	return &auditRecordingWriter{ResponseWriter: w, status: http.StatusOK}
}

func (a *auditRecordingWriter) WriteHeader(code int) {
	if !a.wroteHeader {
		a.status = code
		a.wroteHeader = true
	}
	a.ResponseWriter.WriteHeader(code)
}

func (a *auditRecordingWriter) Write(b []byte) (int, error) {
	if !a.wroteHeader {
		a.wroteHeader = true
	}

	return a.ResponseWriter.Write(b)
}

// Flush forwards to the underlying writer when it supports streaming so SSE
// responses (used heavily by the MCP proxy) continue to stream correctly
// through this wrapper.
func (a *auditRecordingWriter) Flush() {
	if f, ok := a.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// emitProxyAuthAudit fires a proxy.auth.* audit event when the response
// status indicates an authentication or authorization decision. Successful
// responses outside the auth space are uninteresting and are NOT audited from
// here (per-tool audit events live closer to the tool implementations).
func emitProxyAuthAudit(r *http.Request, status int) {
	// 401/403 are unambiguous auth failures. 200/204 on an /oauth path is a
	// successful auth interaction worth recording.
	isAuthPath := strings.HasPrefix(r.URL.Path, "/oauth") ||
		strings.HasPrefix(r.URL.Path, "/.well-known/oauth") ||
		r.URL.Path == "/authorize" ||
		r.URL.Path == "/token"

	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		audit.SafeLog(
			"proxy.auth.failure",
			"", "",
			clientIPFromRequest(r),
			r.UserAgent(),
			false,
			map[string]interface{}{
				"method": r.Method,
				"path":   r.URL.Path,
				"status": status,
			},
			nil,
		)
	case isAuthPath && status >= 200 && status < 300:
		audit.SafeLog(
			"proxy.auth.success",
			"", "",
			clientIPFromRequest(r),
			r.UserAgent(),
			true,
			map[string]interface{}{
				"method": r.Method,
				"path":   r.URL.Path,
				"status": status,
			},
			nil,
		)
	}
}

// clientIPFromRequest extracts a best-effort client IP for audit events. The
// X-Forwarded-For header is honored first (matey routinely sits behind an
// ingress), with a fallback to the raw remote address.
func clientIPFromRequest(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if comma := strings.IndexByte(xff, ','); comma > 0 {
			return strings.TrimSpace(xff[:comma])
		}

		return strings.TrimSpace(xff)
	}

	return r.RemoteAddr
}
