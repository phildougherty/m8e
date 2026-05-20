// internal/server/protocol_bridge.go
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
	"github.com/phildougherty/m8e/internal/logging"
)

// ProtocolBridge owns the MCP JSON-RPC request/response machinery: per-protocol
// HTTP/SSE/streamable transports, session handshakes, SSE stream parsing and
// tools/list discovery. It deliberately knows nothing about HTTP routing, auth
// or the tool cache - those concerns live in their own types.
type ProtocolBridge struct {
	httpClient *http.Client
	sseClient  *http.Client
	ids        *idGenerator
	stats      *connStatsTracker
	logger     *logging.Logger
}

// newProtocolBridge wires a bridge with the shared ID generator and stats
// tracker. The HTTP client timeout is extended for long-running tools such as
// execute_agent; the SSE client has no timeout so streams stay open.
func newProtocolBridge(ids *idGenerator, stats *connStatsTracker, logger *logging.Logger) *ProtocolBridge {
	return &ProtocolBridge{
		httpClient: &http.Client{Timeout: 25 * time.Minute},
		sseClient:  &http.Client{Timeout: 0},
		ids:        ids,
		stats:      stats,
		logger:     logger,
	}
}

// toolsDiscoverySequenceTimeout caps the entire initialize -> initialized ->
// tools/list handshake. Without it, a server that accepts the TCP connection
// but never responds at any step would block the caller indefinitely; the
// per-request HTTP client timeouts only bound individual round trips and
// don't protect the multi-step sequence as a whole.
//
// It is a var rather than a const so tests can lower the bound to keep the
// hanging-server case fast; production code never mutates it.
var toolsDiscoverySequenceTimeout = 30 * time.Second

// makeToolsListRequest makes an MCP tools/list request to discover tools.
func (b *ProtocolBridge) makeToolsListRequest(serverName string, conn *discovery.MCPConnection) ([]Tool, error) {
	// Double-check connection status before making the request
	if conn.Status != "connected" {
		return nil, fmt.Errorf("server %s is not connected (status: %s)", serverName, conn.Status)
	}

	// Create MCP tools/list request with string ID
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "tools/list",
		"params":  map[string]interface{}{},
		"id":      b.ids.nextString(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), toolsDiscoverySequenceTimeout)
	defer cancel()

	// Send request based on protocol
	switch conn.Protocol {
	case "http":
		return b.makeHTTPToolsListRequest(ctx, conn, request)
	case "http-stream":
		return b.makeStreamableHTTPToolsListRequest(ctx, conn, request)
	case "sse":
		return b.makeSSEToolsListRequest(ctx, conn, request)
	default:
		return nil, fmt.Errorf("unsupported protocol for tools discovery: %s", conn.Protocol)
	}
}

