#!/bin/bash
# Documentation generation script for Matey
# Generates comprehensive API documentation, OpenAPI specs, and client examples

set -euo pipefail

# Configuration
DOCS_DIR="docs"
API_DOCS_DIR="${DOCS_DIR}/api"
GENERATED_DIR="${API_DOCS_DIR}/generated"
EXAMPLES_DIR="${API_DOCS_DIR}/examples"
SCHEMAS_DIR="${API_DOCS_DIR}/schemas"
POSTMAN_DIR="${API_DOCS_DIR}/postman"

# Server configuration (can be overridden by environment)
MATEY_HOST="${MATEY_HOST:-localhost}"
MATEY_PORT="${MATEY_PORT:-9876}"
MATEY_API_KEY="${MATEY_API_KEY:-}"
BASE_URL="http://${MATEY_HOST}:${MATEY_PORT}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Setup directories
setup_directories() {
    log_info "Setting up documentation directories..."
    
    mkdir -p "${GENERATED_DIR}"
    mkdir -p "${EXAMPLES_DIR}"
    mkdir -p "${SCHEMAS_DIR}"
    mkdir -p "${POSTMAN_DIR}"
    
    log_success "Documentation directories created"
}

# Check if Matey server is running
check_server() {
    log_info "Checking if Matey server is running at ${BASE_URL}..."
    
    if curl -s --max-time 5 "${BASE_URL}/health" >/dev/null 2>&1; then
        log_success "Matey server is running"
        return 0
    else
        log_warning "Matey server is not running at ${BASE_URL}"
        log_info "Start the server with: make run"
        return 1
    fi
}

# Fetch OpenAPI specifications
fetch_openapi_specs() {
    log_info "Fetching OpenAPI specifications..."
    
    local auth_header=""
    if [ -n "$MATEY_API_KEY" ]; then
        auth_header="-H \"Authorization: Bearer $MATEY_API_KEY\""
    fi
    
    # Fetch combined OpenAPI spec
    if eval curl -s $auth_header "${BASE_URL}/openapi.json" > "${SCHEMAS_DIR}/combined-openapi.json"; then
        log_success "Combined OpenAPI spec saved: ${SCHEMAS_DIR}/combined-openapi.json"
    else
        log_error "Failed to fetch combined OpenAPI spec"
        return 1
    fi
    
    # Fetch server discovery to get individual server specs
    if eval curl -s $auth_header "${BASE_URL}/api/discovery" > "${SCHEMAS_DIR}/discovery.json"; then
        log_success "Discovery information saved: ${SCHEMAS_DIR}/discovery.json"
        
        # Parse discovery response to get server names
        if command_exists jq; then
            local servers=$(jq -r '.discovered_servers[].name' "${SCHEMAS_DIR}/discovery.json" 2>/dev/null || echo "")
            
            if [ -n "$servers" ]; then
                log_info "Fetching individual server OpenAPI specs..."
                
                while IFS= read -r server; do
                    if [ -n "$server" ]; then
                        log_info "Fetching OpenAPI spec for server: $server"
                        if eval curl -s $auth_header "${BASE_URL}/${server}/openapi.json" > "${SCHEMAS_DIR}/${server}-openapi.json"; then
                            log_success "OpenAPI spec for $server saved: ${SCHEMAS_DIR}/${server}-openapi.json"
                        else
                            log_warning "Failed to fetch OpenAPI spec for server: $server"
                        fi
                    fi
                done <<< "$servers"
            else
                log_warning "No servers found in discovery response"
            fi
        else
            log_warning "jq not available, skipping individual server specs"
        fi
    else
        log_error "Failed to fetch discovery information"
    fi
}

