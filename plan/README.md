# dbcloud — self-built PostgreSQL DBaaS (plan, Blueprint 4.0)

A tenant-isolated managed PostgreSQL service that we build and run ourselves, for ≤ ₹9,000/month:

- **Customer databases** on our own machines: CloudNativePG on a **K3s** cluster of three Linux VMs (a **cell**) from any provider that passes our checklist. No provider is chosen yet; everything is built on this PC first.
- **Supabase** only for our admin/management layer: the control-plane database and user sign-in, on the Free plan until its upgrade trigger.
- **Cloudflare** (Free plan) for DNS and the HTTP edge (Tunnel, WAF, Access, Turnstile), SSH/`kubectl` access, the Next.js web apps and **R2** backups.

Research date for all versions, prices and limits: **2026-09-29** (₹96 per US$). Items marked **verify** or "pin at install" must be re-checked when you reach them.

## Files

| File | Contents |
|---|---|
| `Production_PostgreSQL_DBaaS_Blueprint_PDF3.pdf` | Original Blueprint 3 |
| `blueprint.txt` | PDF text with the Rev 4.0 summary and inline amendments |
| `01-architecture.md` | The design: K3s cells on provider-neutral VMs, tenant model, storage, pg-gateway, DNS, control plane, Supabase, identity, MCP, backups, DR, security, observability, failure modes |
| `02-technology-stack.md` | Every component we use, with version, plan and reason; Go vs Rust split; Docker; build vs reuse |
| `03-cost-model.md` | Budget, VM checklist and price bands, plans and prices, tenant packing, break-even, when money is spent |
| `04-build-plan.md` | Step-by-step plan with "Done when" checks, spikes S1–S11, acceptance gates, schema, API, risks |
| `05-dev-environment-and-downloads.md` | Accounts, Windows + WSL2 + Docker setup (done on this PC), pinned tools, install commands, local loop |
| `06-decision-records.md` | 24 ADRs with the condition that would reopen each, plus what 4.0 superseded |
| `07-engineering-standards.md` | Coding and security standards index + PR checklist (rules live in `.kiro/steering/`) |

Reading order: README → 03 → 01 → 02 → 04 → 07 → 05 → 06.

## Decisions at a glance

| Topic | Decision |
|---|---|
| Kubernetes | **K3s v1.36**, 3 servers with embedded etcd per cell; growth nodes join as agents |
| Cell | 3 VMs, one provider, one region, private network: `cp-1` 2 vCPU / 4 GB (control plane only) + `node-1`, `node-2` 8 vCPU / 24 GB / 200 GB NVMe (control plane + platform + tenants) |
| Provider | not chosen; any provider passing `03-…` §2.1 within ₹5,600/month for the cell (in practice the value-VPS class); chosen at Step 1.1 after benchmarks |
| Networking | flannel `wireguard-native` (encrypted between nodes) + K3s's NetworkPolicy controller; only TCP 5432 public |
| Storage | TopoLVM on local NVMe; backups to R2; HA plans later replicate across nodes |
| Postgres | CloudNativePG 1.30.1, PostgreSQL 18.6, one PgBouncer per tenant + direct hostname |
| Plans | `base` 0.1 vCPU (burst 0.5) / 512 MiB / 5 GB **₹299 + GST**; `plus` 0.25 (1) / 1 GiB / 10 GB ₹699 (proposed); `premium` 0.5 (1) / 2 GiB / 20 GB ₹1,399 (proposed); 7-day PITR on all |
| Postgres ingress | **pg-gateway (Rust)** on every tenant node (`hostPort` 5432); DNS has one A record per node, and clients fail over between them |
| HTTP and admin ingress | **Cloudflare Tunnel** per cell; Access for staff tools, SSH and `kubectl`; no public 22/443/6443 |
| DNS | Cloudflare for everything; `*.db.example.com` DNS-only |
| Secrets | SOPS + age in git, decrypted by Flux in the cell; K3s encrypts Secrets at rest |
| Admin DB + user auth | **Supabase Free** (ap-south-1, through the IPv4 pooler) + Supabase Auth → Pro at ~30 tenants; no customer data |
| Jobs | River (transactional outbox) on the Supabase admin DB |
| MCP | Go MCP server (spec 2026-07-28) + our **mcp-auth** OAuth 2.1 server (CIMD, RFC 8707) |
| Backups | Barman Cloud Plugin 0.15 → **Cloudflare R2** (weekly bases + continuous WAL, 8-day bucket lock); etcd snapshots to R2 every 6 h |
| Languages | Rust: pg-gateway. Go: control plane, operator, agent, MCP, CLI. TypeScript: Next.js web apps. Docker-built multi-arch images in ghcr.io |
| Web | **Next.js 16** static exports (public site, customer and admin dashboards) on Cloudflare Workers Static Assets, free |
| Ops | Flux, system-upgrade-controller, VictoriaMetrics/Logs + Grafana, cert-manager |
| Payments | Razorpay Subscriptions (UPI AutoPay, cards) |

## Can the control plane run on one small VM and the rest on AWS or GCP?

Technically yes, but not in this design. K3s supports embedded etcd only when all servers share a private network in one location, cross-cloud kubelet traffic is billed as egress, a broken link freezes the cluster, and hyperscaler VMs alone cost 5–6× the budget. A cell therefore stays in one provider and region; AWS or GCP become a **separate cell** later, managed by the same control plane (`03-…` §2.3).

## What it costs

| Stage | Monthly cost | Holds |
|---|---|---|
| Phase 0 (build on this PC) | **₹0** (+ domain) | local k3d cell + local Supabase |
| **Launch: starter cell, one VM (ADR-025)** | **≈ ₹2,370** | ~22 `base`, ~13 `plus` or ~7 `premium`; **break-even at 9 `base` tenants**; no failover until it grows |
| Reference cell `c1` (3 VMs, from ~18 tenants) | **≈ ₹5,700–5,900** | ~47 `base`, ~27 `plus` or ~15 `premium` tenants; ≈ ₹14,050/month revenue when full with `base` |
| + Supabase Pro (~30 tenants) | **≈ ₹8,100–8,300** total | — |
| + each extra 8 vCPU / 24 GB node | + ≤ ₹2,300, only when revenue covers it | + ~25 `base` tenants |

Break-even is at 9 `base` tenants on the starter VM and about 20 on the reference cell. Payment-gateway fees (~₹12.45 per ₹352.82 `base` payment) come out of revenue. Details: `03-…` §1, §6, §7.

No system is 100 % available. From launch, the cell survives the loss of any one VM (control plane and platform keep running); tenants on a lost node restore from R2 onto a replacement. Launch plans are sold as best-effort with 7-day PITR; measured targets are published only after the acceptance gates pass. HA plans (Phase 3) and a second cell (Phase 4) follow when revenue funds them.

## Open items to confirm (build plan)

1. **Provider choice** (Step 1.1): shortlist against `03-…` §2.1, then spike S2 (disk, fsync, CPU steal) and S8 (private network, transfer, first invoice).
2. Spikes S1 and S3–S11: K3s conformance and NetworkPolicy, gateway failover between node addresses, Barman ↔ R2 under the bucket lock, mcp-auth with real MCP clients, multi-arch images, Supabase Free through the pooler, Cloudflare Tunnel and Access, DNS and certificates, etcd snapshots and K3s upgrades.

## Next step

Create your Linux user in Ubuntu, install the tools in `05-…` §4, then `04-build-plan.md` **Step 0.0** (accounts — no VM provider yet) and **Step 0.1** (repo, tooling, CI). All of Phase 0 runs on this PC at ₹0.
