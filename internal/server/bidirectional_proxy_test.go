// internal/server/bidirectional_proxy_test.go
package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/phildougherty/m8e/internal/config"
	"github.com/phildougherty/m8e/internal/discovery"
	"github.com/phildougherty/m8e/internal/observability"
)

// bidiMockServer is a minimal streamable-HTTP MCP server tailored to the
// bidirectional-relay tests. Each test installs a per-request handler that
// emits the desired sequence of JSON-RPC messages (intermediate progress
// notifications, server-initiated requests, the final result) one line per
// JSON object — the framing the proxy demuxes.
//
// In addition to the streaming POST endpoint, the server exposes an upstream-
// reply collector: when the proxy forwards a client's reply to a server-
// initiated request (sampling/createMessage etc.) as a side POST, we record
// it here so the test can assert on it.
type bidiMockServer struct {
	srv *httptest.Server

	// upstreamReplies holds the JSON-RPC payloads the proxy posts back to us
	// outside the main streaming response (i.e. client replies to server-
	// initiated requests).
	mu              sync.Mutex
	upstreamReplies []map[string]interface{}

	// gotReply is closed once at least one upstream reply has been received.
	gotReply chan struct{}
	once     sync.Once

	// activeRespond is the per-test "main" handler. It is called for the
	// initial POST that the proxy makes to forward the client's request. It
	// is responsible for writing the streamed body.
	activeRespond func(w http.ResponseWriter, r *http.Request, body map[string]interface{})

	// requestCount counts POSTs to the main endpoint (initial request +
	// upstream replies for sampling). The first call is the main request;
	// subsequent calls in this scheme are client replies forwarded as side
	// POSTs. We split on the presence of "method" in the body.
	requestCount int32
}

func newBidiMockServer() *bidiMockServer {
	m := &bidiMockServer{
		gotReply: make(chan struct{}),
	}
	m.srv = httptest.NewServer(http.HandlerFunc(m.handle))

	return m
}

func (m *bidiMockServer) Close() {
	m.srv.Close()
}

func (m *bidiMockServer) URL() string {

	return m.srv.URL
}

func (m *bidiMockServer) handle(w http.ResponseWriter, r *http.Request) {
	atomic.AddInt32(&m.requestCount, 1)
	body, _ := io.ReadAll(r.Body)
	var msg map[string]interface{}
	_ = json.Unmarshal(body, &msg)

	// If this looks like a JSON-RPC response (no method, has id+result|error)
	// it's an upstream reply.
	_, hasResult := msg["result"]
	_, hasError := msg["error"]
	if _, hasMethod := msg["method"].(string); !hasMethod && (hasResult || hasError) {
		m.mu.Lock()
		m.upstreamReplies = append(m.upstreamReplies, msg)
		m.mu.Unlock()
		m.once.Do(func() { close(m.gotReply) })
		w.WriteHeader(http.StatusAccepted)

		return
	}

	if m.activeRespond == nil {
		http.Error(w, "no handler", http.StatusInternalServerError)

		return
	}
	m.activeRespond(w, r, msg)
}

