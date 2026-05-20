# Kubernetes Deployment

The supported way to install Matey on Kubernetes is the in-tree Helm chart at
`charts/matey/`. See **[Helm Deployment Guide](helm.md)** for the complete
walkthrough, values reference, and CRD upgrade strategy.

## Install Paths at a Glance

| Path                                                | What it installs                                                                 | When to use it                                                              |
| --------------------------------------------------- | -------------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| `helm install matey ./charts/matey -n matey`        | CRDs, RBAC, controller-manager Deployment, proxy Deployment + Service, optional NetworkPolicy. | The standard, supported install. Production and development.               |
| `matey install`                                     | CRDs and ClusterRole/Binding **only**. No controller-manager, no proxy.          | You are running the controller out-of-band (e.g. `matey controller-manager` from a separate workload) and want CRDs registered. |
| Hand-rolled manifests                               | Whatever you write yourself.                                                     | Not recommended; the Helm chart is the source of truth.                     |

> **`matey install` is CRDs-and-RBAC only.** It does not deploy a running
> controller. If you run `matey install` and then `matey up`, the MCPServer
> resources will be created but never reconciled into pods because nothing is
> watching them. Use Helm for any setup where you want servers to actually run.

## Why Helm Is the Supported Path

The Helm chart owns the full deployment surface: CRDs, RBAC, the
controller-manager Deployment, the proxy Deployment, the proxy Service, the
controller bootstrap ConfigMap, the proxy auth Secret, and the NetworkPolicy
resources. The chart is linted in CI (`make helm-lint`) and rendered as part
of the release flow (`make helm-template`, `make helm-test`). Hand-rolling
the equivalent YAML duplicates that surface and tends to drift.

## Migrating From `matey install`

If you previously ran `matey install` and want the full Helm-managed install:

```bash
helm install matey ./charts/matey \
  -n matey \
  --create-namespace \
  --set crds.install=false   # CRDs are already present
```

Set `crds.install=false` so Helm does not try to re-install them. You can also
just `kubectl delete clusterrole` / `clusterrolebinding` for the
`matey install`-created RBAC first, then run `helm install` without the
`--set`.

## Production Topics

These topics live in the [Helm Deployment Guide](helm.md):

- Values reference (every key in `charts/matey/values.yaml`).
- Working `helm install --set` examples.
- CRD upgrade strategy (Helm 3 does not upgrade CRDs on `helm upgrade`).
- Conversion webhook (not yet implemented — explicit future work).
- NetworkPolicy configuration (default-deny + targeted allow rules).
- Troubleshooting `helm template`, image pull, and CrashLoopBackOff scenarios.

## What This Page Used To Say

Earlier revisions of this page documented a hand-rolled manifest install,
phantom Helm values (`replicaCount`, `service.type` at top level, `ingress`,
`postgresql.*`), and an OLM install path. None of those reflect the current
chart. This page is now a pointer; the authoritative guide is
[helm.md](helm.md).
