<div align="center">
  <img src="matey.png" alt="Matey Logo" width="200">
</div>

# Matey (m8e)

**Kubernetes-native MCP Server Orchestrator and AI-Powered Infrastructure Management**

> Bridge the gap between AI agents and cloud-native infrastructure with secure, scalable MCP server orchestration.

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Go Version](https://img.shields.io/badge/Go-1.24+-blue.svg)](https://golang.org/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.28+-blue.svg)](https://kubernetes.io/)
[![MCP Protocol](https://img.shields.io/badge/MCP-2025--06--18-green.svg)](https://modelcontextprotocol.io/)

## What is Matey?

Matey is a Kubernetes-native orchestrator that enables AI agents to interact with cloud infrastructure. It provides a platform for deploying, managing, and scaling Model Context Protocol (MCP) servers, with AI-driven automation capabilities.

### Project Maturity

**Matey is at v0.0.4 and under active development — roughly 40% feature-complete.** The core orchestration path is solid: defining MCP servers in `matey.yaml`, installing CRDs and controllers, and running servers on Kubernetes works today and is what you should rely on. Several advertised capabilities — the workflow engine and parts of the memory service — are partially implemented. The **Feature Status** table below tells you exactly what to trust. The ambition is real; the honesty about where it stands is too. Treat this as pre-1.0 software: great for experimentation and self-hosted use, not yet hardened for production-critical workloads.

### Feature Status

| Feature | Status | Notes |
| ------- | ------ | ----- |
| MCP server orchestration (CRDs + controllers) | Stable | Define servers in `matey.yaml`, `matey install`, `matey up/down/ps`. The core path. |
| Multi-protocol transport (HTTP, SSE) | Stable | HTTP and SSE work. WebSocket is partial — see below. |
| WebSocket transport | ⚠️ Partial | Not fully validated end-to-end. |
| Service discovery & health monitoring | Stable | Kubernetes-native discovery of MCP servers. |
| MCP proxy / gateway | Stable | Central gateway for AI-infrastructure communication. |
| CLI tooling (20+ commands) | Stable | Full lifecycle management commands. |
| OAuth 2.1 issuer + JWT | ⚠️ Experimental | Implemented (`internal/auth/oauth.go`) with unit tests; token revocation lacks integration test coverage. Not yet battle-tested. |
| RBAC | ⚠️ Partial | Controllers generate least-privilege ServiceAccounts/Roles for deployed servers, but auth middleware is not wired into the controllers as a request gate. |
| Memory service (PostgreSQL knowledge graph) | ⚠️ Partial | Real schema, CRUD, and Postgres full-text search work. Uses idempotent `CREATE TABLE IF NOT EXISTS` rather than versioned migrations. "Semantic search" is full-text search, not vector/embedding search. |
| Task scheduler (cron) | ⚠️ Partial | Cron scheduling of tasks works via the MCPTaskScheduler CRD. |
| Workflow engine | ⚠️ Experimental | Mid-migration. Create/list/delete/execute workflows work only via the k8s client path against a deployed MCPTaskScheduler; the binary fallback path is not implemented. There is an in-progress consolidation from a separate Workflow CRD onto the MCPTaskScheduler. |
| Audit logging | Working | File and SQL database backends are implemented alongside the in-memory one and selectable via `audit.storage`. The controller-manager and serve-proxy auto-register a global logger (file backend by default, configurable via `M8E_AUDIT_FILE_PATH`), and proxy auth events plus privileged MCP tool calls (`execute_bash`, `matey_up`/`down`, `apply_config`, `reload_proxy`, workflow CRUD, `execute_agent`) emit audit entries on both success and failure paths. |
| k8s diagnostics in context mentions | Working | `@problems` lists real unhealthy pods via the k8s client, `@logs:<svc>` streams real pod logs, `@def:<symbol>` resolves definitions through tree-sitter, `@memory:<query>` calls the wired memory store, and `@workflow[:name]` queries the `MCPTaskScheduler` CRD. Each path returns honest errors (with the missing dependency named) when its backend is unavailable, rather than fabricating placeholder data. Exposed as MCP tools `process_mentions` and `expand_mentions` on the matey MCP server. |
| Helm chart | New (0.1.0) | A Helm chart under `charts/matey/` is newly added — see [Helm deployment docs](docs/deployment/helm.md). |
| Web UI | Not available | Matey has no web UI. Use the CLI. |

### Key Features

- **Kubernetes-Native**: Built from the ground up with Custom Resources and Controllers
- **AI-Powered Automation**: Task scheduling with cron, plus an experimental workflow engine
- **Authentication**: OAuth 2.1 issuer, JWT tokens, and API-key auth (see Feature Status for maturity)
- **Service Discovery**: Automatic Kubernetes-native MCP server discovery and health monitoring
- **Multi-Protocol Support**: HTTP and SSE transports (WebSocket partial)
- **Memory & Context**: PostgreSQL-backed knowledge graph for persistent AI memory (partial)
- **Rich Tooling**: 20+ CLI commands for complete lifecycle management

## Getting Started

### Quick install (one command)

The fastest path. The installer auto-detects your situation: if no cluster is
reachable it installs k3s; otherwise it deploys into your current kube-context.
Either way it deploys m8e via the Helm chart and waits for it to come up.

```bash
# From a clone:
./scripts/install.sh

# Or straight from the web:
curl -sSL https://raw.githubusercontent.com/phildougherty/m8e/main/scripts/install.sh | bash
```

Common variations:

```bash
./scripts/install.sh --k3s                 # force a fresh k3s install
./scripts/install.sh --reinstall-k3s        # wipe & reinstall a broken/crash-looping k3s
./scripts/install.sh --existing             # use current kube-context, never install k3s
./scripts/install.sh --namespace mcp        # install into a different namespace
./scripts/install.sh --dry-run --k3s        # show exactly what it would do
./scripts/install.sh --uninstall            # remove the release
make quickstart                             # equivalent to ./scripts/install.sh
```

`scripts/install.sh --help` documents every flag. The manual steps below are
the same operations the script automates — use them when you want full control.

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

#### Option C: Build from Source
```bash
git clone https://github.com/phildougherty/m8e.git
cd m8e
make build
make install
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

### Registry Credentials Configuration

For accessing private container registries, configure credentials in one of these ways:

#### Option 1: In matey.yaml configuration
```yaml
registry:
  url: ghcr.io
  username: your-username
  password: your-token-or-password
```

#### Option 2: Environment Variables
```bash
# Matey-specific environment variables
export MATEY_REGISTRY_URL=ghcr.io
export MATEY_REGISTRY_USERNAME=your-username
export MATEY_REGISTRY_PASSWORD=your-token-or-password

# GitHub-specific environment variables (fallback)
export GITHUB_USERNAME=your-github-username  # or GITHUB_ACTOR
export GITHUB_TOKEN=your-github-token
# Registry URL defaults to ghcr.io for GitHub
```

**Priority Order:**
1. First tries to load from `matey.yaml` config file
2. If config missing/invalid, falls back to environment variables
3. If no credentials found, skips creating registry secret (public images only)

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
```

### Step 8: Connect Your AI Client

Copy the generated configuration to your AI client:

#### Claude Code
```bash
# Copy to your project directory
cp client-configs/.mcp.json /home/dev/myrepo/.mcp.json
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

Matey provides a platform for AI-infrastructure interaction:

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   AI Clients    │    │  Matey Platform │    │  Infrastructure │
│                 │    │                 │    │                 │
├─────────────────┤    ├─────────────────┤    ├─────────────────┤
│ • Claude Code   │◄──►│ • MCP Proxy     │◄──►│ • Kubernetes    │
│ • Gemini        │    │ • OAuth Server  │    │ • Databases     │
│ • Custom IDEs   │    │ • Memory Graph  │    │ • File Systems  │
│                 │    │ • Task Scheduler│    │ • External APIs │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

> Matey is configured and operated entirely via the CLI. There is no web UI.

### Key Components

- **MCP Proxy**: Central gateway for all AI-infrastructure communication
- **Memory Service**: PostgreSQL-backed knowledge graph for persistent context (partial — see Feature Status)
- **Task Scheduler**: Cron-based task automation, plus an experimental workflow engine
- **OAuth Server**: OAuth 2.1 issuer for authentication and authorization (experimental — see Feature Status)
- **Service Discovery**: Automatic detection and health monitoring of MCP servers

## Advanced Features

### AI-Powered Task Automation

> Status: ⚠️ experimental. Cron-scheduled tasks work via the MCPTaskScheduler CRD. The multi-step workflow engine shown below is mid-migration — workflow create/list/delete/execute currently work only against a deployed MCPTaskScheduler via the k8s client. See the Feature Status table.

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
            namespaces: ["matey", "kube-system"]
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

> Status: ⚠️ partially implemented. The PostgreSQL-backed store, CRUD operations, and full-text search work today. Schema is created idempotently rather than through versioned migrations.

- **Persistent Memory**: AI agents maintain context across sessions
- **Knowledge Graph**: Relationships between infrastructure components
- **Full-Text Search**: Postgres full-text retrieval of relevant entities and observations (not vector/embedding search)
- **Multi-Agent Coordination**: Shared context between different AI instances

### Security

> See [SECURITY.md](SECURITY.md) for details and current maturity. Some items below are aspirational — check the Feature Status table.

- **Authentication & Authorization**: OAuth 2.1 issuer, JWT, and API-key auth (experimental — see Feature Status)
- **Audit Trails**: Logging of AI actions — ⚠️ currently in-memory only, not persisted
- **Role-Based Access**: Controllers generate least-privilege ServiceAccounts for deployed servers (RBAC enforcement in controllers is partial)
- **Secret Management**: Integration with Kubernetes Secrets for API keys and credentials

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
matey memory          # Manage AI memory/context (partial — see Feature Status)
matey task-scheduler  # Manage AI tasks and workflows (workflow engine experimental)
```

## Contributing

We welcome contributions! Matey is pre-1.0 and there is plenty to do — the [Feature Status table](#feature-status) above is effectively a roadmap of what needs finishing. Open an issue or PR to get started.

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

**Ready to experiment with AI and cloud-native infrastructure?** Follow the getting started guide above. Matey is pre-1.0 software under active development — see the Feature Status table for what to rely on, and please file issues for anything that doesn't work as documented.