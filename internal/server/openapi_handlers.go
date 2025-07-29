package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func (h *ProxyHandler) handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	// OpenAPI specs should be publicly accessible for service discovery

	// Create FastAPI-compatible OpenAPI spec
	schema := map[string]interface{}{
		"openapi": "3.1.0",
		"info": map[string]interface{}{
			"title":       "MCP Server Functions",
			"description": "Automatically generated API from MCP Tool Schemas",
			"version":     "1.0.0",
		},
		"servers": []map[string]interface{}{
			{
				"url":         h.Config.GetProxyURL(),
				"description": "MCP Proxy Server",
			},
		},
		"paths": map[string]interface{}{},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"HTTPBearer": map[string]interface{}{
					"type":   "http",
					"scheme": "bearer",
				},
			},
			"schemas": map[string]interface{}{
				"ValidationError": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"detail": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
							},
						},
					},
				},
			},
		},
		"security": []map[string][]string{
			{"HTTPBearer": {}},
		},
	}

	paths := make(map[string]interface{})

	// Discover tools from each server using K8s-native discovery and create endpoints
	allConnections := h.ConnectionManager.GetAllConnections()
	for serverName := range allConnections {
		tools, err := h.discoverServerTools(serverName)
		if err != nil {
			h.logger.Warning("Failed to discover tools for %s: %v", serverName, err)

			continue
		}

		for _, tool := range tools {
			toolPath := fmt.Sprintf("/%s", tool.Name)
			// Create FastAPI-style endpoint
			paths[toolPath] = map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     cases.Title(language.English).String(strings.ReplaceAll(tool.Name, "_", " ")),
					"description": tool.Description,
					"operationId": tool.Name,
					"tags":        []string{"default"},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": tool.Parameters,
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Successful Response",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
									},
								},
							},
						},
						"422": map[string]interface{}{
							"description": "Validation Error",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ValidationError",
									},
								},
							},
						},
					},
					"security": []map[string][]string{
						{"HTTPBearer": {}},
					},
				},
			}
		}
	}

	schema["paths"] = paths

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(schema); err != nil {
		h.logger.Error("Failed to encode OpenAPI spec: %v", err)
	}
}

func (h *ProxyHandler) handleServerOpenAPISpec(w http.ResponseWriter, _ *http.Request, serverName string) {
	h.logger.Info("Generating OpenAPI spec for server: %s", serverName)

	// Create server-specific OpenAPI spec
	schema := map[string]interface{}{
		"openapi": "3.1.0",
		"info": map[string]interface{}{
			"title":       fmt.Sprintf("%s MCP Server", cases.Title(language.English).String(serverName)),
			"description": fmt.Sprintf("%s MCP Server\n\n- [back to tool list](/docs)", serverName),
			"version":     "1.0.0",
		},
		"servers": []map[string]interface{}{
			{
				"url":         h.Config.GetProxyURL(),
				"description": serverName + " MCP Server\n\n- [back to tool list](/docs)"},
		},
		"paths": map[string]interface{}{},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"HTTPBearer": map[string]interface{}{
					"type":   "http",
					"scheme": "bearer",
				},
			},
			"schemas": map[string]interface{}{
				"ValidationError": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"detail": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "object",
							},
						},
					},
				},
			},
		},
		"security": []map[string][]string{{"HTTPBearer": {}}},
	}

	paths := make(map[string]interface{})

	// Get tools for this specific server only
	tools, err := h.discoverServerTools(serverName)
	if err != nil {
		h.logger.Warning("Failed to discover tools for %s: %v", serverName, err)
		// Return empty spec but still valid
		schema["paths"] = paths
	} else {
		h.logger.Info("Discovered %d tools for server %s", len(tools), serverName)
		// Add tools for this server
		for _, tool := range tools {
			toolPath := fmt.Sprintf("/%s", tool.Name)
			paths[toolPath] = map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     cases.Title(language.English).String(strings.ReplaceAll(tool.Name, "_", " ")),
					"description": tool.Description,
					"operationId": tool.Name,
					"tags":        []string{"default"},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": tool.Parameters,
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Successful Response",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
									},
								},
							},
						},
						"422": map[string]interface{}{
							"description": "Validation Error",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ValidationError",
									},
								},
							},
						},
					},
					"security": []map[string][]string{{"HTTPBearer": {}}},
				},
			}
		}
		schema["paths"] = paths
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(schema); err != nil {
		h.logger.Error("Failed to encode server OpenAPI spec for %s: %v", serverName, err)
		h.corsError(w, "Internal server error", http.StatusInternalServerError)
	} else {
		h.logger.Info("Successfully generated OpenAPI spec for server %s with %d paths", serverName, len(paths))
	}
}

