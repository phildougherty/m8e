// internal/server/bidirectional_relay.go
package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/phildougherty/m8e/internal/discovery"
	"github.com/phildougherty/m8e/internal/logging"
)

// clientChannelRegistry holds the set of in-flight clientChannels indexed by
// a key (typically the client-supplied Mcp-Session-Id, falling back to a
// per-request synthetic id). When the upstream MCP server emits a server-
// initiated request mid-stream, the relay forwards it to the client and
// registers a wait keyed by the request id. The client's reply arrives on a
// *separate* HTTP request to the proxy, and the proxy looks up the matching
// channel via this registry to deliver the payload.
type clientChannelRegistry struct {
	mu       sync.RWMutex
	channels map[string]*clientChannel
}

func newClientChannelRegistry() *clientChannelRegistry {

	return &clientChannelRegistry{channels: make(map[string]*clientChannel)}
}

func (r *clientChannelRegistry) put(key string, cc *clientChannel) {
	if key == "" || cc == nil {

		return
	}
	r.mu.Lock()
	r.channels[key] = cc
	r.mu.Unlock()
}

func (r *clientChannelRegistry) remove(key string) {
	if key == "" {

		return
	}
	r.mu.Lock()
	delete(r.channels, key)
	r.mu.Unlock()
}

// get returns the clientChannel registered under key, or nil.
func (r *clientChannelRegistry) get(key string) *clientChannel {
	if key == "" {

		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.channels[key]
}

// resolveAny walks every registered channel and tries to resolve a pending
// wait for idKeyVal. Used when the client reply did not include a session id
// header (the request id is globally unique in practice given our generator).
// Returns true if a wait was resolved.
func (r *clientChannelRegistry) resolveAny(idKeyVal string, payload json.RawMessage) bool {
	r.mu.RLock()
	channels := make([]*clientChannel, 0, len(r.channels))
	for _, c := range r.channels {
		channels = append(channels, c)
	}
	r.mu.RUnlock()
	for _, c := range channels {
		if c.resolvePending(idKeyVal, payload) {

			return true
		}
	}

	return false
}

// clientChannel models a single in-flight client request to the proxy. The
// upstream MCP server may emit intermediate messages on its streamed response
// body before the final response with the matching id arrives:
//
//   - notifications (method set, id absent) are relayed to the client as-is.
//   - server-initiated requests (method and id set) are relayed to the client
//     and the client's response is written back to the server (via a side POST
//     for HTTP-style transports, or via the SSE session endpoint).
//
// The struct is intentionally minimal; it owns the synchronization around the
// outbound stream writer and the wait map keyed by server-initiated request id.
//
// Lifetime: one clientChannel per call to relayBidirectional. It is created
// when the upstream response body is opened and torn down when the final
// response arrives or the client disconnects.
type clientChannel struct {
	// outboundMu serializes writes to the client. http.ResponseWriter is not
	// safe for concurrent use; the relay reader goroutine writes intermediate
	// messages while the application goroutine may write the final response.
	outboundMu sync.Mutex
	out        io.Writer
	flusher    http.Flusher

	// writeMode picks the on-the-wire framing for relayed payloads to the
	// client:
	//   - "sse"     writes "event: message\n" + "data: <json>\n\n"
	//   - "ndjson"  writes one JSON object per line followed by '\n'
	//   - "single"  writes nothing (single request/response semantics — the
	//               caller will write the final response itself); used when
	//               the client did not opt into streaming. In this mode
	//               intermediate notifications are dropped (the response was
	//               a buffered JSON object historically and we preserve that
	//               for compatibility) but server-initiated requests are
	//               failed back to the server with -32601 so the server
	//               doesn't block waiting forever.
	writeMode string

	// headersWritten is set on first relay write so the caller knows the
	// response has been "committed" to streaming.
	headersWritten bool

	// pendingMu guards pending.
	pendingMu sync.Mutex
	// pending maps server-initiated request id (rendered with idKey) to a
	// channel that will receive the client's response payload. The forwarder
	// installs an entry before relaying the request to the client and blocks
	// (with timeout) on the channel afterwards.
	pending map[string]chan json.RawMessage

	// clientResponse is an inbound channel for the parent handler to push
	// client-side responses to server-initiated requests when the client
	// happens to POST them on a separate connection. In the current proxy we
	// do not yet have an out-of-band client→proxy reply path for streamable
	// HTTP (the client side is also single-shot today), so the channel is
	// unused for streamable HTTP but reserved for future expansion. For SSE
	// the relay receives the client reply on the underlying SSE control
	// stream and resolves the pending entry directly via pending.
	logger *logging.Logger
}

// newClientChannel constructs a clientChannel bound to w. mode selects framing.
func newClientChannel(w http.ResponseWriter, mode string, logger *logging.Logger) *clientChannel {
	flusher, _ := w.(http.Flusher)

	return &clientChannel{
		out:       w,
		flusher:   flusher,
		writeMode: mode,
		pending:   make(map[string]chan json.RawMessage),
		logger:    logger,
	}
}

// writeJSON writes a JSON payload to the client using the configured framing.
// Returns true if anything was written.
func (c *clientChannel) writeJSON(payload []byte) (bool, error) {
	if c == nil {
		return false, fmt.Errorf("nil client channel")
	}

	c.outboundMu.Lock()
	defer c.outboundMu.Unlock()

	switch c.writeMode {
	case "sse":
		if _, err := io.WriteString(c.out, "event: message\n"); err != nil {
			return false, err
		}
		if _, err := io.WriteString(c.out, "data: "); err != nil {
			return false, err
		}
		if _, err := c.out.Write(payload); err != nil {
			return false, err
		}
		if _, err := io.WriteString(c.out, "\n\n"); err != nil {
			return false, err
		}
		if c.flusher != nil {
			c.flusher.Flush()
		}
		c.headersWritten = true

		return true, nil
	case "ndjson":
		if _, err := c.out.Write(payload); err != nil {
			return false, err
		}
		if _, err := io.WriteString(c.out, "\n"); err != nil {
			return false, err
		}
		if c.flusher != nil {
			c.flusher.Flush()
		}
		c.headersWritten = true

		return true, nil
	default:

		return false, nil
	}
}

// registerPending registers a pending wait for a server-initiated request id.
func (c *clientChannel) registerPending(idKey string) chan json.RawMessage {
	ch := make(chan json.RawMessage, 1)
	c.pendingMu.Lock()
	c.pending[idKey] = ch
	c.pendingMu.Unlock()

	return ch
}

// resolvePending delivers a client response payload to the matching pending
// wait. No-op if no pending entry exists. Returns true if a wait was resolved.
func (c *clientChannel) resolvePending(idKey string, payload json.RawMessage) bool {
	c.pendingMu.Lock()
	ch, ok := c.pending[idKey]
	if ok {
		delete(c.pending, idKey)
	}
	c.pendingMu.Unlock()
	if !ok {

		return false
	}
	select {
	case ch <- payload:
	default:
	}

	return true
}

// cancelPending closes all in-flight pending waits (used on teardown).
func (c *clientChannel) cancelPending() {
	c.pendingMu.Lock()
	for k, ch := range c.pending {
		close(ch)
		delete(c.pending, k)
	}
	c.pendingMu.Unlock()
}

// idKey renders a JSON-RPC id to a comparable string. JSON-RPC permits string
// or number ids; we normalize to a stable string form so map lookups work
// regardless of representation.
func idKey(id interface{}) string {
	switch v := id.(type) {
	case nil:

		return ""
	case string:

		return "s:" + v
	case json.Number:

		return "n:" + v.String()
	case float64:

		return fmt.Sprintf("n:%v", v)
	case int, int64, int32:

		return fmt.Sprintf("n:%v", v)
	default:
		// Fallback: marshal.
		if b, err := json.Marshal(v); err == nil {

			return "j:" + string(b)
		}

		return fmt.Sprintf("?:%v", v)
	}
}

// idsEqual compares two JSON-RPC ids loosely (matching on rendered form).
func idsEqual(a, b interface{}) bool {

	return idKey(a) == idKey(b)
}

// classifyMessage inspects a parsed JSON-RPC message and returns one of:
//
//	"notification" - method set, id absent  → fan out to client
//	"request"      - method set, id present → server-initiated request
//	"response"     - method absent, id present (and result|error) → response
//	"unknown"      - malformed / not classifiable
func classifyMessage(msg map[string]interface{}) string {
	hasMethod := false
	if m, ok := msg["method"].(string); ok && m != "" {
		hasMethod = true
	}
	_, hasID := msg["id"]
	if hasMethod && !hasID {

		return "notification"
	}
	if hasMethod && hasID {

		return "request"
	}
	if !hasMethod && hasID {

		return "response"
	}

	return "unknown"
}

// upstreamReplyFn is the callback invoked by the relay to push a client-side
// response (to a server-initiated request) back to the upstream MCP server.
// For HTTP-style transports this is implemented as a side POST; for SSE it
// posts to the session endpoint. Returning an error is logged but does not
// abort the relay — the server simply won't get its reply.
type upstreamReplyFn func(ctx context.Context, payload []byte) error

// relayBidirectional reads JSON-RPC messages from upstreamBody one at a time,
// relaying intermediate notifications and server-initiated requests to the
// client (via cc) and returning the final response message whose id matches
// originalID.
//
// The function returns once the final response is observed or upstreamBody is
// exhausted / closed. Errors from individual message handling are logged but
// do not abort the loop — only IO errors on upstreamBody do.
//
// scanBufSize controls the scanner's max line size; MCP messages can be large
// (tool results with image data), so we allow up to 16 MiB per line.
func relayBidirectional(
	ctx context.Context,
	cc *clientChannel,
	upstreamBody io.Reader,
	originalID interface{},
	logger *logging.Logger,
	upstreamReply upstreamReplyFn,
) (map[string]interface{}, error) {
	reader := bufio.NewReaderSize(upstreamBody, 64*1024)
	dec := json.NewDecoder(reader)
	dec.UseNumber()

	// Track whether we've passed a full message to dec.Decode at least once;
	// if the upstream returns a single JSON object (non-streaming) we still
	// want the original behavior.
	for {
		select {
		case <-ctx.Done():

			return nil, ctx.Err()
		default:
		}

		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			if err == io.EOF {

				return nil, io.EOF
			}
			// json.Decoder doesn't always recover from junk between objects;
			// some MCP servers emit pretty-printed JSON or extra whitespace
			// which is fine, but anything else we treat as end-of-stream.

			return nil, fmt.Errorf("upstream decode: %w", err)
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(raw, &msg); err != nil {
			if logger != nil {
				logger.Warning("relay: dropping non-object upstream message: %v", err)
			}

			continue
		}

		// Convert any json.Number ids to native types so idKey lookups match
		// payloads parsed by other code paths.
		if v, ok := msg["id"].(json.Number); ok {
			if i, err := v.Int64(); err == nil {
				msg["id"] = i
			} else if f, err := v.Float64(); err == nil {
				msg["id"] = f
			} else {
				msg["id"] = string(v)
			}
		}

		kind := classifyMessage(msg)
		switch kind {
		case "response":
			// Is it the final response we're waiting for, or a client→server
			// reply that the server is echoing? In a strict streamable-HTTP
			// transport the only "response" we see is the one matching the
			// original request id.
			if idsEqual(msg["id"], originalID) {

				return msg, nil
			}
			// Otherwise just forward to client (rare).
			if _, err := cc.writeJSON(raw); err != nil && logger != nil {
				logger.Warning("relay: forwarding unmatched response failed: %v", err)
			}

		case "notification":
			// Fan out to the client. If the client is in single mode, we
			// silently drop — this preserves legacy non-streaming behavior.
			if cc.writeMode != "single" {
				if _, err := cc.writeJSON(raw); err != nil && logger != nil {
					logger.Warning("relay: forwarding notification failed: %v", err)
				}
			} else if logger != nil {
				logger.Debug("relay: dropping notification %v on non-streaming client", msg["method"])
			}

		case "request":
			// Server-initiated request. Relay to client and wait for the
			// client's response so we can write it back upstream.
			reqID := msg["id"]
			key := idKey(reqID)

			if cc.writeMode == "single" {
				// We have no way to relay this to a non-streaming client. Reply
				// to the upstream server with method-not-found so it unblocks.
				errReply := map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      reqID,
					"error": map[string]interface{}{
						"code":    -32601,
						"message": "client does not support server-initiated requests on this connection",
					},
				}
				if upstreamReply != nil {
					payload, _ := json.Marshal(errReply)
					if err := upstreamReply(ctx, payload); err != nil && logger != nil {
						logger.Warning("relay: failed to send method-not-found upstream: %v", err)
					}
				}

				continue
			}

			waitCh := cc.registerPending(key)
			if _, err := cc.writeJSON(raw); err != nil {
				if logger != nil {
					logger.Warning("relay: forwarding server-initiated request failed: %v", err)
				}
				cc.resolvePending(key, nil)

				continue
			}

			// Wait for the client to reply. Use a generous timeout — server-
			// initiated requests (sampling/createMessage, elicitation/create)
			// can legitimately take a long time as they often involve a UI
			// round-trip.
			go func(key string, reqID interface{}) {
				timeout := time.NewTimer(5 * time.Minute)
				defer timeout.Stop()
				select {
				case <-ctx.Done():
					cc.resolvePending(key, nil)
				case payload, ok := <-waitCh:
					if !ok || payload == nil {
						// Channel closed without a payload: synthesize an
						// error reply upstream so the server doesn't block.
						errReply := map[string]interface{}{
							"jsonrpc": "2.0",
							"id":      reqID,
							"error": map[string]interface{}{
								"code":    -32603,
								"message": "client closed before responding",
							},
						}
						if upstreamReply != nil {
							b, _ := json.Marshal(errReply)
							if err := upstreamReply(ctx, b); err != nil && logger != nil {
								logger.Warning("relay: failed to send synthesized error upstream: %v", err)
							}
						}

						return
					}
					if upstreamReply != nil {
						if err := upstreamReply(ctx, payload); err != nil && logger != nil {
							logger.Warning("relay: failed to forward client reply upstream: %v", err)
						}
					}
				case <-timeout.C:
					cc.resolvePending(key, nil)
					errReply := map[string]interface{}{
						"jsonrpc": "2.0",
						"id":      reqID,
						"error": map[string]interface{}{
							"code":    -32603,
							"message": "client reply timeout",
						},
					}
					if upstreamReply != nil {
						b, _ := json.Marshal(errReply)
						if err := upstreamReply(ctx, b); err != nil && logger != nil {
							logger.Warning("relay: failed to send timeout error upstream: %v", err)
						}
					}
				}
			}(key, reqID)

		default:
			if logger != nil {
				logger.Debug("relay: dropping unclassifiable message: %s", string(raw))
			}
		}
	}
}