// makeHTTPToolsListRequest makes HTTP tools/list request with proper MCP session management
func (b *ProtocolBridge) makeHTTPToolsListRequest(ctx context.Context, conn *discovery.MCPConnection, request map[string]interface{}) ([]Tool, error) {
	if conn.HTTPConnection == nil {
		return nil, fmt.Errorf("no HTTP connection available")
	}

	// Generate session ID for this request sequence
	sessionID := b.ids.nextString()

	// Step 1: Send initialize request
	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      b.ids.nextString(),
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]interface{}{
				"name":    "matey-proxy",
				"version": "1.0.0",
			},
			"capabilities": map[string]interface{}{},
		},
	}

	b.logger.Info("Sending MCP initialize request to %s with session %s and OAuth token: %t",
		conn.HTTPConnection.BaseURL, sessionID, conn.HTTPConnection.AuthToken != "")

	initResponse, err := b.sendHTTPRequestWithSession(ctx, conn.HTTPConnection, sessionID, initRequest)
	if err != nil {
		b.logger.Error("Failed to send MCP initialize request to %s: %v", conn.HTTPConnection.BaseURL, err)
		return nil, fmt.Errorf("failed to initialize HTTP session: %w", err)
	}

	// Check initialize response
	b.logger.Info("MCP initialize response: %v", initResponse)
	if initResponse["error"] != nil {
		b.logger.Error("MCP initialize failed for %s: %v", conn.HTTPConnection.BaseURL, initResponse["error"])
		return nil, fmt.Errorf("initialize failed: %v", initResponse["error"])
	}
	b.logger.Info("MCP initialize succeeded for %s", conn.HTTPConnection.BaseURL)

	// Step 2: Send initialized notification
	initializedNotif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]interface{}{},
	}

	b.logger.Info("Sending MCP initialized notification to %s", conn.HTTPConnection.BaseURL)
	err = b.sendHTTPNotificationWithSession(ctx, conn.HTTPConnection, sessionID, initializedNotif)
	if err != nil {
		b.logger.Error("Failed to send MCP initialized notification to %s: %v", conn.HTTPConnection.BaseURL, err)
		return nil, fmt.Errorf("failed to send initialized notification: %w", err)
	}
	b.logger.Info("MCP initialized notification sent successfully to %s", conn.HTTPConnection.BaseURL)

	// Step 3: Send tools/list request
	toolsRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      b.ids.nextString(),
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}

	b.logger.Info("Sending MCP tools/list request to %s", conn.HTTPConnection.BaseURL)
	toolsResponse, err := b.sendHTTPRequestWithSession(ctx, conn.HTTPConnection, sessionID, toolsRequest)
	if err != nil {
		b.logger.Error("Failed to send MCP tools/list request to %s: %v", conn.HTTPConnection.BaseURL, err)
		return nil, fmt.Errorf("failed to send tools/list request: %w", err)
	}

	b.logger.Info("MCP tools/list response from %s: %v", conn.HTTPConnection.BaseURL, toolsResponse)
	if toolsResponse["error"] != nil {
		b.logger.Error("MCP tools/list failed for %s: %v", conn.HTTPConnection.BaseURL, toolsResponse["error"])
	}

	return parseToolsFromMCPResponse(toolsResponse)
}

// makeStreamableHTTPToolsListRequest makes streamable HTTP tools/list request with proper MCP session management
func (b *ProtocolBridge) makeStreamableHTTPToolsListRequest(ctx context.Context, conn *discovery.MCPConnection, request map[string]interface{}) ([]Tool, error) {
	if conn.StreamableHTTPConnection == nil {
		return nil, fmt.Errorf("no streamable HTTP connection available")
	}

	// Generate session ID for this request sequence
	sessionID := b.ids.nextString()

	// Step 1: Send initialize request
	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      b.ids.nextString(),
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]interface{}{
				"name":    "matey-proxy",
				"version": "1.0.0",
			},
			"capabilities": map[string]interface{}{},
		},
	}

	initResponse, err := b.sendStreamableHTTPRequestWithSession(ctx, conn.StreamableHTTPConnection, sessionID, initRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize streamable HTTP session: %w", err)
	}

	// Check initialize response
	if initResponse["error"] != nil {
		return nil, fmt.Errorf("initialize failed: %v", initResponse["error"])
	}

	// Step 2: Send initialized notification
	initializedNotif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]interface{}{},
	}

	err = b.sendStreamableHTTPNotificationWithSession(ctx, conn.StreamableHTTPConnection, sessionID, initializedNotif)
	if err != nil {
		return nil, fmt.Errorf("failed to send initialized notification: %w", err)
	}

	// Step 3: Send tools/list request
	toolsRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      b.ids.nextString(),
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}

	toolsResponse, err := b.sendStreamableHTTPRequestWithSession(ctx, conn.StreamableHTTPConnection, sessionID, toolsRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to send tools/list request: %w", err)
	}

	return parseToolsFromMCPResponse(toolsResponse)
}