# Generate client examples
generate_examples() {
    log_info "Generating client examples..."
    
    # Create curl examples
    cat > "${EXAMPLES_DIR}/curl-examples.sh" << 'EOF'
#!/bin/bash
# Matey API Examples using curl

# Configuration
MATEY_URL="${MATEY_URL:-http://localhost:9876}"
API_KEY="${MATEY_API_KEY:-your-api-key-here}"

# Helper function for authenticated requests
matey_curl() {
    curl -H "Authorization: Bearer $API_KEY" \
         -H "Content-Type: application/json" \
         "$@"
}

echo "Matey API Examples"
echo "=================="
echo ""

echo "1. Health Check"
echo "---------------"
curl -s "${MATEY_URL}/health" | jq .
echo ""

echo "2. Discovery"
echo "------------"
matey_curl -s "${MATEY_URL}/api/discovery" | jq .
echo ""

echo "3. Server Status"
echo "---------------"
matey_curl -s "${MATEY_URL}/api/servers" | jq .
echo ""

echo "4. Combined OpenAPI Spec"
echo "-----------------------"
matey_curl -s "${MATEY_URL}/openapi.json" | jq '.info'
echo ""

echo "5. Example Tool Execution (Memory Server)"
echo "-----------------------------------------"
matey_curl -X POST \
    -d '{"entity": "test", "attribute": "demo", "value": "example"}' \
    "${MATEY_URL}/memory/store_memory" | jq .
echo ""

echo "6. Direct MCP Communication"
echo "---------------------------"
matey_curl -X POST \
    -d '{
        "jsonrpc": "2.0",
        "id": "example-1",
        "method": "tools/list"
    }' \
    "${MATEY_URL}/memory" | jq .
echo ""
EOF

    chmod +x "${EXAMPLES_DIR}/curl-examples.sh"
    log_success "Curl examples generated: ${EXAMPLES_DIR}/curl-examples.sh"
    
    # Create Python example
    cat > "${EXAMPLES_DIR}/python-client.py" << 'EOF'
#!/usr/bin/env python3
"""
Matey API Client Example in Python
"""

import requests
import json
import os
from typing import Dict, Any, Optional

class MateyClient:
    """Simple Matey API client"""
    
    def __init__(self, base_url: str = "http://localhost:9876", api_key: Optional[str] = None):
        self.base_url = base_url.rstrip('/')
        self.api_key = api_key or os.getenv('MATEY_API_KEY')
        self.session = requests.Session()
        
        if self.api_key:
            self.session.headers.update({
                'Authorization': f'Bearer {self.api_key}',
                'Content-Type': 'application/json'
            })
    
    def health(self) -> Dict[str, Any]:
        """Check server health"""
        response = self.session.get(f"{self.base_url}/health")
        response.raise_for_status()
        return response.json()
    
    def discovery(self) -> Dict[str, Any]:
        """Get server discovery information"""
        response = self.session.get(f"{self.base_url}/api/discovery")
        response.raise_for_status()
        return response.json()
    
    def server_status(self) -> Dict[str, Any]:
        """Get server status information"""
        response = self.session.get(f"{self.base_url}/api/servers")
        response.raise_for_status()
        return response.json()
    
    def execute_tool(self, server: str, tool: str, params: Dict[str, Any]) -> Dict[str, Any]:
        """Execute a tool on a specific server"""
        url = f"{self.base_url}/{server}/{tool}"
        response = self.session.post(url, json=params)
        response.raise_for_status()
        return response.json()
    
    def mcp_request(self, server: str, method: str, params: Optional[Dict[str, Any]] = None) -> Dict[str, Any]:
        """Send a direct MCP request to a server"""
        url = f"{self.base_url}/{server}"
        payload = {
            "jsonrpc": "2.0",
            "id": "python-client-1",
            "method": method
        }
        if params:
            payload["params"] = params
        
        response = self.session.post(url, json=payload)
        response.raise_for_status()
        return response.json()
    
    def get_openapi_spec(self, server: Optional[str] = None) -> Dict[str, Any]:
        """Get OpenAPI specification"""
        if server:
            url = f"{self.base_url}/{server}/openapi.json"
        else:
            url = f"{self.base_url}/openapi.json"
        
        response = self.session.get(url)
        response.raise_for_status()
        return response.json()