// detectClientStreamMode inspects an incoming client request and returns the
// appropriate clientChannel write mode:
//
//   - "sse"    if the client sent Accept: text/event-stream
//   - "ndjson" if the client asked for streaming (X-Streaming: true) or sent
//     a user-agent we know wants chunked JSON
//   - "single" otherwise (legacy buffered single-response path)
func detectClientStreamMode(r *http.Request) string {
	if r == nil {

		return "single"
	}
	accept := r.Header.Get("Accept")
	if strings.Contains(accept, "text/event-stream") {

		return "sse"
	}
	if r.Header.Get("X-Streaming") == "true" {

		return "ndjson"
	}
	ua := r.Header.Get("User-Agent")
	if strings.Contains(ua, "gemini") || strings.Contains(ua, "Gemini") {

		return "ndjson"
	}

	return "single"
}

// writeClientHeadersForMode writes the appropriate response headers on the
// client side for the chosen mode. It must be called BEFORE the first body
// write. Returns true if it actually wrote a header (always true except for
// "single", where the caller is expected to write its own buffered response).
func writeClientHeadersForMode(w http.ResponseWriter, mode string) bool {
	switch mode {
	case "sse":
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		return true
	case "ndjson":
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Streaming", "true")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Transfer-Encoding", "chunked")
		w.WriteHeader(http.StatusOK)

		return true
	}

	return false
}

