# Proxy Research: OpenWebUI Integration Analysis

## Executive Summary

This document analyzes the differences between the old MCP-compose proxy implementation and the new m8e (Kubernetes-native) proxy implementation to identify why OpenWebUI connections are not working. The analysis reveals that while the new implementation has most of the necessary components, there are several key architectural differences that may be preventing successful OpenWebUI integration.

## Problem Statement

OpenWebUI attempts to connect to `https://mcp.robotrad.io/openapi.json` to add it as a tool server, but the connection fails. This suggests issues with:
1. OpenAPI endpoint availability
2. Service discovery and routing
3. Tool discovery and schema generation
4. Authentication or CORS configuration

## Architectural Comparison

### Old MCP-Compose Implementation (Working)

#### 1. **Server Architecture**
- **Native Go HTTP Server**: Direct HTTP server running on port 9876
- **Static Configuration**: Servers defined in compose YAML files
- **Docker Container Management**: Direct container lifecycle management
- **File-based Discovery**: Server configuration loaded from files

#### 2. **HTTP Routing Structure**
```go
// Key endpoints that worked with OpenWebUI
GET  /openapi.json                    // Combined OpenAPI spec for all servers
GET  /{server}/openapi.json          // Server-specific OpenAPI specs
POST /{toolName}                     // Direct tool execution (FastAPI-style)
GET  /api/servers                    // Server status and discovery
GET  /api/discovery                  // MCP discovery endpoint
```

#### 3. **OpenAPI Endpoint Implementation**
```go
func (h *ProxyHandler) handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
    schema := map[string]interface{}{
        "openapi": "3.1.0",
        "info": map[string]interface{}{
            "title":       "MCP Server Functions",
            "description": "Automatically generated API from MCP Tool Schemas",
            "version":     "1.0.0",
        },
        "servers": []map[string]interface{}{
            {
                "url":         "http://192.168.86.201:9876",  // Dynamic host
                "description": "MCP Proxy Server",
            },
        },
        "paths": map[string]interface{}{},
    }
    
    // Discover tools from ALL servers and create unified spec
    for serverName := range h.Manager.config.Servers {
        tools, err := h.discoverServerTools(serverName)
        for _, tool := range tools {
            toolPath := fmt.Sprintf("/%s", tool.Name)
            paths[toolPath] = createOpenAPIToolSpec(tool)
        }
    }
}
```

#### 4. **Tool Discovery Process**
```go
func (h *ProxyHandler) discoverServerTools(serverName string) ([]openapi.ToolSpec, error) {
    // Multi-protocol support
    switch protocol {
    case "sse":
        response, err = h.sendSSEToolsRequestWithRetry(serverName, toolsRequest, timeout, attempt)
    case "http":
        response, err = h.sendHTTPToolsRequestWithRetry(serverName, toolsRequest, timeout, attempt)
    case "stdio":
        response, err = h.sendRawTCPRequestWithRetry(socatHost, socatPort, toolsRequest, timeout, attempt)
    }
    return h.parseToolsResponse(serverName, response)
}
```

#### 5. **CORS Configuration**
```go
// Comprehensive CORS for OpenWebUI
w.Header().Set("Access-Control-Allow-Origin", "*")
w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID, Mcp-Session-Id, X-Client-ID, X-MCP-Capabilities, X-Supports-Notifications")
w.Header().Set("Access-Control-Expose-Headers", "Mcp-Session-Id, Content-Type")
```

#### 6. **Direct Tool Execution**
```go
func (h *ProxyHandler) handleDirectToolCall(w http.ResponseWriter, r *http.Request, toolName string) {
    // Parse arguments from POST body
    var arguments map[string]interface{}
    json.NewDecoder(r.Body).Decode(&arguments)
    
    // Find server that has this tool
    serverName, found := h.findServerForTool(toolName)
    
    // Create MCP tools/call request
    mcpRequest := map[string]interface{}{
        "jsonrpc": "2.0",
        "id":      h.getNextRequestID(),
        "method":  "tools/call",
        "params": map[string]interface{}{
            "name":      toolName,
            "arguments": arguments,
        },
    }
    
    // Execute and return processed result
    result := h.executeToolCall(serverName, mcpRequest)
    cleanResult := h.processMCPContent(result)
    json.NewEncoder(w).Encode(cleanResult)
}
```

### New m8e Implementation (Current)

