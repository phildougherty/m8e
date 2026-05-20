# Helm Deployment Guide

This guide covers deploying Matey with the in-tree Helm chart at
`charts/matey/`.

> **Status: chart v0.1.0, appVersion `0.0.4-dev`.** The chart is the supported
> production install path. Matey itself is pre-1.0 — see the
> [Feature Status table](../../README.md#feature-status) for what to rely on.
> All resources install into the `matey` namespace.

## Prerequisites

- Kubernetes cluster (1.28+).
- Helm 3.0+.
- `kubectl` with cluster-admin (CRD install requires it).

## Quick Start

```bash
# From a clone of m8e:
helm install matey ./charts/matey \
  -n matey \
  --create-namespace
```

Or once the chart is published to an OCI / HTTP repo (release pending):

```bash
helm repo add matey https://phildougherty.github.io/m8e
helm repo update
helm install matey matey/matey -n matey --create-namespace
```

Verify:

```bash
helm status matey -n matey
kubectl get pods -n matey
kubectl get crd | grep matey.ai
```

## What the Chart Installs

- CRDs in `mcp.matey.ai` group: `mcpservers`, `mcpproxies`, `mcpmemories`,
  `mcppostgres`, `mcptaskschedulers`.
- ServiceAccount, ClusterRole, and ClusterRoleBinding for the
  controller-manager.
- A `controller-manager` Deployment that reconciles the CRDs.
- A `proxy` Deployment plus a Service (NodePort by default) that exposes the
  MCP HTTP proxy.
- A ConfigMap with the controller-manager's bootstrap configuration.
- An optional Secret for the proxy API key.
- NetworkPolicy resources (default-deny plus a targeted allow rule set) when
  `networkPolicy.enabled=true`.

## Values Reference

The authoritative value list is `charts/matey/values.yaml`. The most common
keys, by section:

### Top-level

| Key                  | Default            | Description                                        |
| -------------------- | ------------------ | -------------------------------------------------- |
| `nameOverride`       | `""`               | Override the chart name in resource names.         |
| `fullnameOverride`   | `""`               | Override the fully qualified app name.             |
| `image.repository`   | `ghcr.io/phildougherty/matey` | Container image repository.             |
| `image.tag`          | `""` (uses `appVersion`) | Image tag override.                         |
| `image.pullPolicy`   | `IfNotPresent`     | Image pull policy.                                 |
| `imagePullSecrets`   | `[]`               | List of `{name: secretName}` pull secrets.         |
| `namespace.create`   | `true`             | Create the matey namespace as part of the release. |
| `namespace.name`     | `matey`            | Namespace to install into.                         |
| `crds.install`       | `true`             | Install the matey CRDs via the chart.              |
| `rbac.create`        | `true`             | Create ClusterRole + binding.                      |
| `serviceAccount.create` | `true`          | Create the controller-manager SA.                  |
| `serviceAccount.name` | `""`              | SA name override.                                  |
| `serviceAccount.annotations` | `{}`        | SA annotations (e.g. IRSA).                        |
| `podSecurityContext` | hardened           | Applied to all pods.                               |
| `securityContext`    | hardened           | Applied to all containers.                         |

### Controller-Manager

| Key                                              | Default | Description                              |
| ------------------------------------------------ | ------- | ---------------------------------------- |
| `controllerManager.enabled`                      | `true`  | Enable the controller-manager.           |
| `controllerManager.replicaCount`                 | `1`     | Replica count (no leader election yet).  |
| `controllerManager.logLevel`                     | `info`  | `debug` / `info` / `warn` / `error`.     |
| `controllerManager.automountServiceAccountToken` | `true`  | Must stay `true` to talk to the API.     |
| `controllerManager.ports.metrics`                | `8083`  | Metrics port.                            |
| `controllerManager.ports.health`                 | `8082`  | Health / readiness port.                 |
| `controllerManager.ports.webhook`                | `9443`  | Webhook server port.                     |
| `controllerManager.resources`                    | small   | `requests` / `limits` for the pod.       |
| `controllerManager.nodeSelector`                 | `{}`    | Pod node selector.                       |
| `controllerManager.tolerations`                  | `[]`    | Pod tolerations.                         |
| `controllerManager.affinity`                     | `{}`    | Pod affinity.                            |
| `controllerManager.podAnnotations`               | `{}`    | Extra pod annotations.                   |
| `controllerManager.extraEnv`                     | `[]`    | Additional env vars.                     |

### Proxy

| Key                                  | Default     | Description                                          |
| ------------------------------------ | ----------- | ---------------------------------------------------- |
| `proxy.enabled`                      | `true`      | Enable the proxy Deployment + Service.               |
| `proxy.replicaCount`                 | `1`         | Replica count.                                       |
| `proxy.port`                         | `9876`      | Container port.                                      |
| `proxy.apiKey`                       | `""`        | Inline API key (creates a Secret).                   |
| `proxy.apiKeyExistingSecret`         | `""`        | Reference an existing Secret instead.                |
| `proxy.service.type`                 | `NodePort`  | `ClusterIP` / `NodePort` / `LoadBalancer`.           |
| `proxy.service.nodePort`             | `30876`     | NodePort when `type=NodePort`.                       |
| `proxy.service.annotations`          | `{}`        | Annotations on the proxy Service.                    |
| `proxy.resources`                    | small       | `requests` / `limits`.                               |
| `proxy.networkPolicy.allowedIngressCIDRs` | `[0.0.0.0/0]` | CIDRs allowed to reach the proxy port.       |
| `proxy.automountServiceAccountToken` | `true`      | Must stay `true` for in-cluster discovery.           |

### NetworkPolicy

| Key                                          | Default                       | Description                                                                       |
| -------------------------------------------- | ----------------------------- | --------------------------------------------------------------------------------- |
| `networkPolicy.enabled`                      | `true`                        | Render `NetworkPolicy` resources.                                                 |
| `networkPolicy.kubeSystemNamespaceLabel`     | `kubernetes.io/metadata.name` | Namespace label to match for kube-dns egress.                                     |
| `networkPolicy.kubeSystemNamespaceValue`     | `kube-system`                 | Expected value of that label.                                                     |
| `networkPolicy.apiServerCIDRs`               | `[0.0.0.0/0]`                 | CIDRs allowed for egress to the Kubernetes API server.                            |
| `networkPolicy.extraEgressRules`             | `[]`                          | Additional egress rules (webhooks, registries, etc.).                             |

## Working `helm install` Examples

### 1. Minimal install (defaults)

```bash
helm install matey ./charts/matey -n matey --create-namespace
```

### 2. Customise the proxy: ClusterIP-only and supply an API key

```bash
helm install matey ./charts/matey \
  -n matey \
  --create-namespace \
  --set proxy.service.type=ClusterIP \
  --set proxy.apiKey="$(openssl rand -hex 32)"
```

### 3. Pin the image, scale the proxy, restrict ingress CIDR

```bash
helm install matey ./charts/matey \
  -n matey \
  --create-namespace \
  --set image.tag=0.0.4 \
  --set proxy.replicaCount=2 \
  --set 'proxy.networkPolicy.allowedIngressCIDRs={10.0.0.0/8,192.168.0.0/16}'
```

## Using a Values File

```yaml
# values.prod.yaml
image:
  repository: ghcr.io/phildougherty/matey
  tag: "0.0.4"
  pullPolicy: IfNotPresent

controllerManager:
  replicaCount: 1
  logLevel: info
  resources:
    requests: { cpu: 200m, memory: 256Mi }
    limits:   { cpu: 1000m, memory: 1Gi }

proxy:
  replicaCount: 2
  port: 9876
  apiKeyExistingSecret: matey-proxy-auth
  service:
    type: ClusterIP
  networkPolicy:
    allowedIngressCIDRs:
      - 10.0.0.0/8
      - 192.168.0.0/16

networkPolicy:
  enabled: true
  apiServerCIDRs:
    - 10.96.0.1/32   # kubernetes.default.svc ClusterIP in this cluster
```

```bash
helm install matey ./charts/matey -n matey --create-namespace -f values.prod.yaml
```

## Upgrades

```bash
helm upgrade matey ./charts/matey -n matey -f values.prod.yaml
helm history matey -n matey
helm rollback matey 1 -n matey
```

## CRD Upgrade Strategy

CRDs are managed in the Helm chart's `templates/crds/` directory and applied
**at install time only**. Helm 3 deliberately does not apply CRD changes on
`helm upgrade` — this is by design, to prevent silent breaking changes.

When you pull a new chart version that ships modified CRDs, you must apply the
CRDs yourself:

```bash
kubectl apply -f charts/matey/templates/crds/
helm upgrade matey ./charts/matey -n matey -f values.prod.yaml
```

To preview the new CRDs first:

```bash
kubectl diff -f charts/matey/templates/crds/
```

### CRD version bumps

When a CRD's `spec.versions` list changes (a new served version is added or
the storage version is changed), the chart should ship a CRD conversion
webhook so existing custom resources convert cleanly. **The chart does not
ship a conversion webhook yet** — this is a known gap and explicit future
work. Until then, treat any change to `spec.versions` as a manual migration:

