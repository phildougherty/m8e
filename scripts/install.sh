#!/usr/bin/env bash
#
# Matey (m8e) quick-install script.
#
# Two paths, auto-detected:
#   * No reachable cluster  -> installs k3s, then deploys m8e into it.
#   * A reachable cluster   -> deploys m8e into your current kube-context.
#
# Deployment is always via the Helm chart in charts/matey, which is the only
# path that yields a *running* m8e (CRDs + controller-manager + proxy + RBAC).
# `matey install` alone installs CRDs/RBAC but leaves no controller running.
#
# Usage:
#   ./scripts/install.sh [options]
#   curl -sSL https://raw.githubusercontent.com/phildougherty/m8e/main/scripts/install.sh | bash
#
# Options:
#   --k3s                Force a k3s install even if a cluster is reachable.
#   --reinstall-k3s      Wipe and reinstall k3s — use when an existing k3s is
#                        broken (e.g. it crash-loops because the host's network
#                        changed since it was first installed).
#   --existing           Force using the current kube-context; never install k3s.
#   --namespace NAME     Namespace to install into (default: matey).
#   --release NAME       Helm release name (default: matey).
#   --chart PATH         Path to the chart (default: <repo>/charts/matey).
#   --image REPO:TAG     Override the m8e image. Default: build locally for
#                        k3s, or ghcr.io/phildougherty/matey:<appVersion> for
#                        an existing cluster.
#   --values FILE        Extra Helm values file (repeatable).
#   --build-image        Build the m8e image locally and load it (default on
#                        k3s, off for existing clusters).
#   --no-build-image     Never build locally; rely on a pullable image.
#   --skip-wait          Don't wait for the deployments to become ready.
#   --dry-run            Render and print what would happen; change nothing.
#   --uninstall          Remove the Helm release (and the namespace).
#   -h, --help           This help.
#
set -euo pipefail

# --------------------------------------------------------------------------
# Configuration & argument parsing
# --------------------------------------------------------------------------
REPO_URL="https://github.com/phildougherty/m8e.git"
RAW_REPO="https://github.com/phildougherty/m8e"

CLUSTER_MODE="auto"        # auto | k3s | existing
REINSTALL_K3S="false"      # --reinstall-k3s: wipe & reinstall a stale k3s
NAMESPACE="matey"
RELEASE="matey"
CHART_PATH=""
IMAGE_OVERRIDE=""
BUILD_IMAGE="auto"         # auto | yes | no
SKIP_WAIT="false"
DRY_RUN="false"
UNINSTALL="false"
EXTRA_VALUES=()

LOCAL_IMAGE="matey:local"  # tag used when building locally

while [[ $# -gt 0 ]]; do
  case "$1" in
    --k3s)            CLUSTER_MODE="k3s"; shift ;;
    --reinstall-k3s)  CLUSTER_MODE="k3s"; REINSTALL_K3S="true"; shift ;;
    --existing)       CLUSTER_MODE="existing"; shift ;;
    --namespace)      NAMESPACE="$2"; shift 2 ;;
    --release)        RELEASE="$2"; shift 2 ;;
    --chart)          CHART_PATH="$2"; shift 2 ;;
    --image)          IMAGE_OVERRIDE="$2"; shift 2 ;;
    --values)         EXTRA_VALUES+=("$2"); shift 2 ;;
    --build-image)    BUILD_IMAGE="yes"; shift ;;
    --no-build-image) BUILD_IMAGE="no"; shift ;;
    --skip-wait)      SKIP_WAIT="true"; shift ;;
    --dry-run)        DRY_RUN="true"; shift ;;
    --uninstall)      UNINSTALL="true"; shift ;;
    -h|--help)        sed -n '2,36p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $1 (try --help)" >&2; exit 2 ;;
  esac
done

# --------------------------------------------------------------------------
# Output helpers
# --------------------------------------------------------------------------
if [[ -t 1 ]]; then
  C_RESET=$'\033[0m'; C_BOLD=$'\033[1m'; C_BLUE=$'\033[34m'
  C_GREEN=$'\033[32m'; C_YELLOW=$'\033[33m'; C_RED=$'\033[31m'
else
  C_RESET=""; C_BOLD=""; C_BLUE=""; C_GREEN=""; C_YELLOW=""; C_RED=""
fi
step()  { echo "${C_BLUE}${C_BOLD}==>${C_RESET} ${C_BOLD}$*${C_RESET}"; }
info()  { echo "    $*"; }
ok()    { echo "    ${C_GREEN}✓${C_RESET} $*"; }
warn()  { echo "    ${C_YELLOW}!${C_RESET} $*" >&2; }
die()   { echo "${C_RED}${C_BOLD}error:${C_RESET} $*" >&2; exit 1; }
run()   { if [[ "$DRY_RUN" == "true" ]]; then echo "    [dry-run] $*"; else "$@"; fi; }

