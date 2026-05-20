// Package observability provides matey's own Prometheus metrics: server
// lifecycle, controller reconciliation, and proxy request instrumentation.
//
// Callers never touch prometheus types directly. They construct a *Metrics
// (or use the nil-safe no-op via the package-level Nop()) and call the typed
// helper methods. A *Metrics owns a private *prometheus.Registry so it never
// pollutes the global default registry; Handler() exposes that registry over
// HTTP and Collectors() exposes the raw collectors for callers that need to
// register them into a foreign registry (e.g. controller-runtime's).
package observability

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	namespace = "matey"
)

// Metrics is the set of matey's own metrics. Construct it once with New() and
// share the pointer. A nil *Metrics is valid: every method is a no-op on a nil
// receiver, so code paths and tests that never wire observability do not panic.
type Metrics struct {
	registry *prometheus.Registry

	// Server lifecycle.
	serverStarts   *prometheus.CounterVec
	serverStops    *prometheus.CounterVec
	serverRestarts *prometheus.CounterVec

	// Controller reconciliation.
	reconcileTotal    *prometheus.CounterVec
	reconcileErrors   *prometheus.CounterVec
	reconcileDuration *prometheus.HistogramVec

	// Proxy requests.
	proxyRequests *prometheus.CounterVec
	proxyLatency  *prometheus.HistogramVec

	// Connections.
	activeConnections *prometheus.GaugeVec
}

// New constructs a *Metrics backed by a fresh, private registry and registers
// every collector. It panics only on a programming error (duplicate metric
// definition), which would be caught immediately by the package tests.
func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),

		serverStarts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "server",
			Name:      "starts_total",
			Help:      "Total number of MCP server start operations, by server name.",
		}, []string{"server"}),
		serverStops: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "server",
			Name:      "stops_total",
			Help:      "Total number of MCP server stop operations, by server name.",
		}, []string{"server"}),
		serverRestarts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "server",
			Name:      "restarts_total",
			Help:      "Total number of MCP server restart operations, by server name.",
		}, []string{"server"}),

		reconcileTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "controller",
			Name:      "reconcile_total",
			Help:      "Total number of reconcile invocations, by CRD kind.",
		}, []string{"kind"}),
		reconcileErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "controller",
			Name:      "reconcile_errors_total",
			Help:      "Total number of reconcile invocations that returned an error, by CRD kind.",
		}, []string{"kind"}),
		reconcileDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "controller",
			Name:      "reconcile_duration_seconds",
			Help:      "Duration of reconcile invocations in seconds, by CRD kind.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"kind"}),

		proxyRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Subsystem: "proxy",
			Name:      "requests_total",
			Help:      "Total number of proxied requests, by target server, method, and status code.",
		}, []string{"server", "method", "status"}),
		proxyLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Subsystem: "proxy",
			Name:      "request_duration_seconds",
			Help:      "Latency of proxied requests in seconds, by target server and method.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"server", "method"}),

		activeConnections: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Subsystem: "proxy",
			Name:      "active_connections",
			Help:      "Current number of active upstream connections, by target server.",
		}, []string{"server"}),
	}

	m.registry.MustRegister(m.collectors()...)

	// Also register the Go runtime and process collectors on matey's private
	// registry. Without these, `/metrics` returns an empty body on a fresh
	// matey process because every matey-specific metric is a *Vec with no
	// children yet (Prometheus client_golang emits nothing for childless
	// vecs). Go/process collectors always have samples, so the endpoint is
	// non-empty from the first scrape and ops get standard Go runtime
	// metrics (gc, goroutines, memory, fds, uptime) for free.
	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return m
}

// collectors returns every collector owned by m in a stable order. It is used
// both for registering into m's own registry and for Collectors(), which lets
// a caller register matey's metrics into a foreign registry.
func (m *Metrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.serverStarts,
		m.serverStops,
		m.serverRestarts,
		m.reconcileTotal,
		m.reconcileErrors,
		m.reconcileDuration,
		m.proxyRequests,
		m.proxyLatency,
		m.activeConnections,
	}
}

// Nop returns a no-op *Metrics. It is just a typed nil, kept as a constructor
// so call sites read intentionally ("observability.Nop()") rather than passing
// a bare nil literal.
func Nop() *Metrics { return nil }

// Registry returns the private registry backing m, or nil for a no-op Metrics.
// Useful for callers that want to merge it into another gatherer.
func (m *Metrics) Registry() *prometheus.Registry {
	if m == nil {
		return nil
	}

	return m.registry
}

// Collectors returns matey's collectors so they can be registered into a
// foreign registry (for example controller-runtime's metrics.Registry, which
// already serves /metrics on the controller-manager). Returns nil for a no-op
// Metrics.
func (m *Metrics) Collectors() []prometheus.Collector {
	if m == nil {
		return nil
	}

	return m.collectors()
}

// Handler returns an HTTP handler serving m's registry in Prometheus text
// format. For a no-op Metrics it returns a handler that responds 200 with an
// empty body, so wiring a /metrics route never has to nil-check.
func (m *Metrics) Handler() http.Handler {
	if m == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	}

	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// RecordServerStart increments the start counter for the named server.
func (m *Metrics) RecordServerStart(server string) {
	if m == nil {
		return
	}
	m.serverStarts.WithLabelValues(server).Inc()
}

// RecordServerStop increments the stop counter for the named server.
func (m *Metrics) RecordServerStop(server string) {
	if m == nil {
		return
	}
	m.serverStops.WithLabelValues(server).Inc()
}

// RecordServerRestart increments the restart counter for the named server.
func (m *Metrics) RecordServerRestart(server string) {
	if m == nil {
		return
	}
	m.serverRestarts.WithLabelValues(server).Inc()
}

// RecordReconcile records a single reconcile of the given CRD kind: it bumps
// the total counter, observes the duration, and bumps the error counter when
// err is non-nil. This is the one call a reconciler needs at the end of its
// Reconcile method.
func (m *Metrics) RecordReconcile(kind string, dur time.Duration, err error) {
	if m == nil {
		return
	}
	m.reconcileTotal.WithLabelValues(kind).Inc()
	m.reconcileDuration.WithLabelValues(kind).Observe(dur.Seconds())
	if err != nil {
		m.reconcileErrors.WithLabelValues(kind).Inc()
	}
}

// RecordProxyRequest records a proxied request: it bumps the request counter
// (labelled by server, method, and status) and observes the latency.
func (m *Metrics) RecordProxyRequest(server, method, status string, dur time.Duration) {
	if m == nil {
		return
	}
	m.proxyRequests.WithLabelValues(server, method, status).Inc()
	m.proxyLatency.WithLabelValues(server, method).Observe(dur.Seconds())
}

// ConnectionOpened increments the active-connection gauge for the named server.
func (m *Metrics) ConnectionOpened(server string) {
	if m == nil {
		return
	}
	m.activeConnections.WithLabelValues(server).Inc()
}

// ConnectionClosed decrements the active-connection gauge for the named server.
func (m *Metrics) ConnectionClosed(server string) {
	if m == nil {
		return
	}
	m.activeConnections.WithLabelValues(server).Dec()
}

// SetActiveConnections sets the active-connection gauge for the named server to
// an absolute value, for callers that track connection count themselves.
func (m *Metrics) SetActiveConnections(server string, n int) {
	if m == nil {
		return
	}
	m.activeConnections.WithLabelValues(server).Set(float64(n))
}
