# CLI Commands Overview

This document is the authoritative list of `matey` CLI commands. Every command
listed here has a constructor in `internal/cmd/`. Commands not listed here do
not exist.

## Global Flags

All commands support these global flags:

- `--file, -c` (default: `matey.yaml`) — Path to the matey configuration file.
- `--verbose, -v` (default: `false`) — Enable verbose output.
- `--namespace, -n` (default: `matey`) — Kubernetes namespace.
- `--help, -h` — Show help for any command.

## Lifecycle Commands

### `matey up [SERVICE...]`

Create and start MCP services as Kubernetes resources.

```bash
matey up                          # start everything in matey.yaml
matey up memory                   # start one server
matey up memory task-scheduler    # start a subset
```

### `matey down [SERVICE...]`

Stop and remove MCP services and their Kubernetes resources.

```bash
matey down            # remove all
matey down memory     # remove one
```

### `matey start SERVICE...`

Start specific MCP services that are already defined. Requires at least one
service name.

### `matey stop SERVICE...`

Stop specific MCP services. Requires at least one service name.

### `matey restart [SERVICE...]`

Restart MCP services (stop then start).

```bash
matey restart            # restart all
matey restart memory     # restart one
```

## Inspection Commands

### `matey ps`

List running MCP servers with status, replicas, age, and selected pod fields.

```bash
matey ps
matey ps -o json
```

### `matey top`

Interactive live view of MCP servers (similar to `kubectl top`, with extra
columns for MCPServer status).

### `matey logs [SERVER...]`

View logs from MCP server pods. The special names `proxy`, `task-scheduler`,
and `memory` route to those components.

```bash
matey logs                       # all servers
matey logs proxy -f              # follow proxy
matey logs task-scheduler -f     # follow scheduler
```

Flags:

- `--follow, -f` — Follow output.

### `matey events`

Show recent reconciler and pod events for matey-managed resources.

### `matey inspect [resource-type] [resource-name]`

Detailed view of a matey resource.

```bash
matey inspect                          # all resources
matey inspect server filesystem        # specific MCPServer
matey inspect proxy -o yaml
```

Resource types: `server`, `memory`, `proxy`, `task-scheduler`, `postgres`,
`all`.

## Configuration Commands

### `matey validate`

Validate the syntax and semantics of `matey.yaml`.

```bash
matey validate
matey -v validate
```

### `matey create-config`

Generate client configuration for connecting external LLM clients
(Claude Desktop, etc.) to the proxy.

Flags:

- `--output, -o` (default: `client-configs`) — Output directory.
- `--type, -t` (default: `all`) — Client type (`claude`, `anthropic`,
  `openai`, `all`).

### `matey install`

Install Matey CRDs and RBAC. This is a CRDs-and-permissions-only install: it
does not deploy the controller-manager or the proxy. For a complete install
that runs the controller, use the Helm chart (see
[Helm Deployment](../deployment/helm.md)).

Flags:

- `--dry-run` — Print resources without applying.

### `matey reload`

Trigger the proxy to re-discover MCPServer resources without a restart.

Flags:

- `--port, -p` (default: `9876`) — Proxy port.
- `--api-key` — Proxy API key.

## Service Commands

### `matey proxy`

Run the development MCP proxy server in the foreground. In production the
proxy runs in-cluster via the Helm chart; this command is for local
development and testing against a connected cluster.

Flags:

- `--port, -p` (default: `9876`) — Listen port.
- `--api-key, -k` — API key for proxy authentication.

### `matey memory`

Manage the PostgreSQL-backed MCPMemory resource.

> Status: partial. Store, CRUD, and Postgres full-text search work. Schema is
> idempotent rather than migration-based; search is full-text, not vector.

Flags:

- `--enable` — Mark enabled in config.
- `--disable` — Mark disabled in config.

### `matey task-scheduler`

Manage the MCPTaskScheduler resource (cron and workflow execution).

Flags:

- `--enable` — Mark enabled in config.
- `--disable` — Mark disabled in config.

## Utility Commands

### `matey completion [bash|zsh|fish|powershell]`

Generate a shell completion script.

```bash
echo 'source <(matey completion bash)' >>~/.bashrc
echo 'source <(matey completion zsh)' >>~/.zshrc
matey completion fish | source
```

## Hidden / Internal Commands

These commands exist but are hidden from `--help` because they only run inside
matey-managed pods, not from an operator shell. They are documented here so
you can match them when reading logs or process listings.

- `matey controller-manager` — entrypoint for the controller-manager pod.
- `matey mcp-server` — entrypoint for an in-cluster MCP server pod wrapper.
- `matey serve-proxy` — entrypoint for the in-cluster proxy pod (different
  from the operator-facing `matey proxy`).
- `matey scheduler-server` — entrypoint for the task-scheduler pod.
- `matey postgres` — entrypoint for the bundled PostgreSQL helper.

## Commands Documented Elsewhere That Do Not Exist

For users moving from older docs: `matey chat`, `matey toolbox`,
`matey workflow`, `matey scale`, `matey autoscale`, `matey ping`, and
`matey configure` are not part of the current CLI. Use the alternatives in the
sections above:

| Old reference         | Current equivalent                                       |
| --------------------- | -------------------------------------------------------- |
| `matey scale X=N`     | Edit `replicas:` in `matey.yaml`, then `matey up X`      |
| `matey autoscale`     | Create a standard `HorizontalPodAutoscaler` resource     |
| `matey ping`          | `matey ps`, `matey inspect`, `kubectl get mcpserver`     |
| `matey configure`     | Edit `matey.yaml` and re-run `matey up` / Helm `upgrade` |
| `matey chat`          | (removed; use any MCP-aware client against the proxy)    |
| `matey toolbox`       | (removed; compose MCPServers directly in `matey.yaml`)   |
| `matey workflow`      | (use `matey task-scheduler` + workflow CRDs)             |

## Common Workflows

### Initial Setup

```bash
helm install matey ./charts/matey -n matey --create-namespace
matey create-config
matey up
```

### Daily Operations

```bash
matey ps
matey logs proxy -f
matey restart memory
```

### Debugging

```bash
matey inspect server
matey top
matey events
matey logs <server> -f
```

## Getting Help

- Append `--help` to any command for the canonical flags and defaults.
- Open a [GitHub issue](https://github.com/phildougherty/m8e/issues) for bug
  reports or missing functionality.
