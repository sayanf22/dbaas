---
inclusion: always
---

# dbcloud — project context

dbcloud is a self-built, multi-tenant managed PostgreSQL service (DBaaS). The plan lives in `plan/` (start with `plan/README.md`); `plan/01-architecture.md` is the source of truth for design, `plan/03-cost-model.md` for costs and capacity, and `plan/04-build-plan.md` for the order of work. If code and plan disagree, stop and flag it. Don't silently diverge.

## What runs where

- **Customer databases** run on our own machines: CloudNativePG clusters on a **K3s** cluster of three Linux VMs in one region (a **cell**, first cell `c1`). `cp-1` (2 vCPU / 4 GB) runs only the K3s control plane; `node-1` and `node-2` (8 vCPU / 24 GB, local NVMe) run the control plane too, plus the platform and about 53 `base` tenants. Growth nodes join as K3s agents. **Launch runs on the starter cell (ADR-025):** only `node-1`, a single K3s server with embedded etcd and one replica per platform component, until ~20 `base` tenants; then `cp-1` and `node-2` join (build plan Step 1.13). Code must work unchanged in both shapes. **No VM provider is chosen yet**: the design runs on any provider passing `plan/03-cost-model.md` §2.1, and Phase 0 is built entirely on the developer's PC (k3d + local Supabase).
- **Supabase** is only our admin/management layer: the control-plane database (orgs, users, tenant catalog, operations, billing, audit, River job tables) and Supabase Auth for dashboard sign-in. It starts on the **Free** plan and is reached only through the Supavisor pooler over IPv4. Customer data never goes to Supabase.
- **Cloudflare** (Free plan) serves DNS (including the DNS-only wildcard `*.db.example.com` with one A record per tenant node), fronts HTTP (Tunnel, WAF, Access, Turnstile), provides admin SSH/`kubectl` access through Access, hosts the Next.js static exports of the public site and dashboards (Workers Static Assets) and stores backups and etcd snapshots (R2). Customer Postgres traffic never goes through Cloudflare's proxy: clients connect to `pg-gateway` on the nodes' public IPs (port 5432).
- Plans: `base` (0.1 vCPU, 512 MiB, 5 GB, ₹299 + GST), `plus` (0.25 vCPU, 1 GiB, 10 GB), `premium` (0.5 vCPU, 2 GiB, 20 GB); all single-instance with 7-day PITR.
- Budget: the whole platform costs **≤ ₹9,000/month**.

## Components and languages

| Path | Component | Language |
|---|---|---|
| `gateway/` | pg-gateway: Postgres TLS/SNI router on the data path | Rust |
| `cmd/control-api`, `internal/...` | public + internal API | Go |
| `cmd/worker` | River jobs: placement, capacity, DNS, billing, drills, retention | Go |
| `cmd/cluster-agent` | per-cell pull agent | Go |
| `cmd/tenant-operator`, `operator/` | `TenantDatabase` operator (controller-runtime) | Go |
| `cmd/mcp-server` | remote MCP server (official Go SDK) | Go |
| `cmd/mcp-auth`, `internal/oauth` | OAuth 2.1 server for MCP clients (zitadel/oidc) | Go |
| `cmd/dbcloud` | CLI | Go |
| `web/site`, `web/customer`, `web/admin`, `web/ui` | public site, customer + admin dashboards, shared shadcn/ui package | TypeScript (Next.js 16 App Router, static export) |
| `db/` | goose migrations + sqlc queries for the admin DB | SQL |
| `deploy/` | Terraform, node bootstrap (cloud-init), K3s config, Flux, Helm | HCL / YAML / Bash |
| `build/`, `gateway/Dockerfile` | container images (ghcr.io) | Dockerfile |
| `.github/workflows/` | CI | YAML |
| `hack/` | small dev scripts | Bash |

## Non-negotiable invariants

1. One PostgreSQL instance + one database per tenant; never split one database's writes across clusters.
2. The control plane never stores or proxies customer rows.
3. Static stability: running customer databases keep serving if the control plane, Supabase, or Cloudflare's proxy/Tunnel/API is down.
4. Every change is asynchronous, idempotent, retry-safe and audited (River job + `operations` row).
5. Customers and AI agents never get Kubernetes, VM, R2 or Supabase credentials.
6. Nothing is called production-ready until its acceptance gate in `plan/04-build-plan.md` passes.
7. **Spend cap:** no code, manifest or Terraform may create a billable resource that the build-plan step and `plan/03-cost-model.md` don't name. Every pod declares CPU and memory requests; node capacity is shared by platform and tenants.
8. **Provider-neutral:** nothing outside `deploy/terraform/modules/vm-<provider>` and the `ProviderAdapter` implementations may depend on a specific VM provider. A cell never spans providers or regions.

## Steering files

Always loaded: this file, `10-engineering-standards.md`, `15-security-and-reliability.md`. Loaded when you touch matching files: `20-go.md` (`**/*.go`), `30-rust.md` (`gateway/**`), `35-sql.md` (`db/**`), `40-kubernetes-operator.md` (`**/*operator/**`), `50-web.md` (`web/**`), `55-containers.md` (`**/*Dockerfile*`), `60-infrastructure.md` (`deploy/**`), `70-ci.md` (`.github/**`), `75-shell.md` (`**/*.sh`).