// writeJSONLine writes one JSON-RPC message to a chunked streaming response
// (one object per line, newline terminator, immediate flush).
func writeJSONLine(t *testing.T, w http.ResponseWriter, payload map[string]interface{}) {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := w.Write(b); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		t.Fatalf("write nl: %v", err)
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func buildBidiProxy(t *testing.T, mockURL, protocol string) (*ProxyHandler, *MockServiceDiscovery) {
	t.Helper()
	mockDiscovery := NewMockServiceDiscovery()
	cfg := &config.ComposeConfig{Logging: config.LoggingConfig{Level: "warn"}}
	proxy, err := NewProxyHandler(cfg, "default", "test-api-key", observability.Nop())
	if err != nil {
		t.Fatalf("NewProxyHandler: %v", err)
	}
	proxy.ServiceDiscovery = mockDiscovery
	proxy.ConnectionManager = discovery.NewDynamicConnectionManager(mockDiscovery, proxy.Logger)
	if err := proxy.Start(); err != nil {
		t.Fatalf("proxy.Start: %v", err)
	}

	mockDiscovery.AddService(discovery.ServiceEndpoint{
		Name:         "bidi-server",
		URL:          mockURL,
		Protocol:     protocol,
		Port:         8080,
		Capabilities: []string{"tools"},
	})

	// Give the connection manager a moment to materialize the connection.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := proxy.ConnectionManager.GetConnection("bidi-server"); err == nil && conn != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	return proxy, mockDiscovery
}

// readNDJSON reads JSON-RPC messages from a chunked / line-framed response
// body and returns them in order.
func readNDJSON(t *testing.T, body io.Reader) []map[string]interface{} {
	t.Helper()
	out := make([]map[string]interface{}, 0)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {

			continue
		}
		var msg map[string]interface{}
		if err := json.Unmarshal(line, &msg); err != nil {

			continue
		}
		out = append(out, msg)
	}

	return out
}

// TestStreamableBidirectionalProgressNotifications drives a tools/call
// against a streamable-HTTP mock that emits a notifications/progress
// notification before the final response. The relay must forward the
// progress notification to the client and then the final result.
func TestStreamableBidirectionalProgressNotifications(t *testing.T) {
	mock := newBidiMockServer()
	defer mock.Close()

	mock.activeRespond = func(w http.ResponseWriter, r *http.Request, body map[string]interface{}) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Streaming", "true")
		w.Header().Set("Transfer-Encoding", "chunked")
		w.WriteHeader(http.StatusOK)

		id := body["id"]

		writeJSONLine(t, w, map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "notifications/progress",
			"params": map[string]interface{}{
				"progressToken": "abc",
				"progress":      0.5,
				"total":         1.0,
			},
		})
		time.Sleep(20 * time.Millisecond)
		writeJSONLine(t, w, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": "done"},
				},
			},
		})
	}

	proxy, _ := buildBidiProxy(t, mock.URL(), "http-stream")
	defer proxy.Stop()

	reqBody, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "req-1",
		"method":  "tools/call",
		"params":  map[string]interface{}{"name": "x", "arguments": map[string]interface{}{}},
	})

	req := httptest.NewRequest("POST", "/bidi-server", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("X-Streaming", "true")

	rec := httptest.NewRecorder()
	proxy.HandleMCPRequest(rec, req, "bidi-server")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", rec.Code, rec.Body.String())
	}

	msgs := readNDJSON(t, rec.Body)
	if len(msgs) < 2 {
		t.Fatalf("expected >=2 messages (progress + result), got %d: %s", len(msgs), rec.Body.String())
	}
	sawProgress := false
	sawResult := false
	for _, m := range msgs {
		if method, _ := m["method"].(string); method == "notifications/progress" {
			sawProgress = true
		}
		if id, _ := m["id"].(string); id == "req-1" {
			sawResult = true
		}
	}
	if !sawProgress {
		t.Errorf("expected to see progress notification in client stream, got: %+v", msgs)
	}
	if !sawResult {
		t.Errorf("expected to see final result with id req-1 in client stream, got: %+v", msgs)
	}
	// Order check: progress must precede final result.
	progressIdx, resultIdx := -1, -1
	for i, m := range msgs {
		if method, _ := m["method"].(string); method == "notifications/progress" && progressIdx < 0 {
			progressIdx = i
		}
		if id, _ := m["id"].(string); id == "req-1" {
			resultIdx = i
		}
	}
	if progressIdx > resultIdx {
		t.Errorf("expected progress to arrive before final result, got progress=%d result=%d", progressIdx, resultIdx)
	}
}