// makeSSEToolsListRequest makes SSE tools/list request with proper MCP session management
func (b *ProtocolBridge) makeSSEToolsListRequest(ctx context.Context, conn *discovery.MCPConnection, request map[string]interface{}) ([]Tool, error) {
	if conn.SSEConnection == nil {
		return nil, fmt.Errorf("no SSE connection available")
	}

	b.logger.Info("Starting SSE tools/list request for server %s", conn.Name)

	// Step 1: Establish SSE connection and get session endpoint
	sessionEndpoint, err := b.establishSSESession(ctx, conn.SSEConnection)
	if err != nil {
		return nil, fmt.Errorf("failed to establish SSE session: %w", err)
	}

	sessionURL := conn.SSEConnection.BaseURL + sessionEndpoint
	b.logger.Info("Got session URL: %s", sessionURL)

	// Step 2: Send initialize request (don't wait for response)
	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      b.ids.nextString(),
		"method":  "initialize",
		"params": map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]interface{}{
				"name":    "matey-proxy",
				"version": "1.0.0",
			},
			"capabilities": map[string]interface{}{},
		},
	}

	err = b.sendSSENotificationWithAuth(ctx, sessionURL, conn.SSEConnection.AuthToken, initRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to send initialize request: %w", err)
	}

	// Step 3: Send initialized notification (don't wait for response)
	initializedNotif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]interface{}{},
	}

	err = b.sendSSENotificationWithAuth(ctx, sessionURL, conn.SSEConnection.AuthToken, initializedNotif)
	if err != nil {
		b.logger.Warning("Failed to send initialized notification: %v (continuing anyway)", err)
	} else {
		b.logger.Info("Sent initialized notification successfully")
	}

	// SSE notifications are fire-and-forget POSTs, so the server's initialize
	// reply hasn't arrived yet and we can't strictly enforce causal ordering
	// here without a server-side ack channel. A short ctx-aware wait gives the
	// peer a chance to process before we issue tools/list, while still
	// honouring cancellation (the bare time.Sleep used previously did not).
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("tools/list discovery cancelled: %w", ctx.Err())
	case <-time.After(100 * time.Millisecond):
	}

	// Step 4: Send tools/list request
	toolsRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      b.ids.nextString(),
		"method":  "tools/list",
		"params":  map[string]interface{}{},
	}

	toolsResponse, err := b.sendSSERequestAndWaitForResponseWithAuth(ctx, sessionURL, conn.SSEConnection.AuthToken, toolsRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to send tools/list request: %w", err)
	}

	b.logger.Info("Got tools/list response: %v", toolsResponse)
	return parseToolsFromMCPResponse(toolsResponse)
}

// establishSSESession establishes an SSE session and returns the session endpoint
func (b *ProtocolBridge) establishSSESession(ctx context.Context, sseConn *discovery.MCPSSEConnection) (string, error) {
	sseURL := sseConn.BaseURL + "/sse"
	b.logger.Debug("Establishing SSE session to: %s", sseURL)

	// Try GET first (some SSE servers expect GET for initial handshake)
	req, err := http.NewRequestWithContext(ctx, "GET", sseURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create handshake request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")

	// Add OAuth token if available
	if sseConn.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+sseConn.AuthToken)
		b.logger.Debug("Added OAuth Bearer token to SSE session handshake for %s", sseURL)
	}

	// Send handshake request
	resp, err := sseConn.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send handshake request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			b.logger.Warning("Failed to close response body: %v", err)
		}
	}()

	b.logger.Debug("SSE handshake response status: %d", resp.StatusCode)

	// Parse SSE stream for session endpoint
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		b.logger.Debug("SSE handshake line: %s", line)

		// Handle empty lines
		if line == "" {
			continue
		}

		// Look for endpoint event
		if strings.HasPrefix(line, "event: endpoint") {
			// Next line should contain the session endpoint
			if scanner.Scan() {
				dataLine := scanner.Text()
				b.logger.Debug("SSE handshake data line: %s", dataLine)
				if strings.HasPrefix(dataLine, "data: ") {
					endpoint := strings.TrimPrefix(dataLine, "data: ")
					b.logger.Info("Found SSE session endpoint: %s", endpoint)
					return endpoint, nil
				}
			}
		}

		// Some servers may send session info in different formats
		if strings.HasPrefix(line, "data: ") {
			dataContent := strings.TrimPrefix(line, "data: ")
			b.logger.Debug("SSE handshake data content: %s", dataContent)

			// Try to parse as JSON to see if it contains session info
			var sessionInfo map[string]interface{}
			if err := json.Unmarshal([]byte(dataContent), &sessionInfo); err == nil {
				if sessionEndpoint, ok := sessionInfo["endpoint"].(string); ok {
					b.logger.Info("Found session endpoint in JSON: %s", sessionEndpoint)
					return sessionEndpoint, nil
				}
				if sessionPath, ok := sessionInfo["path"].(string); ok {
					b.logger.Info("Found session path in JSON: %s", sessionPath)
					return sessionPath, nil
				}
			}

			// If it looks like a path, use it directly
			if strings.HasPrefix(dataContent, "/") {
				b.logger.Info("Using data content as session endpoint: %s", dataContent)
				return dataContent, nil
			}
		}
	}

	return "", fmt.Errorf("no session endpoint found in SSE handshake response")
}