1. Apply the new CRD (`kubectl apply -f charts/matey/templates/crds/`).
2. List custom resources of the changed kind, save them, and re-apply them
   under the new version.
3. Then run `helm upgrade`.

For routine field-level changes (new optional fields, looser validation),
`kubectl apply` of the CRD followed by `helm upgrade` is sufficient.

### Uninstall

CRDs installed via the chart's `crds/` directory are **not removed** on
`helm uninstall` (also by design). To remove them:

```bash
helm uninstall matey -n matey
kubectl delete crd \
  mcpservers.mcp.matey.ai \
  mcpproxies.mcp.matey.ai \
  mcpmemories.mcp.matey.ai \
  mcppostgres.mcp.matey.ai \
  mcptaskschedulers.mcp.matey.ai
```

Deleting the CRDs will delete every custom resource of those kinds in the
cluster. Back up first if needed.

## Troubleshooting

### Lint and render locally

```bash
make helm-lint
make helm-template
make helm-test     # requires a connected cluster
```

### Image pull errors

```bash
kubectl get pods -n matey
kubectl describe pod -n matey -l app.kubernetes.io/component=controller-manager

# Provide pull secret:
kubectl create secret docker-registry regcred \
  --docker-server=ghcr.io \
  --docker-username=user --docker-password=pat
# values.yaml: imagePullSecrets: [{name: regcred}]
```

### Pods crash-looping

```bash
kubectl logs -n matey -l app.kubernetes.io/component=controller-manager --tail=200
kubectl logs -n matey -l app.kubernetes.io/component=proxy --tail=200
```

### Inspect rendered manifests

```bash
helm template matey ./charts/matey -n matey > rendered.yaml
helm get manifest matey -n matey
```

### NetworkPolicy blocking traffic

If a CNI that enforces NetworkPolicy is installed and traffic is unexpectedly
denied:

```bash
kubectl get networkpolicy -n matey
kubectl describe networkpolicy -n matey
```

Either widen `proxy.networkPolicy.allowedIngressCIDRs` and
`networkPolicy.apiServerCIDRs`, add to `networkPolicy.extraEgressRules`, or
temporarily set `networkPolicy.enabled=false` and re-run `helm upgrade`.

## See Also

- [`charts/matey/values.yaml`](../../charts/matey/values.yaml) — full values list with comments.
- [CLI Commands Overview](../cli/commands-overview.md) — operator commands.
- [Quick Start](../getting-started/quick-start.md) — first-time setup.
