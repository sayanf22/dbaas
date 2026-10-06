# dbcloud — self-built PostgreSQL DBaaS (plan, Blueprint 4.0)

A tenant-isolated managed PostgreSQL service that we build and run ourselves, for ≤ ₹9,000/month:

- **Customer databases** run on our own machines: CloudNativePG on a **K3s** cell. K3s is a CNCF-certified Kubernetes distribution, pinned `v1.36.4+k3s1`; k3d runs it locally, plus small QEMU/KVM VMs for the storage engine.
- **Compute and data are separated.** Compute nodes run Postgres; tenant data lives on storage nodes (LINSTOR + DRBD 9) and R2 keeps the durable backup copy. The launch cell is three Hostinger India VMs:
  - `control-1`: the K3s server, platform, control plane, admin DB and Auth;
  - `storage-1`: every tenant volume;
  - `compute-1`: tenant Postgres and the gateway.
- **Gated launch:** the VMs are accepted only after storage spike S12 passes on them; everything is built on this PC first.
- **Self-hosted admin layer:** the control-plane database (CloudNativePG) and user sign-in (Supabase Auth, self-hosted) run on `control-1`. No Supabase subscription.
- **Cloudflare** (Free plan) for DNS and the HTTP edge (Tunnel, WAF, Access, Turnstile), SSH/`kubectl` access, the Next.js web apps and **R2** backups.

Research date for versions, prices and limits: **2026-09-29**, storage, Hostinger and Auth facts **2026-10-07** (₹96 per US$). Items marked **verify**, "pin at install" or **planning value** must be re-checked when you reach them.

## Files

| File | Contents |
|---|---|
| `Production_PostgreSQL_DBaaS_Blueprint_PDF3.pdf` | Original Blueprint 3 |
| `blueprint.txt` | PDF text with the Rev 4.0 summary and inline amendments |
| `01-architecture.md` | The design: three-role K3s cells, separated storage (LINSTOR/DRBD, R2, WAL receiver), tenant model and autoscaling, pg-gateway, DNS, control plane, self-hosted admin DB and Auth, identity, MCP, backups, DR, security, observability, failure modes |
| `02-technology-stack.md` | Every component we use, with version, plan and reason; Go vs Rust split; Docker; build vs reuse |
| `03-cost-model.md` | Budget, VM checklist, Hostinger facts and fallbacks, plans with caps, tenant packing per node role, break-even, when money is spent |
| `04-build-plan.md` | Step-by-step plan with "Done when" checks, the deferred high-availability design, spikes S1–S13, acceptance gates, schema, API, risks |
| `05-dev-environment-and-downloads.md` | Accounts, Windows + WSL2 + Docker + QEMU/KVM setup, pinned tools, install commands, local loop |
| `06-decision-records.md` | 31 ADRs (ADR-027 to ADR-031 are the current storage, admin, cell and autoscaling decisions) with the condition that would reopen each |
| `07-engineering-standards.md` | Coding and security standards index + PR checklist (rules live in `.kiro/steering/`) |

Reading order: README → 03 → 01 → 02 → 04 → 07 → 05 → 06.

## Decisions at a glance