def main():
    """Example usage"""
    # Initialize client
    client = MateyClient()
    
    try:
        # Check health
        print("Health Status:")
        health = client.health()
        print(json.dumps(health, indent=2))
        print()
        
        # Get discovery information
        print("Discovery Information:")
        discovery = client.discovery()
        print(json.dumps(discovery, indent=2))
        print()
        
        # Get server status
        print("Server Status:")
        status = client.server_status()
        print(json.dumps(status, indent=2))
        print()
        
        # Example tool execution (if memory server is available)
        try:
            print("Example Tool Execution:")
            result = client.execute_tool("memory", "store_memory", {
                "entity": "demo",
                "attribute": "language",
                "value": "python"
            })
            print(json.dumps(result, indent=2))
            print()
        except requests.exceptions.RequestException as e:
            print(f"Tool execution failed: {e}")
            print()
        
        # Get OpenAPI spec
        print("OpenAPI Specification Info:")
        spec = client.get_openapi_spec()
        print(f"Title: {spec.get('info', {}).get('title')}")
        print(f"Version: {spec.get('info', {}).get('version')}")
        print(f"Paths: {len(spec.get('paths', {}))}")
        
    except requests.exceptions.RequestException as e:
        print(f"API request failed: {e}")
    except Exception as e:
        print(f"Error: {e}")

if __name__ == "__main__":
    main()
EOF

    log_success "Python client example generated: ${EXAMPLES_DIR}/python-client.py"
    
    # Create JavaScript/Node.js example
    cat > "${EXAMPLES_DIR}/nodejs-client.js" << 'EOF'
#!/usr/bin/env node
/**
 * Matey API Client Example in Node.js
 */

const axios = require('axios');

class MateyClient {
    constructor(baseUrl = 'http://localhost:9876', apiKey = null) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.apiKey = apiKey || process.env.MATEY_API_KEY;
        
        this.client = axios.create({
            baseURL: this.baseUrl,
            headers: {
                'Content-Type': 'application/json',
                ...(this.apiKey && { 'Authorization': `Bearer ${this.apiKey}` })
            }
        });
    }
    
    async health() {
        const response = await this.client.get('/health');
        return response.data;
    }
    
    async discovery() {
        const response = await this.client.get('/api/discovery');
        return response.data;
    }
    
    async serverStatus() {
        const response = await this.client.get('/api/servers');
        return response.data;
    }
    
    async executeTool(server, tool, params) {
        const response = await this.client.post(`/${server}/${tool}`, params);
        return response.data;
    }
    
    async mcpRequest(server, method, params = null) {
        const payload = {
            jsonrpc: '2.0',
            id: 'nodejs-client-1',
            method: method
        };
        
        if (params) {
            payload.params = params;
        }
        
        const response = await this.client.post(`/${server}`, payload);
        return response.data;
    }
    
    async getOpenAPISpec(server = null) {
        const url = server ? `/${server}/openapi.json` : '/openapi.json';
        const response = await this.client.get(url);
        return response.data;
    }
}

async function main() {
    const client = new MateyClient();
    
    try {
        // Check health
        console.log('Health Status:');
        const health = await client.health();
        console.log(JSON.stringify(health, null, 2));
        console.log();
        
        // Get discovery information
        console.log('Discovery Information:');
        const discovery = await client.discovery();
        console.log(JSON.stringify(discovery, null, 2));
        console.log();
        
        // Get server status
        console.log('Server Status:');
        const status = await client.serverStatus();
        console.log(JSON.stringify(status, null, 2));
        console.log();
        
        // Example tool execution
        try {
            console.log('Example Tool Execution:');
            const result = await client.executeTool('memory', 'store_memory', {
                entity: 'demo',
                attribute: 'language',
                value: 'javascript'
            });
            console.log(JSON.stringify(result, null, 2));
            console.log();
        } catch (error) {
            console.log(`Tool execution failed: ${error.message}`);
            console.log();
        }
        
        // Get OpenAPI spec info
        console.log('OpenAPI Specification Info:');
        const spec = await client.getOpenAPISpec();
        console.log(`Title: ${spec.info?.title}`);
        console.log(`Version: ${spec.info?.version}`);
        console.log(`Paths: ${Object.keys(spec.paths || {}).length}`);
        
    } catch (error) {
        console.error(`Error: ${error.message}`);
    }
}

