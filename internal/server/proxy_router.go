// internal/server/proxy_router.go
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
	"time"

	"github.com/phildougherty/m8e/internal/discovery"
)

// This file holds the HTTP/SSE request routing surface of the proxy: choosing
// the right per-protocol transport for a discovered server and forwarding the
// request/response (including SSE framing). It works alongside ProtocolBridge,
// which owns the lower-level MCP JSON-RPC machinery.

// HandleMCPRequest handles MCP requests using discovered services
func (h *ProxyHandler) HandleMCPRequest(w http.ResponseWriter, r *http.Request, serverName string) {
	// Instrument the request: wrap w so the metric records the FINAL status
	// written to the client, and record on the way out (defer) so error paths
	// are counted too. The server label is the resolved connection name once
	// known, falling back to the requested serverName when resolution fails.
	start := time.Now()
	rec := newStatusRecorder(w)
	w = rec
	metricServer := serverName
	defer func() {
		h.recordProxyRequest(metricServer, r.Method, rec.status, start)
	}()

	// Get connection for the server
	conn, err := h.ConnectionManager.GetConnection(serverName)
	if err != nil {
		h.Logger.Error("Failed to get connection for server %s: %v", serverName, err)
		h.writeErrorResponse(w, fmt.Sprintf("Server %s not available: %v", serverName, err), http.StatusServiceUnavailable)
		return
	}
	metricServer = conn.Name

	// CRITICAL FIX: Extract and set OAuth token from request
	h.setOAuthTokenOnConnection(r, conn)

	// Update connection stats. The active-connection gauge brackets the
	// in-flight request against this server: Opened here, Closed when the
	// handler returns (including the long-lived SSE stream path, which blocks
	// in the forwarder until the stream ends).
	h.updateConnectionStats(conn.Name, true)
	h.metrics.ConnectionOpened(conn.Name)
	defer h.metrics.ConnectionClosed(conn.Name)

	// Route based on protocol
	switch conn.Protocol {
	case "http":
		h.handleHTTPRequest(w, r, conn)
	case "http-stream":
		h.handleStreamableHTTPRequest(w, r, conn)
	case "sse":
		h.handleSSERequest(w, r, conn)
	default:
		h.writeErrorResponse(w, fmt.Sprintf("Unsupported protocol: %s", conn.Protocol), http.StatusBadRequest)
	}
}

// handleHTTPRequest handles HTTP protocol requests
func (h *ProxyHandler) handleHTTPRequest(w http.ResponseWriter, r *http.Request, conn *discovery.MCPConnection) {
	if conn.HTTPConnection == nil {
		h.writeErrorResponse(w, "HTTP connection not available", http.StatusServiceUnavailable)
		return
	}

	// Detect if this is a Gemini CLI request
	userAgent := r.Header.Get("User-Agent")
	acceptHeader := r.Header.Get("Accept")
	isGeminiRequest := strings.Contains(userAgent, "gemini") ||
		strings.Contains(userAgent, "Gemini")

	// If it's a Gemini request asking for SSE (text/event-stream), redirect to SSE handler
	if isGeminiRequest && strings.Contains(acceptHeader, "text/event-stream") {
		h.Logger.Info("Detected Gemini CLI SSE request for HTTP server %s, redirecting to SSE handler", conn.Name)
		// Find the corresponding SSE connection if available
		if sseConn, err := h.ConnectionManager.GetConnection(conn.Name); err == nil && sseConn.Protocol == "sse" {
			h.handleSSERequest(w, r, sseConn)
		} else {
			h.Logger.Warning("Gemini CLI requested SSE for HTTP-only server %s", conn.Name)
			h.forwardK8sHTTPRequest(w, r, conn.HTTPConnection)
		}
	} else {
		// Forward regular HTTP request to the actual server (works for both regular clients and Gemini CLI with httpUrl)
		h.forwardK8sHTTPRequest(w, r, conn.HTTPConnection)
	}
}