// parseSSEResponse parses SSE stream for MCP JSON-RPC response
func (b *ProtocolBridge) parseSSEResponse(reader io.Reader) (map[string]interface{}, error) {
	scanner := bufio.NewScanner(reader)

	for scanner.Scan() {
		line := scanner.Text()
		b.logger.Debug("SSE response line: %s", line)

		// Handle empty lines
		if line == "" {
			continue
		}

		// Look for SSE event: response format (proper MCP format)
		if strings.HasPrefix(line, "event: response") {
			b.logger.Info("Found SSE event: response")
			// Next line should be the data
			if scanner.Scan() {
				dataLine := scanner.Text()
				b.logger.Info("SSE data line: %s", dataLine)
				if strings.HasPrefix(dataLine, "data: ") {
					jsonData := strings.TrimPrefix(dataLine, "data: ")
					b.logger.Info("SSE JSON data: %s", jsonData)

					var response MCPResponse
					if err := json.Unmarshal([]byte(jsonData), &response); err == nil {
						b.logger.Debug("Successfully parsed SSE response with ID: %s", response.ID)
						// Convert to map for compatibility
						responseMap := make(map[string]interface{})
						responseMap["jsonrpc"] = response.JSONRPC
						responseMap["id"] = response.ID
						responseMap["result"] = response.Result
						responseMap["error"] = response.Error
						return responseMap, nil
					} else {
						b.logger.Debug("Failed to parse SSE response as MCPResponse: %v", err)
					}
				}
			}
		}

		// Look for SSE event: message format (alternative format from mcp-compose)
		if strings.HasPrefix(line, "event: message") {
			b.logger.Info("Found SSE event: message")
			// Next line should be the data
			if scanner.Scan() {
				dataLine := scanner.Text()
				b.logger.Info("SSE message data line: %s", dataLine)
				if strings.HasPrefix(dataLine, "data: ") {
					jsonData := strings.TrimPrefix(dataLine, "data: ")
					b.logger.Info("SSE message JSON data: %s", jsonData)

					var response map[string]interface{}
					if err := json.Unmarshal([]byte(jsonData), &response); err == nil {
						b.logger.Debug("Successfully parsed SSE message response: %v", response)
						// Check if this is our tools/list response
						if response["id"] != nil && (response["result"] != nil || response["error"] != nil) {
							return response, nil
						}
					} else {
						b.logger.Debug("Failed to parse SSE message response: %v", err)
					}
				}
			}
		}

		// Look for any other event formats
		if strings.HasPrefix(line, "event: ") && !strings.HasPrefix(line, "event: endpoint") {
			eventType := strings.TrimPrefix(line, "event: ")
			b.logger.Debug("Found SSE event: %s", eventType)
			// Next line should be the data
			if scanner.Scan() {
				dataLine := scanner.Text()
				b.logger.Debug("SSE event data line: %s", dataLine)
				if strings.HasPrefix(dataLine, "data: ") {
					jsonData := strings.TrimPrefix(dataLine, "data: ")
					b.logger.Debug("SSE event JSON data: %s", jsonData)

					var response map[string]interface{}
					if err := json.Unmarshal([]byte(jsonData), &response); err == nil {
						b.logger.Debug("Successfully parsed SSE event response: %v", response)
						// Check if this is our tools/list response
						if response["id"] != nil && (response["result"] != nil || response["error"] != nil) {
							return response, nil
						}
					} else {
						b.logger.Debug("Failed to parse SSE event response: %v", err)
					}
				}
			}
		}

		// Fallback: handle legacy data: format
		if strings.HasPrefix(line, "data: ") {
			jsonData := strings.TrimPrefix(line, "data: ")
			b.logger.Debug("Legacy SSE data: %s", jsonData)

			// Parse JSON-RPC response
			var response map[string]interface{}
			if err := json.Unmarshal([]byte(jsonData), &response); err != nil {
				b.logger.Debug("Failed to parse legacy SSE data: %v", err)
				continue // Skip invalid JSON
			}

			b.logger.Debug("Parsed legacy SSE response: %v", response)
			// Check if this is our tools/list response
			if response["id"] != nil && (response["result"] != nil || response["error"] != nil) {
				return response, nil
			}
		}
	}

	b.logger.Info("No valid MCP response found in SSE stream")
	return nil, fmt.Errorf("no valid MCP response found in SSE stream")
}