have()  { command -v "$1" >/dev/null 2>&1; }
need()  { have "$1" || die "required command not found: $1"; }

# --------------------------------------------------------------------------
# Locate (or fetch) the repo so we have the chart and source to build from.
# This makes `curl ... | bash` work as well as running from a clone.
# --------------------------------------------------------------------------
locate_repo() {
  local here
  here="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")/.." 2>/dev/null && pwd || true)"
  if [[ -n "$here" && -f "$here/go.mod" && -d "$here/charts/matey" ]]; then
    REPO_DIR="$here"
    info "using repository at $REPO_DIR"
    return
  fi
  # Piped execution (curl | bash): clone into a cache dir.
  need git
  REPO_DIR="${MATEY_CACHE_DIR:-$HOME/.cache/matey/src}"
  if [[ -d "$REPO_DIR/.git" ]]; then
    info "updating cached repository at $REPO_DIR"
    run git -C "$REPO_DIR" pull --ff-only
  else
    info "cloning $REPO_URL -> $REPO_DIR"
    run git clone --depth 1 "$REPO_URL" "$REPO_DIR"
  fi
}

# --------------------------------------------------------------------------
# Cluster: install k3s, or confirm an existing cluster is reachable.
# --------------------------------------------------------------------------
cluster_reachable() {
  have kubectl && kubectl cluster-info >/dev/null 2>&1
}

# ensure_k3s_running starts the k3s systemd service if it is installed but
# not active. "k3s is installed" does NOT imply "k3s is running" — the service
# can be stopped, or left disabled so it never starts on boot.
ensure_k3s_running() {
  if ! have systemctl; then
    warn "systemctl not found; assuming k3s is managed another way"
    return 0
  fi
  if systemctl is-active --quiet k3s; then
    ok "k3s service is active"
    return 0
  fi
  info "k3s service is not running — starting it (may prompt for sudo)"
  # enable --now both starts it now and makes it survive a reboot.
  run sudo systemctl enable --now k3s
  ok "k3s service started"
}

# k3s_crash_help prints the journal tail plus remediation, then exits. Called
# when k3s is up as a unit but the process keeps dying — most often a stale
# install whose host networking changed since it was first set up.
k3s_crash_help() {
  echo
  if have systemctl; then
    sudo journalctl -u k3s -n 20 --no-pager 2>/dev/null | sed 's/^/    /' || true
  fi
  die "k3s is installed but crash-looping. This usually means a stale k3s whose
  host network changed since it was installed. Reinstall it cleanly:

    sudo /usr/local/bin/k3s-uninstall.sh   &&   $0 --k3s

  or just re-run this installer with:  --reinstall-k3s"
}

install_k3s() {
  step "Installing k3s"

  if [[ "$REINSTALL_K3S" == "true" ]] && have k3s; then
    info "--reinstall-k3s: removing the existing k3s install first"
    if [[ -x /usr/local/bin/k3s-uninstall.sh ]]; then
      run sudo /usr/local/bin/k3s-uninstall.sh
    else
      warn "k3s-uninstall.sh not found; continuing with a fresh install on top"
    fi
  fi

  if have k3s && [[ -f /etc/rancher/k3s/k3s.yaml && "$REINSTALL_K3S" != "true" ]]; then
    ok "k3s already installed"
  else
    need curl
    # --write-kubeconfig-mode 644 so a non-root user can read the kubeconfig.
    run sh -c 'curl -sfL https://get.k3s.io | INSTALL_K3S_EXEC="--write-kubeconfig-mode 644" sh -'
    ok "k3s installed"
  fi

  # Installed != running. Make sure the service is up before we wait on the API.
  ensure_k3s_running

  export KUBECONFIG="/etc/rancher/k3s/k3s.yaml"
  info "KUBECONFIG=$KUBECONFIG"
  if [[ "$DRY_RUN" != "true" ]]; then
    # Use `k3s kubectl` here: k3s always ships it, whereas a standalone
    # kubectl may not be on PATH yet on a fresh box.
    local tries=0
    until k3s kubectl get nodes >/dev/null 2>&1; do
      # k3s.service has Restart=always, so a crash-looping k3s never shows as
      # "failed" — it just keeps restarting. Watch NRestarts instead: if it
      # climbs, the process is dying on startup and waiting 120s is pointless.
      if have systemctl; then
        local restarts
        restarts="$(systemctl show -p NRestarts --value k3s 2>/dev/null || echo 0)"
        if [[ "${restarts:-0}" =~ ^[0-9]+$ && "${restarts:-0}" -ge 3 ]]; then
          k3s_crash_help
        fi
      fi
      tries=$((tries + 1))
      if [[ $tries -gt 60 ]]; then
        die "k3s did not become ready within ~120s — check: sudo journalctl -u k3s -n 50"
      fi
      sleep 2
    done
    ok "k3s API server is ready"
  fi
}