// postUpstreamReply posts a JSON-RPC reply payload back to a streamable HTTP
// MCP server using the connection's session id. This is the side channel we
// use when a server-initiated request arrives mid-stream.
func postUpstreamReply(ctx context.Context, conn *discovery.MCPStreamableHTTPConnection, payload []byte) error {
	if conn == nil {

		return fmt.Errorf("nil streamable HTTP connection")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", conn.BaseURL, bytes.NewReader(payload))
	if err != nil {

		return fmt.Errorf("create upstream reply request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if conn.SessionID != "" {
		req.Header.Set("Mcp-Session-Id", conn.SessionID)
	}
	if conn.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+conn.AuthToken)
	}
	resp, err := conn.Client.Do(req)
	if err != nil {

		return fmt.Errorf("upstream reply POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// We don't expect a meaningful body — drain a bit to allow connection reuse.
	_, _ = io.CopyN(io.Discard, resp.Body, 64*1024)
	if resp.StatusCode >= 400 {

		return fmt.Errorf("upstream reply returned %d", resp.StatusCode)
	}

	return nil
}

// postUpstreamSSEReply posts a JSON-RPC reply payload back to an SSE MCP
// server via its session endpoint.
func postUpstreamSSEReply(ctx context.Context, sessionURL, authToken string, client *http.Client, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, "POST", sessionURL, bytes.NewReader(payload))
	if err != nil {

		return fmt.Errorf("create SSE reply request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	resp, err := client.Do(req)
	if err != nil {

		return fmt.Errorf("SSE reply POST: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.CopyN(io.Discard, resp.Body, 64*1024)
	if resp.StatusCode >= 400 {

		return fmt.Errorf("SSE reply returned %d", resp.StatusCode)
	}

	return nil
}
