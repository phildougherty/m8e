<div align="center">
  <img src="matey.png" alt="Matey Logo" width="200">
</div>

# Matey (m8e)

**Kubernetes-native MCP Server Orchestrator**

> Bridge the gap between Docker Compose simplicity and Kubernetes power for Model Context Protocol (MCP) applications.

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
[![Go Version](https://img.shields.io/badge/Go-1.24+-blue.svg)](https://golang.org/)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.28+-blue.svg)](https://kubernetes.io/)
[![MCP Protocol](https://img.shields.io/badge/MCP-2024--11--05-green.svg)](https://modelcontextprotocol.io/)

## 🚀 What is Matey?

Matey is a production-ready Kubernetes-native orchestrator for Model Context Protocol (MCP) servers. It combines the familiar simplicity of Docker Compose configuration with the power and scalability of Kubernetes, making it easy to deploy, manage, and scale MCP applications in cloud-native environments.

### Key Benefits

- **🎯 Familiar Configuration**: Use Docker Compose-style YAML configurations that automatically translate to Kubernetes resources
- **☸️ Kubernetes Native**: Built from the ground up for Kubernetes with Custom Resources and Controllers
- **🔄 MCP Protocol Support**: Full support for MCP 2024-11-05 specification with HTTP, SSE, WebSocket, and STDIO transports
- **🤖 AI Integration**: Built-in support for OpenAI, Claude, Ollama, and OpenRouter with fallback chains
- **🔐 Enterprise Security**: OAuth 2.1, JWT, RBAC, and comprehensive audit logging
- **📊 Rich CLI**: 20+ commands for complete lifecycle management
- **🔍 Service Discovery**: Automatic Kubernetes-native service discovery and health checking
- **🛠️ Developer Friendly**: Hot reload, configuration validation, and extensive testing

## 📦 Quick Start

### Prerequisites

- Kubernetes cluster (1.28+)
- `kubectl` configured
- Go 1.24+ (for building from source)

### Installation

#### Option 1: Install from GitHub Releases

```bash
# Download the latest release
curl -L https://github.com/phildougherty/m8e/releases/latest/download/matey-linux-amd64.tar.gz | tar xz
sudo mv matey /usr/local/bin/
```

#### Option 2: Build from Source

```bash
git clone https://github.com/phildougherty/m8e.git
cd m8e
make build
sudo make install
```

#### Option 3: Using Helm

```bash
helm repo add matey https://phildougherty.github.io/m8e
helm install matey matey/matey
```

### 5-Minute Setup

1. **Install CRDs and Controllers**:
   ```bash
   matey install
   ```

2. **Create a basic configuration**:
   ```bash
   matey create-config --template quickstart
   ```

3. **Start your MCP servers**:
   ```bash
   matey up
   ```

4. **Check status**:
   ```bash
   matey ps
   ```

### Basic Configuration Example

Create a `matey.yaml` file:

```yaml
version: "3.8"
services:
  filesystem-server:
    image: mcp/filesystem-server:latest
    protocol: stdio
    command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    
  web-search:
    image: mcp/web-search:latest
    protocol: http
    port: 8080
    environment:
      - SEARCH_API_KEY=${SEARCH_API_KEY}
    
  memory-service:
    image: postgres:15
    environment:
      - POSTGRES_DB=matey_memory
      - POSTGRES_USER=matey
      - POSTGRES_PASSWORD=${DB_PASSWORD}
    volumes:
      - matey-data:/var/lib/postgresql/data

ai_providers:
  - name: openai
    type: openai
    api_key: ${OPENAI_API_KEY}
    model: gpt-4

auth:
  enabled: true
  oauth:
    providers:
      - name: github
        client_id: ${GITHUB_CLIENT_ID}
        client_secret: ${GITHUB_CLIENT_SECRET}
```

## 🛠️ Core Features

### MCP Protocol Support
- **Full MCP 2024-11-05 specification compliance**
- **Multiple transports**: HTTP, Server-Sent Events, WebSocket, STDIO
- **Capabilities**: Resources, Tools, Prompts, Sampling, Logging, Roots
- **Progress tracking** and change notifications
- **Resource subscriptions** and lifecycle management

### Kubernetes Integration
- **6 Custom Resource Definitions** for complete lifecycle management
- **Automatic service discovery** and health checking
- **Native Kubernetes networking** and security
- **Horizontal scaling** with built-in load balancing
- **Cross-namespace** service discovery support

### AI & Workflow Engine
- **Multi-provider AI support**: OpenAI, Claude, Ollama, OpenRouter
- **Interactive chat interface**: `matey chat`
- **Cron-based task scheduling** with workflow orchestration
- **PostgreSQL-backed persistence** for memory and state
- **Tool ecosystem integration**

### Security & Compliance
- **OAuth 2.1 with PKCE** for secure authentication
- **JWT token management** with configurable TTL
- **Role-based access control (RBAC)** with fine-grained permissions
- **Comprehensive audit logging** for compliance
- **TLS/SSL support** for secure communication

## 📚 Documentation

- **[Getting Started Guide](docs/getting-started/)** - Complete setup walkthrough
- **[Configuration Reference](docs/configuration/)** - All configuration options
- **[CLI Commands](docs/cli/)** - Complete command reference
- **[Deployment Guide](docs/deployment/)** - Kubernetes deployment options
- **[Development Guide](docs/development/)** - Contributing and development setup

## 🔧 CLI Commands

### Core Orchestration
```bash
matey up           # Start all services
matey down         # Stop all services
matey ps           # List service status
matey logs         # View service logs
matey restart      # Restart services
```

### Configuration Management
```bash
matey create-config    # Create configuration files
matey validate         # Validate configuration
matey reload          # Hot reload configuration
```

### Advanced Features
```bash
matey chat            # Interactive AI chat
matey proxy           # Start HTTP proxy
matey workflow        # Manage workflows
matey inspect         # Debug services
```

## 🏗️ Architecture

Matey follows a cloud-native architecture:

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   matey CLI     │    │  Kubernetes     │    │  MCP Servers    │
│                 │    │  Controllers    │    │                 │
├─────────────────┤    ├─────────────────┤    ├─────────────────┤
│ • Commands      │───▶│ • MCPServer     │───▶│ • HTTP/SSE      │
│ • Configuration │    │ • MCPMemory     │    │ • WebSocket     │
│ • Validation    │    │ • MCPProxy      │    │ • STDIO         │
│ • Hot Reload    │    │ • Workflows     │    │ • Custom Tools  │
└─────────────────┘    └─────────────────┘    └─────────────────┘
```

## 🤝 Contributing

We welcome contributions! Please see our [Contributing Guide](docs/development/contributing.md) for details.

### Development Setup

```bash
git clone https://github.com/phildougherty/m8e.git
cd m8e
make dev-setup
make test
```

### Running Tests

```bash
make test              # Run all tests
make test-race         # Run with race detection
make test-coverage     # Generate coverage report
make lint             # Run linting
```

## 📄 License

This project is licensed under the GNU Affero General Public License v3.0 - see the [LICENSE](LICENSE) file for details.

## 🔗 Links

- **Documentation**: [docs/](docs/)
- **Issues**: [GitHub Issues](https://github.com/phildougherty/m8e/issues)
- **Discussions**: [GitHub Discussions](https://github.com/phildougherty/m8e/discussions)
- **MCP Protocol**: [modelcontextprotocol.io](https://modelcontextprotocol.io/)

## 🌟 Why Matey?

> "Ahoy! Navigate the seas of MCP orchestration with the reliability of Kubernetes and the simplicity of Docker Compose."

Matey makes it easy to:
- **Migrate from Docker Compose** to Kubernetes without rewriting configurations
- **Scale MCP applications** horizontally with Kubernetes native features
- **Integrate AI providers** seamlessly into your MCP workflows
- **Maintain security** with enterprise-grade authentication and authorization
- **Monitor and debug** with comprehensive logging and inspection tools

---

**Ready to set sail?** Start with our [Quick Start Guide](docs/getting-started/quick-start.md) and join the growing community of MCP orchestration users!