// handleStreamableHTTPRequest handles streamable HTTP protocol requests
func (h *ProxyHandler) handleStreamableHTTPRequest(w http.ResponseWriter, r *http.Request, conn *discovery.MCPConnection) {
	if conn.StreamableHTTPConnection == nil {
		h.writeErrorResponse(w, "Streamable HTTP connection not available", http.StatusServiceUnavailable)
		return
	}

	// Detect if this is a Gemini CLI request that expects streaming
	userAgent := r.Header.Get("User-Agent")
	acceptHeader := r.Header.Get("Accept")
	expectsStreaming := strings.Contains(userAgent, "gemini") ||
		strings.Contains(userAgent, "Gemini") ||
		strings.Contains(acceptHeader, "text/event-stream") ||
		r.Header.Get("X-Streaming") == "true"

	if expectsStreaming {
		h.Logger.Info("Detected streaming request for streamable HTTP server %s", conn.Name)
		h.forwardStreamableHTTPRequest(w, r, conn.StreamableHTTPConnection, true)
	} else {
		// Forward regular request to streamable HTTP server
		h.forwardStreamableHTTPRequest(w, r, conn.StreamableHTTPConnection, false)
	}
}

// handleSSERequest handles SSE protocol requests
func (h *ProxyHandler) handleSSERequest(w http.ResponseWriter, r *http.Request, conn *discovery.MCPConnection) {
	if conn.SSEConnection == nil {
		h.writeErrorResponse(w, "SSE connection not available", http.StatusServiceUnavailable)
		return
	}

	// Check if client expects text/event-stream (like Gemini CLI)
	acceptHeader := r.Header.Get("Accept")
	if strings.Contains(acceptHeader, "text/event-stream") {
		h.Logger.Info("Client expects text/event-stream for server %s, providing SSE format", conn.Name)
		h.handleSSEStreamRequest(w, r, conn)
	} else {
		// Forward regular request to SSE server
		h.forwardSSERequest(w, r, conn.SSEConnection)
	}
}