| Topic | Decision |
|---|---|
| Kubernetes distribution | **K3s `v1.36.4+k3s1`** with embedded etcd: one server (`control-1`) at launch; storage and compute nodes join as agents; high availability (3 servers) designed but deferred |
| Cell | one provider, one region, **three node roles** (ADR-029): `control-1` (pool `control`), `storage-1` (pool `storage`, tainted), `compute-1` (pool `compute`) |
| Provider | **Hostinger India** (Mumbai): KVM 2 + KVM 8 + KVM 4, gated by spike S12; fallback DigitalOcean BLR1; AWS Mumbai later for very large databases |
| Networking | flannel `wireguard-native` (encrypted between nodes) + K3s's NetworkPolicy controller; DRBD over DRBD TLS; only TCP 5432 public, on compute nodes; Hostinger has no private network, so every node-to-node path is encrypted and allow-listed (recorded deviation) |
| Storage | **LINSTOR + DRBD 9** (Piraeus Operator v2.12.0, ADR-027): tenant volumes on storage nodes, DRBD diskless clients on compute nodes; grows online per database up to its plan cap (launch maximum ≈ 200 GB), and per platform by adding storage nodes; online moves between storage nodes |
| Durability | R2 holds continuous WAL + weekly base backups (7-day PITR); a streaming WAL receiver on `control-1` keeps RPO < 1 s; object storage is never on the commit path (ADR-031) |
| Postgres | CloudNativePG 1.30.1, PostgreSQL 18.6, one PgBouncer per tenant + direct hostname |
| Autoscaling | users pick min/max inside plan caps; CPU request = min, limit = max; a **compute governor** scales memory inside the range without restarts; metered in CU-seconds (proposed 1 CU = 1 vCPU + 2 GiB; unit and price are owner decisions) (ADR-030) |
| Plans | `base` 0.1–0.5 vCPU / 256–512 MiB / 5 GB (cap 20 GB) **₹299 + GST**; `plus` 0.25–1 / 512 MiB–1 GiB / 10 GB (cap 50 GB) ₹699 (proposed); `premium` 0.5–1 / 1–2 GiB / 20 GB (cap 200 GB) ₹1,399 (proposed); 7-day PITR on all |
| Postgres ingress | **pg-gateway (Rust)** on every compute node (`hostPort` 5432); DNS has one A record per compute node, and clients fail over between them once there are two |
| HTTP and admin ingress | **Cloudflare Tunnel** per cell (`api`, `mcp`, `oauth`, `auth`); Access for staff tools, SSH and `kubectl`; no public 22/443/6443 |
| DNS | Cloudflare for everything; `*.db.example.com` DNS-only |
| Secrets | SOPS + age in git, decrypted by Flux in the cell; K3s encrypts Secrets at rest |
| Admin DB + user auth | **self-hosted on `control-1`** (ADR-028): CNPG `dbcloud-admin` with WAL + base backups to R2 (RPO ≤ 60 s) and Supabase Auth at `auth.example.com`; no customer data |
| Jobs | River (transactional outbox) on the admin DB |
| MCP | Go MCP server (spec 2026-07-28) + our **mcp-auth** OAuth 2.1 server (CIMD, RFC 8707) |
| Backups | Barman Cloud Plugin 0.15 → **Cloudflare R2** (weekly bases + continuous WAL, 8-day bucket lock); etcd snapshots to R2 every 6 h |
| Languages | Rust: pg-gateway. Go: control plane, operator, agent, compute governor, WAL receiver, MCP, CLI. TypeScript: Next.js web apps. Docker-built multi-arch images in ghcr.io |
| Web | **Next.js 16** static exports (public site, customer and admin dashboards) on Cloudflare Workers Static Assets, free; UI work starts only at Step 0.11 |
| Ops | Flux, system-upgrade-controller, VictoriaMetrics/Logs + Grafana, cert-manager |
| Payments | Razorpay Subscriptions (UPI AutoPay, cards) |

## How this compares with Neon