ensure_cluster() {
  step "Resolving target cluster"
  case "$CLUSTER_MODE" in
    existing)
      cluster_reachable || die "no reachable cluster for --existing (check kubectl context)"
      ok "using current kube-context: $(kubectl config current-context 2>/dev/null || echo unknown)"
      USING_K3S="false"
      ;;
    k3s)
      install_k3s
      USING_K3S="true"
      ;;
    auto)
      # Prefer a local k3s if one is installed. Its kubeconfig at
      # /etc/rancher/k3s/k3s.yaml is authoritative; an ambient ~/.kube/config
      # may be stale — e.g. left pointing at a previous k3s install whose CA
      # was regenerated on reinstall, which then fails TLS verification under
      # helm even though `kubectl cluster-info` appeared to "work".
      if [[ -f /etc/rancher/k3s/k3s.yaml ]]; then
        info "local k3s detected — using it (pass --existing to target your kube-context instead)"
        install_k3s   # idempotent: detects the existing install, just ensures it is running
        USING_K3S="true"
      elif cluster_reachable; then
        ok "found a reachable cluster: $(kubectl config current-context 2>/dev/null || echo unknown)"
        info "(pass --k3s to install a fresh k3s instead)"
        USING_K3S="false"
      else
        info "no reachable cluster — installing k3s"
        install_k3s
        USING_K3S="true"
      fi
      ;;
  esac
}

# --------------------------------------------------------------------------
# Image: for k3s we build locally and import into k3s's containerd, because
# the published image may not be available. For an existing cluster we trust
# the chart's default pullable image unless told otherwise.
# --------------------------------------------------------------------------
resolve_build_decision() {
  if [[ "$BUILD_IMAGE" == "auto" ]]; then
    if [[ "$USING_K3S" == "true" && -z "$IMAGE_OVERRIDE" ]]; then
      BUILD_IMAGE="yes"
    else
      BUILD_IMAGE="no"
    fi
  fi
}

build_and_load_image() {
  [[ "$BUILD_IMAGE" == "yes" ]] || return 0
  step "Building the m8e image locally"
  need docker
  run docker build -t "$LOCAL_IMAGE" "$REPO_DIR"
  ok "built $LOCAL_IMAGE"

  if [[ "$USING_K3S" == "true" ]]; then
    step "Importing image into k3s containerd"
    # k3s does not share docker's image store, so docker save | ctr import.
    # CRITICAL: -n k8s.io. The kubelet pulls images from the "k8s.io"
    # containerd namespace; `k3s ctr images import` defaults to the "default"
    # namespace, so without -n k8s.io the image lands where the kubelet never
    # looks and pods fail later with ErrImageNeverPull.
    run sh -c "docker save '$LOCAL_IMAGE' | sudo k3s ctr -n k8s.io images import -"
    # Verify it actually landed — a silent import failure otherwise only
    # surfaces minutes later as ErrImageNeverPull once pods are scheduled.
    if [[ "$DRY_RUN" != "true" ]]; then
      if sudo k3s ctr -n k8s.io images ls -q 2>/dev/null | grep -q "$LOCAL_IMAGE"; then
        ok "imported $LOCAL_IMAGE into k3s (namespace k8s.io)"
      else
        die "image import reported success but $LOCAL_IMAGE is not in k3s's
  k8s.io namespace. Check: sudo k3s ctr -n k8s.io images ls | grep matey"
      fi
    fi
  else
    warn "built $LOCAL_IMAGE locally but the target is not k3s;"
    warn "the cluster nodes must be able to pull this image, or push it to a registry."
  fi
  IMAGE_OVERRIDE="$LOCAL_IMAGE"
}

# --------------------------------------------------------------------------
# Helm: ensure helm exists, then deploy the chart.
# --------------------------------------------------------------------------
ensure_helm() {
  if have helm; then
    return 0
  fi
  step "Installing Helm"
  need curl
  run sh -c 'curl -fsSL https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash'
  have helm || [[ "$DRY_RUN" == "true" ]] || die "helm install failed"
  ok "helm installed"
}

