#!/usr/bin/env bash
# Validates our Kubernetes manifests with kubeconform against the pinned Kubernetes minor and the CRD schemas
# of the vendored operators (60-infrastructure.md "manifests pass kubeconform"). Covers every Flux layer under
# deploy/flux (rendered with kustomize, as kustomize-controller does), the local S3 and Flux sync objects, and
# the tenant template rendered with example values. Not covered: the git-ignored, per-developer
# deploy/local/s3/secret.sops.yaml, and K3s/k3d config files, which aren't Kubernetes API objects.
# Requires: kubeconform, kubectl (for `kubectl kustomize`), envsubst, network access for schemas (cached in
# ~/.cache/kubeconform). No inputs. Read-only apart from the cache, safe to re-run.
set -Eeuo pipefail
trap 'echo "check-manifests: failed at line $LINENO" >&2' ERR

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
k8s_version="1.36.0"   # matches the pinned K3s v1.36 minor in tools.versions
crd_schemas='https://raw.githubusercontent.com/datreeio/CRDs-catalog/main/{{.Group}}/{{.ResourceKind}}_{{.ResourceAPIVersion}}.json'
# Kustomize roots that Flux applies (deploy/flux/clusters/local/*.yaml). They reference deploy/vendor, which
# is outside each root, hence LoadRestrictionsNone (kustomize-controller builds the same way).
layers=(flux/system flux/infrastructure/cert-manager flux/infrastructure/controllers flux/infrastructure/services flux/clusters/local)

# Temporary directory for rendered manifests; global so the EXIT trap can still see it after main returns.
work=""

main() {
  local cache="${HOME}/.cache/kubeconform" layer files=()
  work="$(mktemp -d)"
  trap 'rm -rf "$work"' EXIT
  mkdir -p "$cache"
  for layer in "${layers[@]}"; do
    kubectl kustomize --load-restrictor=LoadRestrictionsNone "${root}/deploy/${layer}" >"${work}/${layer//\//-}.yaml"
    files+=("${work}/${layer//\//-}.yaml")
  done
  bash "${root}/hack/render-tenant-example.sh" >"${work}/tenant.yaml"
  files+=("${work}/tenant.yaml" "${root}/deploy/local/s3/seaweedfs.yaml" "${root}/deploy/local/flux-sync.yaml")
  # CustomResourceDefinitions are skipped: kubeconform's catalog has no schema for them at 1.36, and every CRD
  # here is a vendored upstream file pinned in deploy/vendor/SHA256SUMS that the API server validates on apply.
  kubeconform -strict -summary -cache "$cache" -kubernetes-version "$k8s_version" \
    -skip CustomResourceDefinition \
    -schema-location default -schema-location "$crd_schemas" "${files[@]}"
}

main "$@"