if (require.main === module) {
    main();
}

module.exports = MateyClient;
EOF

    log_success "Node.js client example generated: ${EXAMPLES_DIR}/nodejs-client.js"
}

# Generate Postman collection
generate_postman_collection() {
    log_info "Generating Postman collection..."
    
    cat > "${POSTMAN_DIR}/matey-api-collection.json" << EOF
{
    "info": {
        "name": "Matey API Collection",
        "description": "Comprehensive collection for testing Matey MCP Proxy APIs",
        "version": "1.0.0",
        "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
    },
    "auth": {
        "type": "bearer",
        "bearer": [
            {
                "key": "token",
                "value": "{{api_key}}",
                "type": "string"
            }
        ]
    },
    "variable": [
        {
            "key": "base_url",
            "value": "http://localhost:9876",
            "type": "string"
        },
        {
            "key": "api_key",
            "value": "your-api-key-here",
            "type": "string"
        }
    ],
    "item": [
        {
            "name": "Health & Discovery",
            "item": [
                {
                    "name": "Health Check",
                    "request": {
                        "method": "GET",
                        "header": [],
                        "url": {
                            "raw": "{{base_url}}/health",
                            "host": ["{{base_url}}"],
                            "path": ["health"]
                        }
                    }
                },
                {
                    "name": "Discovery",
                    "request": {
                        "method": "GET",
                        "header": [],
                        "url": {
                            "raw": "{{base_url}}/api/discovery",
                            "host": ["{{base_url}}"],
                            "path": ["api", "discovery"]
                        }
                    }
                },
                {
                    "name": "Server Status",
                    "request": {
                        "method": "GET",
                        "header": [],
                        "url": {
                            "raw": "{{base_url}}/api/servers",
                            "host": ["{{base_url}}"],
                            "path": ["api", "servers"]
                        }
                    }
                }
            ]
        },
        {
            "name": "OpenAPI Specs",
            "item": [
                {
                    "name": "Combined OpenAPI Spec",
                    "request": {
                        "method": "GET",
                        "header": [],
                        "url": {
                            "raw": "{{base_url}}/openapi.json",
                            "host": ["{{base_url}}"],
                            "path": ["openapi.json"]
                        }
                    }
                },
                {
                    "name": "Memory Server OpenAPI Spec",
                    "request": {
                        "method": "GET",
                        "header": [],
                        "url": {
                            "raw": "{{base_url}}/memory/openapi.json",
                            "host": ["{{base_url}}"],
                            "path": ["memory", "openapi.json"]
                        }
                    }
                }
            ]
        },
        {
            "name": "Tool Execution",
            "item": [
                {
                    "name": "Store Memory",
                    "request": {
                        "method": "POST",
                        "header": [
                            {
                                "key": "Content-Type",
                                "value": "application/json"
                            }
                        ],
                        "body": {
                            "mode": "raw",
                            "raw": "{\n  \"entity\": \"user\",\n  \"attribute\": \"name\",\n  \"value\": \"John Doe\"\n}"
                        },
                        "url": {
                            "raw": "{{base_url}}/memory/store_memory",
                            "host": ["{{base_url}}"],
                            "path": ["memory", "store_memory"]
                        }
                    }
                },
                {
                    "name": "Search Memory",
                    "request": {
                        "method": "POST",
                        "header": [
                            {
                                "key": "Content-Type",
                                "value": "application/json"
                            }
                        ],
                        "body": {
                            "mode": "raw",
                            "raw": "{\n  \"query\": \"user\",\n  \"limit\": 10\n}"
                        },
                        "url": {
                            "raw": "{{base_url}}/memory/search_memory",
                            "host": ["{{base_url}}"],
                            "path": ["memory", "search_memory"]
                        }
                    }
                }
            ]
        },
        {
            "name": "Direct MCP Communication",
            "item": [
                {
                    "name": "List Tools",
                    "request": {
                        "method": "POST",
                        "header": [
                            {
                                "key": "Content-Type",
                                "value": "application/json"
                            }
                        ],
                        "body": {
                            "mode": "raw",
                            "raw": "{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"postman-1\",\n  \"method\": \"tools/list\"\n}"
                        },
                        "url": {
                            "raw": "{{base_url}}/memory",
                            "host": ["{{base_url}}"],
                            "path": ["memory"]
                        }
                    }
                },
                {
                    "name": "Call Tool",
                    "request": {
                        "method": "POST",
                        "header": [
                            {
                                "key": "Content-Type",
                                "value": "application/json"
                            }
                        ],
                        "body": {
                            "mode": "raw",
                            "raw": "{\n  \"jsonrpc\": \"2.0\",\n  \"id\": \"postman-2\",\n  \"method\": \"tools/call\",\n  \"params\": {\n    \"name\": \"store_memory\",\n    \"arguments\": {\n      \"entity\": \"test\",\n      \"attribute\": \"demo\",\n      \"value\": \"postman\"\n    }\n  }\n}"
                        },
                        "url": {
                            "raw": "{{base_url}}/memory",
                            "host": ["{{base_url}}"],
                            "path": ["memory"]
                        }
                    }
                }
            ]
        },
        {
            "name": "Administration",
            "item": [
                {
                    "name": "Reload Configuration",
                    "request": {
                        "method": "POST",
                        "header": [],
                        "url": {
                            "raw": "{{base_url}}/api/reload",
                            "host": ["{{base_url}}"],
                            "path": ["api", "reload"]
                        }
                    }
                },
                {
                    "name": "Connection Status",
                    "request": {
                        "method": "GET",
                        "header": [],
                        "url": {
                            "raw": "{{base_url}}/api/connections",
                            "host": ["{{base_url}}"],
                            "path": ["api", "connections"]
                        }
                    }
                }
            ]
        }
    ]
}
EOF
    
    log_success "Postman collection generated: ${POSTMAN_DIR}/matey-api-collection.json"
}