// forwardStreamableHTTPRequest forwards a streamable HTTP request to the target server.
//
// This is the bidirectional message relay for the streamable-HTTP transport.
// The upstream MCP server may emit any number of intermediate JSON-RPC
// messages — progress notifications, sampling/createMessage server-initiated
// requests, resources/updated notifications — before the final response with
// the matching id arrives. We demux them, relaying notifications and
// server-initiated requests to the client and writing the client's responses
// to server-initiated requests back upstream via a side POST.
//
// Behavior is preserved for the common case: a client doing a plain single
// request/response gets one buffered JSON response, exactly as before.
func (h *ProxyHandler) forwardStreamableHTTPRequest(w http.ResponseWriter, r *http.Request, conn *discovery.MCPStreamableHTTPConnection, streaming bool) {
	// MCP servers expect requests to be sent to the root path
	targetURL := conn.BaseURL + "/"
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	// Buffer the client request body so we can both forward it upstream and
	// inspect it to identify the original JSON-RPC request id. The id is what
	// distinguishes the final response from intermediate server-initiated
	// requests on the upstream stream.
	var clientBody []byte
	if r.Body != nil {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			h.writeErrorResponse(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		clientBody = b
	}
	var clientReq map[string]interface{}
	_ = json.Unmarshal(clientBody, &clientReq)
	originalID := clientReq["id"]

	req, err := http.NewRequest(r.Method, targetURL, bytes.NewReader(clientBody))
	if err != nil {
		h.writeErrorResponse(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	// Copy headers
	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	// Add streaming-specific headers if needed
	if streaming {
		req.Header.Set("X-Streaming", "true")
		req.Header.Set("Cache-Control", "no-cache")
		req.Header.Set("Accept", "application/json, text/event-stream")
	}

	// Add session ID if available
	sessionID := conn.SessionID
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	// Make the request
	resp, err := conn.Client.Do(req)
	if err != nil {
		h.updateConnectionStats(conn.BaseURL, false)
		h.writeErrorResponse(w, "Failed to forward request", http.StatusBadGateway)
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			h.Logger.Warning("Failed to close response body: %v", err)
		}
	}()

	// Update session ID if provided in response
	if newSessionID := resp.Header.Get("Mcp-Session-Id"); newSessionID != "" {
		if newSessionID != conn.SessionID {
			h.Logger.Info("Server %s updated Mcp-Session-Id from '%s' to '%s'", conn.BaseURL, conn.SessionID, newSessionID)
			conn.SessionID = newSessionID
		}
	}

	// Decide whether to run the bidirectional relay or fall back to legacy
	// byte-copy / buffered behavior.
	//
	// We engage the relay whenever:
	//   - the upstream is using a streaming framing (chunked / X-Streaming /
	//     text/event-stream), AND
	//   - we have an identifiable original request id so we know what "final"
	//     looks like.
	//
	// For non-streaming upstreams (the original tools/list path against a
	// regular HTTP server, for example) we keep the existing buffered path so
	// the request/response shape on the wire is unchanged.
	upstreamIsStreaming := resp.Header.Get("Transfer-Encoding") == "chunked" ||
		resp.Header.Get("X-Streaming") == "true" ||
		strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")

	if upstreamIsStreaming && originalID != nil {
		h.runBidirectionalStreamableHTTPRelay(w, r, conn, resp, originalID)
		return
	}

	// Handle streaming vs regular response (legacy path)
	if streaming && upstreamIsStreaming {
		h.forwardStreamingResponse(w, resp)
	} else {
		h.forwardRegularResponse(w, r, resp)
	}
}

// runBidirectionalStreamableHTTPRelay drives the relay for a streamable HTTP
// upstream. It assumes resp.Body is the upstream response stream and reads
// JSON-RPC messages off it until it sees the final response whose id matches
// originalID.
func (h *ProxyHandler) runBidirectionalStreamableHTTPRelay(
	w http.ResponseWriter,
	r *http.Request,
	conn *discovery.MCPStreamableHTTPConnection,
	resp *http.Response,
	originalID interface{},
) {
	// Decide the client-side framing. If the client did not ask for streaming
	// we still need to drain intermediate messages — we just don't write them
	// to the client (single-response semantics preserved). For sampling/
	// elicitation server-initiated requests, the relay will synthesize an
	// upstream error so the server unblocks.
	mode := detectClientStreamMode(r)
	cc := newClientChannel(w, mode, h.Logger)
	defer cc.cancelPending()

	// Register the channel so out-of-band client replies (separate HTTP POST
	// carrying a JSON-RPC response to a server-initiated request) can be
	// routed back here. Prefer the client-supplied Mcp-Session-Id; fall back
	// to the connection's session id.
	regKey := r.Header.Get("Mcp-Session-Id")
	if regKey == "" {
		regKey = conn.SessionID
	}
	if regKey != "" && h.activeChannels != nil {
		h.activeChannels.put(regKey, cc)
		defer h.activeChannels.remove(regKey)
	}

	if mode != "single" {
		writeClientHeadersForMode(w, mode)
	}

	ctx := r.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	reply := func(rctx context.Context, payload []byte) error {
		return postUpstreamReply(rctx, conn, payload)
	}

	finalMsg, err := relayBidirectional(ctx, cc, resp.Body, originalID, h.Logger, reply)
	if err != nil && err != io.EOF {
		h.Logger.Warning("relay (streamable HTTP) ended with error: %v", err)
	}
	if finalMsg == nil {
		if !cc.headersWritten {
			// Nothing went out yet; signal an error.
			h.writeErrorResponse(w, "upstream closed before final response", http.StatusBadGateway)
		}
		return
	}

	// Emit the final response. In streaming modes it goes through the same
	// framing as intermediate messages; in single mode we write it directly.
	final, err := json.Marshal(finalMsg)
	if err != nil {
		h.Logger.Error("relay: failed to marshal final response: %v", err)
		if !cc.headersWritten {
			h.writeErrorResponse(w, "failed to encode response", http.StatusInternalServerError)
		}
		return
	}

	if mode == "single" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(final)
		return
	}

	if _, werr := cc.writeJSON(final); werr != nil {
		h.Logger.Warning("relay: failed to write final response to client: %v", werr)
	}
}

// forwardStreamingResponse forwards a streaming response
func (h *ProxyHandler) forwardStreamingResponse(w http.ResponseWriter, resp *http.Response) {
	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Ensure streaming headers are set
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("X-Streaming", "true")
	w.Header().Set("Cache-Control", "no-cache")

	// Copy status code
	w.WriteHeader(resp.StatusCode)

	// Stream the response
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeErrorResponse(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	buffer := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			if _, err := w.Write(buffer[:n]); err != nil {
				h.Logger.Warning("Failed to write streaming response chunk: %v", err)
				return
			}
			flusher.Flush()
		}
		if err != nil {
			break
		}
	}
}

