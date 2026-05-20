# Matey API Documentation

## Overview

Matey provides comprehensive HTTP-based APIs for interacting with MCP (Model Context Protocol) servers through a unified proxy interface. The APIs are fully documented using OpenAPI 3.1.0 specifications and support authentication, discovery, and tool execution.

## Base URLs

- **Development**: `http://localhost:9876`
- **Production**: Configure based on your deployment

## Authentication

Matey supports multiple authentication methods:

### Bearer Token Authentication
```http
Authorization: Bearer <your-api-key>
```

### OAuth 2.1 (Enterprise)
- **Authorization Code Flow** with PKCE
- **Client Credentials Flow**
- **Device Authorization Flow**

## API Endpoints

### 1. OpenAPI Specifications

#### Combined OpenAPI Spec
```
GET /openapi.json
```
Returns the combined OpenAPI specification for all configured MCP servers.

#### Server-Specific OpenAPI Spec
```
GET /{server-name}/openapi.json
```
Returns the OpenAPI specification for a specific MCP server.

**Example:**
```bash
curl -H "Authorization: Bearer your-api-key" \
  http://localhost:9876/memory/openapi.json
```

### 2. Discovery Endpoints

#### Health Check
```
GET /health
```
Returns the overall health status of the proxy and connected servers.

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2024-12-21T10:30:00Z",
  "version": "0.0.4",
  "servers": {
    "total": 5,
    "healthy": 4,
    "unhealthy": 1
  }
}
```

#### Service Discovery
```
GET /api/discovery
```
Returns detailed information about all discovered MCP servers and their capabilities.

**Response:**
```json
{
  "discovered_servers": [
    {
      "name": "memory",
      "protocol": "http",
      "capabilities": {
        "tools": true,
        "resources": true,
        "prompts": false
      },
      "tools": [
        {
          "name": "store_memory",
          "description": "Store information in the knowledge graph",
          "annotations": {
            "destructiveHint": false,
            "idempotentHint": false,
            "readOnlyHint": false
          }
        }
      ]
    }
  ],
  "connection_status": {
    "memory": {
      "connected": true,
      "lastSeen": "2024-12-21T10:29:45Z",
      "errorCount": 0
    }
  }
}
```

#### Server Status
```
GET /api/servers
```
Returns the status of all configured servers including container/process status and connection health.

### 3. Tool Execution

#### Execute Tool on Specific Server
```
POST /{server-name}/{tool-name}
Content-Type: application/json
Authorization: Bearer <api-key>
```

**Request Body:**
```json
{
  "param1": "value1",
  "param2": "value2"
}
```

**Response:**
```json
{
  "content": [
    {
      "type": "text",
      "text": "Tool execution result"
    }
  ],
  "isError": false,
  "_meta": {
    "progressToken": "abc123",
    "annotations": {
      "readOnlyHint": true
    }
  }
}
```

#### Direct MCP Communication
```
POST /{server-name}
Content-Type: application/json
Authorization: Bearer <api-key>
```

**MCP Protocol Request:**
```json
{
  "jsonrpc": "2.0",
  "id": "request-1",
  "method": "tools/call",
  "params": {
    "name": "tool_name",
    "arguments": {
      "param": "value"
    }
  }
}
```

### 4. Administrative Endpoints

#### Reload Configuration
```
POST /api/reload
Authorization: Bearer <api-key>
```
Hot-reloads the proxy configuration without restarting services.

#### Connection Management
```
GET /api/connections
```
Returns detailed information about active connections to MCP servers.

## Error Handling

All APIs use standard HTTP status codes and provide detailed error information:

### Error Response Format
```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Invalid parameter format",
    "details": {
      "field": "param1",
      "expected": "string",
      "received": "number"
    }
  },
  "request_id": "req_123456789"
}
```

### Common Error Codes

| HTTP Status | Error Code | Description |
|-------------|------------|-------------|
| 400 | `VALIDATION_ERROR` | Invalid request parameters |
| 401 | `UNAUTHORIZED` | Missing or invalid authentication |
| 403 | `FORBIDDEN` | Insufficient permissions |
| 404 | `NOT_FOUND` | Server or tool not found |
| 422 | `MCP_ERROR` | MCP protocol error |
| 500 | `INTERNAL_ERROR` | Server-side error |
| 502 | `BACKEND_ERROR` | MCP server communication error |
| 503 | `UNAVAILABLE` | Service temporarily unavailable |

## Rate Limiting

API endpoints are subject to rate limiting to ensure fair usage:

- **Default Rate**: 100 requests per minute per API key
- **Burst Limit**: 20 requests per second
- **Tool Execution**: 10 concurrent executions per server

Rate limit headers are included in responses:
```http
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1640995200
```

## Pagination

List endpoints support pagination using query parameters:

```
GET /api/servers?page=1&limit=20&sort=name&order=asc
```

**Parameters:**
- `page`: Page number (default: 1)
- `limit`: Items per page (default: 20, max: 100)
- `sort`: Sort field (name, status, created_at)
- `order`: Sort order (asc, desc)

## Webhooks

Matey supports webhooks for real-time notifications:

### Webhook Events
- `server.connected`: MCP server connection established
- `server.disconnected`: MCP server connection lost
- `tool.executed`: Tool execution completed
- `health.changed`: Server health status changed

### Webhook Configuration
```yaml
webhooks:
  - url: "https://your-webhook-endpoint.com/matey"
    events: ["server.connected", "server.disconnected"]
    secret: "webhook-secret-key"
    headers:
      X-Custom-Header: "value"
