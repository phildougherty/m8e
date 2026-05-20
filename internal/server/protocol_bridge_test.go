// internal/server/protocol_bridge_test.go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/phildougherty/m8e/internal/discovery"
	"github.com/phildougherty/m8e/internal/logging"
)

// TestMakeHTTPToolsListRequest_HangingServerHonoursTimeout verifies that a
// server which never responds to the tools/list step cannot block the caller
// past the overall discovery-sequence timeout. The previous code chained
// per-request timeouts but had no overall cap; an upstream that successfully
// completed initialize and then hung on tools/list would block forever.
func TestMakeHTTPToolsListRequest_HangingServerHonoursTimeout(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)
		atomic.AddInt32(&hits, 1)
		method, _ := req["method"].(string)
		switch method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":"1","result":{"protocolVersion":"2024-11-05"}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusOK)
		default:
			// tools/list -- hang until the client gives up
			<-r.Context().Done()
		}
	}))
	defer srv.Close()

	bridge := &ProtocolBridge{
		httpClient: &http.Client{Timeout: 25 * time.Minute},
		sseClient:  &http.Client{Timeout: 0},
		ids:        &idGenerator{},
		stats:      newConnStatsTracker(),
		logger:     logging.NewLogger("error"),
	}

	conn := &discovery.MCPConnection{
		Name:     "test-server",
		Protocol: "http",
		Status:   "connected",
		HTTPConnection: &discovery.MCPHTTPConnection{
			BaseURL: srv.URL,
			Client:  &http.Client{Timeout: 25 * time.Minute},
		},
	}

	// Drop the discovery cap so the test does not block for the production
	// 30s default; restore on exit.
	origTimeout := toolsDiscoverySequenceTimeout
	toolsDiscoverySequenceTimeout = 500 * time.Millisecond
	defer func() { toolsDiscoverySequenceTimeout = origTimeout }()

	doneCh := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := bridge.makeToolsListRequest("test-server", conn)
		doneCh <- err
	}()

	// Expect makeToolsListRequest to return within the (lowered) cap plus a
	// generous grace window. If the cap weren't being honoured, this would
	// hang forever (hanging server) and the timeout below would fire.
	hardCeiling := 5 * time.Second
	select {
	case err := <-doneCh:
		elapsed := time.Since(start)
		if err == nil {
			t.Fatalf("expected error from hanging tools/list, got nil after %v", elapsed)
		}
		if elapsed > hardCeiling {
			t.Fatalf("makeToolsListRequest blocked %v, expected return within %v", elapsed, hardCeiling)
		}
	case <-time.After(hardCeiling):
		t.Fatalf("makeToolsListRequest did not return within %v", hardCeiling)
	}

	// Sanity: initialize + initialized + tools/list = 3 hits.
	if got := atomic.LoadInt32(&hits); got < 3 {
		t.Fatalf("expected >=3 HTTP hits (initialize, initialized, tools/list), got %d", got)
	}
}