// forwardRegularResponse forwards a regular response with OpenWebUI processing
func (h *ProxyHandler) forwardRegularResponse(w http.ResponseWriter, r *http.Request, resp *http.Response) {
	// Read response body first to check if we need to process it for OpenWebUI
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.Logger.Error("Failed to read response body: %v", err)
		return
	}

	// Check if this is a tools/call response that needs OpenWebUI processing
	if h.openWebUI.shouldProcess(r, responseBody) {
		h.Logger.Info("Processing MCP response for OpenWebUI compatibility")
		processedResponse := h.openWebUI.processResponse(responseBody)
		if processedResponse != nil {
			// Return plain text for OpenWebUI
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, err = w.Write(processedResponse)
			if err != nil {
				h.Logger.Error("Failed to write processed response: %v", err)
			}
			return
		}
	}

	// Copy response headers for non-OpenWebUI response
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Copy status code
	w.WriteHeader(resp.StatusCode)

	// Copy original response body for non-OpenWebUI or failed processing
	_, err = w.Write(responseBody)
	if err != nil {
		h.Logger.Error("Failed to copy response body: %v", err)
	}
}

// forwardK8sHTTPRequest forwards an HTTP request to the target server
func (h *ProxyHandler) forwardK8sHTTPRequest(w http.ResponseWriter, r *http.Request, conn *discovery.MCPHTTPConnection) {
	// MCP servers expect requests to be sent to the root path
	targetURL := conn.BaseURL + "/"
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	req, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		h.writeErrorResponse(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	// Copy headers
	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	// Make the request
	resp, err := conn.Client.Do(req)
	if err != nil {
		h.updateConnectionStats(conn.BaseURL, false)
		h.writeErrorResponse(w, "Failed to forward request", http.StatusBadGateway)
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			h.Logger.Warning("Failed to close response body: %v", err)
		}
	}()

	// Read response body first to check if we need to process it for OpenWebUI
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		h.Logger.Error("Failed to read response body: %v", err)
		return
	}

	// Check if this is a tools/call response that needs OpenWebUI processing
	if h.openWebUI.shouldProcess(r, responseBody) {
		h.Logger.Info("Processing MCP response for OpenWebUI compatibility")
		processedResponse := h.openWebUI.processResponse(responseBody)
		if processedResponse != nil {
			// Return plain text for OpenWebUI
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, err = w.Write(processedResponse)
			if err != nil {
				h.Logger.Error("Failed to write processed response: %v", err)
			}
			return
		}
	}

	// Copy response headers for non-OpenWebUI response
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Copy status code
	w.WriteHeader(resp.StatusCode)

	// Copy original response body for non-OpenWebUI or failed processing
	_, err = w.Write(responseBody)
	if err != nil {
		h.Logger.Error("Failed to copy response body: %v", err)
	}
}