func (h *ProxyHandler) handleServerDocs(w http.ResponseWriter, _ *http.Request, serverName string) {
	h.logger.Debug("Serving docs for server: %s", serverName)

	docsHTML := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>%s MCP Server</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; margin: 40px; line-height: 1.6; }
        .container { max-width: 800px; margin: 0 auto; }
        h1 { color: #2c3e50; border-bottom: 2px solid #3498db; padding-bottom: 10px; }
        .link-box { background: #f8f9fa; padding: 20px; border-radius: 8px; margin: 20px 0; }
        .link-box a { color: #2980b9; text-decoration: none; font-weight: 500; }
        .link-box a:hover { text-decoration: underline; }
        .back-link { margin-top: 30px; }
    </style>
</head>
<body>
    <div class="container">
        <h1>%s MCP Server</h1>
        <p>This is the documentation page for the <strong>%s</strong> MCP server.</p>
        <div class="link-box">
            <h3>OpenAPI Specification</h3>
            <p><a href="/%s/openapi.json">View OpenAPI Spec (JSON)</a></p>
            <p>Use this URL in OpenWebUI tools configuration:</p>
            <code>%s/%s/openapi.json</code>
        </div>
        <div class="back-link">
            <p><a href="/">← Back to main proxy dashboard</a></p>
        </div>
    </div>
</body>
</html>`, serverName, serverName, serverName, serverName, h.Config.GetProxyURL(), serverName)

	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte(docsHTML))
}


func (h *ProxyHandler) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	var bodyBuilder strings.Builder
	bodyBuilder.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>MCP Compose Proxy (HTTP/SSE Mode)</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif; margin: 0; background-color: #f0f2f5; color: #333; padding: 20px;}
        .container { max-width: 1200px; margin: 0 auto; }
        header { background-color: #2c3e50; color: white; padding: 20px 25px; border-radius: 8px; margin-bottom: 25px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        header h1 { margin: 0; font-size: 2em; font-weight: 600;}
        header p { margin: 5px 0 0; font-size: 1em; opacity: 0.85; }
        h2 { color: #34495e; border-bottom: 2px solid #dfe6e9; padding-bottom: 10px; margin-top: 35px; font-size: 1.6em;}
        .server-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(320px, 1fr)); gap: 20px; }
        .server { background-color: #ffffff; padding: 20px; border: 1px solid #dde1e6; border-radius: 6px; box-shadow: 0 4px 8px rgba(0,0,0,0.07); transition: transform 0.2s ease-in-out, box-shadow 0.2s ease-in-out; }
        .server:hover { transform: translateY(-3px); box-shadow: 0 6px 12px rgba(0,0,0,0.1); }
        .server h3 { margin-top: 0; color: #2c3e50; }
        .server a { text-decoration: none; color: #3498db; font-weight: 500; margin-right: 15px; }
        .server a:hover { color: #2575ae; text-decoration: underline; }
        .status, .connection-status { font-size: 0.95em; margin-top: 5px; line-height: 1.5; }
        .status strong, .connection-status strong { color: #4a5568; }
        .status-dot { display: inline-block; width: 10px; height: 10px; border-radius: 50%; margin-right: 7px; }
        .running .status-dot { background-color: #2ecc71; }
        .stopped .status-dot { background-color: #e74c3c; }
        .unknown .status-dot { background-color: #f39c12; }
        .api-links { margin-top: 40px; padding: 25px; background-color: #ffffff; border-radius: 8px; box-shadow: 0 4px 8px rgba(0,0,0,0.05); }
        .api-links ul { list-style-type: none; padding: 0; }
        .api-links li { margin-bottom: 12px; }
        .api-links a { text-decoration: none; color: #2980b9; font-weight: 500; }
        .api-links a:hover { text-decoration: underline; color: #1c5a7d; }
        .openwebui-config { background: #e8f5e8; padding: 15px; border-radius: 6px; margin-top: 20px; }
        .openwebui-config code { background: #fff; padding: 2px 6px; border-radius: 3px; color: #c7254e; }
    </style>
</head>
<body>
    <div class="container">
    <header>
        <h1>MCP Compose Proxy</h1>
        <p>Orchestrating Model Context Protocol Servers with HTTP/SSE Transport</p>
    </header>
    <h2>Available MCP Servers:</h2>
    <div class="server-list">`)

	// Use K8s-native discovery for server status
	allConnections := h.ConnectionManager.GetAllConnections()
	serverNames := make([]string, 0, len(allConnections))
	for name := range allConnections {
		serverNames = append(serverNames, name)
	}

	for _, name := range serverNames {
		// In K8s-native mode, we check connection status
		statusClass := "unknown"
		statusDotClass := "unknown"
		
		if conn, err := h.ConnectionManager.GetConnection(name); err == nil && conn.Status == "connected" {
			statusClass = "running"
			statusDotClass = "running"
		} else {
			statusClass = "stopped"
			statusDotClass = "stopped"
		}

		var displayedConnectionStatus string
		if conn, err := h.ConnectionManager.GetConnection(name); err == nil {
			if conn.Status == "connected" {
				displayedConnectionStatus = "● Connected via K8s Discovery"
			} else {
				displayedConnectionStatus = fmt.Sprintf("○ %s", conn.Status)
			}
		} else {
			displayedConnectionStatus = "○ No Active Connection via Proxy"
		}

		bodyBuilder.WriteString(fmt.Sprintf(`
    <div class="server %s">
        <h3>%s</h3>
        <div class="status"><span class="status-dot %s"></span><strong>Container/Process Status:</strong> %s</div>
        <div class="connection-status"><strong>Proxy HTTP Connection:</strong> %s</div>
        <div style="margin-top: 15px;">
            <a href="/%s/docs">📖 Docs</a>
            <a href="/%s/openapi.json">📋 OpenAPI Spec</a>
            <a href="/%s">🔧 Direct Access</a>
        </div>
        <div class="openwebui-config">
            <strong>For OpenWebUI:</strong><br>
            <code>%s/%s/openapi.json</code>
        </div>
    </div>`, statusClass, name, statusDotClass, statusClass, displayedConnectionStatus, name, name, name, h.Config.GetProxyURL(), name))
	}

	bodyBuilder.WriteString(`</div>
    <div class="api-links">
        <h2>Diagnostic API Endpoints:</h2>
        <ul>
            <li><a href="/api/servers">/api/servers</a> &ndash; List servers and their proxy connection status.</li>
            <li><a href="/api/status">/api/status</a> &ndash; Overall proxy health and server summary.</li>
            <li><a href="/api/discovery">/api/discovery</a> &ndash; MCP discovery endpoint.</li>
            <li><a href="/api/connections">/api/connections</a> &ndash; Detailed status of active HTTP connections.</li>
            <li><a href="/openapi.json">/openapi.json</a> &ndash; Combined OpenAPI specification.</li>
        </ul>
    </div>
    <div style="margin-top: 40px; padding: 25px; background-color: #fff3cd; border-radius: 8px;">
        <h3>🎯 OpenWebUI Integration</h3>
        <p>Add each server individually to OpenWebUI as separate tools servers:</p>
        <ul>`)

	for _, name := range serverNames {
		bodyBuilder.WriteString(fmt.Sprintf(`
            <li><strong>%s:</strong> <code>%s/%s/openapi.json</code></li>`, name, h.Config.GetProxyURL(), name))
	}

	bodyBuilder.WriteString(`
        </ul>
        <p><strong>API Key:</strong> <code>myapikey</code></p>
    </div>
    </div>
</body>
</html>`)

	_, err := w.Write([]byte(bodyBuilder.String()))
	if err != nil {
		h.logger.Error("Failed to write index HTML response: %v", err)
	}
}
