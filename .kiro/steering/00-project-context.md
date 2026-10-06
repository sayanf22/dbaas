---
inclusion: always
---

# dbcloud — project context

dbcloud is a self-built, multi-tenant managed PostgreSQL service (DBaaS). The plan lives in `plan/` (start with `plan/README.md`); `plan/01-architecture.md` is the source of truth for design, `plan/03-cost-model.md` for costs and capacity, and `plan/04-build-plan.md` for the order of work. If code and plan disagree, stop and flag it. Don't silently diverge.

## What runs where

- **Customer databases** run on our own machines: CloudNativePG clusters on a **K3s** cluster in one region (a **cell**, first cell `c1`). K3s is a CNCF-certified Kubernetes distribution shipped as a single binary, pinned `v1.36.4+k3s1`; local development runs it in Docker with **k3d**, and tests the storage engine on small QEMU/KVM VMs inside WSL (DRBD is kernel-global, so it can't run in k3d). We don't run kubeadm-built upstream Kubernetes or a managed Kubernetes service; say "Kubernetes" only for the API and ecosystem.
- **Launch cell (ADR-029): three node roles.**
  - **`control-1`** (`dbcloud.io/pool=control`) is the single K3s server (embedded etcd, `cluster-init`). It runs every platform and control-plane workload, the LINSTOR controller, the admin DB and Auth, and the WAL receiver.
  - **`compute-N`** (`dbcloud.io/pool=compute`, K3s agents) run tenant Postgres + PgBouncer and pg-gateway (`hostPort` 5432). They keep **no tenant data on their disks**: tenant volumes are DRBD diskless clients, and the node's RAM is the hot cache.
  - **`storage-N`** (`dbcloud.io/pool=storage`, taint `dbcloud.io/pool=storage:NoSchedule`, K3s agents) run the LINSTOR satellite and DRBD 9 on an LVM-thin pool holding all tenant volumes; only storage DaemonSets run there.
  - Tenant pods select `pool=compute`; platform pods select `pool=control`.
- **Launch hardware:** Hostinger India (`control-1` KVM 2, `storage-1` KVM 8, `compute-1` KVM 4), accepted only if spike S12 passes. Fallback DigitalOcean BLR1; AWS Mumbai later for very large databases.
- **Storage (ADR-027, ADR-031):** LINSTOR + DRBD 9 through the Piraeus Operator. StorageClasses `tenant-network` (tenant volumes on storage nodes, diskless on compute) and `platform-local` (`control-1`'s own pool). R2 holds the WAL archive and base backups (7-day PITR); object storage is never on the commit path. One database is capped by one storage node's pool; the platform grows by adding storage nodes.
- **High availability is deferred** (`plan/04-build-plan.md` "Deferred (not scheduled): high availability"): `control-1..3`, placement count 2 + a DRBD tie-breaker, 2 replicas per platform Deployment. It is not a build step, but code must stay HA-compatible: no single-node assumptions for any role.
- **Admin DB and Auth are self-hosted on `control-1` (ADR-028):** CNPG cluster `dbcloud-admin` (orgs, users, tenant catalog, operations, billing, audit, River job tables), reached through its PgBouncer Pooler (transaction mode) or direct session connections, backed up to R2. Dashboard sign-in uses self-hosted **Supabase Auth** at `auth.example.com`. Supabase (the hosted service) isn't used; `supabase start` remains only for local development until Step 0.6. Customer data never goes to the admin DB.
- **Cloudflare** (Free plan) serves DNS (including the DNS-only wildcard `*.db.example.com` with one A record per compute node), fronts HTTP (Tunnel, WAF, Access, Turnstile), provides admin SSH/`kubectl` access through Access, hosts the Next.js static exports of the public site and dashboards (Workers Static Assets) and stores backups and etcd snapshots (R2). Customer Postgres traffic never goes through Cloudflare's proxy: clients connect to `pg-gateway` on the compute nodes' public IPs (port 5432).
- **Plans:** fixed prices with caps, and users pick a compute range inside them (ADR-030: CPU and memory request = min, limit = max; a compute governor scales memory inside the range).
  - `base`: 0.1–0.5 vCPU, 256–512 MiB, 5 GB (cap 20 GB), ₹299 + GST.
  - `plus`: 0.25–1 vCPU, 512 MiB–1 GiB, 10 GB (cap 50 GB).
  - `premium`: 0.5–1 vCPU, 1–2 GiB, 20 GB (cap 200 GB).
  - All are single-instance with 7-day PITR. The CU unit and prices are owner decisions.
- **Budget:** the whole platform costs **≤ ₹9,000/month** (launch ≈ ₹8,067 at renewal prices).
- **Terminology:** in this repo **OCI** means Open Container Initiative (image format, Flux `OCIRepository` artifacts, OCI registries), never a cloud provider. Phase 0 is local development on the developer's PC.

## Components and languages

| Path | Component | Language |
|---|---|---|
| `gateway/` | pg-gateway: Postgres TLS/SNI router on the data path | Rust |
| `cmd/control-api`, `internal/...` | public + internal API | Go |
| `cmd/worker` | River jobs: placement, capacity, fencing, volume moves, DNS, metering, billing, drills, retention | Go |
| `cmd/cluster-agent` | per-cell pull agent | Go |
| `cmd/tenant-operator`, `operator/` | `TenantDatabase` operator (controller-runtime) | Go |
| `cmd/compute-governor` | per-compute-node memory governor (cgroup v2) | Go |
| `cmd/wal-receiver` | streaming WAL receivers on `control-1` | Go |
| `cmd/mcp-server` | remote MCP server (official Go SDK) | Go |
| `cmd/mcp-auth`, `internal/oauth` | OAuth 2.1 server for MCP clients (zitadel/oidc) | Go |
| `cmd/dbcloud` | CLI | Go |
| `web/site`, `web/customer`, `web/admin`, `web/ui` | public site, customer + admin dashboards, shared shadcn/ui package (skeletons until Step 0.11) | TypeScript (Next.js 16 App Router, static export) |
| `db/` | goose migrations + sqlc queries for the admin DB | SQL |
| `deploy/` | Terraform, node bootstrap (cloud-init, roles `control`/`compute`/`storage`), K3s config, Flux, Helm | HCL / YAML / Bash |
| `build/`, `gateway/Dockerfile` | container images (ghcr.io) | Dockerfile |
| `.github/workflows/` | CI | YAML |
| `hack/` | small dev scripts | Bash |

## Non-negotiable invariants

1. One PostgreSQL instance + one database per tenant; never split one database's writes across clusters.
2. The control plane never stores or proxies customer rows.
3. Static stability: the data path survives control-plane loss.
   - If `control-1` (K3s API, control plane, admin DB, Auth) or Cloudflare's proxy/Tunnel/API is down, running customer databases keep serving.
   - A compute node keeps serving while `control-1` is down because DRBD replication is kernel-to-kernel between compute and storage nodes, and the gateway serves from its route cache.
   - Losing a storage node means its tenants are restored (R2 + the WAL receiver) unless it comes back.
4. Every change is asynchronous, idempotent, retry-safe and audited (River job + `operations` row).
5. Customers and AI agents never get Kubernetes, VM, R2 or admin-DB credentials.
6. Nothing is called production-ready until its acceptance gate in `plan/04-build-plan.md` passes.
7. **Spend cap:** no code, manifest or Terraform may create a billable resource that the build-plan step and `plan/03-cost-model.md` don't name. Every pod declares CPU and memory requests; node capacity is shared by platform and tenants, and tenant capacity respects the provider's CPU baseline on credit-based VMs.
8. **Provider-neutral:** nothing outside `deploy/terraform/modules/vm-<provider>` and the `ProviderAdapter` implementations may depend on a specific VM provider. A cell never spans providers or regions.

## Steering files

Always loaded: this file, `10-engineering-standards.md`, `15-security-and-reliability.md`, `80-dev-loop.md`. Loaded when you touch matching files: `20-go.md` (`**/*.go`), `30-rust.md` (`gateway/**`), `35-sql.md` (`db/**`), `40-kubernetes-operator.md` (`**/*operator/**`), `50-web.md` (`web/**`), `55-containers.md` (`**/*Dockerfile*`), `60-infrastructure.md` (`deploy/**`), `70-ci.md` (`.github/**`), `75-shell.md` (`**/*.sh`).