```

## SDK and Client Libraries

### Official SDKs
- **Go**: `github.com/phildougherty/m8e/pkg/client`
- **Python**: `pip install matey-client` (coming soon)
- **Node.js**: `npm install @matey/client` (coming soon)

### Example Usage (Go)
```go
package main

import (
    "github.com/phildougherty/m8e/pkg/client"
)

func main() {
    client := client.New("http://localhost:9876", "your-api-key")
    
    // Discover servers
    servers, err := client.Discovery()
    if err != nil {
        panic(err)
    }
    
    // Execute tool
    result, err := client.ExecuteTool("memory", "store_memory", map[string]interface{}{
        "entity": "user",
        "attribute": "name",
        "value": "John Doe",
    })
    if err != nil {
        panic(err)
    }
    
    fmt.Println("Tool result:", result)
}
```

### Example Usage (curl)
```bash
# Discover available servers and tools
curl -H "Authorization: Bearer your-api-key" \
  http://localhost:9876/api/discovery

# Execute a memory storage tool
curl -X POST \
  -H "Authorization: Bearer your-api-key" \
  -H "Content-Type: application/json" \
  -d '{"entity": "user", "attribute": "name", "value": "John Doe"}' \
  http://localhost:9876/memory/store_memory

# Get server-specific OpenAPI spec
curl -H "Authorization: Bearer your-api-key" \
  http://localhost:9876/memory/openapi.json
```

## Integration Examples

### OpenWebUI Integration
1. **Add Tool Server**: Use the server-specific OpenAPI URL
   ```
   http://localhost:9876/{server-name}/openapi.json
   ```

2. **Configure Authentication**: Use Bearer token authentication
   ```
   Authorization: Bearer your-api-key
   ```

3. **Test Connection**: Use the built-in test functionality

### Postman Collection
A comprehensive Postman collection is available at:
```
https://github.com/phildougherty/m8e/tree/main/docs/api/postman
```

### OpenAPI Code Generation
Generate client code using OpenAPI generators:
```bash
# Install OpenAPI Generator
npm install -g @openapitools/openapi-generator-cli

# Generate Python client
openapi-generator-cli generate \
  -i http://localhost:9876/openapi.json \
  -g python \
  -o ./generated/python-client

# Generate TypeScript client
openapi-generator-cli generate \
  -i http://localhost:9876/openapi.json \
  -g typescript-axios \
  -o ./generated/typescript-client
```

## Security Considerations

### Production Deployment
1. **Use HTTPS**: Always deploy with TLS encryption
2. **API Key Rotation**: Regularly rotate API keys
3. **Network Security**: Use firewalls and network policies
4. **Audit Logging**: Enable comprehensive audit logging
5. **Rate Limiting**: Configure appropriate rate limits

### Authentication Best Practices
1. **Strong API Keys**: Use cryptographically secure random keys (32+ characters)
2. **Scope Limitation**: Use OAuth scopes to limit access
3. **Token Expiration**: Set appropriate token lifetimes
4. **Secure Storage**: Store credentials securely (env vars, secret managers)

## Monitoring and Observability

### Metrics
Matey exposes Prometheus metrics at `/metrics`:
- Request rate and latency
- Error rates by endpoint
- Connection health status
- Tool execution metrics

### Health Checks
Multiple health check endpoints:
- `/health`: Overall health
- `/health/ready`: Readiness probe
- `/health/live`: Liveness probe

### Logging
Structured JSON logging with configurable levels:
```yaml
logging:
  level: info
  format: json
  outputs:
    - console
    - file: /var/log/matey/api.log
```

## Support and Resources

- **Documentation**: [https://docs.matey.dev](https://docs.matey.dev)
- **API Reference**: [https://api.matey.dev](https://api.matey.dev)
- **GitHub Issues**: [https://github.com/phildougherty/m8e/issues](https://github.com/phildougherty/m8e/issues)
- **Community Discord**: [https://discord.gg/matey](https://discord.gg/matey)
- **Support Email**: support@matey.dev

## Changelog

### v0.0.4 (Current)
- Enhanced OpenAPI documentation
- Improved error handling
- Added webhook support
- OAuth 2.1 implementation

### v0.0.3
- Basic OpenAPI support
- Health check endpoints
- Authentication middleware

For complete changelog, see [CHANGELOG.md](../../CHANGELOG.md).