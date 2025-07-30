# CLI Commands Overview

This document provides a comprehensive overview of all Matey CLI commands, organized by category and use case.

## Global Flags

All commands support these global flags:

- `--file, -c` (default: "matey.yaml") - Specify matey configuration file
- `--verbose, -v` (default: false) - Enable verbose output
- `--namespace, -n` (default: "default") - Kubernetes namespace
- `--help, -h` - Show help for any command

## Command Categories

### 1. Installation & Setup

#### `matey install`
Install Matey CRDs and required Kubernetes resources. Must be run before using 'matey up' for the first time.

```bash
matey install [flags]
```

**Flags:**
- `--dry-run` - Print resources without installing

**What it installs:**
- Custom Resource Definitions (MCPServer, MCPMemory, MCPTaskScheduler, MCPProxy, Workflow)
- RBAC resources (ServiceAccount, ClusterRole, ClusterRoleBinding)
- Necessary cluster permissions

**Example:**
```bash
# Install with dry-run to see what would be installed
matey install --dry-run

# Install for real
matey install
```

---

### 2. Core Orchestration

#### `matey up [SERVICE...]`
Create and start MCP services using Kubernetes resources.

```bash
matey up [SERVICE...] [flags]
```

**Examples:**
```bash
matey up                    # Start all enabled services
matey up memory             # Start only memory service
matey up memory task-scheduler  # Start memory and task scheduler
```

#### `matey down [SERVICE...]`
Stop and remove MCP services and their Kubernetes resources.

```bash
matey down [SERVICE...] [flags]
```

**Examples:**
```bash
matey down                  # Stop all services
matey down memory           # Stop only memory service
```

#### `matey start [SERVICE...]`
Start specific MCP services. Requires at least one service name.

```bash
matey start SERVICE... [flags]
```

#### `matey stop [SERVICE...]`
Stop specific MCP services. Requires at least one service name.

```bash
matey stop SERVICE... [flags]
```

#### `matey restart [SERVICE...]`
Restart MCP services (stop then start).

```bash
matey restart [SERVICE...] [flags]
```

**Examples:**
```bash
matey restart                    # Restart all enabled services
matey restart memory             # Restart only memory service
```

---

### 3. Monitoring & Status

#### `matey ps`
Show running MCP servers with detailed process information.

```bash
matey ps [flags]
```

**Flags:**
- `--watch, -w` - Watch for live updates
- `--format, -f` (default: "table") - Output format (table, json, yaml)
- `--filter` - Filter by status, namespace, or labels

**Information displayed:**
- Pod status, resource usage, restart counts
- Network endpoints and port mappings
- Volume mounts and persistent storage
- Health check status and readiness
- Labels, annotations, and metadata

#### `matey top`
Display a live view of MCP servers with real-time updates.

```bash
matey top [flags]
```

**Flags:**
- `--refresh` (default: 2s) - Refresh interval
- `--sort, -s` (default: "name") - Sort by field (name, status, restarts, age)
- `--desc` - Sort in descending order

**Interactive controls:**
- `q`/`Ctrl+C` - Quit
- `s` - Sort by status
- `n` - Sort by name
- `r` - Sort by restarts
- `a` - Sort by age
- `h` - Toggle help
- `Space` - Reverse sort order

#### `matey logs [SERVER...]`
View logs from MCP servers.

```bash
matey logs [SERVER...] [flags]
```

**Flags:**
- `--follow, -f` - Follow log output

**Special services:**
- `proxy` - Shows logs from proxy pods
- `task-scheduler` - Shows logs from task-scheduler pods
- `memory` - Shows logs from memory server pods

**Examples:**
```bash
matey logs                    # Show logs from all servers
matey logs proxy -f           # Follow proxy logs
matey logs task-scheduler -f  # Follow task scheduler logs
```

---

### 4. Service Management

#### `matey memory`
Manage the PostgreSQL-backed memory MCP server.

```bash
matey memory [flags]
```

**Flags:**
- `--enable` - Enable in config
- `--disable` - Disable service

**Features:**
- PostgreSQL backend for reliability
- Graph-based knowledge storage with 11 MCP tools
- Entity and relationship management
- Observation tracking
- Full-text search capabilities

#### `matey task-scheduler`
Manage the task scheduler service.

```bash
matey task-scheduler [flags]
```

**Flags:**
- `--enable` - Enable in config
- `--disable` - Disable service

**Features:**
- Built-in cron scheduling with AI-powered expression generation
- 14 MCP tools for workflow and task management
- Kubernetes Jobs for reliable task execution
- OpenRouter and Ollama integration for LLM-powered workflows

