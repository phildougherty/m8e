package observability

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// scrape runs m.Handler() against an in-memory request and returns the body.
func scrape(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	m.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handler returned status %d, want 200", rec.Code)
	}
	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}

	return string(body)
}

func TestNewRegistersWithoutPanic(t *testing.T) {
	// New() calls MustRegister; a duplicate or malformed metric definition
	// would panic here. Constructing twice also proves each Metrics owns an
	// independent registry.
	_ = New()
	_ = New()
}

func TestHandlerServesPrometheusText(t *testing.T) {
	m := New()
	m.RecordServerStart("filesystem")

	body := scrape(t, m)
	if !strings.Contains(body, "# HELP matey_server_starts_total") {
		t.Errorf("scrape missing HELP line for matey_server_starts_total:\n%s", body)
	}
	if !strings.Contains(body, "# TYPE matey_server_starts_total counter") {
		t.Errorf("scrape missing TYPE line for matey_server_starts_total:\n%s", body)
	}
}

func TestServerLifecycleCounters(t *testing.T) {
	m := New()
	m.RecordServerStart("memory")
	m.RecordServerStart("memory")
	m.RecordServerStop("memory")
	m.RecordServerRestart("memory")

	body := scrape(t, m)
	want := []string{
		`matey_server_starts_total{server="memory"} 2`,
		`matey_server_stops_total{server="memory"} 1`,
		`matey_server_restarts_total{server="memory"} 1`,
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("scrape missing series %q:\n%s", w, body)
		}
	}
}

func TestRecordReconcile(t *testing.T) {
	m := New()
	m.RecordReconcile("MCPServer", 5*time.Millisecond, nil)
	m.RecordReconcile("MCPServer", 5*time.Millisecond, errors.New("boom"))

	body := scrape(t, m)
	want := []string{
		`matey_controller_reconcile_total{kind="MCPServer"} 2`,
		`matey_controller_reconcile_errors_total{kind="MCPServer"} 1`,
		`matey_controller_reconcile_duration_seconds_count{kind="MCPServer"} 2`,
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("scrape missing series %q:\n%s", w, body)
		}
	}
}

func TestRecordProxyRequest(t *testing.T) {
	m := New()
	m.RecordProxyRequest("github", "tools/call", "200", 12*time.Millisecond)
	m.RecordProxyRequest("github", "tools/call", "200", 8*time.Millisecond)
	m.RecordProxyRequest("github", "tools/call", "500", 1*time.Millisecond)

	body := scrape(t, m)
	want := []string{
		`matey_proxy_requests_total{method="tools/call",server="github",status="200"} 2`,
		`matey_proxy_requests_total{method="tools/call",server="github",status="500"} 1`,
		`matey_proxy_request_duration_seconds_count{method="tools/call",server="github"} 3`,
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("scrape missing series %q:\n%s", w, body)
		}
	}
}

func TestConnectionGauge(t *testing.T) {
	m := New()
	m.ConnectionOpened("search")
	m.ConnectionOpened("search")
	m.ConnectionClosed("search")

	body := scrape(t, m)
	if !strings.Contains(body, `matey_proxy_active_connections{server="search"} 1`) {
		t.Errorf("scrape missing expected gauge value 1:\n%s", body)
	}

	m.SetActiveConnections("search", 7)
	body = scrape(t, m)
	if !strings.Contains(body, `matey_proxy_active_connections{server="search"} 7`) {
		t.Errorf("scrape missing expected gauge value 7:\n%s", body)
	}
}

func TestCollectorsExposed(t *testing.T) {
	m := New()
	got := len(m.Collectors())
	if got != 9 {
		t.Errorf("Collectors() returned %d collectors, want 9", got)
	}
}

func TestNopMetricsIsNilSafe(t *testing.T) {
	// also covers Nop(), which returns a typed nil
	m := Nop()

	// None of these must panic on a nil receiver.
	m.RecordServerStart("x")
	m.RecordServerStop("x")
	m.RecordServerRestart("x")
	m.RecordReconcile("MCPServer", time.Second, errors.New("e"))
	m.RecordProxyRequest("x", "m", "200", time.Second)
	m.ConnectionOpened("x")
	m.ConnectionClosed("x")
	m.SetActiveConnections("x", 3)

	if m.Registry() != nil {
		t.Error("Nop().Registry() should be nil")
	}
	if m.Collectors() != nil {
		t.Error("Nop().Collectors() should be nil")
	}

	// Handler on a no-op Metrics must still serve 200.
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("Nop().Handler() returned status %d, want 200", rec.Code)
	}
}