deploy() {
  step "Deploying m8e via Helm"
  local chart="${CHART_PATH:-$REPO_DIR/charts/matey}"
  [[ -d "$chart" || "$DRY_RUN" == "true" ]] || die "chart not found at $chart"

  local args=(upgrade --install "$RELEASE" "$chart"
    --namespace "$NAMESPACE" --create-namespace)

  if [[ -n "$IMAGE_OVERRIDE" ]]; then
    local repo="${IMAGE_OVERRIDE%:*}" tag="${IMAGE_OVERRIDE##*:}"
    [[ "$repo" == "$tag" ]] && tag="latest"
    args+=(--set "image.repository=$repo" --set "image.tag=$tag")
    # A locally-built/imported image must never be re-pulled.
    if [[ "$IMAGE_OVERRIDE" == "$LOCAL_IMAGE" ]]; then
      args+=(--set "image.pullPolicy=Never")
    fi
    info "image: $repo:$tag"
  fi

  for v in "${EXTRA_VALUES[@]:-}"; do
    [[ -n "$v" ]] && args+=(--values "$v")
  done

  if [[ "$SKIP_WAIT" != "true" ]]; then
    args+=(--wait --timeout 5m)
  fi
  if [[ "$DRY_RUN" == "true" ]]; then
    args+=(--dry-run)
  fi

  run helm "${args[@]}"
  ok "helm release '$RELEASE' applied to namespace '$NAMESPACE'"
}

wait_for_rollout() {
  [[ "$SKIP_WAIT" == "true" || "$DRY_RUN" == "true" ]] && return 0
  step "Waiting for m8e to become ready"
  # --wait above already gates on this, but surface per-deployment status.
  local deploys
  deploys="$(kubectl -n "$NAMESPACE" get deploy -o name 2>/dev/null || true)"
  if [[ -z "$deploys" ]]; then
    warn "no deployments found in namespace $NAMESPACE yet"
    return 0
  fi
  while read -r d; do
    [[ -z "$d" ]] && continue
    kubectl -n "$NAMESPACE" rollout status "$d" --timeout=2m && ok "$d ready"
  done <<< "$deploys"
}

# --------------------------------------------------------------------------
# Uninstall
# --------------------------------------------------------------------------
do_uninstall() {
  step "Uninstalling m8e"
  need helm
  run helm uninstall "$RELEASE" --namespace "$NAMESPACE" || warn "release not found"
  # CRDs installed by the chart's crds/ are intentionally left — deleting a
  # CRD deletes every custom resource of that kind. Remove them explicitly if
  # you really want a clean slate:
  info "note: CRDs are left in place. To remove them and ALL m8e resources:"
  info "  kubectl delete crd mcpservers.mcp.matey.ai mcpmemories.mcp.matey.ai \\"
  info "    mcpproxies.mcp.matey.ai mcppostgres.mcp.matey.ai mcptaskschedulers.mcp.matey.ai"
  run kubectl delete namespace "$NAMESPACE" --ignore-not-found
  ok "uninstalled"
}

# --------------------------------------------------------------------------
# Summary
# --------------------------------------------------------------------------
print_next_steps() {
  echo
  step "m8e is installed"
  if [[ "$USING_K3S" == "true" ]]; then
    info "k3s kubeconfig: ${C_BOLD}export KUBECONFIG=/etc/rancher/k3s/k3s.yaml${C_RESET}"
  fi
  info "Check status:   ${C_BOLD}kubectl -n $NAMESPACE get pods${C_RESET}"
  info "Controller logs:${C_BOLD} kubectl -n $NAMESPACE logs deploy/${RELEASE}-controller-manager${C_RESET}"
  info "Build the CLI:  ${C_BOLD}cd $REPO_DIR && make build${C_RESET}  (then ./build/matey ps)"
  info "Define servers in matey.yaml, then: ${C_BOLD}matey up${C_RESET}"
  echo
}

# --------------------------------------------------------------------------
# Main
# --------------------------------------------------------------------------
main() {
  echo "${C_BOLD}Matey (m8e) installer${C_RESET}"
  echo

  step "Preflight"
  # Note: kubectl is NOT required here. On a fresh box with no cluster, the
  # k3s install provides kubectl — requiring it up front would wrongly abort
  # the very path that installs it. It is checked after ensure_cluster.
  locate_repo
  ok "preflight passed"

  if [[ "$UNINSTALL" == "true" ]]; then
    # Uninstall still needs a cluster context; honour --existing/k3s kubeconfig.
    [[ "$CLUSTER_MODE" == "k3s" ]] && export KUBECONFIG="/etc/rancher/k3s/k3s.yaml"
    need kubectl
    do_uninstall
    exit 0
  fi

  ensure_cluster
  # By now kubectl exists: either it was already present (existing cluster) or
  # the k3s install put it on PATH.
  need kubectl
  resolve_build_decision
  ensure_helm
  build_and_load_image
  deploy
  wait_for_rollout
  print_next_steps
}

main