- **Neon:** it removes the storage ceiling by making object storage the source of truth, with NVMe caches and pageservers in front. No production-ready open-source way exists in 2026 to give vanilla Postgres that tier (Neon's public repository has been frozen since mid-2025 at PG 17).
- **What we take:** the separation of compute from data, with compute RAM as the hot cache. Commits stay on a fast network block device (≈ 0.3–1.5 ms planning value), and R2 is the bottomless durable copy.
- **What follows:** one database is capped by one storage node; the platform as a whole isn't. An object-store page tier is a deferred research track (ADR-031, spike S13); architecture §7.7 has the details.

## Can the control plane run on one small VM and the rest on AWS or GCP?

Technically yes, but not in this design. K3s supports embedded etcd only when all servers sit in one location, cross-cloud kubelet and storage traffic is billed as egress and adds milliseconds to every commit, and a broken link freezes the cluster. A cell therefore stays in one provider and region. A whole cell on AWS is possible later (EC2 + gp3 under LINSTOR, architecture §4.3). Another provider or region becomes a **separate cell**, managed by the same control plane (`03-…` §2.3).

## What it costs

| Stage | Monthly cost (ex-GST) | Holds |
|---|---|---|
| Phase 0 (local development on this PC) | **₹0** (+ domain) | local k3d cell + QEMU/KVM storage test bed |
| **Launch: `control-1` + `storage-1` + `compute-1` (ADR-029)** | **≈ ₹8,067** at renewal prices (≈ ₹4,167 on the intro term, a 24-month prepay of ≈ ₹98,328) | per compute node: 8 `base` (strict CPU rule) or 26 `base` (usage-gated rule); ≈ 280 GB of tenant volumes; break-even at 29 `base` tenants (15 on intro prices) |
| + `compute-2` | + ₹2,399, only when revenue covers it | compute failover in ≈ 1–2 min; tenants double; break-even 37 `base` at renewal |
| + `storage-2` | + ₹4,399, only when revenue covers it | +≈ 350 GB pool; online volume moves |
| High availability (deferred) | + two control VMs and doubled storage, only when revenue covers them | survives the loss of any one VM with RPO 0 |

- **Owner decisions:** whether the CPU guarantee is sold strictly against Hostinger's 30 % CPU baseline or usage-gated. Under the strict rule, `base` at ₹299 never breaks even on Hostinger (`03-…` §6.1, §7). CU prices, extra-storage prices and the Hostinger term are also owner decisions.
- **Fees:** payment-gateway fees (~₹12.45 per ₹352.82 `base` payment) come out of revenue.
- **Details:** `03-…` §1, §6, §7.

No system is 100 % available. The launch cell has no high availability:
- **Compute-node loss:** tenants wait for the VM until `compute-2` exists; after that they fail over in ≈ 1–2 min.
- **Storage-node loss:** its volumes are down until it returns or tenants restore from R2 + the WAL receiver.
- **`control-1` loss:** changes and sign-in stop but databases keep serving.

Launch plans are sold as best-effort; measured targets are published only after the acceptance gates pass. High availability is designed but deferred (`04-…` "Deferred (not scheduled): high availability"). HA plans (Phase 3) and a second cell (Phase 4) follow when revenue funds them.

## Open items to confirm (build plan)

1. **Storage spike S12 on Hostinger** (Step 1.0): RTT, DRBD fsync, transfer accounting (support answer in writing), CPU credits, DRBD module on the template kernel, disk layout, failover and fencing; reject criteria and the DigitalOcean fallback are in `04-…` §3.
2. **Owner decisions** (`03-…` §7):
   - the CPU-capacity rule (strict or usage-gated);
   - `base` pricing or its guaranteed CPU;
   - the CU unit and price per CU-hour;
   - the price per extra storage GB-month;
   - the Hostinger term.
3. **Other spikes:** S1, S3–S7, S9–S11: K3s conformance and NetworkPolicy, gateway failover between node addresses, Barman ↔ R2 under the bucket lock, mcp-auth with real MCP clients, multi-arch images, the self-hosted admin DB and Auth, Cloudflare Tunnel and Access, DNS and certificates, etcd snapshots and K3s upgrades.

## Next step

Create your Linux user in Ubuntu, install the tools in `05-…` §4, then `04-build-plan.md` **Step 0.0** (accounts — no VM provider yet) and **Step 0.1** (repo, tooling, CI), then **Step 0.3b** (separated storage on the local QEMU/KVM test bed). All of Phase 0 runs on this PC at ₹0.
