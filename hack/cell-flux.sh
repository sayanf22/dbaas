#!/usr/bin/env bash
# GitOps for the local cell (plan/04 Step 0.3): installs the pinned Flux controllers, gives Flux the local age
# key, pushes deploy/ as an OCI artifact to the local registry and waits until every layer is Ready. Rerun
# after changing anything under deploy/ to roll it out the way production does (Flux, not kubectl apply).
# Requires: kubectl, flux, sops, age-keygen, openssl, jq; a running cell (hack/cell.sh up).
# Safe to re-run: every step is idempotent. Secrets are read from files, never passed as arguments.
set -Eeuo pipefail
trap 'echo "cell-flux: failed at line $LINENO" >&2' ERR

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ctx=k3d-local
state="${root}/.local"
kc() { kubectl --context "$ctx" "$@"; }
# Temporary paths, global so the EXIT trap can remove them on any exit, including a failed sops run:
# plain_secret holds the unencrypted S3 identity, docker_config the empty Docker config for `flux push`.
plain_secret=""
docker_config=""
# cleanup: removes whichever temporary paths were created.
cleanup() {
  [[ -z "$plain_secret" ]] || rm -f "$plain_secret"
  [[ -z "$docker_config" ]] || rm -rf "$docker_config"
}
trap cleanup EXIT

# local_keys: the developer's age key and random local S3 identity (CSPRNG), created once in .local/.
local_keys() {
  mkdir -p "$state" && chmod 700 "$state"
  umask 077
  [[ -f "${state}/age-local.agekey" ]] || age-keygen -o "${state}/age-local.agekey" 2>/dev/null
  if [[ ! -f "${state}/s3.env" ]]; then
    printf 'ACCESS_KEY_ID=%s\nACCESS_SECRET_KEY=%s\n' "$(openssl rand -hex 10)" "$(openssl rand -hex 20)" >"${state}/s3.env"
  fi
}

# s3_secret: SeaweedFS identity as a SOPS-encrypted Secret in the artifact (git-ignored, see kustomization).
s3_secret() {
  local id secret out="${root}/deploy/local/s3/secret.sops.yaml"
  id="$(grep '^ACCESS_KEY_ID=' "${state}/s3.env" | cut -d= -f2)"
  secret="$(grep '^ACCESS_SECRET_KEY=' "${state}/s3.env" | cut -d= -f2)"
  # mktemp creates the file 0600; the umask from local_keys keeps it private too.
  plain_secret="$(mktemp)"
  # The keys reach jq through its environment ($ENV), not --arg, so they never appear in `ps` output.
  S3_ID="$id" S3_SECRET="$secret" jq -n \
    '{identities: [{name: "backup", credentials: [{accessKey: $ENV.S3_ID, secretKey: $ENV.S3_SECRET}], actions: ["Read", "Write", "List", "Tagging"]}]}' |
    kc -n dbcloud-system create secret generic seaweedfs-s3-config --from-file=s3.json=/dev/stdin --dry-run=client -o yaml >"$plain_secret"
  # Encrypt for this developer's own key; the plaintext file is removed right after (and by the EXIT trap if
  # sops fails). Runs outside the repo so sops uses only these flags, not the repository's .sops.yaml rules
  # (which are keyed by path).
  (cd "$(dirname "$plain_secret")" && sops --encrypt --age "$(age-keygen -y "${state}/age-local.agekey")" \
    --encrypted-regex '^(data|stringData)$' --input-type yaml --output-type yaml "$plain_secret") >"$out"
  rm -f "$plain_secret"
}

# main: Flux controllers -> age key Secret -> encrypted S3 identity -> OCI push -> wait for every layer.
main() {
  local_keys
  kc kustomize --load-restrictor=LoadRestrictionsNone "${root}/deploy/flux/system" | kc apply --server-side -f - >/dev/null
  kc -n flux-system rollout status deploy --timeout=300s
  kc -n flux-system create secret generic sops-age --from-file=age.agekey="${state}/age-local.agekey" \
    --dry-run=client -o yaml | kc apply -f - >/dev/null

  s3_secret
  # The local registry is anonymous. An empty Docker config keeps flux from calling the host's credential
  # helper (Docker Desktop's fails intermittently from WSL) and from sending any stored credentials to it.
  docker_config="$(mktemp -d)"
  DOCKER_CONFIG="$docker_config" flux push artifact oci://127.0.0.1:5050/dbcloud-manifests:local --path="${root}/deploy" \
    --source="$(git -C "$root" config --get remote.origin.url || echo local)" \
    --revision="$(git -C "$root" branch --show-current)@sha1:$(git -C "$root" rev-parse HEAD)" \
    --ignore-paths='vendor/images.lock,**/*.md' >/dev/null
  kc apply -f "${root}/deploy/local/flux-sync.yaml" >/dev/null

  flux --context "$ctx" reconcile source oci dbcloud -n flux-system >/dev/null
  local k
  for k in flux-system cert-manager controllers services local-s3; do
    kc -n flux-system wait "kustomization/${k}" --for=condition=Ready --timeout=900s >/dev/null
    echo "ready: ${k}"
  done
  # Backup bucket; `weed shell` talks to the in-process master. Creating an existing bucket is a no-op.
  printf 's3.bucket.create -name dbcloud-backups\n' | kc -n dbcloud-system exec -i deploy/seaweedfs -- weed shell >/dev/null 2>&1 || true
  echo "local cell ready (context ${ctx})"
}

main "$@"
