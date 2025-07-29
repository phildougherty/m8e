<div align="center">
  <img src="matey.png" alt="Matey Logo" width="200">
</div>

# Matey (m8e)

**Kubernetes-native MCP Server Orchestrator and AI-Powered Infrastructure Management**

> Bridge the gap between AI agents and cloud-native infrastructure with secure, scalable MCP server orchestration.

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Go Version](https://img.shields.io/badge/Go-1.24+-blue.svg)](https://golang.org/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.28+-blue.svg)](https://kubernetes.io/)
[![MCP Protocol](https://img.shields.io/badge/MCP-2024--11--05-green.svg)](https://modelcontextprotocol.io/)

## What is Matey?

Matey is a production-ready Kubernetes-native orchestrator that enables AI agents to securely interact with cloud infrastructure. It provides a comprehensive platform for deploying, managing, and scaling Model Context Protocol (MCP) servers while offering powerful AI-driven automation capabilities.

### Key Features

- **Kubernetes-Native**: Built from the ground up with Custom Resources and Controllers
- **AI-Powered Automation**: Task scheduling with AI reasoning and autonomous workflows  
- **Enterprise Security**: OAuth 2.1, JWT tokens, RBAC, and comprehensive audit logging
- **Service Discovery**: Automatic Kubernetes-native MCP server discovery and health monitoring
- **Multi-Protocol Support**: HTTP, SSE, WebSocket, and STDIO transport protocols
- **Memory & Context**: PostgreSQL-backed knowledge graph for persistent AI memory
- **Rich Tooling**: 20+ CLI commands for complete lifecycle management

## Getting Started

### Prerequisites

1. **Kubernetes Cluster** - One of the following:
   - **Cloud Provider**: EKS, GKE, AKS, or DigitalOcean Kubernetes
   - **Local Development**: k3s, minikube, or Docker Desktop
   - **Self-Managed**: kubeadm or similar

2. **kubectl** configured and connected to your cluster

### Step 1: Get a Kubernetes Cluster

#### Option A: Cloud Provider (Recommended for Production)
```bash
# AWS EKS
aws eks create-cluster --name matey-cluster --version 1.28

# Google GKE  
gcloud container clusters create matey-cluster --num-nodes=3

# Azure AKS
az aks create --resource-group myResourceGroup --name matey-cluster
```

#### Option B: Local Development with k3s
```bash
# Install k3s (lightweight Kubernetes)
curl -sfL https://get.k3s.io | sh -
sudo chmod 644 /etc/rancher/k3s/k3s.yaml
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
```

#### Option C: Docker Desktop
Enable Kubernetes in Docker Desktop settings, then verify:
```bash
kubectl cluster-info
```

### Step 2: Install Matey

#### Option A: Homebrew (macOS/Linux)
```bash
brew install phildougherty/tap/matey
```

#### Option B: Go Install
```bash
go install github.com/phildougherty/m8e/cmd/matey@latest
```

#### Option C: Download Binary
```bash
# Linux/macOS
curl -L https://github.com/phildougherty/m8e/releases/latest/download/matey-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m).tar.gz | tar xz
sudo mv matey /usr/local/bin/
```

### Step 3: Create Your Configuration

Create a `matey.yaml` file:

```yaml
version: "1"

# Container registry
registry:
    url: mcp.robotrad.io

# Proxy configuration  
proxy:
    url: mcp.robotrad.io

# Authentication
proxy_auth:
    enabled: true
    api_key: your-secure-api-key

# OAuth configuration
oauth:
    enabled: true
    issuer: https://mcp.robotrad.io
    endpoints:
        authorization: /oauth/authorize
        token: /oauth/token
    tokens:
        access_token_ttl: 1h
        refresh_token_ttl: 168h
    grant_types:
        - authorization_code
        - refresh_token
    scopes_supported:
        - mcp:tools
        - mcp:resources

# OAuth clients
oauth_clients:
    claude-code:
        client_id: claude-code
        name: Claude Code
        redirect_uris:
            - http://localhost:8080/oauth/callback
        scopes:
            - mcp:tools
            - mcp:resources
        grant_types:
            - authorization_code
            - refresh_token
        public_client: true

# Task scheduler for AI workflows
task_scheduler:
    enabled: true
    port: 8018
    database_url: postgresql://scheduler:password@postgres:5432/scheduler
    postgres_enabled: true
    mcp_proxy_url: https://mcp.robotrad.io
    mcp_proxy_api_key: your-secure-api-key
    workspace: /workspace

# Memory service for AI context
memory:
    enabled: true
    port: 3001
    database_url: postgresql://memory:password@postgres:5432/memory
    postgres_enabled: true

# MCP servers
servers:
    filesystem:
        image: mcp.robotrad.io/filesystem:latest
        env:
            HTTP_PORT: "8001"
        http_port: 8001
        protocol: http
        capabilities:
            - tools
            - resources
        volumes:
            - "/workspace:/workspace:rw"
        authentication:
            enabled: true
            required_scope: mcp:tools

    web-search:
        image: mcp.robotrad.io/searxng:latest
        env:
            HTTP_PORT: "8002"
        http_port: 8002
        protocol: http
        capabilities:
            - tools
        authentication:
            enabled: true
            required_scope: mcp:tools
```

### Step 4: Install Matey Components

Deploy the Kubernetes controllers and CRDs:

```bash
matey install
```

This installs:
- Custom Resource Definitions (CRDs)
- RBAC permissions
- Controller manager
- Service discovery components

### Step 5: Start Your Services

Launch all configured MCP servers:

```bash
matey up
```

### Step 6: Monitor Your Deployment

Check service status:
```bash
# View running services
matey ps

# Monitor resource usage  
matey top

# View detailed service information
matey inspect <service-name>
```

### Step 7: Configure Your AI Client

Generate configuration for your AI client:

```bash
# For Claude Code
matey create-config -t claude-code

# For Gemini
matey create-config -t gemini

# For custom IDE integration
matey create-config -t generic
```

### Step 8: Connect Your AI Client

Copy the generated configuration to your AI client:

#### Claude Code
```bash
# Copy to Claude Code config directory
cp .claude-code-config.json ~/.config/claude-code/config.json
```

#### Gemini/Other Clients
```bash
# Use the generated MCP configuration
cat mcp-config.json
```

The configuration includes:
- Server endpoints and authentication
- Available tools and capabilities
- OAuth client credentials
- Connection parameters

## Core Architecture

Matey provides a comprehensive platform for AI-infrastructure interaction:

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   AI Clients    │    │  Matey Platform │    │  Infrastructure │
│                 │    │                 │    │                 │
├─────────────────┤    ├─────────────────┤    ├─────────────────┤
│ • Claude Code   │◄──►│ • MCP Proxy     │◄──►│ • Kubernetes    │
│ • Gemini        │    │ • OAuth Server  │    │ • Databases     │
│ • Custom IDEs   │    │ • Memory Graph  │    │ • File Systems  │
│ • Web UIs       │    │ • Task Scheduler│    │ • External APIs │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

### Key Components

- **MCP Proxy**: Central gateway for all AI-infrastructure communication
- **Memory Service**: PostgreSQL-backed knowledge graph for persistent context
- **Task Scheduler**: AI-powered workflow automation with cron scheduling
- **OAuth Server**: Enterprise-grade authentication and authorization
- **Service Discovery**: Automatic detection and health monitoring of MCP servers

## Advanced Features

### AI-Powered Task Automation

```yaml
task_scheduler:
  workflows:
    - name: infrastructure-monitoring
      schedule: "*/15 * * * *"  # Every 15 minutes
      description: "AI-driven infrastructure health monitoring"
      steps:
        - name: collect-metrics
          tool: kubernetes_metrics
          parameters:
            namespaces: ["default", "kube-system"]
        - name: analyze-health
          tool: ai_analyze
          parameters:
            data: "{{steps.collect-metrics.output}}"
            prompt: "Analyze infrastructure health and identify issues"
        - name: auto-remediate
          tool: kubectl_apply
          condition: "{{steps.analyze-health.issues_found}}"
          parameters:
            actions: "{{steps.analyze-health.remediation_steps}}"
```

### Memory and Context Management

- **Persistent Memory**: AI agents maintain context across sessions
- **Knowledge Graph**: Relationships between infrastructure components
- **Semantic Search**: Intelligent retrieval of relevant information
- **Multi-Agent Coordination**: Shared context between different AI instances

### Security and Compliance

- **Zero-Trust Architecture**: All communication authenticated and authorized
- **Audit Trails**: Comprehensive logging of all AI actions
- **Role-Based Access**: Fine-grained permissions for different AI capabilities
- **Secret Management**: Secure handling of API keys and credentials

## CLI Reference

### Core Commands
```bash
matey install          # Install Kubernetes components
matey up              # Start all services
matey down            # Stop all services  
matey ps              # List service status
matey top             # Monitor resource usage
matey logs <service>  # View service logs
matey restart         # Restart services
```

### Configuration Management
```bash
matey create-config   # Generate configurations
matey validate        # Validate configuration files
matey reload          # Hot reload configuration
matey inspect         # Debug services
```

### Advanced Operations
```bash
matey proxy           # Start development proxy
matey memory          # Manage AI memory/context
matey scheduler       # Manage AI workflows
```

## Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

### Development Setup
```bash
git clone https://github.com/phildougherty/m8e.git
cd m8e
make dev-setup
make test
```

## License

This project is licensed under the GNU Affero General Public License v3.0 - see the [LICENSE](LICENSE) file for details.

## Support

- **Documentation**: [GitHub Wiki](https://github.com/phildougherty/m8e/wiki)
- **Issues**: [GitHub Issues](https://github.com/phildougherty/m8e/issues)
- **Discussions**: [GitHub Discussions](https://github.com/phildougherty/m8e/discussions)
- **MCP Protocol**: [modelcontextprotocol.io](https://modelcontextprotocol.io/)

---

**Ready to empower your AI with cloud-native infrastructure?** Follow the getting started guide above and join the growing community of AI-infrastructure automation users!