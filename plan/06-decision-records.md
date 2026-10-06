# 06 — Architecture Decision Records (Blueprint 4.0)

Format: context → decision → consequences → revisit when.
Status: **Accepted**, **Accepted (spike-gated: Sx)** = accepted unless the named spike in `04-build-plan.md` fails, in which case the listed fallback is adopted and this record is updated, or **Superseded by ADR-xxx** = kept for history; the newer record governs. A note on earlier blueprint revisions is at the end.

Current launch architecture in one line: a three-role K3s cell (`control-1`, `storage-1`, `compute-1`, ADR-029) with tenant volumes on LINSTOR/DRBD storage nodes and diskless compute (ADR-027), the admin DB and Auth self-hosted on `control-1` (ADR-028), compute autoscaling inside user caps (ADR-030) and R2 as the durable tier, never on the commit path (ADR-031).

---

### ADR-001 — Cells are K3s clusters on plain VMs
**Status:** Accepted (spike-gated: S1, S11)
**Context:** Managed Kubernetes services on mainstream clouds break the ₹9,000/month budget, and building upstream Kubernetes with kubeadm means running more components ourselves. The original blueprint used K3s.
**Decision:** Every cell is one **K3s** cluster (a CNCF-certified Kubernetes distribution shipped as a single binary), pinned to `v1.36.4+k3s1`, on Linux VMs in one region with embedded etcd. The launch cell has one server, `control-1`, started with `cluster-init`; storage and compute nodes join as agents (ADR-029); high availability adds two more servers to the same etcd (deferred, `04-…` "Deferred (not scheduled): high availability"). Local development runs the same release in Docker with k3d, plus small QEMU/KVM VMs for the storage engine (`04-…` Step 0.3b). Cells are independent failure and scale units registered with the control plane.
**Consequences:** No control-plane fee; we run etcd ourselves (snapshots to R2; quorum runbooks once there are three servers). CNPG officially targets vanilla Kubernetes, so conformance and CNPG smoke tests run on every K3s bump. Our code talks only to the Kubernetes API, never to K3s internals.
**Revisit when:** a contract requires an SLA-backed Kubernetes control plane, or revenue makes a managed control plane cheaper than operating etcd.

### ADR-002 — Provider-neutral, one provider and region per cell
**Status:** Accepted (spike-gated: S8, S12)
**Context:** K3s supports embedded etcd only with all servers in one location; cross-provider nodes add latency, egress fees and partition risk (`03-…` §2.3).
**Decision:** A cell uses VMs from exactly one provider in one region. Providers are checked against `03-…` §2.1; the launch provider is Hostinger India (Mumbai), gated by spike S12, with DigitalOcean BLR1 as the fallback and AWS Mumbai as the later big-database cell (ADR-029). Provider-specific code lives only in `deploy/terraform/modules/vm-<provider>` and a `ProviderAdapter` implementation. A second region or provider is a separate cell.
**Consequences:** Moving providers means building a new cell and migrating tenants, not editing the cell. No managed load balancer, block storage or secret manager is relied on; on AWS that means no ELB/NLB, EBS CSI, Secrets Manager/KMS for workloads or instance profile (architecture §4.3). Where a provider has no private network (Hostinger), the deviation is recorded in ADR-029 and every node-to-node path is encrypted and allow-listed.
**Revisit when:** not expected.

### ADR-003 — flannel `wireguard-native` + K3s's network-policy controller
**Status:** Accepted (spike-gated: S1)
**Context:** Tenant isolation needs enforced NetworkPolicy; node-to-node pod traffic carries customer data over a provider network we don't control (on Hostinger, the public interface).
**Decision:** K3s's bundled flannel with the `wireguard-native` backend between the nodes' addresses, and K3s's embedded network-policy controller (kube-router) for NetworkPolicy. No extra CNI. Storage replication runs on the host network and is encrypted separately with DRBD TLS (ADR-027).
**Consequences:** Pod traffic between nodes is encrypted; no extra pods. The CNI is fixed at cluster creation; if S1 finds a policy gap, the fallback is Calico in a new cell.
**Revisit when:** S1 finds a NetworkPolicy semantics gap we rely on.

### ADR-004 — Storage: TopoLVM on local NVMe; redundancy by Postgres replication
**Status:** Superseded by ADR-027 (2026-10-07). TopoLVM served Step 0.3 on the local cell and is being removed.
**Context:** Network block storage costs extra and isn't guaranteed across providers; CNPG recommends shared-nothing local storage.
**Decision (historical):** `tenant-local` TopoLVM thick LVs in `tenantvg` on each node's NVMe for every plan; HA plans got redundancy from a replica on another node.
**Why superseded:** it ties each database to the disk of the VM that runs it, so a compute VM can't be replaced without a restore, and one VM's disk caps both compute and storage. The owner's goal is compute on VMs and data separated from them.

