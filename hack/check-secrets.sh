#!/usr/bin/env bash
# Fails when a secret could reach git or a browser bundle (60-infrastructure.md "Secrets", 50-web.md):
#   1. a Kubernetes Secret under deploy/ (vendored upstream manifests excluded) without SOPS metadata;
#   2. a NEXT_PUBLIC_* variable name in web/ that looks secret (these are baked into public bundles).
# Requires: yq (mikefarah v4), grep. No inputs. Read-only, safe to re-run. Used by CI and the pre-commit hook.
set -Eeuo pipefail
trap 'echo "check-secrets: failed at line $LINENO" >&2' ERR

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# unencrypted_secrets: prints "<file>: <kind>/<name>" for each Secret without a `sops` block.
unencrypted_secrets() {
  local file
  while IFS= read -r -d '' file; do
    # yq evaluates every YAML document in the file; FILE is passed through the environment, not spliced in.
    FILE="${file#"$root"/}" yq e -N 'select(.kind == "Secret" and (has("sops") | not)) | strenv(FILE) + ": Secret/" + (.metadata.name // "?")' "$file"
  done < <(find "${root}/deploy" -path "${root}/deploy/vendor" -prune -o -type f \( -name '*.yaml' -o -name '*.yml' \) -print0)
}

# public_secret_names: NEXT_PUBLIC_ names containing words that mark server-side secrets.
public_secret_names() {
  [[ -d "${root}/web" ]] || return 0
  grep -rhoE 'NEXT_PUBLIC_[A-Z0-9_]+' "${root}/web" --include='*.ts' --include='*.tsx' --include='*.mjs' \
    --include='*.js' --include='.env*' --exclude-dir=node_modules --exclude-dir=.next --exclude-dir=out \
    | sort -u | grep -E 'SECRET|TOKEN|PASSWORD|PRIVATE|SERVICE_ROLE|_DSN|DATABASE_URL' || true
}

main() {
  local secrets names status=0
  secrets="$(unencrypted_secrets)"
  if [[ -n "$secrets" ]]; then
    printf 'check-secrets: unencrypted Kubernetes Secret (encrypt with sops):\n%s\n' "$secrets" >&2
    status=1
  fi
  names="$(public_secret_names)"
  if [[ -n "$names" ]]; then
    printf 'check-secrets: secret-looking NEXT_PUBLIC_ variables (public in the bundle):\n%s\n' "$names" >&2
    status=1
  fi
  [[ "$status" -eq 0 ]] && echo "check-secrets: ok"
  return "$status"
}

main "$@"
