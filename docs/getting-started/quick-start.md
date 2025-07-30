# Quick Start Guide

Get up and running with Matey in under 5 minutes!

## Prerequisites

- Kubernetes cluster with `kubectl` configured
- Matey installed (see [Installation Guide](installation.md))

## Step 1: Install Matey Components

```bash
# Install CRDs and controllers
matey install

# Verify installation
kubectl get crd | grep matey
```

Expected output:
```
mcpmemories.matey.io          2024-01-15T10:30:00Z
mcpproxies.matey.io           2024-01-15T10:30:00Z
mcpservers.matey.io           2024-01-15T10:30:00Z
mcptaskschedulers.matey.io    2024-01-15T10:30:00Z
workflows.matey.io            2024-01-15T10:30:00Z
```

## Step 2: Create Your First Configuration

Create a `matey.yaml` file:

```yaml
version: "3.8"

services:
  # Simple filesystem MCP server
  filesystem:
    image: mcp/filesystem-server:latest
    protocol: stdio
    command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/workspace"]
    volumes:
      - ./workspace:/workspace
    replicas: 1
    
  # Web search MCP server
  web-search:
    image: mcp/web-search:latest
    protocol: http
    port: 8080
    environment:
      - SEARCH_API_KEY=${SEARCH_API_KEY}
    replicas: 2
    
  # Memory service
  memory:
    image: postgres:15
    port: 5432
    environment:
      - POSTGRES_DB=matey_memory
      - POSTGRES_USER=matey
      - POSTGRES_PASSWORD=${DB_PASSWORD}
    volumes:
      - matey-memory:/var/lib/postgresql/data

# AI provider configuration
ai_providers:
  - name: openai
    type: openai
    api_key: ${OPENAI_API_KEY}
    model: gpt-4
    fallback: claude
    
  - name: claude
    type: claude
    api_key: ${CLAUDE_API_KEY}
    model: claude-3-sonnet-20240229

# Basic authentication
auth:
  enabled: true
  type: jwt
  secret: ${JWT_SECRET}

# Service discovery
discovery:
  enabled: true
  cross_namespace: false
  health_check_interval: 30s
```

## Step 3: Set Up Environment Variables

Create a `.env` file:

```bash
# Database
DB_PASSWORD=secure_password_here

# AI Providers
OPENAI_API_KEY=sk-your-openai-key
CLAUDE_API_KEY=sk-ant-your-claude-key

# Authentication
JWT_SECRET=your-super-secret-jwt-key

# Search (optional)
SEARCH_API_KEY=your-search-api-key
```

## Step 4: Validate Configuration

```bash
# Check configuration syntax and semantics
matey validate

# Check with verbose output
matey validate --verbose
```

## Step 5: Deploy Your Services

```bash
# Start all services
matey up

# Check status
matey ps
```

Expected output:
```
NAME         STATUS    PROTOCOL  REPLICAS  READY  AGE
filesystem   Running   stdio     1/1       1      2m
web-search   Running   http      2/2       2      2m
memory       Running   tcp       1/1       1      2m
```

## Step 6: Test Your Setup

```bash
# View logs
matey logs filesystem

# Test AI integration
matey chat

# Inspect a service
matey inspect filesystem
```

## Step 7: Interact with Your MCP Servers

### Using the CLI

```bash
# List available tools
matey toolbox list

# Execute a tool
matey toolbox exec filesystem read_file --path /workspace/README.md

# Start a workflow
matey workflow start --name data-processing
```

### Using the Proxy

```bash
# Start the HTTP proxy
matey proxy --port 8080

# Access via HTTP
curl http://localhost:8080/filesystem/tools
```

## Common Next Steps

### Scale Your Services

```bash
# Scale web-search to 5 replicas
matey scale web-search=5

# Auto-scale based on CPU
matey autoscale web-search --min=2 --max=10 --cpu-percent=80
```

### Add More Services

Edit your `matey.yaml` to add more MCP servers:

```yaml
services:
  # ... existing services ...
  
  database-tools:
    image: mcp/database-tools:latest
    protocol: http
    port: 8081
    environment:
      - DB_URL=postgresql://user:pass@db:5432/mydb
    depends_on:
      - memory
      
  code-analysis:
    image: mcp/code-analysis:latest
    protocol: sse
    port: 8082
    volumes:
      - ./src:/code:ro
```

### Set Up Monitoring

```bash
# Enable metrics
matey configure --metrics-enabled=true

# View metrics
matey top

# Set up alerts
matey configure --alerts-webhook=https://hooks.slack.com/your-webhook
```

## Troubleshooting

### Services Not Starting

```bash
# Check detailed status
matey ps --all

# View logs with timestamps
matey logs --timestamps --since=1h

# Describe Kubernetes resources
matey inspect filesystem --kubernetes
```

### Configuration Issues

```bash
# Validate with detailed output
matey validate --verbose

# Check specific service
matey validate --service filesystem

# Test connectivity
matey ping filesystem
```

### Permission Problems

```bash
# Check RBAC
kubectl auth can-i create pods --as=system:serviceaccount:default:matey

# Verify CRDs
kubectl get crd mcpservers.matey.io -o yaml
```

## What's Next?

- **[Configuration Reference](../configuration/matey-yaml.md)** - Learn about all configuration options
- **[CLI Commands](../cli/commands-overview.md)** - Explore all available commands
- **[Features Guide](../features/)** - Deep dive into specific features
- **[Deployment Guide](../deployment/)** - Production deployment strategies
- **[Development Guide](../development/)** - Contributing to Matey

## Example Projects

Check out these example configurations:

- **[Basic Setup](../configuration/examples/basic.yaml)** - Simple MCP server setup
- **[AI-Powered Workflow](../configuration/examples/ai-workflow.yaml)** - Complex AI integration
- **[Multi-Environment](../configuration/examples/multi-env.yaml)** - Development, staging, production
- **[Enterprise Setup](../configuration/examples/enterprise.yaml)** - Full enterprise features

## Need Help?

- **[Troubleshooting Guide](../troubleshooting.md)** - Common issues and solutions
- **[GitHub Issues](https://github.com/phildougherty/m8e/issues)** - Report bugs or request features
- **[Community Discussions](https://github.com/phildougherty/m8e/discussions)** - Ask questions and share experiences