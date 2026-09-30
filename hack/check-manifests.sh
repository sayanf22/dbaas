#!/usr/bin/env bash
# Validates our Kubernetes manifests with kubeconform against the pinned Kubernetes minor and the CRD schemas
# of the vendored operators (60-infrastructure.md "manifests pass kubeconform"). Covers deploy/local and the
# tenant template rendered with example values. Requires: kubeconform, envsubst, network access for schemas
# (cached in ~/.cache/kubeconform). No inputs. Read-only apart from the cache, safe to re-run.
set -Eeuo pipefail
trap 'echo "check-manifests: failed at line $LINENO" >&2' ERR

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
k8s_version="1.36.0"   # matches the pinned K3s v1.36 minor in tools.versions
crd_schemas='https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json'

# Temporary directory for rendered manifests; global so the EXIT trap can still see it after main returns.
work=""

main() {
  local cache="${HOME}/.cache/kubeconform"
  work="$(mktemp -d)"
  trap 'rm -rf "$work"' EXIT
  mkdir -p "$cache"
  bash "${root}/hack/render-tenant-example.sh" >"${work}/tenant.yaml"
  sed 's#SEAWEEDFS_IMAGE#docker.io/chrislusf/seaweedfs:4.48#' "${root}/deploy/local/seaweedfs.yaml" >"${work}/seaweedfs.yaml"
  kubeconform -strict -summary -cache "$cache" -kubernetes-version "$k8s_version" \
    -schema-location default -schema-location "$crd_schemas" \
    "${work}/tenant.yaml" "${work}/seaweedfs.yaml"
}

main "$@"