// TestStreamableBidirectionalServerInitiatedRequest drives a tools/call
// against a streamable-HTTP mock that emits a sampling/createMessage server-
// initiated request before the final response. The relay must forward the
// request to the client; the test then drives the client side by posting a
// reply back to the proxy, and asserts the mock received the reply as an
// upstream side-POST and the final result still arrived.
func TestStreamableBidirectionalServerInitiatedRequest(t *testing.T) {
	mock := newBidiMockServer()
	defer mock.Close()

	mock.activeRespond = func(w http.ResponseWriter, r *http.Request, body map[string]interface{}) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Streaming", "true")
		w.Header().Set("Transfer-Encoding", "chunked")
		w.WriteHeader(http.StatusOK)

		id := body["id"]

		// Server-initiated request.
		writeJSONLine(t, w, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      "srv-req-1",
			"method":  "sampling/createMessage",
			"params": map[string]interface{}{
				"messages": []map[string]interface{}{
					{"role": "user", "content": map[string]interface{}{"type": "text", "text": "hi"}},
				},
			},
		})

		// Wait for the client reply (delivered to mock.gotReply via side POST).
		select {
		case <-mock.gotReply:
		case <-time.After(3 * time.Second):
			t.Errorf("mock server timed out waiting for client reply")
		}

		// Now finish the original request.
		writeJSONLine(t, w, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": "completed"},
				},
			},
		})
	}

	proxy, _ := buildBidiProxy(t, mock.URL(), "http-stream")
	defer proxy.Stop()

	// We need to drive the client side concurrently: read the streaming
	// response, watch for the sampling/createMessage request, then POST a
	// reply back to the proxy. We can't use httptest.NewRecorder directly
	// because the relay writes intermediate messages to the response writer
	// before returning. Use an in-process channel for the client read.

	// Custom ResponseWriter that flushes through a pipe so the test goroutine
	// can read messages incrementally.
	pr, pw := io.Pipe()
	rw := newStreamingRecorder(pw)

	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		reqBody, _ := json.Marshal(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      "req-2",
			"method":  "tools/call",
			"params":  map[string]interface{}{"name": "x", "arguments": map[string]interface{}{}},
		})
		req := httptest.NewRequest("POST", "/bidi-server", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer test-api-key")
		req.Header.Set("X-Streaming", "true")
		req.Header.Set("Mcp-Session-Id", "client-session-1")
		proxy.HandleMCPRequest(rw, req, "bidi-server")
		_ = pw.Close()
	}()

	// Read messages incrementally.
	reader := bufio.NewReader(pr)
	seenSamplingReq := false
	seenFinal := false
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			trim := bytes.TrimSpace(line)
			if len(trim) > 0 {
				var msg map[string]interface{}
				if json.Unmarshal(trim, &msg) == nil {
					if method, _ := msg["method"].(string); method == "sampling/createMessage" {
						seenSamplingReq = true
						// POST reply back to proxy.
						reply := map[string]interface{}{
							"jsonrpc": "2.0",
							"id":      msg["id"],
							"result": map[string]interface{}{
								"model": "test-model",
								"content": map[string]interface{}{
									"type": "text",
									"text": "hello",
								},
							},
						}
						replyBody, _ := json.Marshal(reply)
						replyReq := httptest.NewRequest("POST", "/bidi-server", bytes.NewReader(replyBody))
						replyReq.Header.Set("Content-Type", "application/json")
						replyReq.Header.Set("Authorization", "Bearer test-api-key")
						replyReq.Header.Set("Mcp-Session-Id", "client-session-1")
						// Route through handleServerForward path via ServeHTTP.
						replyRec := httptest.NewRecorder()
						proxy.ServeHTTP(replyRec, replyReq)
						if replyRec.Code != http.StatusAccepted {
							t.Errorf("expected 202 from proxy when delivering client reply, got %d body=%s", replyRec.Code, replyRec.Body.String())
						}
					}
					if id, _ := msg["id"].(string); id == "req-2" && msg["result"] != nil {
						seenFinal = true
					}
				}
			}
		}
		if err != nil {

			break
		}
	}

	select {
	case <-clientDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("client goroutine did not finish")
	}

	if !seenSamplingReq {
		t.Errorf("client did not see sampling/createMessage server-initiated request")
	}
	if !seenFinal {
		t.Errorf("client did not see final result for req-2")
	}

	// Verify the mock received the upstream reply.
	mock.mu.Lock()
	gotReplies := len(mock.upstreamReplies)
	var firstReply map[string]interface{}
	if gotReplies > 0 {
		firstReply = mock.upstreamReplies[0]
	}
	mock.mu.Unlock()
	if gotReplies == 0 {
		t.Errorf("mock server never received the client's reply upstream")
	} else {
		if firstReply["id"] != "srv-req-1" {
			t.Errorf("upstream reply has wrong id: %+v", firstReply)
		}
	}
}