// sendHTTPRequestWithSession sends HTTP request with session management
func (b *ProtocolBridge) sendHTTPRequestWithSession(ctx context.Context, httpConn *discovery.MCPHTTPConnection, sessionID string, request map[string]interface{}) (map[string]interface{}, error) {
	// Marshal request
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", httpConn.BaseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set session headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Mcp-Session-Id", sessionID)

	// CRITICAL FIX: Add OAuth token if available from connection context
	if httpConn.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+httpConn.AuthToken)
		b.logger.Info("Added OAuth Bearer token to MCP request for %s (token length: %d)", httpConn.BaseURL, len(httpConn.AuthToken))
	} else {
		b.logger.Warning("No OAuth token available for MCP request to %s - server may reject connection", httpConn.BaseURL)
	}

	resp, err := httpConn.Client.Do(req)
	if err != nil {
		b.logger.Error("HTTP request failed to %s: %v (this may indicate network connectivity issues)", httpConn.BaseURL, err)
		return nil, fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			b.logger.Warning("Failed to close response body: %v", err)
		}
	}()

	// Log response status for debugging
	b.logger.Info("HTTP response from %s: status=%d", httpConn.BaseURL, resp.StatusCode)
	if resp.StatusCode >= 400 {
		b.logger.Error("HTTP error response from %s: status=%d (authentication may have failed)", httpConn.BaseURL, resp.StatusCode)
	}

	// Parse response
	var mcpResponse map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&mcpResponse); err != nil {
		b.logger.Error("Failed to decode JSON response from %s: %v", httpConn.BaseURL, err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return mcpResponse, nil
}

// sendHTTPNotificationWithSession sends HTTP notification with session management
func (b *ProtocolBridge) sendHTTPNotificationWithSession(ctx context.Context, httpConn *discovery.MCPHTTPConnection, sessionID string, notification map[string]interface{}) error {
	// Marshal notification
	payload, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", httpConn.BaseURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set session headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Mcp-Session-Id", sessionID)

	// Add OAuth token if available
	if httpConn.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+httpConn.AuthToken)
		b.logger.Debug("Added OAuth Bearer token to MCP notification for %s", httpConn.BaseURL)
	}

	resp, err := httpConn.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			b.logger.Warning("Failed to close response body: %v", err)
		}
	}()

	// Check response status
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// sendStreamableHTTPRequestWithSession sends streamable HTTP request with session management
func (b *ProtocolBridge) sendStreamableHTTPRequestWithSession(ctx context.Context, streamableConn *discovery.MCPStreamableHTTPConnection, sessionID string, request map[string]interface{}) (map[string]interface{}, error) {
	// Marshal request
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", streamableConn.BaseURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers including streaming headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Streaming", "true")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Mcp-Session-Id", sessionID)

	// Add OAuth token if available
	if streamableConn.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+streamableConn.AuthToken)
		b.logger.Debug("Added OAuth Bearer token to streamable MCP request for %s", streamableConn.BaseURL)
	}

	resp, err := streamableConn.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			b.logger.Warning("Failed to close response body: %v", err)
		}
	}()

	// Parse response - handle both streaming and regular responses
	var mcpResponse map[string]interface{}

	if resp.Header.Get("Transfer-Encoding") == "chunked" || resp.Header.Get("X-Streaming") == "true" {
		// Handle chunked/streaming response
		scanner := bufio.NewScanner(resp.Body)
		var jsonData []byte

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			// Try to parse each line as JSON
			var lineData map[string]interface{}
			if err := json.Unmarshal(line, &lineData); err == nil {
				// This is a complete JSON object
				mcpResponse = lineData
				break
			}

			// Otherwise, accumulate the line
			jsonData = append(jsonData, line...)
		}

		// If no complete line found, try accumulated data
		if mcpResponse == nil && len(jsonData) > 0 {
			if err := json.Unmarshal(jsonData, &mcpResponse); err != nil {
				return nil, fmt.Errorf("failed to parse accumulated JSON: %w", err)
			}
		}
	} else {
		// Handle regular JSON response
		if err := json.NewDecoder(resp.Body).Decode(&mcpResponse); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
	}

	if mcpResponse == nil {
		return nil, fmt.Errorf("no valid response received")
	}

	return mcpResponse, nil
}