#### 1. **Server Architecture**
- **Kubernetes-Native**: MCPProxy CRD with controller-based deployment
- **Service Discovery**: Dynamic discovery via Kubernetes APIs
- **Container Management**: Kubernetes deployments and services
- **CRD-based Configuration**: Declarative configuration via Kubernetes resources

#### 2. **HTTP Routing Structure**
```go
// Current endpoint structure
GET  /openapi.json                    // ✅ Global OpenAPI spec
GET  /{server}/openapi.json          // ✅ Server-specific specs  
POST /{toolName}                     // ✅ Direct tool calls
GET  /api/servers                    // ✅ Server status
GET  /api/discovery                  // ✅ Discovery endpoint
```

#### 3. **Service Discovery Process**
```go
// Current implementation uses Kubernetes service discovery
func (h *ProxyHandler) GetDiscoveredServers() (map[string]ServerInfo, error) {
    // Query Kubernetes API for MCP servers
    servers, err := h.kubeClient.CoreV1().Services(h.namespace).List(context.TODO(), metav1.ListOptions{
        LabelSelector: "mcp.matey.ai/server=true",
    })
    
    // Build server map from Kubernetes services
    serverMap := make(map[string]ServerInfo)
    for _, service := range servers.Items {
        serverMap[service.Name] = ServerInfo{
            Name:     service.Name,
            Endpoint: fmt.Sprintf("http://%s:%d", service.Spec.ClusterIP, service.Spec.Ports[0].Port),
            Protocol: service.Labels["mcp.matey.ai/protocol"],
        }
    }
    return serverMap, nil
}
```

#### 4. **OpenAPI Generation**
```go
// Enhanced OpenAPI with MCP annotations
func (h *ProxyHandler) handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
    schema := &openapi.OpenAPISpec{
        OpenAPI: "3.1.0",
        Info: openapi.Info{
            Title:       "MCP Server Functions",
            Description: "Automatically generated API from MCP Tool Schemas",
            Version:     "1.0.0",
        },
        Servers: []openapi.Server{
            {
                URL:         fmt.Sprintf("http://%s", r.Host), // Dynamic host
                Description: "MCP Proxy Server",
            },
        },
        Paths:      make(map[string]openapi.PathItem),
        Components: openapi.Components{
            SecuritySchemes: map[string]openapi.SecurityScheme{
                "HTTPBearer": {
                    Type:   "http",
                    Scheme: "bearer",
                },
            },
        },
    }
    
    // Get discovered servers from Kubernetes
    servers, err := h.GetDiscoveredServers()
    if err != nil {
        http.Error(w, "Failed to discover servers", http.StatusInternalServerError)
        return
    }
    
    // Discover tools from all servers
    for serverName, serverInfo := range servers {
        tools, err := h.discoverServerTools(serverName, serverInfo)
        if err != nil {
            continue
        }
        
        for _, tool := range tools {
            h.addToolToSpec(schema, tool, serverName)
        }
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(schema)
}
```

## Key Differences and Potential Issues

### 1. **Service Discovery Mechanism**

**Old MCP-compose:**
- Static configuration from compose files
- Direct container management
- Immediate availability of server endpoints

**New m8e:**
- Kubernetes service discovery
- Relies on proper service labeling and selection
- **Potential Issue**: Services may not be properly labeled or discoverable

### 2. **Server URL Generation**

**Old MCP-compose:**
```go
// Hard-coded but worked for specific deployment
"url": "http://192.168.86.201:9876"
```

**New m8e:**
```go
// Dynamic but may not resolve correctly
"url": fmt.Sprintf("http://%s", r.Host)
```

**Potential Issue**: Host header may not be what OpenWebUI expects

### 3. **Tool Discovery Process**

**Old MCP-compose:**
- Direct connection to containers
- Known server configurations
- Immediate tool discovery

**New m8e:**
- Kubernetes service-to-service communication
- **Potential Issue**: Network policies or service mesh may block communication

### 4. **OpenAPI Endpoint Availability**

**Analysis of current implementation:**
- ✅ `/openapi.json` endpoint exists
- ✅ Tool discovery mechanism implemented
- ✅ OpenAPI schema generation with MCP annotations
- ❓ **Unknown**: Whether Kubernetes service discovery is working correctly

## Diagnostic Analysis

### 1. **Service Discovery Issues**
The most likely cause is that the Kubernetes service discovery is not finding the MCP servers properly. This could be due to:

```go
// Check if services are properly labeled
kubectl get services -l mcp.matey.ai/server=true

// Check service endpoints
kubectl get endpoints

// Check MCPProxy resources
kubectl get mcpproxy
```

