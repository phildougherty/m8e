// internal/compose/render_test.go
package compose

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderStatusEmpty(t *testing.T) {
	var buf bytes.Buffer
	RenderStatus(&buf, &ComposeStatus{Services: map[string]*ServiceStatus{}})

	out := buf.String()
	if !strings.Contains(out, "Services (0 total, 0 running)") {
		t.Errorf("expected summary line for empty status, got:\n%s", out)
	}
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "RESTARTS") {
		t.Errorf("expected table header, got:\n%s", out)
	}
}

func TestRenderStatusRowsAndCounts(t *testing.T) {
	status := &ComposeStatus{Services: map[string]*ServiceStatus{
		"zebra": {
			Name:           "zebra",
			Status:         "running",
			Type:           "mcp-server",
			StartTime:      "2026-05-14T10:00:00Z",
			ProxyConnected: true,
			HealthStatus:   "healthy",
			RestartCount:   2,
		},
		"alpha": {
			Name:           "alpha",
			Status:         "stopped",
			Type:           "mcp-server",
			ProxyConnected: false,
			HealthStatus:   "unknown",
			RestartCount:   0,
		},
		"matey-proxy": {
			Name:         "matey-proxy",
			Status:       "running",
			Type:         "matey-core",
			HealthStatus: "healthy",
		},
	}}

	var buf bytes.Buffer
	RenderStatus(&buf, status)
	out := buf.String()

	if !strings.Contains(out, "Services (3 total, 2 running)") {
		t.Errorf("expected 3 total / 2 running, got:\n%s", out)
	}

	// Rows present for each service.
	for _, name := range []string{"alpha", "zebra", "matey-proxy"} {
		if !strings.Contains(out, name) {
			t.Errorf("expected row for %q, got:\n%s", name, out)
		}
	}

	// Alphabetical ordering: alpha before matey-proxy before zebra.
	iAlpha := strings.Index(out, "alpha")
	iProxy := strings.Index(out, "matey-proxy")
	iZebra := strings.Index(out, "zebra")
	if iAlpha >= iProxy || iProxy >= iZebra {
		t.Errorf("expected alphabetical row order alpha < matey-proxy < zebra, got positions %d, %d, %d", iAlpha, iProxy, iZebra)
	}

	// Proxy column: connected for connected server, N/A for matey-core.
	if !strings.Contains(out, "connected") {
		t.Errorf("expected 'connected' proxy status, got:\n%s", out)
	}
	if !strings.Contains(out, "disconnected") {
		t.Errorf("expected 'disconnected' proxy status for alpha, got:\n%s", out)
	}
	if !strings.Contains(out, "N/A") {
		t.Errorf("expected 'N/A' proxy status for matey-core, got:\n%s", out)
	}

	// Long start time is truncated to 19 chars (no trailing 'Z').
	if strings.Contains(out, "2026-05-14T10:00:00Z") {
		t.Errorf("expected start time truncated to 19 chars, got:\n%s", out)
	}
	if !strings.Contains(out, "2026-05-14T10:00:00") {
		t.Errorf("expected truncated start time present, got:\n%s", out)
	}
}
