# dbcloud — self-built PostgreSQL DBaaS (plan, Blueprint 4.0)

A tenant-isolated managed PostgreSQL service that we build and run ourselves, for ≤ ₹9,000/month:

- **Customer databases** on our own machines: CloudNativePG on a **K3s** cluster (a **cell**) from any provider that passes our checklist. Launch runs on a two-VM starter cell: a control VM `ctl-1` (2 vCPU / 8 GB, the K3s server and the whole platform) and a tenant VM `node-1` (4 vCPU / 16 GB, only databases); planning example Hostinger KVM 2 + KVM 4. More tenant VMs join as it grows, and two more control VMs for HA. No provider is chosen yet (Hostinger, AWS and Azure are priced in `03-…` §2.2); Phase 0 is local development on this PC, and everything after it is a production build.
- **Supabase** only for our admin/management layer: the control-plane database and user sign-in, on the Free plan until its upgrade trigger.
- **Cloudflare** (Free plan) for DNS and the HTTP edge (Tunnel, WAF, Access, Turnstile), SSH/`kubectl` access, the Next.js web apps and **R2** backups.

Research date for all versions, prices and limits: **2026-09-29** (₹96 per US$); Hostinger prices and the language-split evidence: **2026-10-07**; the two-VM layout with AWS and Azure prices: **2026-10-14**. Items marked **verify** or "pin at install" must be re-checked when you reach them.

## Files

| File | Contents |
|---|---|
| `Production_PostgreSQL_DBaaS_Blueprint_PDF3.pdf` | Original Blueprint 3 |
| `blueprint.txt` | PDF text with the Rev 4.0 summary and inline amendments |
| `01-architecture.md` | The design: K3s cells on provider-neutral VMs, tenant model, storage, pg-gateway, DNS, control plane, Supabase, identity, MCP, backups, DR, security, observability, failure modes |
| `02-technology-stack.md` | Every component we use, with version, plan and reason; Go vs Rust split; Docker; build vs reuse |
| `03-cost-model.md` | Budget, VM checklist and price bands, plans and prices, tenant packing, break-even, when money is spent |
| `04-build-plan.md` | Progress table, step-by-step plan with "Done when" checks, spikes S1–S11, acceptance gates, schema, API, risks |
| `05-dev-environment-and-downloads.md` | Accounts, Windows + WSL2 + Docker setup (done on this PC), pinned tools, install commands, local loop |
| `06-decision-records.md` | 27 ADRs with the condition that would reopen each, plus what 4.0 superseded |
| `07-engineering-standards.md` | Coding and security standards index + PR checklist (rules live in `.kiro/steering/`) |

Reading order: README → 03 → 01 → 02 → 04 → 07 → 05 → 06.

## Decisions at a glance

| Topic | Decision |
|---|---|
| Kubernetes | **K3s v1.36**: servers with embedded etcd on control VMs (1 at launch, 3 from Step 1.13), agents on tenant VMs |
| Cell | Launch: **two-VM starter cell** (ADR-025), one provider, one region. `ctl-1` 2 vCPU / 8 GB / 100 GB NVMe: K3s server, every platform and control-plane workload, no tenants, no inbound from the internet (label `dbcloud.io/pool=control`). `node-1` 4 vCPU / 16 GB / 200 GB NVMe: K3s agent, only tenant databases and per-node DaemonSets, public TCP 5432 (label and taint `dbcloud.io/pool=tenant`), ≈ 17 `base` tenants. Growth: `node-2`, `node-3`, … as tenant agents. HA (Step 1.13): `ctl-2`, `ctl-3` join as servers |
| Provider | not chosen; any provider passing `03-…` §2.1 within ₹3,600/month for the starter cell at renewal prices (in practice the value-VPS class); planning example **Hostinger KVM 2 + KVM 4** (₹1,199 + ₹2,399 renewal, private network unconfirmed); AWS (≈ ₹12,300) and Azure (≈ ₹14,100–17,900) priced for the same layout and over budget; chosen at Step 1.1 after benchmarks |
| Networking | flannel `wireguard-native` (encrypted between nodes, over a private network where the provider has one, otherwise over public IPs with nftables allow-lists) + K3s's NetworkPolicy controller; only TCP 5432 public, on tenant VMs |
| Storage | TopoLVM on local NVMe; backups to R2; HA plans later replicate across nodes |
| Postgres | CloudNativePG 1.30.1, PostgreSQL 18.6, one PgBouncer per tenant + direct hostname |
| Plans | `base` 0.1 vCPU (burst 0.5) / 512 MiB / 5 GB **₹299 + GST**; `plus` 0.25 (1) / 1 GiB / 10 GB ₹699 (proposed); `premium` 0.5 (1) / 2 GiB / 20 GB ₹1,399 (proposed); 7-day PITR on all |
| Postgres ingress | **pg-gateway (Rust)** on every tenant VM (`hostPort` 5432); DNS has one A record per tenant VM, and clients fail over between them once there are two or more |
| HTTP and admin ingress | **Cloudflare Tunnel** per cell; Access for staff tools, SSH and `kubectl`; no public 22/443/6443 |
| DNS | Cloudflare for everything; `*.db.example.com` DNS-only |
| Secrets | SOPS + age in git, decrypted by Flux in the cell; K3s encrypts Secrets at rest |
| Admin DB + user auth | **Supabase Free** (ap-south-1, through the IPv4 pooler) + Supabase Auth → Pro at ~30 tenants; no customer data (hosting the admin DB on `ctl-1` is recorded as an open owner option, ADR-007) |
| Jobs | River (transactional outbox) on the Supabase admin DB |
| MCP | Go MCP server (spec 2026-07-28) + our **mcp-auth** OAuth 2.1 server (CIMD, RFC 8707) |
| Backups | Barman Cloud Plugin 0.15 → **Cloudflare R2** (weekly bases + continuous WAL, 8-day bucket lock); etcd snapshots to R2 every 6 h |
| Languages | By workload (ADR-027): **Rust** for the customer connection/data path and per-connection, latency- or memory-critical code (pg-gateway today). **Go** for the Kubernetes control plane and business logic (operator, agent, API, River worker, MCP, CLI). PgBouncer stays the pooler. TypeScript: Next.js web apps. Docker-built multi-arch images in ghcr.io |
| Web | **Next.js 16** static exports (public site, customer and admin dashboards) on Cloudflare Workers Static Assets, free |
| Ops | Flux, system-upgrade-controller, VictoriaMetrics/Logs + Grafana, cert-manager |
| Payments | Razorpay Subscriptions (UPI AutoPay, cards) |