// forwardSSERequest forwards an SSE request to the target server.
//
// SSE is naturally streaming, so the original implementation simply byte-
// copied the upstream response to the client. That works for notifications
// (which flow server→client) but does not handle server-initiated requests:
// the proxy must route the client's reply back to the server's session
// endpoint. This implementation parses the upstream SSE stream into JSON-RPC
// messages and relays each one to the client; for server-initiated requests
// it spawns a side-POST to the session endpoint with the client's reply.
//
// Notifications still pass straight through. The client sees the same
// event-stream wire format as before.
func (h *ProxyHandler) forwardSSERequest(w http.ResponseWriter, r *http.Request, conn *discovery.MCPSSEConnection) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)

	// Establish the SSE session endpoint so we can route client replies for
	// server-initiated requests back to the server. If this fails we fall
	// back to a one-way relay (notifications still work).
	sessionEndpoint, sessErr := h.establishSSESession(r.Context(), conn)
	sessionURL := ""
	if sessErr == nil {
		sessionURL = conn.BaseURL + sessionEndpoint
	} else {
		h.Logger.Warning("SSE: could not pre-establish session for upstream replies: %v", sessErr)
	}

	// Open the long-lived SSE GET to the upstream.
	req, err := http.NewRequest("GET", conn.BaseURL, nil)
	if err != nil {
		h.writeErrorResponse(w, "Failed to create SSE request", http.StatusInternalServerError)
		return
	}
	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if conn.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+conn.AuthToken)
	}

	resp, err := conn.Client.Do(req)
	if err != nil {
		h.updateConnectionStats(conn.BaseURL, false)
		h.writeErrorResponse(w, "Failed to connect to SSE server", http.StatusBadGateway)
		return
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			h.Logger.Warning("Failed to close response body: %v", err)
		}
	}()

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.writeErrorResponse(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	cc := &clientChannel{
		out:       w,
		flusher:   flusher,
		writeMode: "sse",
		pending:   make(map[string]chan json.RawMessage),
		logger:    h.Logger,
	}
	defer cc.cancelPending()

	regKey := r.Header.Get("Mcp-Session-Id")
	if regKey == "" {
		regKey = conn.SessionID
	}
	if regKey != "" && h.activeChannels != nil {
		h.activeChannels.put(regKey, cc)
		defer h.activeChannels.remove(regKey)
	}

	ctx := r.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// On SSE we drive a custom loop because we want to preserve the upstream
	// SSE event names (so e.g. "endpoint" events pass through) while still
	// intercepting JSON-RPC messages buried in "data:" lines.
	h.driveSSEBidirectional(ctx, cc, resp.Body, sessionURL, conn.AuthToken)
}

// driveSSEBidirectional reads an upstream SSE stream and relays it to the
// client (cc). For each parsed JSON-RPC message it intercepts:
//
//	notification → forwarded as-is (already would be)
//	request      → forwarded as-is; spawns a goroutine that waits for the
//	               client's reply (posted to the session endpoint) and routes
//	               it back to the upstream session URL.
//	response     → forwarded as-is
//
// Because SSE is a continuous stream rather than a single-request response,
// there is no notion of a "final" message — we run until the stream is closed.
func (h *ProxyHandler) driveSSEBidirectional(
	ctx context.Context,
	cc *clientChannel,
	body io.Reader,
	sessionURL, authToken string,
) {
	reader := bufio.NewReaderSize(body, 64*1024)
	var eventName string
	var dataBuf bytes.Buffer
	httpClient := h.sseClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 0}
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			h.Logger.Warning("SSE upstream read error: %v", err)
			return
		}
		raw := strings.TrimRight(line, "\r\n")

		// Always forward the byte line verbatim to the client so we don't
		// strip SSE event names / comments / heartbeats. The bidirectional
		// interception is layered on top — when we observe a complete event
		// with a JSON-RPC payload we additionally handle it.
		cc.outboundMu.Lock()
		_, _ = io.WriteString(cc.out, line)
		if cc.flusher != nil {
			cc.flusher.Flush()
		}
		cc.headersWritten = true
		cc.outboundMu.Unlock()

		if raw == "" {
			// End of an SSE event. Process accumulated data.
			if dataBuf.Len() > 0 {
				payload := bytes.TrimSpace(dataBuf.Bytes())
				dataBuf.Reset()
				h.handleSSEUpstreamPayload(ctx, cc, payload, eventName, sessionURL, authToken, httpClient)
			}
			eventName = ""

			if err == io.EOF {
				return
			}
			continue
		}

		if strings.HasPrefix(raw, "event:") {
			eventName = strings.TrimSpace(strings.TrimPrefix(raw, "event:"))
		} else if strings.HasPrefix(raw, "data:") {
			d := strings.TrimPrefix(raw, "data:")
			d = strings.TrimPrefix(d, " ")
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(d)
		}

		if err == io.EOF {
			return
		}
	}
}