# Generate HTML documentation
generate_html_docs() {
    log_info "Generating HTML documentation..."
    
    cat > "${GENERATED_DIR}/index.html" << 'EOF'
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Matey API Documentation</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
            line-height: 1.6;
            color: #333;
            max-width: 1200px;
            margin: 0 auto;
            padding: 20px;
            background-color: #f8f9fa;
        }
        .container {
            background: white;
            padding: 30px;
            border-radius: 8px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
        }
        h1 {
            color: #2c3e50;
            border-bottom: 3px solid #3498db;
            padding-bottom: 10px;
        }
        h2 {
            color: #34495e;
            margin-top: 30px;
        }
        .grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
            gap: 20px;
            margin: 20px 0;
        }
        .card {
            background: #f8f9fa;
            padding: 20px;
            border-radius: 6px;
            border-left: 4px solid #3498db;
        }
        .card h3 {
            margin-top: 0;
            color: #2c3e50;
        }
        .card a {
            color: #3498db;
            text-decoration: none;
            font-weight: 500;
        }
        .card a:hover {
            text-decoration: underline;
        }
        .code-block {
            background: #2c3e50;
            color: #ecf0f1;
            padding: 15px;
            border-radius: 5px;
            overflow-x: auto;
            margin: 10px 0;
        }
        .endpoint {
            background: #e8f5e8;
            padding: 10px;
            border-radius: 4px;
            margin: 5px 0;
            font-family: monospace;
        }
        .method {
            background: #007bff;
            color: white;
            padding: 2px 8px;
            border-radius: 3px;
            font-size: 0.8em;
            margin-right: 8px;
        }
        .method.post { background: #28a745; }
        .method.get { background: #007bff; }
        .method.put { background: #ffc107; color: #333; }
        .method.delete { background: #dc3545; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Matey API Documentation</h1>
        <p>Welcome to the Matey API documentation. Matey is a Kubernetes-native MCP (Model Context Protocol) server orchestrator that provides a unified HTTP proxy interface for interacting with multiple MCP servers.</p>
        
        <h2>Quick Start</h2>
        <div class="code-block">
# Check if Matey is running
curl http://localhost:9876/health

# Discover available servers and tools
curl -H "Authorization: Bearer your-api-key" \
  http://localhost:9876/api/discovery

# Get OpenAPI specification
curl -H "Authorization: Bearer your-api-key" \
  http://localhost:9876/openapi.json
        </div>

        <h2>Key Features</h2>
        <div class="grid">
            <div class="card">
                <h3>🔍 Service Discovery</h3>
                <p>Automatically discovers and manages multiple MCP servers with real-time health monitoring.</p>
            </div>
            <div class="card">
                <h3>📋 OpenAPI Integration</h3>
                <p>Comprehensive OpenAPI 3.1.0 specifications for all MCP tools with automatic generation.</p>
            </div>
            <div class="card">
                <h3>🔐 Authentication</h3>
                <p>Multiple authentication methods including Bearer tokens and OAuth 2.1 with PKCE.</p>
            </div>
            <div class="card">
                <h3>🛠️ Tool Execution</h3>
                <p>Execute MCP tools through simple HTTP endpoints with full error handling.</p>
            </div>
        </div>

        <h2>Core Endpoints</h2>
        
        <div class="endpoint">
            <span class="method get">GET</span> /health
            <br><small>Check server health and status</small>
        </div>
        
        <div class="endpoint">
            <span class="method get">GET</span> /api/discovery
            <br><small>Discover available MCP servers and their capabilities</small>
        </div>
        
        <div class="endpoint">
            <span class="method get">GET</span> /openapi.json
            <br><small>Get combined OpenAPI specification for all servers</small>
        </div>
        
        <div class="endpoint">
            <span class="method get">GET</span> /{server}/openapi.json
            <br><small>Get OpenAPI specification for a specific server</small>
        </div>
        
        <div class="endpoint">
            <span class="method post">POST</span> /{server}/{tool}
            <br><small>Execute a specific tool on a server</small>
        </div>
        
        <div class="endpoint">
            <span class="method post">POST</span> /{server}
            <br><small>Send direct MCP protocol messages to a server</small>
        </div>

        <h2>Documentation Resources</h2>
        <div class="grid">
            <div class="card">
                <h3>📚 API Reference</h3>
                <p>Complete API documentation with examples and response schemas.</p>
                <a href="../README.md">View API Reference</a>
            </div>
            <div class="card">
                <h3>🧪 Examples</h3>
                <p>Client examples in multiple programming languages.</p>
                <a href="../examples/">Browse Examples</a>
            </div>
            <div class="card">
                <h3>📨 Postman Collection</h3>
                <p>Ready-to-use Postman collection for API testing.</p>
                <a href="../postman/matey-api-collection.json">Download Collection</a>
            </div>
            <div class="card">
                <h3>🔧 OpenAPI Schemas</h3>
                <p>Machine-readable API specifications for code generation.</p>
                <a href="../schemas/">View Schemas</a>
            </div>
        </div>

        <h2>Getting Started</h2>
        <ol>
            <li><strong>Start Matey:</strong> <code>make run</code></li>
            <li><strong>Check Health:</strong> <code>curl http://localhost:9876/health</code></li>
            <li><strong>Discover Services:</strong> <code>curl -H "Authorization: Bearer your-key" http://localhost:9876/api/discovery</code></li>
            <li><strong>Execute Tools:</strong> Use the API endpoints to interact with MCP servers</li>
        </ol>

        <h2>Integration Examples</h2>
        <div class="code-block">
# Python
import requests
client = requests.Session()
client.headers.update({'Authorization': 'Bearer your-api-key'})
result = client.post('http://localhost:9876/memory/store_memory', 
                    json={'entity': 'user', 'attribute': 'name', 'value': 'John'})

# JavaScript
const response = await fetch('http://localhost:9876/memory/store_memory', {
    method: 'POST',
    headers: {
        'Authorization': 'Bearer your-api-key',
        'Content-Type': 'application/json'
    },
    body: JSON.stringify({entity: 'user', attribute: 'name', value: 'John'})
});

# curl
curl -X POST \
    -H "Authorization: Bearer your-api-key" \
    -H "Content-Type: application/json" \
    -d '{"entity": "user", "attribute": "name", "value": "John"}' \
    http://localhost:9876/memory/store_memory
        </div>

        <footer style="margin-top: 40px; padding-top: 20px; border-top: 1px solid #ddd; text-align: center; color: #666;">
            <p>Matey - Kubernetes-native MCP Server Orchestrator</p>
            <p><a href="https://github.com/phildougherty/m8e">GitHub Repository</a> | <a href="https://docs.matey.dev">Documentation</a></p>
        </footer>
    </div>
</body>
</html>
EOF
    
    log_success "HTML documentation generated: ${GENERATED_DIR}/index.html"
}

# Generate package.json for Node.js example
generate_package_json() {
    cat > "${EXAMPLES_DIR}/package.json" << 'EOF'
{
  "name": "matey-client-examples",
  "version": "1.0.0",
  "description": "Matey API client examples",
  "main": "nodejs-client.js",
  "scripts": {
    "start": "node nodejs-client.js",
    "test": "node nodejs-client.js"
  },
  "dependencies": {
    "axios": "^1.6.0"
  },
  "keywords": ["matey", "mcp", "api", "client"],
  "author": "Matey Team",
  "license": "AGPL-3.0"
}
EOF
    
    log_success "Package.json generated for Node.js example"
}

# Main function
main() {
    echo "=========================================="
    echo "      MATEY DOCUMENTATION GENERATOR"
    echo "=========================================="
    echo ""
    
    case "${1:-all}" in
        "all")
            setup_directories
            if check_server; then
                fetch_openapi_specs
            else
                log_warning "Skipping OpenAPI spec fetching - server not running"
            fi
            generate_examples
            generate_postman_collection
            generate_html_docs
            generate_package_json
            ;;
        "specs")
            setup_directories
            check_server && fetch_openapi_specs
            ;;
        "examples")
            setup_directories
            generate_examples
            generate_package_json
            ;;
        "postman")
            setup_directories
            generate_postman_collection
            ;;
        "html")
            setup_directories
            generate_html_docs
            ;;
        "clean")
            log_info "Cleaning generated documentation..."
            rm -rf "${GENERATED_DIR}" "${EXAMPLES_DIR}" "${SCHEMAS_DIR}" "${POSTMAN_DIR}"
            log_success "Generated documentation cleaned"
            ;;
        *)
            echo "Usage: $0 [all|specs|examples|postman|html|clean]"
            echo "  all      - Generate all documentation (default)"
            echo "  specs    - Fetch OpenAPI specifications only"
            echo "  examples - Generate client examples only"
            echo "  postman  - Generate Postman collection only"
            echo "  html     - Generate HTML documentation only"
            echo "  clean    - Clean generated documentation"
            echo ""
            echo "Environment variables:"
            echo "  MATEY_HOST     - Matey server host (default: localhost)"
            echo "  MATEY_PORT     - Matey server port (default: 9876)"
            echo "  MATEY_API_KEY  - API key for authentication"
            exit 1
            ;;
    esac
    
    echo ""
    echo "=========================================="
    echo "     DOCUMENTATION GENERATION COMPLETE"
    echo "=========================================="
    echo ""
    echo "Generated files:"
    [ -d "$GENERATED_DIR" ] && echo "  📄 HTML docs: $GENERATED_DIR/"
    [ -d "$EXAMPLES_DIR" ] && echo "  💻 Examples: $EXAMPLES_DIR/"
    [ -d "$SCHEMAS_DIR" ] && echo "  📋 OpenAPI specs: $SCHEMAS_DIR/"
    [ -d "$POSTMAN_DIR" ] && echo "  📨 Postman collection: $POSTMAN_DIR/"
    echo ""
    echo "Next steps:"
    echo "  1. Review the generated documentation"
    echo "  2. Test the API using the examples"
    echo "  3. Import the Postman collection for testing"
    echo "  4. Use OpenAPI specs for client generation"
}

# Run main function
main "$@"