// TestStreamableBidirectionalResourceUpdatedNotification asserts that
// after the client successfully sends resources/subscribe to the server,
// subsequent notifications/resources/updated emitted on the open response
// stream are relayed to the client.
//
// We model this on the bounded "per-open-response" scope from the brief: any
// notifications/* arriving on the upstream stream during a client request's
// lifetime are forwarded to that client. This is what the bidirectional
// relay does by construction.
func TestStreamableBidirectionalResourceUpdatedNotification(t *testing.T) {
	mock := newBidiMockServer()
	defer mock.Close()

	mock.activeRespond = func(w http.ResponseWriter, r *http.Request, body map[string]interface{}) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Streaming", "true")
		w.Header().Set("Transfer-Encoding", "chunked")
		w.WriteHeader(http.StatusOK)

		id := body["id"]

		// Simulate a successful resources/subscribe response then a
		// resources/updated notification then a final ack response to keep
		// the stream open if the client makes a subsequent request. Here
		// our request *is* the subscribe, so we emit the subscribe response
		// then the notification before the stream ends.
		writeJSONLine(t, w, map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "notifications/resources/updated",
			"params":  map[string]interface{}{"uri": "file:///watched"},
		})
		time.Sleep(20 * time.Millisecond)
		writeJSONLine(t, w, map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"result":  map[string]interface{}{},
		})
	}

	proxy, _ := buildBidiProxy(t, mock.URL(), "http-stream")
	defer proxy.Stop()

	reqBody, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "sub-1",
		"method":  "resources/subscribe",
		"params":  map[string]interface{}{"uri": "file:///watched"},
	})
	req := httptest.NewRequest("POST", "/bidi-server", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-api-key")
	req.Header.Set("X-Streaming", "true")

	rec := httptest.NewRecorder()
	proxy.HandleMCPRequest(rec, req, "bidi-server")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	msgs := readNDJSON(t, rec.Body)
	sawUpdate := false
	for _, m := range msgs {
		if method, _ := m["method"].(string); method == "notifications/resources/updated" {
			sawUpdate = true

			break
		}
	}
	if !sawUpdate {
		t.Errorf("expected client to receive notifications/resources/updated, got: %+v", msgs)
	}
}

// streamingRecorder is an http.ResponseWriter that pipes writes through a
// io.PipeWriter so a concurrent reader can consume them incrementally. It is
// used by TestStreamableBidirectionalServerInitiatedRequest to interleave
// reads and replies.
type streamingRecorder struct {
	header http.Header
	status int
	pw     *io.PipeWriter
	mu     sync.Mutex
}

func newStreamingRecorder(pw *io.PipeWriter) *streamingRecorder {

	return &streamingRecorder{header: make(http.Header), pw: pw}
}

func (s *streamingRecorder) Header() http.Header {

	return s.header
}

func (s *streamingRecorder) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.pw.Write(b)
}

func (s *streamingRecorder) WriteHeader(code int) {
	s.status = code
}

func (s *streamingRecorder) Flush() {
	// no-op: pipe writes are immediate
}

// Compile-time interface checks.
var _ http.ResponseWriter = (*streamingRecorder)(nil)
var _ http.Flusher = (*streamingRecorder)(nil)