## Can the control VM run at one provider and the tenant VMs on AWS, Azure or GCP?

Technically yes, but not in this design. K3s supports embedded etcd only when all servers are in one location, cross-cloud kubelet traffic and backup uploads are billed as egress, a broken link freezes the cluster, and a hyperscaler tenant VM alone costs ≈ 3.4–4.5× its value-VPS equivalent. A cell therefore stays in one provider and region; AWS, Azure or GCP become a **separate cell** later, managed by the same control plane (`03-…` §2.3).

## What it costs

| Stage | Monthly cost | Holds |
|---|---|---|
| Phase 0 (build on this PC) | **₹0** (+ domain) | local k3d cell + local Supabase |
| **Launch: two-VM starter cell (ADR-025)** | **≈ ₹3,670** at renewal (Hostinger KVM 2 ₹1,199 + KVM 4 ₹2,399 + domain); ≈ ₹1,970 during a prepaid intro term | ~17 `base`, ~10 `plus` or ~5 `premium` on `node-1`; **break-even at 13 `base` tenants** (7 on the intro prices); no tenant failover with one tenant VM |
| + `node-2` (at ≈ 14 tenants, when the owner funds it) | **≈ ₹6,070** | ~34 `base`; break-even 22 |
| + Supabase Pro (~30 tenants) | **≈ ₹8,470** total | ~34 `base`; break-even 30 |
| + each further 4 vCPU / 16 GB tenant VM | + ≤ ₹2,400, only when revenue covers it | + ~17 `base` tenants; pays for itself at 9 |
| + HA control plane (`ctl-2`, `ctl-3`, Step 1.13) | + ≈ ₹2,400, when revenue covers it | no extra tenants; the control plane survives losing one control VM |

Break-even on the starter cell is (₹1,199 + ₹2,399 + ₹70) ÷ ₹286.55 net per tenant = 12.8, so 13 `base` tenants; a full `node-1` nets ≈ ₹1,200/month. Only the value-VPS class fits: the same two VMs cost ≈ ₹12,300 on AWS Lightsail and ≈ ₹14,100–17,900 on Azure (Central India) before egress, and neither breaks even on one tenant VM. Payment-gateway fees (~₹12.45 per ₹352.82 `base` payment) come out of revenue; GST may be added to VM invoices. Details: `03-…` §1, §2.2, §6.1, §7.

No system is 100 % available. If `ctl-1` fails, running databases keep serving and only management waits (`503` + `Retry-After`) until it is rebuilt from the etcd snapshot in R2. If `node-1` fails, its tenants restore from R2 onto a replacement VM. From Step 1.13 the control plane survives the loss of any one control VM; with two or more tenant VMs, only the lost VM's tenants are affected. Launch plans are sold as best-effort with 7-day PITR; measured targets are published only after the acceptance gates pass. HA plans (Phase 3) and a second cell (Phase 4) follow when revenue funds them.

## Open items to confirm (build plan)

1. **Provider choice** (Step 1.1): shortlist against `03-…` §2.1 (Hostinger KVM 2 + KVM 4 is on it), then spike S2 on both VMs (disk, fsync, sustained CPU, steal, `tenantvg` size) and S8 (private network, unconfirmed for Hostinger; without one `node-1` joins over public IPs with nftables allow-lists and WireGuard; transfer; first invoice).
2. Spikes S1 and S3–S11: K3s conformance and NetworkPolicy, gateway failover between node addresses, Barman ↔ R2 under the bucket lock, mcp-auth with real MCP clients, multi-arch images, Supabase Free through the pooler, Cloudflare Tunnel and Access, DNS and certificates, etcd snapshots and K3s upgrades.

## Next step

Steps 0.2 and 0.3 are complete (the local cell's profiles were reshaped to the two-VM layout on 2026-10-14; the acceptance re-run is pending) and Step 0.4 (`TenantDatabase` CRD + tenant-operator) is in progress; Step 0.0 (accounts) and the rest of Step 0.1 are still open. The progress table at the top of `04-build-plan.md` is the current status. All of Phase 0 runs on this PC at ₹0; no VM is rented before Gate G0.
