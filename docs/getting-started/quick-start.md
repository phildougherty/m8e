# Quick Start Guide

Get up and running with Matey in under 5 minutes.

## Prerequisites

- Kubernetes cluster (1.28+) with `kubectl` configured.
- Helm 3 (recommended) or the `matey` binary installed locally.
- See the [Installation Guide](installation.md) for setup details.

## Step 1: Install the Matey Components

The supported install path is the Helm chart, which deploys CRDs, RBAC, the
controller-manager, and the MCP proxy in one step:

```bash
helm install matey ./charts/matey -n matey --create-namespace
```

`matey install` (without Helm) only installs CRDs and RBAC; it does not run a
controller-manager and is intended for advanced users who manage the
controller out-of-band. See the [Helm Deployment Guide](../deployment/helm.md)
for details.

Verify the CRDs are registered:

```bash
kubectl get crd | grep matey.ai
```

Expected output (all CRDs use the `mcp.matey.ai` API group):

```
mcpmemories.mcp.matey.ai          2026-05-15T10:30:00Z
mcpproxies.mcp.matey.ai           2026-05-15T10:30:00Z
mcpservers.mcp.matey.ai           2026-05-15T10:30:00Z
mcppostgres.mcp.matey.ai          2026-05-15T10:30:00Z
mcptaskschedulers.mcp.matey.ai    2026-05-15T10:30:00Z
```

## Step 2: Create Your First Configuration

Create a `matey.yaml` file. The schema is documented in
[Configuration Reference](../configuration/matey-yaml.md).

```yaml
version: "3.8"

servers:
  # Simple filesystem MCP server.
  filesystem:
    image: mcp/filesystem-server:latest
    protocol: stdio
    command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/workspace"]
    volumes:
      - ./workspace:/workspace
    replicas: 1

  # HTTP MCP server.
  web-search:
    image: mcp/web-search:latest
    protocol: http
    port: 8080
    environment:
      - SEARCH_API_KEY=${SEARCH_API_KEY}
    replicas: 2
```

## Step 3: Set Up Environment Variables

Copy `.env.example` to `.env` and fill in the values that apply to you. Matey
reads these from the environment of the process running `matey up`; in-cluster
secrets are managed through the proxy `apiKeyExistingSecret` value and through
each MCPServer spec.

```bash
cp .env.example .env
$EDITOR .env
```

## Step 4: Validate the Configuration

```bash
matey validate
matey -v validate    # verbose
```

## Step 5: Deploy Your Servers

```bash
matey up                # apply all servers from matey.yaml
matey ps                # list running MCPServer resources
```

Expected output (column set is approximate; the actual table is wider):

```
NAME         STATUS    PROTOCOL  REPLICAS  READY  AGE
filesystem   Running   stdio     1/1       1      2m
web-search   Running   http      2/2       2      2m
```

## Step 6: Inspect and Tail Logs

```bash
matey logs filesystem            # logs from one server
matey logs proxy -f              # follow the proxy
matey logs task-scheduler -f     # follow the task scheduler
matey inspect server filesystem  # detail view for one MCPServer
matey top                        # interactive live view
matey events                     # recent reconciler events
```

## Step 7: Reach Your MCP Servers Through the Proxy

The proxy is deployed by the Helm chart and exposed as a Service (NodePort
30876 by default — see `proxy.service` in the chart values).

```bash
# Cluster-internal:
kubectl port-forward -n matey svc/matey-proxy 9876:9876

# Then:
curl http://localhost:9876/health
curl http://localhost:9876/api/discovery
curl http://localhost:9876/filesystem/tools
```

To run a local development proxy outside the cluster, against the same
in-cluster MCPServer resources:

```bash
matey proxy --port 9876
```

## Common Next Steps

### Change the Number of Replicas

Matey does not ship a `matey scale` command. Edit `matey.yaml` and re-apply:

```yaml
servers:
  web-search:
    replicas: 5
```

```bash
matey up web-search
```

For autoscaling, define a standard Kubernetes `HorizontalPodAutoscaler`
against the Deployment the MCPServer controller creates; there is no
matey-native autoscale subcommand.

### Add More Servers

Append new entries under `servers:` in `matey.yaml` and run `matey up`. The
controller reconciles new MCPServer resources without restarting existing ones.

```yaml
servers:
  database-tools:
    image: mcp/database-tools:latest
    protocol: http
    port: 8081
    environment:
      - DB_URL=postgresql://user:pass@db:5432/mydb
    depends_on:
      - memory
```

### Restart, Stop, Remove

```bash
matey restart filesystem
matey stop filesystem
matey down filesystem        # removes the MCPServer resource
```

## Troubleshooting

### Servers Not Starting

```bash
matey ps
matey events                                # surface controller-manager events
matey logs filesystem -f                    # tail pod logs
kubectl describe mcpserver filesystem -n matey
```

### Configuration Issues

```bash
matey -v validate
kubectl describe mcpserver <name> -n matey  # status.conditions show config errors
```

### Connectivity / Permission Problems

```bash
# Service is up but unreachable: check the proxy pod and Service.
kubectl get pods -n matey -l app.kubernetes.io/component=proxy
kubectl get svc -n matey

# RBAC sanity check for the controller-manager.
kubectl auth can-i list mcpservers.mcp.matey.ai \
  --as=system:serviceaccount:matey:matey-controller-manager

# CRDs registered?
kubectl get crd mcpservers.mcp.matey.ai -o yaml
```

## What's Next?

- **[Configuration Reference](../configuration/matey-yaml.md)** — all `matey.yaml` keys.
- **[CLI Commands](../cli/commands-overview.md)** — the complete command surface.
- **[Helm Deployment](../deployment/helm.md)** — the supported install path.
- **[Feature Status](../../README.md#feature-status)** — what is stable in this pre-1.0 release.

## Example Projects

- **[Basic Setup](../configuration/examples/basic.yaml)** — simple MCP server setup.
- **[Production](../configuration/examples/production.yaml)** — production-oriented configuration.

## Need Help?

- **[GitHub Issues](https://github.com/phildougherty/m8e/issues)** — bug reports and feature requests.
- **[Community Discussions](https://github.com/phildougherty/m8e/discussions)** — questions and shared experience.