// sendStreamableHTTPNotificationWithSession sends streamable HTTP notification with session management
func (b *ProtocolBridge) sendStreamableHTTPNotificationWithSession(ctx context.Context, streamableConn *discovery.MCPStreamableHTTPConnection, sessionID string, notification map[string]interface{}) error {
	// Marshal notification
	payload, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", streamableConn.BaseURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Streaming", "true")
	req.Header.Set("Mcp-Session-Id", sessionID)

	// Add OAuth token if available
	if streamableConn.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+streamableConn.AuthToken)
		b.logger.Debug("Added OAuth Bearer token to streamable MCP notification for %s", streamableConn.BaseURL)
	}

	resp, err := streamableConn.Client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make HTTP request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			b.logger.Warning("Failed to close response body: %v", err)
		}
	}()

	// Check response status
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// sendSSERequestAndWaitForResponseWithAuth sends SSE request with OAuth token and waits for response
func (b *ProtocolBridge) sendSSERequestAndWaitForResponseWithAuth(ctx context.Context, sessionURL, authToken string, request map[string]interface{}) (map[string]interface{}, error) {
	// Marshal request
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	b.logger.Info("Sending SSE request to %s: %s", sessionURL, string(payload))

	// Cap this individual round-trip while still honouring the parent ctx
	// (the parent enforces the overall discovery timeout).
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "POST", sessionURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create session request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")

	// Add OAuth token if available
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
		b.logger.Debug("Added OAuth Bearer token to SSE request for %s", sessionURL)
	}

	// Send request and read SSE stream
	resp, err := b.sseClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send session request: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			b.logger.Warning("Failed to close response body: %v", err)
		}
	}()

	b.logger.Info("SSE request response status: %d", resp.StatusCode)

	// Handle non-200 responses
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("SSE request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse SSE stream for MCP JSON-RPC response
	return b.parseSSEResponse(resp.Body)
}

// sendSSENotificationWithAuth sends SSE notification with OAuth token without waiting for response
func (b *ProtocolBridge) sendSSENotificationWithAuth(ctx context.Context, sessionURL, authToken string, notification map[string]interface{}) error {
	// Marshal notification
	payload, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	// Create HTTP request to session endpoint
	req, err := http.NewRequestWithContext(ctx, "POST", sessionURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create session request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	// Add OAuth token if available
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
		b.logger.Debug("Added OAuth Bearer token to SSE notification for %s", sessionURL)
	}

	// Send notification
	resp, err := b.sseClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send session notification: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			b.logger.Warning("Failed to close response body: %v", err)
		}
	}()

	// Check response status
	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("SSE notification failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// parseToolsFromMCPResponse parses tools from an MCP tools/list response.
func parseToolsFromMCPResponse(response map[string]interface{}) ([]Tool, error) {
	result, ok := response["result"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid MCP response format")
	}

	toolsArray, ok := result["tools"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("no tools array in response")
	}

	var tools []Tool
	for _, toolItem := range toolsArray {
		toolMap, ok := toolItem.(map[string]interface{})
		if !ok {
			continue
		}

		tool := Tool{
			Name:        getString(toolMap, "name"),
			Description: getString(toolMap, "description"),
			Parameters:  getMap(toolMap, "inputSchema"),
		}

		if tool.Name != "" {
			tools = append(tools, tool)
		}
	}

	return tools, nil
}

// Helper functions for parsing JSON
func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return ""
}

func getMap(m map[string]interface{}, key string) map[string]interface{} {
	if val, ok := m[key].(map[string]interface{}); ok {
		return val
	}
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}