### ADR-005 — Custom Rust pg-gateway for Postgres ingress
**Status:** Accepted (spike-gated: S3)
**Context:** Every tenant needs a stable hostname on port 5432, per-tenant allowlists, limits, suspension errors and correct handling of SSLRequest, direct TLS and cancel requests. Traefik's Postgres STARTTLS routing can't route plaintext CancelRequests (they carry no SNI) or answer with Postgres errors; PgDog is AGPL and doesn't route by SNI; pgcat is unmaintained.
**Decision:** Build `pg-gateway` in Rust (tokio, rustls, kube-rs), borrowing handshake patterns from Neon's Apache-2.0 proxy. It terminates TLS, resolves tenants by SNI (or `options=endpoint`), enforces access and limits, and relays the pooled hostname to the tenant's PgBouncer (transaction mode) and the `-direct` hostname to Postgres itself. It dials pod IPs from EndpointSlices and always strips `SCRAM-SHA-256-PLUS`. Cancel keys encode the issuing replica and an HMAC; a cancel that lands on another replica is forwarded over internal mTLS. Readiness requires a complete route list; a replica that starts during an API outage bootstraps from a peer's signed snapshot. It runs as a DaemonSet on every **compute node** with `hostPort` 5432, and DNS returns one A record per compute node (ADR-013).
**Consequences:** We own a data-path component, so it gets fuzzing, a client test matrix, one-node-at-a-time rollout and per-tenant metrics. No load balancer to pay for; libpq, pgx and Node.js try each address, so a dead node is skipped. `channel_binding=require` isn't available because TLS ends at the gateway.
**Revisit when:** customers require end-to-end channel binding, or the provider offers a cheap floating IP (then one IP replaces the multi-record scheme).

### ADR-006 — Cloudflare Tunnel for all HTTP ingress and admin access
**Status:** Accepted (spike-gated: S9)
**Context:** HTTP services need TLS, WAF and DDoS protection without exposing an origin; admins need SSH and `kubectl` without public ports or a paid bastion.
**Decision:** One Cloudflare tunnel **per cell** for HTTP apps (cloudflared Deployment on `control-1`; 1 replica on the launch cell, 2 with HA), including `auth.example.com` for the self-hosted Auth (ADR-028). A second, host-level tunnel (`cloudflared` systemd service on each VM) exposes SSH and the K3s API as Cloudflare Access applications restricted to the owner with MFA. No public 443, 22 or 6443 on any VM. From Phase 4, **Cloudflare Load Balancing** spreads `api`, `mcp` and `oauth` across two cells.
**Consequences:** Origins are unreachable except through Cloudflare. Proxy read timeout is 125 s, so MCP streams send heartbeats. Customer Postgres traffic never uses Cloudflare's proxy. The provider console is the break-glass path when Cloudflare is down.
**Revisit when:** not expected.

### ADR-007 — Supabase as the admin/management database (only), Free first
**Status:** Superseded by ADR-028 (2026-10-07).
**Decision (historical):** Supabase Free in `ap-south-1` through the Supavisor pooler, then Pro at a trigger; our own nightly `pg_dump` to R2.
**Why superseded:** Supabase Free has no backups or PITR, 500 MB and pauses after a week of low activity; Pro is US$25/month (≈ ₹2,400) and PITR adds US$100/month (supabase.com/pricing, checked 2026-10-01). The control plane already runs on our VMs, and CNPG gives the admin DB continuous WAL archiving to R2 at no extra cost. Supabase stays the documented fallback (ADR-028).

### ADR-008 — Supabase Auth for user identity
**Status:** Superseded by ADR-028 (2026-10-07). The Auth software stays; it is now self-hosted.
**Decision (historical):** hosted Supabase Auth with Turnstile, TOTP MFA, Google/GitHub, custom SMTP, ES256 JWTs verified via JWKS.

### ADR-009 — Own OAuth 2.1 server (`mcp-auth`) for MCP clients
**Status:** Accepted (spike-gated: S5)
**Context:** MCP 2026-07-28 wants Client ID Metadata Documents, audience-bound tokens (RFC 8707) and loopback redirects on any port. Supabase's OAuth 2.1 server is beta, has no CIMD, fixes `aud` to `authenticated` and rejects ephemeral loopback ports.
**Decision:** `mcp-auth` in Go on zitadel/oidc (OpenID-certified): PKCE, CIMD (SSRF-guarded fetcher), DCR fallback. Thin middleware adds `resource` → `aud` (RFC 8707, kept across refresh), a default `mcp` scope and the RFC 9207 `iss` response parameter. The first-party CLI uses the device authorization grant (RFC 8628) with `aud` = `https://api.example.com`. Login is delegated to the self-hosted Supabase Auth (ADR-028); consent is our dashboard page. Signing keys are Kubernetes Secrets, encrypted at rest.
**Consequences:** A security-sensitive component we own, covered by the MCP conformance suite and an external review before public MCP launch.
**Revisit when:** Supabase Auth supports CIMD and RFC 8707.