### 2. **Network Connectivity**
The proxy may not be able to reach the MCP servers due to:
- Network policies blocking traffic
- Service mesh configuration
- ClusterIP vs NodePort service types
- DNS resolution issues

### 3. **Tool Discovery Failures**
Even if services are discovered, tool discovery may fail due to:
- Incorrect protocol configuration
- Authentication issues between services
- Timeout configurations
- MCP server startup timing

## Recommendations for Fixing OpenWebUI Integration

### 1. **Verify Service Discovery**
```bash
# Check if MCP servers are properly labeled
kubectl get services -l mcp.matey.ai/server=true -o wide

# Check MCPProxy resources
kubectl get mcpproxy -o yaml

# Test proxy connectivity
kubectl port-forward svc/matey-proxy 9876:80
curl http://localhost:9876/openapi.json
```

### 2. **Debug Tool Discovery**
```go
// Add debug logging to tool discovery
func (h *ProxyHandler) discoverServerTools(serverName string, serverInfo ServerInfo) ([]openapi.ToolSpec, error) {
    log.Printf("Discovering tools for server: %s, endpoint: %s", serverName, serverInfo.Endpoint)
    
    // Test basic connectivity
    resp, err := http.Get(fmt.Sprintf("%s/health", serverInfo.Endpoint))
    if err != nil {
        log.Printf("Health check failed for %s: %v", serverName, err)
        return nil, err
    }
    
    // Continue with tool discovery...
}
```

### 3. **Fix Server URL Generation**
```go
// Use explicit server configuration instead of r.Host
func (h *ProxyHandler) getServerURL(r *http.Request) string {
    // Check if we have explicit proxy configuration
    if h.config.ExternalURL != "" {
        return h.config.ExternalURL
    }
    
    // Use NodePort service URL if available
    if h.config.NodePortURL != "" {
        return h.config.NodePortURL
    }
    
    // Fall back to request host
    return fmt.Sprintf("http://%s", r.Host)
}
```

### 4. **Add Comprehensive Health Checks**
```go
// Add endpoint to verify proxy health
func (h *ProxyHandler) handleHealthCheck(w http.ResponseWriter, r *http.Request) {
    health := map[string]interface{}{
        "status": "ok",
        "timestamp": time.Now().Unix(),
        "services": make(map[string]interface{}),
    }
    
    servers, err := h.GetDiscoveredServers()
    if err != nil {
        health["status"] = "error"
        health["error"] = err.Error()
    } else {
        for name, server := range servers {
            health["services"].(map[string]interface{})[name] = map[string]interface{}{
                "endpoint": server.Endpoint,
                "protocol": server.Protocol,
                "reachable": h.testServerConnectivity(server.Endpoint),
            }
        }
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(health)
}
```

### 5. **Implement Fallback Service Discovery**
```go
// Add fallback to static configuration if Kubernetes discovery fails
func (h *ProxyHandler) GetDiscoveredServers() (map[string]ServerInfo, error) {
    // Try Kubernetes service discovery first
    servers, err := h.discoverFromKubernetes()
    if err == nil && len(servers) > 0 {
        return servers, nil
    }
    
    // Fall back to static configuration
    log.Printf("Kubernetes discovery failed, falling back to static config: %v", err)
    return h.discoverFromStaticConfig()
}
```

## Conclusion

The new m8e implementation has all the necessary components for OpenWebUI integration, but the Kubernetes-native architecture introduces complexity that may be preventing successful connections. The most likely issues are:

1. **Service Discovery**: Kubernetes service discovery may not be finding MCP servers
2. **Network Connectivity**: Services may not be able to communicate within the cluster
3. **URL Generation**: Server URLs in OpenAPI specs may not be accessible from outside the cluster
4. **Timing**: Tool discovery may be failing due to server startup timing

The solution requires debugging the service discovery mechanism, ensuring proper network connectivity, and potentially adding fallback mechanisms to handle edge cases in the Kubernetes environment.

## Next Steps

1. **Debug Service Discovery**: Verify that MCP servers are properly labeled and discoverable
2. **Test Network Connectivity**: Ensure proxy can reach MCP servers
3. **Verify OpenAPI Endpoint**: Test that `/openapi.json` returns valid schemas
4. **Add Health Checks**: Implement comprehensive health checking for diagnostics
5. **Consider Hybrid Approach**: Allow both Kubernetes and static configuration for flexibility

The architectural foundation is solid, but the implementation needs refinement to handle the complexities of Kubernetes-native service discovery and networking.