// handleSSEUpstreamPayload inspects a single SSE event's JSON payload and, if
// it is a server-initiated request, waits for the client's reply and posts it
// to the SSE session endpoint.
func (h *ProxyHandler) handleSSEUpstreamPayload(
	ctx context.Context,
	cc *clientChannel,
	payload []byte,
	eventName string,
	sessionURL, authToken string,
	httpClient *http.Client,
) {
	// "endpoint" events carry the session path, not a JSON-RPC message — skip.
	if eventName == "endpoint" {
		return
	}
	var msg map[string]interface{}
	if err := json.Unmarshal(payload, &msg); err != nil {
		// Not JSON; nothing to intercept.
		return
	}
	kind := classifyMessage(msg)
	if kind != "request" {
		return
	}
	if sessionURL == "" {
		// We have no way to post a reply upstream. Just log.
		h.Logger.Warning("SSE: dropping server-initiated request %v (no session URL)", msg["method"])
		return
	}
	reqID := msg["id"]
	key := idKey(reqID)
	waitCh := cc.registerPending(key)

	go func() {
		timeout := time.NewTimer(5 * time.Minute)
		defer timeout.Stop()
		select {
		case <-ctx.Done():
			cc.resolvePending(key, nil)
		case reply, ok := <-waitCh:
			if !ok || reply == nil {
				errReply := map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      reqID,
					"error": map[string]interface{}{
						"code":    -32603,
						"message": "client closed before responding",
					},
				}
				b, _ := json.Marshal(errReply)
				if err := postUpstreamSSEReply(ctx, sessionURL, authToken, httpClient, b); err != nil {
					h.Logger.Warning("SSE: synthesized error upstream reply failed: %v", err)
				}
				return
			}
			if err := postUpstreamSSEReply(ctx, sessionURL, authToken, httpClient, reply); err != nil {
				h.Logger.Warning("SSE: forwarding client reply upstream failed: %v", err)
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
			b, _ := json.Marshal(errReply)
			if err := postUpstreamSSEReply(ctx, sessionURL, authToken, httpClient, b); err != nil {
				h.Logger.Warning("SSE: timeout error upstream reply failed: %v", err)
			}
		}
	}()
}

// handleSSEStreamRequest handles SSE requests that need text/event-stream format
func (h *ProxyHandler) handleSSEStreamRequest(w http.ResponseWriter, r *http.Request, conn *discovery.MCPConnection) {
	// For GET requests to SSE endpoints, provide proper SSE stream
	if r.Method == "GET" {
		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)

		// Send initial server info as SSE events
		serverInfo := map[string]interface{}{
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{
					"listChanged": true,
				},
			},
			"serverInfo": map[string]interface{}{
				"name":    conn.Name,
				"version": "1.0.0",
			},
			"protocol":        "sse",
			"protocolVersion": "2024-11-05",
			"status":          "connected",
		}

		infoData, _ := json.Marshal(serverInfo)

		// Send as SSE event
		if _, err := fmt.Fprintf(w, "event: message\n"); err != nil {
			h.Logger.Warning("Failed to write SSE event header: %v", err)
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", string(infoData)); err != nil {
			h.Logger.Warning("Failed to write SSE data: %v", err)
		}

		// Flush to ensure data is sent immediately
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		// Keep the stream open briefly so the initial event reaches the
		// client before close. The bare time.Sleep used here previously
		// ignored client disconnects, so a closed connection still pinned
		// the goroutine for the full second.
		select {
		case <-r.Context().Done():
		case <-time.After(1 * time.Second):
		}
		return
	}

	// For POST requests, handle as regular MCP requests but return SSE format
	h.forwardSSERequest(w, r, conn.SSEConnection)
}
