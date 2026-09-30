#!/usr/bin/env bash
# Local-cell stand-in for the tenant-operator (replaced in build-plan Step 0.4): renders one tenant from
# deploy/tenant/tenant.yaml.tmpl + a plan file and applies it, or deletes it.
#   tenant.sh render <ref> <plan>    print manifests (images from images.lock unless PG_IMAGE/PGB_IMAGE are set)
#   tenant.sh create <ref> <plan>    apply + copy the local S3 credentials + wait until Ready
#   tenant.sh delete <ref> --yes     delete the tenant namespace (all its data)
#   tenant.sh list
# <ref> must match ^t-[a-z0-9]{8}$; <plan> is base|plus|premium. Requires: kubectl, envsubst, the local cell
# (hack/cell.sh up). create is idempotent (re-apply); delete needs --yes because it destroys data.
set -Eeuo pipefail
trap 'echo "tenant: failed at line $LINENO" >&2' ERR

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ctx="${KUBE_CONTEXT:-k3d-starter}"
# shellcheck disable=SC2016 # reason: the literal ${NAME} list is envsubst's input, it must not expand here
vars='${TENANT} ${PLAN} ${STORAGE} ${S3_BUCKET} ${S3_ENDPOINT} ${PG_IMAGE} ${PGB_IMAGE} ${PG_CPU_REQ} ${PG_CPU_LIM} ${PG_MEM} ${MAX_CONNECTIONS} ${SHARED_BUFFERS} ${ARCHIVE_TIMEOUT} ${POOL_MAX_CLIENTS} ${POOL_SIZE} ${SIDECAR_CPU_REQ} ${SIDECAR_CPU_LIM} ${SIDECAR_MEM} ${PGB_CPU_REQ} ${PGB_CPU_LIM} ${PGB_MEM}'

die() { echo "tenant: $*" >&2; exit 2; }
kc() { kubectl --context "$ctx" "$@"; }

# check_args <ref> <plan>: allow-list validation; nothing from the arguments reaches a shell or path unchecked.
check_args() {
  [[ "${1:-}" =~ ^t-[a-z0-9]{8}$ ]] || die "tenant ref must match ^t-[a-z0-9]{8}\$"
  [[ "${2:-}" =~ ^(base|plus|premium)$ ]] || die "plan must be base, plus or premium"
}

# image <prefix>: digest-pinned reference from deploy/vendor/images.lock.
image() { grep -m1 "^$1" "${root}/deploy/vendor/images.lock"; }

render() {
  check_args "$@"
  (
    set -a
    # shellcheck source=/dev/null # plan files are data in deploy/tenant/plans, validated by check_args
    source "${root}/deploy/tenant/plans/$2.env"
    set +a
    export TENANT="$1"
    export S3_BUCKET="${S3_BUCKET:-dbcloud-backups}"
    export S3_ENDPOINT="${S3_ENDPOINT:-http://seaweedfs-s3.dbcloud-system.svc:8333}"
    export PG_IMAGE="${PG_IMAGE:-$(image ghcr.io/cloudnative-pg/postgresql:)}"
    export PGB_IMAGE="${PGB_IMAGE:-$(image ghcr.io/cloudnative-pg/pgbouncer:)}"
    envsubst "$vars" <"${root}/deploy/tenant/tenant.yaml.tmpl"
  )
}

create() {
  check_args "$@"
  local s3env="${root}/.local/s3.env"
  [[ -f "$s3env" ]] || die "no ${s3env}; run hack/cell.sh up first"
  kc apply -f <(render "$1" "$2" | yq e 'select(.kind == "Namespace")') >/dev/null
  # S3 credentials are read from the file, never passed as arguments (15-security §4).
  kc -n "$1" create secret generic backup-s3 --from-env-file="$s3env" --dry-run=client -o yaml | kc apply -f - >/dev/null
  kc apply -f <(render "$1" "$2") >/dev/null
  echo "created $1 ($2)"
}

delete() {
  [[ "${1:-}" =~ ^t-[a-z0-9]{8}$ ]] || die "tenant ref must match ^t-[a-z0-9]{8}\$"
  [[ "${2:-}" == "--yes" ]] || die "delete destroys all data of $1; repeat with --yes"
  kc delete namespace "$1" --wait=true
}

list() { kc get clusters.postgresql.cnpg.io -A -o custom-columns=TENANT:.metadata.namespace,PHASE:.status.phase,READY:.status.readyInstances; }

main() {
  local cmd="${1:-}"
  shift || true
  case "$cmd" in
    render|create|delete) "$cmd" "$@" ;;
    list) list ;;
    *) die "usage: tenant.sh render|create <ref> <plan> | delete <ref> --yes | list" ;;
  esac
}

main "$@"