### ADR-010 — River for outbox + job queue
**Status:** Accepted
**Context:** State changes and their follow-up jobs must commit atomically.
**Decision:** River jobs inserted in the same transaction as the desired-state change (works through the admin DB's PgBouncer Pooler in transaction mode); unique jobs per idempotency key; the coordinator (LISTEN/NOTIFY, leader election) uses direct session connections to the admin DB (ADR-028).
**Consequences:** One component instead of outbox + poller + queue. River's `PollOnly` mode is the fallback if session connections become scarce.
**Revisit when:** measured job volume needs a streaming system.

### ADR-011 — Pull-based cluster agent + `TenantDatabase` operator
**Status:** Accepted
**Context:** The Kubernetes API must stay private; the design must work for one cell or many, on any provider.
**Decision:** Per-cell agent pulls desired specs over mTLS; operator renders all tenant objects from one CRD; pg-gateway reads routes from the same CRD. The agent deletes only on explicit tombstones, rejects generation rollbacks and quarantines unknown objects.
**Consequences:** Cells are reached outbound only; adding a cell = installing the agent. Control-plane outages freeze changes, never traffic.
**Revisit when:** not expected.

### ADR-012 — Cloudflare R2 for backups and etcd snapshots
**Status:** Accepted (spike-gated: S4, S11)
**Context:** Backups must be off-cell, provider-neutral, cheap to store and free to restore, and give every plan point-in-time recovery; etcd and the admin DB need off-cell copies too.
**Decision:** One R2 bucket per cell for tenants (prefix per tenant), one for etcd snapshots and one for the admin DB (`dbcloud-admin-pg`, ADR-028); Barman Cloud Plugin with checksum env vars and `AWS_DEFAULT_REGION=auto`. Continuous WAL (zstd) and **weekly** base backups in per-tenant slots (uncompressed: R2 rejects the uneven multipart parts compressed barman streams produce). Retention checks every 6 h (R2 bills LIST as Class A). One bucket-wide **8-day bucket lock** per backup bucket. K3s etcd snapshots every 6 h, 28 kept, via `--etcd-s3`. R2 is the durable, bottomless tier of the storage design (ADR-031).
**Consequences:** 7-day PITR on every plan. R2 is free up to 10 GB and 1 M Class A operations, then US$0.015/GB-month (`03-…` §8). No object versioning in R2, so the lock is the protection. No India jurisdiction; India-only customers get an India S3-compatible `ObjectStore`.
**Revisit when:** barman fixes uneven part sizes, or S4 fails.

### ADR-013 — Cloudflare DNS for database hostnames, one A record per compute node
**Status:** Accepted (spike-gated: S3, S10)
**Context:** Per-query-billed DNS could create an unbounded bill; there is no managed load balancer; tenants are routed by TLS SNI.
**Decision:** `*.db.example.com` is a **DNS-only** wildcard in the Cloudflare zone with one A record per compute node (each runs pg-gateway), TTL 300 s. The worker adds and removes node records through the Cloudflare API. Tenants in a non-default cell get explicit records. cert-manager uses the Cloudflare DNS-01 solver. DNSSEC is Cloudflare's automatic signing.
**Consequences:** DNS is free and unmetered. Clients skip a dead node's address. A Cloudflare authoritative-DNS outage affects new lookups after the TTL. The Free zone allows 200 records.
**Revisit when:** explicit records approach the zone limit, or a floating IP replaces the multi-record scheme (ADR-005).

### ADR-014 — Cedar for authorization
**Status:** Accepted
**Decision:** cedar-go evaluated in-process (control-api, mcp-server) with entities from the admin DB; policies versioned and tested in `policies/cedar`.
**Revisit when:** cross-org sharing needs a relationship graph (then OpenFGA).

### ADR-015 — VictoriaMetrics, VictoriaLogs, Grafana per cell
**Status:** Accepted
**Decision:** VictoriaMetrics single-node + vmagent + vmalert + Alertmanager from Helm charts, without an operator; VictoriaLogs + Fluent Bit (including the K3s audit log); Grafana behind Cloudflare Access; an external Workers Cron probe for whole-cell outages. All on `control-1`, volumes on `platform-local`.
**Revisit when:** multi-cell retention needs → VictoriaMetrics cluster version.

### ADR-016 — Flux for GitOps, SOPS for secrets; tenants are not in git
**Status:** Accepted
**Decision:** Flux reconciles platform components per cell and decrypts **SOPS**-encrypted secrets with an age key that exists only in the cell and the owner's password manager. K3s encrypts Secrets at rest. Tenant objects come only from the admin DB via agent/operator.
**Consequences:** No paid secret manager; rotating a secret is a commit. Losing the age key means re-creating secrets, so it is backed up offline.
**Revisit when:** dynamic credentials are needed (OpenBao, Phase 5).

### ADR-017 — Capacity controller with a budget guard and a provider adapter
**Status:** Accepted
**Context:** Placement depends on compute-node CPU and memory (against the CPU baseline on credit-based VMs), storage-pool free space, pod slots, plan envelopes and headroom; spend must never run ahead of revenue; the provider may have no API.
**Decision:** A worker loop computes demand per cell and node role and never plans beyond the owner-set `max_nodes`; at the limit, new databases are waitlisted. Nodes are requested through `ProviderAdapter`: the launch implementation `manual` alerts the owner with the exact node to create from the bootstrap; a provider-API implementation (`hostinger` if S12 confirms its power-state and stop endpoints, `aws` for an AWS cell) is added when used. The same adapter powers the compute-node fencer (ADR-027). Nodes are removed only after evacuation; storage volumes move online before a storage node is retired.
**Revisit when:** not expected.

### ADR-018 — No instance credentials in workloads
**Status:** Accepted (spike-gated: S1)
**Decision:** No workload uses provider instance credentials or metadata services. NetworkPolicy blocks 169.254.169.254 and private/link-local ranges for egress in every namespace except explicit allows. Each external API (Cloudflare, R2, Razorpay, email, the VM provider's API for the worker) gets a scoped token held as a SOPS-encrypted Secret, readable only by the service that uses it.
**Revisit when:** not expected.

### ADR-019 — Cell-based scaling
**Status:** Accepted
**Decision:** Planned limits per cell: 50 nodes, 1,500 tenants. At 70 % of any limit the next cell is provisioned; placement stops using a cell at 100 %. The same code serves one cell or many, on any provider. The Phase 2 scale gate (1,000 hibernated synthetic tenants, no compute cost) measures the limits.
**Revisit when:** load tests show a cell can safely hold more.

### ADR-020 — Language split: Rust for the data path, Go for everything else
**Status:** Accepted
**Decision:** Rust for `pg-gateway` only. Go for control-api, worker, cluster-agent, tenant-operator, compute governor, WAL receiver controller, mcp-server, mcp-auth, CLI. TypeScript with Next.js for the web apps (ADR-021). Every backend component ships as a Docker-built multi-arch image (`amd64` + `arm64`) in ghcr.io.
**Revisit when:** another component lands on the per-connection data path.

### ADR-021 — Next.js static exports on Workers Static Assets
**Status:** Accepted
**Context:** We want Next.js for the public site and both dashboards, at no hosting cost. Full Next.js on Cloudflare needs the OpenNext adapter, and the Workers Free plan's 10 ms CPU per request is too tight for server rendering. Next.js's recent critical vulnerabilities are server-side.
**Decision:** Three Next.js 16 App Router apps (`web/site`, `web/customer`, `web/admin`) built with `output: 'export'` and deployed with wrangler to Workers Static Assets. They run fully in the browser: the Supabase Auth client (`@supabase/auth-js`) against `https://auth.example.com` for sign-in (ADR-028), the generated control-api client with TanStack Query for data. Resource pages take IDs as query parameters. The admin app sits behind Cloudflare Access. A post-build step writes a hash-based CSP into `_headers`.
**Consequences:** Hosting is free and unlimited; no server-side Next.js code runs in production. No route handlers, proxy, server actions, ISR or default image optimization.
**Revisit when:** a feature truly needs server rendering; then OpenNext on Workers Paid (US$5/month) from revenue.

### ADR-022 — Plans and QoS
**Status:** Accepted for the plan catalog; its QoS rule ("memory request = limit on every container") is **superseded by ADR-030** (2026-10-07).
**Decision:** Three launch plans (`03-…` §4): `base` 0.1 vCPU guaranteed / 0.5 burst, 512 MiB, 5 GB, ₹299 + GST; `plus` 0.25 / 1, 1 GiB, 10 GB; `premium` 0.5 / 1, 2 GiB, 20 GB. CPU request = the guaranteed vCPU, CPU limit = the burst. Each plan's RAM is now the memory cap; the request is the user's minimum (ADR-030).
**Consequences:** Small plans feel fast when a node is idle and still get their guaranteed share under contention.
**Revisit when:** noisy-neighbour metrics show burstable tenants harming others, or the price review (CU pricing, ADR-030) changes a plan.

### ADR-023 — We build the DBaaS; we reuse building blocks
**Status:** Accepted
**Decision:** Reuse focused components (K3s, CloudNativePG, PgBouncer, LINSTOR/DRBD through the Piraeus Operator, River, zitadel/oidc, Supabase Auth self-hosted), not packaged DBaaS products and not Neon's storage engine (ADR-031).
**Consequences:** We own the control plane, operator, gateway, compute governor and MCP stack, and with them the product and its economics.

### ADR-024 — Budget ≤ ₹9,000/month and provider choice by checklist
**Status:** Accepted (spike-gated: S12)
**Context:** The whole platform must cost ≤ ₹9,000/month. Separated storage needs three VMs at launch (ADR-029), so price per GB of RAM and disk decides the provider.
**Decision:** The launch cell is Hostinger India KVM 2 + KVM 8 + KVM 4 at **₹7,997/month renewal** (₹4,097 intro on a 24-month prepay), ex-GST (`03-…` §1, §2.2). It is accepted only after spike S12 passes inside Hostinger's 30-day money-back window; the fallback is DigitalOcean BLR1 as a 2-node design (`03-…` §2.2). Everything else stays on free plans until revenue pays for more. Supabase Pro is no longer part of the plan (ADR-028).
**Consequences:** Launch cost ≈ ₹8,067/month at renewal including the domain (≈ ₹4,167 during the intro term). The 24-month prepay at intro prices is ≈ ₹98,328 upfront; the term is an owner decision. Break-even depends on the CPU-capacity rule in `03-…` §6.1 and §7 (owner decision, ADR-030).
**Provider record (S12):** prices, checklist results, S12 numbers, the support answer on transfer accounting and the final choice are recorded here.
**Revisit when:** revenue makes a provider with network block volumes (AWS gp3) affordable for a big-database cell, or the chosen provider fails the checklist.

### ADR-025 — Launch on one 2 vCPU / 4 GB VM; high availability deferred
**Status:** Superseded by ADR-029 (2026-10-07). High availability stays deferred.
**Decision (historical):** one VM `node-1` running the K3s server, the platform and the tenants on TopoLVM.
**What carries over:** embedded etcd with `cluster-init` rather than SQLite (K3s's profiling measured 1,606 MB vs 1,596 MB, https://docs.k3s.io/reference/resource-profiling), one replica per platform component, no single-node assumptions, and the local `reference` profile as the optional HA compatibility check. The Step 0.3 measurements (10 `base` tenants Ready in 79 s on k3d, ≈ 104–127 MiB idle per tenant against 720 Mi reserved) feed `03-…` §5–§6.

### ADR-026 — SeaweedFS as the local S3; Flux reads the local cell from an OCI (Open Container Initiative) artifact
**Status:** Accepted (2026-09-30, Step 0.3)
**Context:** Phase 0 needs an S3-compatible store that stands in for R2, and a Flux source, on one PC with no cloud accounts. MinIO, planned for this, stopped publishing community-edition releases and images in 2025-10, so there is no supported, patched build to pin. For Flux, a git server inside the cell or pulling from GitHub on every change would add a component or a network dependency to every local edit.
**Decision:** (1) The local cell uses **SeaweedFS** 4.48 (Apache-2.0, single pod `weed server -s3`, digest-pinned) as its S3 endpoint, only in `deploy/local/s3`. Production keeps R2; nothing outside the local overlay references SeaweedFS. (2) Locally, `hack/cell-flux.sh` pushes `deploy/` as an OCI artifact (`flux push artifact`) to a localhost-only registry, and an `OCIRepository` feeds the same Kustomization tree production uses. Production Flux reads git (Step 1.3).
**Consequences:** Barman's S3 client is exercised against a real S3 API locally; R2-specific behaviour (multipart part sizes, checksum headers, `region auto`) is still proven only by spike S4 on R2. SeaweedFS needed an upload-buffer cap and a 512 Mi limit to survive a 1 GB base backup. Local rollouts go through Flux exactly as in production, but the local source is an artifact, not a commit, so git history is not what the local cell runs.
**Revisit when:** MinIO or another maintained, permissively licensed S3 server fits better, or the local cell moves to a git source.

### ADR-027 — Separated storage: LINSTOR + DRBD 9 on storage nodes, diskless compute
**Status:** Accepted (2026-10-07; spike-gated: Step 0.3b locally, S12 on the launch provider)
**Context:** The owner wants compute only on VMs, data separated from them, storage that grows very large, low latency, and no PlanetScale-style fixed NVMe per compute VM. Hostinger sells no add-on disks and KVM 8 (400 GB) is its top plan. Engines compared (`.local/reports/research-storage-split.md` §2, §6.1; `research-bottomless.md` §2):
- **Mayastor:** each storage node burns 2 busy-polling cores and pins 2 GiB of hugepages; on a CPU-credit VPS that drains the credit balance.
- **Ceph RBD:** the only engine without a per-node ceiling, but heavy (≈ 4 GiB per OSD), and `size: 1` pools lose data. Worth revisiting at 3–5 storage nodes on ≥ 10 GbE.
- **Kernel nvmet + CSI drivers:** immature projects, no replication, and 2026 pre-auth CVEs in `nvmet-tcp`/`nvme-tcp`.
- **Object-store block devices:** every commit waits for an object PUT (tens to hundreds of ms).
**Decision:**
- **Engine:** **LINSTOR + DRBD 9 through the Piraeus Operator v2.12.0** (LINSTOR 1.35.2, LINSTOR CSI 1.13.1, DRBD 9.3.4; GPL-2.0 / GPL-3.0 / Apache-2.0). It is kernel code with ≈ 32 MiB RAM per TiB per peer and supports diskless clients, single-replica volumes, LVM-thin pools, snapshots, online expansion and online migration between storage nodes.
- **`tenant-network` StorageClass:**
  - placement count 1 at launch, storage pools only on `dbcloud.io/pool=storage` nodes, diskless access from compute nodes, LVM-thin, online expansion, snapshots;
  - DRBD options `on-no-data-accessible: suspend-io`, `on-suspended-primary-outdated: force-secondary`, `rr-conflict: retry-connect`.
- **`platform-local` StorageClass:** one replica on `control-1`'s own LINSTOR pool, for the admin DB, VictoriaMetrics, VictoriaLogs and the WAL receiver.
- **Network:** DRBD runs on the host network (a satellite restart doesn't cut the data path) with **DRBD TLS (kTLS)**. DRBD and LINSTOR ports are allowed only from peer node IPs (nftables). flannel `wireguard-native` stays for pod traffic.
- **Compute-node failover without HA:** a fencer in the worker's capacity controller confirms the VM is powered off through `ProviderAdapter` (the `AMBIGUOUS` rule applies), applies `node.kubernetes.io/out-of-service`, and CNPG recreates the pod on another compute node, where DRBD reattaches diskless. kube-controller-manager runs with `disable-force-detach-on-timeout: true`.
**Consequences:**
- **Latency** (planning values, measured in Step 0.3b and S12): each commit costs one network round trip plus the storage node's flush, ≈ 0.3–1.5 ms on a same-datacenter network. Reads from compute RAM are unchanged; cache misses take ≈ 0.3–1 ms. Compute nodes are therefore RAM-heavy.
- **Bandwidth:** a 1 Gbps port caps bulk traffic at ≈ 115 MB/s, so DRBD resync (`c-max-rate`), backups and egress are shaped. A 100 GB move takes ≈ 15 min.
- **Ceiling:** one database is capped by one storage node's pool. The launch maximum is ≈ 200 GB on a 400 GB node, with per-plan caps in `03-…` §4. Total capacity grows by adding or enlarging storage nodes; volumes move online with `linstor resource toggle-disk --migrate-from`.
- **Node loss:** losing a compute node costs ≈ 1–2 min with a second compute node; with one, the database waits for the VM. Losing the storage node stops its volumes until it returns, or tenants restore from R2 plus the WAL receiver (ADR-031).
- **Fencing:** single-primary DRBD + `suspend-io` fences a half-dead compute node by construction.
- **Operations:** the DRBD module is built per kernel by the Piraeus loader, so kernel updates are gated on a module build test.
- **HA later:** placement count 2 across two storage nodes plus a diskless tie-breaker gives DRBD quorum, the Piraeus HA controller (≈ 10 s failover) and RPO 0.
**Revisit when:** ≥ 3–5 storage nodes on ≥ 10 GbE (Ceph striping); a customer needs a database larger than the biggest storage node (bigger nodes or an AWS gp3 cell, up to 64 TiB); or an object-store page tier becomes production-ready (ADR-031).

### ADR-028 — Admin DB and Auth self-hosted on `control-1`
**Status:** Accepted (2026-10-07; spike-gated: S7)
**Context:** Supabase Free has no backups or PITR, 500 MB, and pauses after a week of low activity. Pro costs US$25/month (≈ ₹2,400) and PITR adds US$100/month (supabase.com/pricing, 2026-10-01). The control plane already runs on our VMs; CNPG and R2 already give tenants continuous WAL archiving and PITR at no extra cost.
**Decision:**
- **Admin DB:** CNPG cluster `dbcloud-admin` in namespace `dbcloud-control` on `platform-local`.
  - Durability: WAL archive and base backups to its own R2 bucket `dbcloud-admin-pg` with a bucket lock. The nightly `ExportAdminDB` logical dump is kept as a second, format-independent copy. A weekly restore drill runs.
  - Connections: a PgBouncer `Pooler` (transaction mode, pgx `QueryExecModeExec`) for API paths. Direct session connections for the River coordinator and LISTEN, migrations and dumps.
  - TLS: `sslmode=verify-full` against the CNPG cluster CA.
- **Auth:** self-hosted **Supabase Auth** (github.com/supabase/auth, MIT, Go; image by digest) on the admin DB's `auth` schema, at `https://auth.example.com` through Cloudflare Tunnel + WAF.
  - Features: ES256 signing keys with a published JWKS (key variables `JWT_KEYS` / `JWT_JWKS` per Supabase's self-hosting docs; names verified at implementation), Turnstile CAPTCHA, TOTP MFA, custom SMTP, rate limits.
  - Keys live only in SOPS-encrypted Secrets.
- **Local development:** `supabase start` stays only until Step 0.6 replaces it with the same admin DB and Auth in the local cell.
**Consequences:**
- **Cost and durability:** ₹0 instead of ₹2,400–12,000/month, and admin-DB RPO falls from 24 h (Free + nightly dump) to `archive_timeout` (60 s).
- **Operations:** we operate one more Postgres and an identity service, covered by restore drills, alerts and the security gate.
- **Static stability:** an outage of `control-1` stops sign-in and changes but never running databases.
- **Fallback:** dump and restore into managed Postgres or Supabase.
**Revisit when:** admin write volume or compliance needs outgrow one CNPG instance, or the Phase 4 second cell needs a cross-cell admin-DB topology.

### ADR-029 — Launch cell: three node roles (`control-1`, `storage-1`, `compute-1`); high availability deferred
**Status:** Accepted (2026-10-07; spike-gated: S12)
**Context:** Separated storage (ADR-027) needs storage and compute on different VMs; the control plane and platform should not compete with tenants for CPU credits; the budget is ₹9,000/month.
**Decision:**
- **`control-1`** (label `dbcloud.io/pool=control`): the single K3s server (embedded etcd, `cluster-init`). It runs every platform and control-plane workload (Flux, cert-manager, CNPG + Barman plugin, Piraeus/LINSTOR controller + CSI controller, observability, control-api, worker, tenant-operator, cluster-agent, mcp-server, mcp-auth, River UI, cloudflared, admin DB, Auth, WAL receiver). Nothing is reachable inbound from the internet; HTTP leaves through the outbound Tunnel.
- **`compute-N`** (K3s agents, `dbcloud.io/pool=compute`): tenant Postgres and PgBouncer pods and pg-gateway (`hostPort` 5432, public). Tenant volumes are DRBD diskless clients, so no tenant data sits on these disks. They are RAM-heavy, because their RAM is the hot cache.
- **`storage-N`** (K3s agents, `dbcloud.io/pool=storage`, taint `dbcloud.io/pool=storage:NoSchedule`): LINSTOR satellite on the host network, DRBD 9 and an LVM-thin pool on local NVMe holding all tenant volumes. Only storage DaemonSets run there.
- **Scheduling:** tenant pods use nodeSelector `pool=compute`, platform pods `pool=control`.
- **Launch hardware:** Hostinger India (Mumbai): `control-1` KVM 2 (2 vCPU / 8 GB / 100 GB), `storage-1` KVM 8 (8 vCPU / 32 GB / 400 GB), `compute-1` KVM 4 (4 vCPU / 16 GB / 200 GB).
- **Recorded deviation from `03-…` §2.1 item 4:** Hostinger documents no private network. All node-to-node traffic therefore crosses the public interface and is encrypted (WireGuard, DRBD TLS, K3s TLS) and allow-listed by peer IP.
- **Fallbacks:** DigitalOcean BLR1 (free VPC, Volumes up to 16 TiB) as a 2-node design. AWS Mumbai is the later big-database cell.
**Consequences:**
- **Capacity:** sold against Hostinger's 30 % CPU baseline, and the transfer allowance is alerted at 70 % (`03-…` §2.2, §6.1).
- **Failure:** a lost compute node fails over once `compute-2` exists (ADR-027). A lost storage node means a restore. A lost `control-1` stops changes and sign-in while databases keep serving, then is rebuilt from the etcd snapshot and R2.
- **Code:** stays HA-compatible.
**Revisit when:** S12 fails (fallback provider), or the HA trigger in `04-…` is met: three servers (`control-1..3`), placement count 2 + tie-breaker, 2 platform replicas + PDBs.

### ADR-030 — Compute autoscaling inside user caps: Burstable tenants + compute governor; CU metering
**Status:** Accepted (2026-10-07; the CU unit, its price and the CPU-capacity rule are owner decisions)
**Context:** Users should choose a range (for example up to 1 vCPU / 2 GB) and the database should scale inside it. Neon resizes VMs, which needs `/dev/kvm`, unavailable on our VMs. CNPG 1.30 restarts the pod on any `.spec.resources` change (PR #11116, which would allow in-place resize, is unmerged). PostgreSQL can't resize `shared_buffers` at runtime (`.local/reports/research-building-blocks.md` §2–§3, §10.1).
**Decision:**
- **Range:** users pick min and max inside per-plan caps (`03-…` §4). CNPG CPU request = min, CPU limit = max; memory request = min, memory limit = max (the hard cap). Tenants are Burstable; platform pods keep request = limit.
- **Compute governor:** a Go DaemonSet on compute nodes. It steers each tenant's `memory.high` and `memory.reclaim` inside [floor, max] from PSI and `memory.stat`. It keeps the node's sum of targets ≤ allocatable − headroom, so kubelet eviction is never reached.
- **Postgres parameters:** `shared_buffers` is sized for the min; `max_connections` and `effective_cache_size` from the max.
- **Range edits:** the governor never patches CNPG resources. A plan-range edit is a rare, announced, restart-based change.
- **Metering:** allocated compute per second plus GB-months. Proposed unit **1 CU = 1 vCPU + 2 GiB, in 0.25 steps**; the unit and the price per CU-hour are owner decisions. Until then the fixed monthly plans apply.
- **Priority:** priority classes `dbcloud-platform` > `dbcloud-tenant`.
**Consequences:**
- CPU scales instantly between min and max; memory grows and shrinks without restarts.
- The governor needs write access to the cgroup filesystem (PSA exemption, second reviewer).
- Capacity admission uses Σ min. On credit-based VMs it also has to respect the CPU baseline: the strict rule (Σ min ≤ baseline) or a usage-gated rule is an owner decision with the numbers in `03-…` §6.1 and §7.
- **Scale-to-zero later:** an idle floor (ms wake) and CNPG hibernation woken by pg-gateway.
**Revisit when:** CNPG ships an in-place resource strategy, Kubernetes MemoryQoS covers the governor's job, or the owner sets CU prices.

### ADR-031 — No object storage on the commit path; R2 is the bottomless durable tier; object-store page tier deferred
**Status:** Accepted (2026-10-07)
**Context:**
- **How Neon removes the ceiling:** object storage (S3) is its source of truth; pageservers cache layers on NVMe; big tenants are sharded across pageservers; commits wait only for a safekeeper quorum. Neon's plans page now says paid storage is unlimited (its earlier docs said 16 TB per branch).
- **What exists in open source (2026):** no production-ready way to give vanilla Postgres that tier. Neon's public repository has been frozen since mid-2025 at PG 17, and self-hosting it is unsupported.
- **Why not object-store block devices:** they put an object PUT on every commit (`.local/reports/research-bottomless.md` §1–§2, §6).
**Decision:**
- **Hot path:** Postgres commits only to the DRBD volume (ADR-027).
- **R2:** holds the continuous WAL archive and weekly base backups (7-day PITR, unchanged) and, later, fully offloaded hibernated databases.
- **WAL receiver:** an async streaming receiver per tenant on `control-1` (`pg_receivewal --synchronous --slot`, not listed in `synchronous_standby_names`) cuts RPO for storage-node loss below ≈ 1 s with no commit penalty. Synchronous RPO 0 is used only with two receivers on two machines.
- **Deferred track: "object-store page tier":**
  - **Neon-lite spike (S13, non-launch):** WAL on the network volume, PGDATA on a ZeroFS NBD device over R2 with a large local cache; ZeroFS is AGPL-3.0. Pass criteria are in `research-bottomless.md` §6.3: pgbench p99 within 2× of local for a cached working set, no data loss under kill -9, R2 operations within budget.
  - **Re-evaluation triggers:** a maintained Neon fork with PG 18 and a release cadence; SlateDB 1.0 plus a published Postgres benchmark on an object-store filesystem with a quorum WAL; OrioleDB's S3 mode GA on unpatched Postgres; or many tenants above ≈ 1–2 TB.
**Consequences:** Commit latency stays at network-block speed. The per-database ceiling is one storage node's pool (ADR-027), stated honestly per plan; the platform as a whole has no ceiling because storage nodes are added. Each WAL receiver uses a replication slot, so `max_slot_wal_keep_size` caps retained WAL and slot lag alerts.
**Revisit when:** a trigger above fires, or S13 passes and a product need appears.

---

## Earlier revisions

Blueprint revisions before 4.0 targeted a single cloud provider; ADR-001 and ADR-002 replaced them. ADR-004, ADR-007, ADR-008 and ADR-025 (and ADR-022's memory rule) were superseded on 2026-10-07 by ADR-027 to ADR-030.
