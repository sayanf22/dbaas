#!/usr/bin/env bash
# Local cell lifecycle (plan/04 Step 0.3): k3d cell "local" in the starter shape (ADR-025: server-0 = ctl-1,
# the K3s server running the platform; agent-0 = node-1, the tainted tenant node) or the HA shape
# "reference" (3 servers + 1 agent), the local OCI registry, and one LVM volume group per node.
#   cell.sh up [starter|reference]   create (or keep) the cell, then hand over to hack/cell-flux.sh
#   cell.sh down                     delete the cell, its registry, volume groups and loop files
#   cell.sh status
# Requires: docker, k3d, kubectl, sudo rule from `hack/local-host.sh setup`. Safe to re-run: `up` keeps an
# existing cell and only re-applies manifests; the profile is fixed when the cell is created.
set -Eeuo pipefail
trap 'echo "cell: failed at line $LINENO" >&2' ERR

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
vendor="${root}/deploy/vendor"
cluster=local
ctx="k3d-${cluster}"
registry=dbcloud-registry
host_tool=/usr/local/sbin/dbcloud-local-host
export REPO="$root" LOCAL_STATE=/var/lib/dbcloud-local

# die <message>: prints the message to stderr and exits 2.
die() { echo "cell: $*" >&2; exit 2; }

# image <prefix>: digest-pinned reference from images.lock.
image() { grep -m1 "^$1" "${vendor}/images.lock"; }

# lvm_nodes <profile>: k3d container names of every node of the profile. Each needs a VG + lvmd: tenant
# nodes for tenant volumes, control nodes for the platform PVCs (VictoriaMetrics, local S3) on tenant-local.
lvm_nodes() {
  case "$1" in
    starter) echo "k3d-${cluster}-server-0 k3d-${cluster}-agent-0" ;;
    reference) echo "k3d-${cluster}-server-0 k3d-${cluster}-server-1 k3d-${cluster}-server-2 k3d-${cluster}-agent-0" ;;
    *) die "profile must be starter or reference" ;;
  esac
}

# exists: true when the k3d cluster "local" already exists.
exists() { k3d cluster list -o json | jq -e --arg n "$cluster" '.[] | select(.name == $n)' >/dev/null; }

# up [profile]: verifies vendored files, creates the registry, VGs and cell if missing, then syncs Flux.
up() {
  local profile="${1:-starter}" nodes
  nodes="$(lvm_nodes "$profile")"
  [[ -x "$host_tool" ]] || die "run once: wsl -d Ubuntu-24.04 -u root -- bash hack/local-host.sh setup"
  (cd "$vendor" && sha256sum -c --quiet SHA256SUMS) || die "vendored files don't match deploy/vendor/SHA256SUMS"

  if ! k3d registry list -o json | jq -e --arg n "k3d-${registry}" '.[] | select(.name == $n)' >/dev/null; then
    # Bound to localhost: Flux artifacts are pushed from this PC only.
    k3d registry create "$registry" --port 127.0.0.1:5050 --image "$(image docker.io/library/registry:)" --no-help
  fi
  if ! exists; then
    # shellcheck disable=SC2086 # reason: $nodes is a list of validated container names
    sudo -n "$host_tool" lvm-up $nodes
    K3S_IMAGE="$(image docker.io/rancher/k3s:)" k3d cluster create --config "${root}/deploy/local/k3d-${profile}.yaml"
  else
    echo "cell ${cluster} exists; keeping it (delete with 'cell.sh down' to change the profile)"
  fi
  kubectl --context "$ctx" wait --for=condition=Ready node --all --timeout=300s
  bash "${root}/hack/cell-flux.sh"
}

# down: deletes the cell, its registry and every node's VG (all local tenant data). Safe if already gone.
down() {
  local nodes
  nodes="$(k3d node list -o json 2>/dev/null | jq -r --arg c "$cluster" '.[] | select(.runtimeLabels["k3d.cluster"] == $c) | .name' | grep -E 'server|agent' || true)"
  # Registry first: while it is attached, k3d can't remove the cluster's network.
  k3d registry delete "k3d-${registry}" 2>/dev/null || true
  k3d cluster delete "$cluster"
  # Remove the VGs of every node of the larger profile, a superset of the starter's, whichever profile ran.
  # shellcheck disable=SC2046 # reason: fixed, validated container names
  sudo -n "$host_tool" lvm-down $(lvm_nodes reference)
  [[ -z "$nodes" ]] || echo "removed nodes: $(tr '\n' ' ' <<<"$nodes")"
}

# status: read-only view of nodes, Flux objects, CNPG clusters and the host LVM state.
status() {
  kubectl --context "$ctx" get nodes -o wide
  kubectl --context "$ctx" get kustomizations.kustomize.toolkit.fluxcd.io,helmreleases.helm.toolkit.fluxcd.io -A 2>/dev/null || true
  kubectl --context "$ctx" get clusters.postgresql.cnpg.io -A 2>/dev/null || true
  sudo -n "$host_tool" status
}

# main <command> [args]: dispatches to up, down or status.
main() {
  local cmd="${1:-}"
  shift || true
  case "$cmd" in
    up) up "$@" ;;
    down) down ;;
    status) status ;;
    *) die "usage: cell.sh up [starter|reference] | down | status" ;;
  esac
}

main "$@"
