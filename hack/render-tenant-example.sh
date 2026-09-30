#!/usr/bin/env bash
# Renders the tenant template for every plan with example values to stdout, so CI can validate it with
# kubeconform without a cluster or images.lock. The values are fakes, never deployed.
# Requires: envsubst, yq (via hack/tenant.sh). No inputs. Read-only, safe to re-run.
set -Eeuo pipefail
trap 'echo "render-tenant-example: failed at line $LINENO" >&2' ERR

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

main() {
  local plan
  export S3_BUCKET="example-backups" S3_ENDPOINT="http://s3.example.invalid:8333"
  export PG_IMAGE="ghcr.io/cloudnative-pg/postgresql:18.6-standard-trixie"
  export PGB_IMAGE="ghcr.io/cloudnative-pg/pgbouncer:1.26.0"
  for plan in base plus premium; do
    bash "${root}/hack/tenant.sh" render "t-example${plan:0:1}" "$plan"
    echo "---"
  done
}

main "$@"
