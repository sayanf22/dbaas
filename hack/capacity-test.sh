#!/usr/bin/env bash
# Capacity and isolation test on the local cell (plan/03 §5-§6, ADR-025): creates N tenants of one plan,
# waits until all are Ready, then for each: connects through its PgBouncer with the generated `app` role,
# writes a row, checks WAL archiving to S3; checks that tenant 1 can't reach tenant 2; prints measured memory.
# Usage: capacity-test.sh <count 1-50> <plan> [--keep]. Requires: kubectl, psql, jq, the local cell.
# Creates tenants t-cap000NN (idempotent); deletes them and their local S3 backups at the end unless --keep.
set -Eeuo pipefail
trap 'echo "capacity-test: failed at line $LINENO" >&2' ERR

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ctx="${KUBE_CONTEXT:-k3d-local}"
# VictoriaMetrics' HTTP API through the Kubernetes API server's service proxy.
vm=/api/v1/namespaces/observability/services/victoria-metrics-victoria-metrics-single-server:8428/proxy
kc() { kubectl --context "$ctx" "$@"; }

# probe <ns>: SQL round trip through the tenant's pooler as the tenant's own role; password via env, not args.
probe() {
  local ns="$1" port="$2" pw pf rc=0
  pw="$(kc -n "$ns" get secret db-app -o jsonpath='{.data.password}' | base64 -d)"
  kc -n "$ns" port-forward svc/pooler "${port}:5432" >/dev/null 2>&1 & pf=$!
  sleep 2
  PGPASSWORD="$pw" PGCONNECT_TIMEOUT=5 psql -qtA "host=127.0.0.1 port=${port} dbname=app user=app sslmode=require" \
    -c "create table if not exists probe(at timestamptz default now()); insert into probe default values; select count(*) from probe" >/dev/null || rc=1
  kill "$pf" 2>/dev/null; wait "$pf" 2>/dev/null || true
  return "$rc"
}

main() {
  local n="${1:-}" plan="${2:-}" keep="${3:-}" i ns ok=0 archived=0
  [[ "$n" =~ ^[0-9]+$ && "$n" -ge 1 && "$n" -le 50 ]] || { echo "usage: capacity-test.sh <1-50> <plan> [--keep]" >&2; return 2; }
  local refs=()
  for ((i = 1; i <= n; i++)); do refs+=("$(printf 't-cap%05d' "$i")"); done

  local start=$SECONDS
  for ns in "${refs[@]}"; do bash "${root}/hack/tenant.sh" create "$ns" "$plan"; done
  for ns in "${refs[@]}"; do kc -n "$ns" wait cluster/db --for=condition=Ready --timeout=1200s >/dev/null; done
  for ns in "${refs[@]}"; do kc -n "$ns" rollout status deploy/pooler --timeout=300s >/dev/null; done
  echo "all ${n} ${plan} tenants Ready after $((SECONDS - start)) s"

  i=15432
  for ns in "${refs[@]}"; do
    if probe "$ns" "$i"; then ok=$((ok + 1)); else echo "connect FAILED: $ns"; fi
    i=$((i + 1))
    [[ "$(kc -n "$ns" get cluster db -o jsonpath='{.status.conditions[?(@.type=="ContinuousArchiving")].status}')" == "True" ]] && archived=$((archived + 1))
  done
  echo "connected + wrote through PgBouncer: ${ok}/${n}; WAL archiving to S3: ${archived}/${n}"

  if [[ "$n" -ge 2 ]]; then
    if kc -n "${refs[0]}" exec db-1 -c postgres -- pg_isready -q -t 5 -h "pooler.${refs[1]}.svc" -p 5432; then
      echo "ISOLATION FAILED: ${refs[0]} reached ${refs[1]}"
    else
      echo "isolation: ${refs[0]} cannot reach ${refs[1]} (expected)"
    fi
    kc -n "${refs[0]}" exec db-1 -c postgres -- pg_isready -q -t 5 -h "pooler.${refs[0]}.svc" -p 5432 && echo "control: ${refs[0]} reaches its own pooler"
  fi

  echo "--- measured (VictoriaMetrics, cAdvisor working set, MiB) vs requested"
  # metrics-server is disabled (deploy/k3s/server.yaml); VictoriaMetrics' cAdvisor scrape is the source.
  kc get --raw "${vm}/api/v1/query?query=$(jq -rn --arg q 'sum(container_memory_working_set_bytes{namespace=~"t-cap.*",container!=""})' '$q|@uri')" |
    jq -r --argjson n "$n" '.data.result[0].value[1] // "0" | tonumber / 1048576 | "tenant containers: \(floor) MiB in use (\(. / $n | floor) MiB per tenant)"'
  kc get node -o json | jq -r '.items[0].status.allocatable | "node allocatable: cpu \(.cpu), memory \(.memory)"'
  kc describe node | awk '/Allocated resources/,/Events/' | grep -E 'cpu|memory' | head -2

  if [[ "$keep" != "--keep" ]]; then
    for ns in "${refs[@]}"; do bash "${root}/hack/tenant.sh" delete "$ns" --yes >/dev/null & done
    wait
    # The refs are fixed, and barman refuses to archive into a non-empty prefix, so the next run needs them
    # gone from the local S3 too (refs are generated above, so they are safe in weed shell input).
    printf 'fs.rm -r /buckets/dbcloud-backups/%s\n' "${refs[@]}" |
      kc -n dbcloud-system exec -i deploy/seaweedfs -- weed shell >/dev/null
    echo "test tenants and their backups deleted"
  fi
}

main "$@"
