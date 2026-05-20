# Your First Deployment

This guide walks you through deploying your first MCP server with Matey,
explaining each step in detail. It assumes you have a Kubernetes cluster
reachable via `kubectl` and that the matey chart has been installed (see
[Helm Deployment](../deployment/helm.md)).

## Understanding the Basics

### What is an MCP Server?

An MCP (Model Context Protocol) server is a service that exposes:

- **Tools** — Functions an AI model can call.
- **Resources** — Data an AI model can read.
- **Prompts** — Reusable prompt templates.

### How Matey Works

Matey reads a Compose-style `matey.yaml` and turns it into Kubernetes resources:

1. Each `servers:` entry becomes a `MCPServer` custom resource
   (`mcpservers.mcp.matey.ai`).
2. The controller-manager reconciles each `MCPServer` into a Deployment,
   Service, ConfigMap, and (if needed) PVC.
3. The proxy discovers each `MCPServer` and exposes its tools over HTTP.

## Step-by-Step Deployment

### Step 1: Choose a First Server

A simple filesystem server is a good first deployment:

```yaml
# matey.yaml
version: "3.8"

servers:
  filesystem:
    image: mcp/filesystem-server:latest
    protocol: stdio
    command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/data"]
    volumes:
      - ./data:/data
    resources:
      limits:
        memory: "128Mi"
        cpu: "100m"
      requests:
        memory: "64Mi"
        cpu: "50m"
```

### Step 2: Create the Data Directory

```bash
mkdir -p ./data
echo "Hello from MCP!" > ./data/welcome.txt
```

### Step 3: Deploy the Server

```bash
matey up filesystem
matey ps
```

Expected output:

```
NAME         STATUS    PROTOCOL  REPLICAS  READY  AGE
filesystem   Running   stdio     1/1       1      30s
```

### Step 4: Test the Server

```bash
matey logs filesystem
matey inspect server filesystem
```

To list tools exposed by the server, query the proxy:

```bash
kubectl port-forward -n matey svc/matey-proxy 9876:9876 &
curl http://localhost:9876/filesystem/tools
```

### Step 5: What Just Happened

`matey up filesystem` created an `MCPServer` custom resource. The
controller-manager reconciled it into a Deployment, a Service, and a
ConfigMap. Inspect them:

```bash
kubectl get mcpservers -n matey
kubectl get deployments -n matey
kubectl get services -n matey
kubectl get configmaps -n matey
```

## Adding More Complexity

### Step 6: Add a Second Server

Add another entry under `servers:`:

```yaml
servers:
  # ... existing filesystem ...

  database-tools:
    image: mcp/database-tools:latest
    protocol: http
    port: 8080
    environment:
      - DB_URL=postgresql://user:pass@matey-postgres:5432/mydb
    resources:
      limits:
        memory: "256Mi"
        cpu: "150m"
      requests:
        memory: "128Mi"
        cpu: "75m"
```

### Step 7: Apply the Update

```bash
matey up
matey ps
```

The existing `filesystem` server is left alone; only the new
`database-tools` server is created.

### Step 8: Inspect the New Server

```bash
matey logs database-tools
matey inspect server database-tools
```

## Working With AI Providers

Matey reads provider credentials from environment variables that the proxy
and any in-cluster MCP server can see. Add the relevant keys to your `.env`
(see [.env.example](../../.env.example)) and either:

- export them locally before running `matey up` (CLI flow), or
- pass them in-cluster as Secret-backed env vars on the proxy Deployment
  (Helm `proxy.extraEnv` plus a Secret reference).

The supported provider env vars:

- `OPENROUTER_API_KEY`
- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`

## Monitoring and Debugging

### Step 9: Watch Status

```bash
matey top                       # interactive view
matey ps                        # snapshot
matey logs --follow             # all server logs
matey logs filesystem --follow  # one server
matey events                    # recent reconciler events
```

### Step 10: Debug Common Issues

```bash
matey validate                                   # config sanity check
matey -v validate                                # verbose
kubectl describe mcpserver filesystem -n matey   # status.conditions
kubectl describe pod -n matey -l app.kubernetes.io/instance=filesystem
```

There is no `matey ping`. To check connectivity to a server through the
proxy:

```bash
curl http://localhost:9876/filesystem/tools
```

To check connectivity to a server pod directly:

```bash
kubectl exec -n matey deploy/filesystem -- /bin/sh -c 'echo ok'
```

## Resource Usage

```bash
kubectl top pods -n matey
matey inspect server filesystem
```

## Scaling

Matey does **not** provide `matey scale` or `matey autoscale` subcommands.

To change replicas, edit `replicas:` in `matey.yaml` and re-run:

```yaml
servers:
  filesystem:
    replicas: 3
```

```bash
matey up filesystem
```

For autoscaling, create a standard `HorizontalPodAutoscaler` targeting the
Deployment the controller created (the Deployment name matches the
`MCPServer` name).

## Security Considerations

The matey Helm chart applies hardened defaults out of the box:

- `runAsNonRoot: true`, `readOnlyRootFilesystem: true`,
  `allowPrivilegeEscalation: false`, all capabilities dropped,
  `seccompProfile.type: RuntimeDefault`.
- NetworkPolicy: default-deny ingress and egress, with a targeted allow
  policy that opens only the proxy port, the controller metrics port,
  in-namespace pod-to-pod traffic, DNS, and the Kubernetes API server.

To tighten the proxy's externally-reachable CIDR set, set
`proxy.networkPolicy.allowedIngressCIDRs` in your Helm values. See
[Helm Deployment](../deployment/helm.md) for the full values reference.

## Persistence

Volumes in `matey.yaml` become PersistentVolumeClaims when bound to a
named volume rather than a host path:

```yaml
servers:
  filesystem:
    volumes:
      - filesystem-data:/data

volumes:
  filesystem-data:
    size: 5Gi
    storageClassName: standard
```

## Next Steps

- **[CLI Commands Overview](../cli/commands-overview.md)** — the complete command surface.
- **[matey.yaml Configuration](../configuration/matey-yaml.md)** — full configuration reference.
- **[Environment Variables](../configuration/environment-variables.md)** — env var reference.
- **[Helm Deployment](../deployment/helm.md)** — production install path and CRD upgrade strategy.
- **[Feature Status](../../README.md#feature-status)** — what is stable in this pre-1.0 release.

## A Note on Older Docs

Earlier revisions of this guide referenced commands that do not exist in the
current CLI: `matey chat`, `matey toolbox`, `matey workflow`, `matey scale`,
`matey autoscale`, `matey ping`, `matey exec`, `matey backup`,
`matey configure`. Use the alternatives documented above:
edit `matey.yaml` and `matey up` for any scaling/config change,
`kubectl exec` for direct shell access, and standard CronJobs or external
backup tooling for backups.