#### `matey proxy`
Run a system MCP proxy server.

```bash
matey proxy [flags]
```

**Flags:**
- `--port, -p` (default: 9876) - Port to run proxy on
- `--api-key, -k` - API key for authentication

**Features:**
- Automatic service discovery via Kubernetes labels
- Dynamic connection management
- Support for HTTP, SSE, and STDIO protocols
- Built-in health checking and retry logic

#### `matey reload`
Reload MCP proxy configuration to discover new servers.

```bash
matey reload [flags]
```

**Flags:**
- `--port, -p` (default: 9876) - Proxy server port
- `--api-key` - API key for authentication

---

### 5. Toolbox Management

#### `matey toolbox`
Manage MCP toolboxes - collections of servers working together.

##### `matey toolbox create [NAME]`
Create a new MCP toolbox.

```bash
matey toolbox create [NAME] [flags]
```

**Flags:**
- `--template, -t` - Use pre-built template
- `--file, -f` - Create from configuration file
- `--description, -d` - Toolbox description

**Available templates:**
- `coding-assistant` - Complete coding assistant
- `rag-stack` - Retrieval Augmented Generation
- `research-agent` - Research workflow

##### `matey toolbox list`
List all MCP toolboxes.

```bash
matey toolbox list [flags]
```

**Flags:**
- `--format, -f` (default: "table") - Output format

##### `matey toolbox up [TOOLBOX_NAME]`
Start an MCP toolbox.

```bash
matey toolbox up [TOOLBOX_NAME] [flags]
```

**Flags:**
- `--wait, -w` - Wait for toolbox to be ready
- `--timeout` - Timeout for waiting

##### `matey toolbox down [TOOLBOX_NAME]`
Stop an MCP toolbox.

```bash
matey toolbox down [TOOLBOX_NAME] [flags]
```

**Flags:**
- `--remove-volumes` - Remove persistent volumes
- `--force` - Force deletion without confirmation

##### `matey toolbox logs [TOOLBOX_NAME]`
Show logs from a toolbox.

```bash
matey toolbox logs [TOOLBOX_NAME] [flags]
```

**Flags:**
- `--server, -s` - Show logs from specific server only
- `--follow, -f` - Follow log output
- `--tail` (default: 50) - Number of lines to show

##### `matey toolbox status [TOOLBOX_NAME]`
Show detailed status of a toolbox.

```bash
matey toolbox status [TOOLBOX_NAME] [flags]
```

**Flags:**
- `--format, -f` (default: "table") - Output format
- `--watch, -w` - Watch for status changes

---

### 6. Workflow Management

#### `matey workflow`
Manage workflows for complex automation tasks.

##### `matey workflow create [name]`
Create a new workflow.

```bash
matey workflow create [name] [flags]
```

**Flags:**
- `--file, -f` - Workflow definition file
- `--template` - Template name to use
- `--param` - Template parameters (key=value)
- `--schedule` - Cron schedule expression
- `--timezone` - Timezone for schedule
- `--dry-run` - Print workflow without creating

##### `matey workflow list`
List workflows.

```bash
matey workflow list [flags]
```

**Flags:**
- `--output, -o` (default: "table") - Output format
- `--all-namespaces` - List from all namespaces

##### `matey workflow get <name>`
Get workflow details.

```bash
matey workflow get <name> [flags]
```

**Flags:**
- `--output, -o` (default: "yaml") - Output format

##### `matey workflow delete <name>`
Delete a workflow.

```bash
matey workflow delete <name> [flags]
```

##### `matey workflow pause <name>`
Pause a workflow.

```bash
matey workflow pause <name> [flags]
```

##### `matey workflow resume <name>`
Resume a paused workflow.

```bash
matey workflow resume <name> [flags]
```

##### `matey workflow logs <name>`
Get workflow execution logs.

```bash
matey workflow logs <name> [flags]
```

**Flags:**
- `--step` - Get logs for specific step
- `--follow, -f` - Follow log output
- `--tail` (default: 100) - Number of lines to show

##### `matey workflow execute <name>`
Manually execute a workflow.

```bash
matey workflow execute <name> [flags]
```

**Flags:**
- `--wait` - Wait for execution to complete
- `--timeout` (default: 30m) - Timeout for waiting

---

### 7. Resource Inspection

#### `matey inspect [resource-type] [resource-name]`
Display detailed information about MCP resources.

