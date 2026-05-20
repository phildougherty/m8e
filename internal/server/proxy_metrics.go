// internal/server/proxy_metrics.go
package server

import (
	"net/http"
	"strconv"
	"time"
)

// statusRecorder wraps an http.ResponseWriter to remember the status code that
// was actually written to the client, so request metrics record the FINAL
// status (not a stale zero) even on handler paths that never call WriteHeader
// explicitly. It transparently forwards Write/WriteHeader and preserves the
// http.Flusher behaviour the SSE/streaming forwarders rely on.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

// newStatusRecorder wraps w. The default status is 200, matching net/http: a
// handler that writes a body without calling WriteHeader implicitly sends 200.
func newStatusRecorder(w http.ResponseWriter) *statusRecorder {
	return &statusRecorder{ResponseWriter: w, status: http.StatusOK}
}

// WriteHeader records the status code and forwards it once, mirroring
// net/http's "first WriteHeader wins" semantics.
func (s *statusRecorder) WriteHeader(code int) {
	if s.wroteHeader {
		return
	}
	s.status = code
	s.wroteHeader = true
	s.ResponseWriter.WriteHeader(code)
}

// Write forwards the body and, like net/http, treats an implicit write as a
// 200 response if WriteHeader was never called.
func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wroteHeader {
		s.wroteHeader = true
	}

	return s.ResponseWriter.Write(b)
}

// Flush forwards to the underlying writer when it supports http.Flusher, so SSE
// and chunked-streaming forwarders keep working through the wrapper.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// recordProxyRequest records a single completed proxied request against the
// metrics registry: the request counter (labelled server/method/status) and the
// latency histogram. It is nil-safe via the underlying *observability.Metrics.
// Pass the wall-clock start captured when the request entered the handler.
func (h *ProxyHandler) recordProxyRequest(server, method string, status int, start time.Time) {
	if status == 0 {
		status = http.StatusOK
	}
	h.metrics.RecordProxyRequest(server, method, strconv.Itoa(status), time.Since(start))
}
