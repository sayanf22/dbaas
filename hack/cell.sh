#!/usr/bin/env bash
# Creates or deletes the local starter cell: k3d (one K3s server on embedded etcd) + cert-manager + CloudNativePG
# + Barman Cloud plugin + SeaweedFS as local S3. Everything comes from deploy/vendor (pinned, checksummed).
# Requires: docker, k3d, kubectl, openssl, jq. Usage: cell.sh up|down|status. Safe to re-run: `up` skips an
# existing cluster and re-applies manifests (server-side apply). S3 keys are random and kept in .local/ (git-ignored).
set -Eeuo pipefail
trap 'echo "cell: failed at line $LINENO" >&2' ERR

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
vendor="${root}/deploy/vendor"
name="starter"
ctx="k3d-${name}"
state="${root}/.local"

load_versions() {
  local key value
  while IFS='=' read -r key value; do
    [[ -z "$key" || "$key" == \#* ]] && continue
    printf -v "$key" '%s' "${value%$'\r'}"
  done <"${root}/tools.versions"
}

# image <repo-prefix>: the digest-pinned reference from images.lock.
image() { grep -m1 "^$1" "${vendor}/images.lock"; }

kc() { kubectl --context "$ctx" "$@"; }

up() {
  (cd "$vendor" && sha256sum -c --quiet SHA256SUMS)          # refuse tampered or partial manifests
  if ! k3d cluster list -o json | jq -e --arg n "$name" '.[] | select(.name == $n)' >/dev/null; then
    k3d cluster create --config "${root}/deploy/local/k3d-starter.yaml" --image "$(image docker.io/rancher/k3s:)"
  fi
  kc wait --for=condition=Ready node --all --timeout=180s

  kc apply --server-side -f "${vendor}/cert-manager/${CERT_MANAGER}/cert-manager.yaml" >/dev/null
  kc -n cert-manager rollout status deploy --timeout=300s
  kc apply --server-side -f "${vendor}/cnpg/${CNPG}/cnpg-${CNPG}.yaml" >/dev/null
  kc -n cnpg-system rollout status deploy/cnpg-controller-manager --timeout=300s
  kc apply --server-side -f "${vendor}/barman-cloud/${BARMAN_PLUGIN}/manifest.yaml" >/dev/null
  kc -n cnpg-system rollout status deploy/barman-cloud --timeout=300s

  s3_keys
  kc apply -f <(sed "s#SEAWEEDFS_IMAGE#$(image docker.io/chrislusf/seaweedfs:)#" "${root}/deploy/local/seaweedfs.yaml") >/dev/null
  kc -n dbcloud-system create secret generic seaweedfs-s3-config --from-file=s3.json="${state}/s3.json" \
    --dry-run=client -o yaml | kc apply -f - >/dev/null
  kc -n dbcloud-system rollout status deploy/seaweedfs --timeout=300s
  # Bucket for backups; `weed shell` talks to the in-process master. Re-creating an existing bucket is a no-op.
  printf 's3.bucket.create -name dbcloud-backups\n' | kc -n dbcloud-system exec -i deploy/seaweedfs -- weed shell >/dev/null 2>&1 || true
  echo "starter cell ready (context ${ctx})"
}

# s3_keys: creates the local S3 identity once (CSPRNG, 160/320 bits) and keeps it in .local/.
s3_keys() {
  mkdir -p "$state" && chmod 700 "$state"
  [[ -f "${state}/s3.env" ]] && return
  local id secret
  id="$(openssl rand -hex 10)"; secret="$(openssl rand -hex 20)"
  umask 077
  printf 'ACCESS_KEY_ID=%s\nACCESS_SECRET_KEY=%s\n' "$id" "$secret" >"${state}/s3.env"
  jq -n --arg id "$id" --arg s "$secret" \
    '{identities: [{name: "backup", credentials: [{accessKey: $id, secretKey: $s}], actions: ["Read", "Write", "List", "Tagging"]}]}' \
    >"${state}/s3.json"
}

down() { k3d cluster delete "$name"; }

status() {
  kc get nodes -o wide
  kc get clusters.postgresql.cnpg.io -A 2>/dev/null || true
}

main() {
  load_versions
  case "${1:-}" in
    up|down|status) "$1" ;;
    *) echo "usage: cell.sh up|down|status" >&2; return 2 ;;
  esac
}

main "$@"