```bash
matey inspect [resource-type] [resource-name] [flags]
```

**Flags:**
- `--output, -o` (default: "table") - Output format
- `--show-conditions` - Show condition details
- `--wide` - Show additional columns

**Resource types:**
- `memory` - MCPMemory resources
- `server` - MCPServer resources
- `task-scheduler` - MCPTaskScheduler resources
- `proxy` - MCPProxy resources
- `workflow` - Workflow resources
- `all` - All resource types (default)

**Examples:**
```bash
matey inspect                          # Show all resources
matey inspect memory                   # Show all memory resources
matey inspect server my-server         # Show specific server
matey inspect task-scheduler -o json   # Show task scheduler in JSON
```

---

### 8. AI & Chat

#### `matey chat`
Launch natural scrolling terminal chat with AI assistance.

```bash
matey chat [flags]
```

**Features:**
- Natural language interaction with MCP services
- AI-powered assistance for cluster management
- Support for multiple AI providers
- Function calling to interact with MCP tools

**Interactive commands:**
- `/help` - Show available commands
- `/status` - Show system and connection status
- `/clear` - Clear chat history
- `/provider <name>` - Switch AI provider
- `/model <name>` - Switch AI model
- `/compact` - Switch to compact function output
- `/verbose` - Switch to verbose function output
- `/exit` or `/quit` - Exit chat

#### `matey mcp-server`
Start the Matey MCP server.

```bash
matey mcp-server [flags]
```

**Flags:**
- `--port, -p` (default: "8081") - Port to run MCP server on
- `--matey-binary` - Path to matey binary

**Features:**
- Provides MCP tools for interacting with the cluster
- HTTP service accessible by MCP clients
- Tools for status checking, service management, log viewing

---

### 9. Utilities

#### `matey validate`
Validate the compose file syntax and structure.

```bash
matey validate [flags]
```

#### `matey completion [bash|zsh|fish|powershell]`
Generate autocompletion script for the specified shell.

```bash
matey completion [bash|zsh|fish|powershell] [flags]
```

**Installation examples:**
```bash
# Bash
echo 'source <(matey completion bash)' >>~/.bashrc

# Zsh
echo 'source <(matey completion zsh)' >>~/.zshrc

# Fish
matey completion fish | source
```

#### `matey create-config`
Create client configuration for MCP servers.

```bash
matey create-config [flags]
```

**Flags:**
- `--output, -o` (default: "client-configs") - Output directory
- `--type, -t` (default: "all") - Client type (claude, anthropic, openai, all)

**Features:**
- Generates ready-to-use configuration files for LLM clients
- Supports Claude Desktop, Anthropic API clients, OpenAI compatible clients
- Creates wrapper scripts for container-based servers

---

## Common Workflows

### Initial Setup
```bash
# Install Matey components
matey install

# Create configuration
matey create-config

# Start services
matey up
```

### Daily Operations
```bash
# Check status
matey ps

# View logs
matey logs --follow

# Restart services
matey restart
```

### Debugging
```bash
# Inspect resources
matey inspect all

# Check detailed status
matey top

# View specific logs
matey logs memory --follow
```

### AI Assistance
```bash
# Start chat interface
matey chat

# Use natural language commands like:
# "Show me the status of all services"
# "Restart the memory service"
# "What's in the logs for the proxy?"
```

### Toolbox Management
```bash
# Create a coding assistant toolbox
matey toolbox create coding-assistant --template coding-assistant

# Start the toolbox
matey toolbox up coding-assistant

# Check status
matey toolbox status coding-assistant
```

### Workflow Automation
```bash
# Create a workflow
matey workflow create data-processing --template batch-processing

# Execute manually
matey workflow execute data-processing --wait

# Check execution logs
matey workflow logs data-processing --follow
```

## Best Practices

1. **Always validate** configuration before applying: `matey validate`
2. **Use verbose mode** for debugging: `matey -v [command]`
3. **Monitor services** regularly: `matey ps` or `matey top`
4. **Check logs** when troubleshooting: `matey logs [service] -f`
5. **Use toolboxes** for common server combinations
6. **Leverage AI chat** for complex operations: `matey chat`
7. **Create workflows** for repetitive tasks
8. **Use inspection** for detailed resource information: `matey inspect`

## Getting Help

- Use `--help` flag with any command for detailed information
- Use `matey chat` for natural language assistance
- Check the [troubleshooting guide](../troubleshooting.md) for common issues
- Visit [GitHub Issues](https://github.com/phildougherty/m8e/issues) for bug reports