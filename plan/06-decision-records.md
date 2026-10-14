# 06 — Architecture Decision Records (Blueprint 4.0)

Format: context → decision → consequences → revisit when.
Status: **Accepted**, **Superseded by ADR-x**, or **Accepted (spike-gated: Sx)** = accepted unless the named spike in `04-build-plan.md` fails, in which case the listed fallback is adopted and this record is updated. Records superseded in 4.0 are listed at the end.

---

### ADR-001 — Cells are K3s clusters on plain VMs
**Status:** Accepted (spike-gated: S1, S11)
**Context:** Oracle Cloud can't be used, and managed Kubernetes on mainstream clouds breaks the ₹9,000/month budget. The original blueprint used K3s.
**Decision:** Every cell is one **K3s v1.36** cluster on Linux VMs in one region, in two node pools: **control VMs** (`dbcloud.io/pool=control`) are the K3s servers with embedded etcd and run the platform and control plane; **tenant VMs** (`dbcloud.io/pool=tenant`, tainted) are K3s agents and run only tenant databases and per-node DaemonSets. The first cell launches with one control VM (`ctl-1`) and one tenant VM (`node-1`) (ADR-025); more tenant agents join as capacity grows, and two more servers (`ctl-2`, `ctl-3`) join for an HA control plane at Step 1.13. Cells are independent failure and scale units registered with the control plane.
**Consequences:** No control-plane fee; we run etcd ourselves (snapshots to R2, quorum runbooks). K3s is a CNCF-certified distribution; CNPG officially targets vanilla Kubernetes, so conformance and CNPG smoke tests run on every K3s bump. The same code runs on any conformant Kubernetes, so a managed Kubernetes cell can be added later.
**Revisit when:** a contract requires an SLA-backed Kubernetes control plane, or revenue makes a managed control plane cheaper than operating etcd.

### ADR-002 — Provider-neutral, one provider and region per cell
**Status:** Accepted (spike-gated: S8)
**Context:** No provider is chosen yet. K3s supports embedded etcd only with all servers on one private network in one location; cross-provider nodes add latency, egress fees and partition risk (`03-…` §2.3).
**Decision:** A cell uses VMs from exactly one provider in one region, preferably with a free private network (without one, nodes join over public IPs with nftables allow-lists and WireGuard, `03-…` §2.1 item 4). Providers are chosen against the checklist in `03-…` §2.1; Hostinger is the launch planning example, and AWS and Azure are priced for the same layout (`03-…` §2.2). Provider-specific code lives only in `deploy/terraform/modules/vm-<provider>` (for example `vm-hostinger`, `vm-aws`, `vm-azure`) and a `ProviderAdapter` implementation. A second region or provider (including AWS, Azure or GCP) is a separate cell.
**Consequences:** Moving providers means building a new cell and migrating tenants, not editing the cell. No managed load balancer, block storage or secret manager is relied on.
**Revisit when:** not expected.

### ADR-003 — flannel `wireguard-native` + K3s's network-policy controller
**Status:** Accepted (spike-gated: S1)
**Context:** Tenant isolation needs enforced NetworkPolicy; node-to-node pod traffic (control VM ↔ tenant VMs, e.g. operator → Postgres, vmagent scrapes) carries customer data over the provider's network, which is private only where the provider offers one.
**Decision:** K3s's bundled flannel with the `wireguard-native` backend over the private network (or the public IPs where there is none), and K3s's embedded network-policy controller (kube-router) for NetworkPolicy. No extra CNI.
**Consequences:** Pod traffic between nodes is encrypted; no extra pods. The CNI is fixed at cluster creation; if S1 finds a policy gap, the fallback is Calico in a new cell.
**Revisit when:** S1 finds a NetworkPolicy semantics gap we rely on.

### ADR-004 — Storage: TopoLVM on local NVMe; redundancy by Postgres replication
**Status:** Accepted (spike-gated: S2)
**Context:** Network block storage costs extra and isn't guaranteed across providers; CNPG recommends shared-nothing local storage.
**Decision:** `tenant-local` TopoLVM thick LVs in `tenantvg` on each node's NVMe for every plan. HA plans (Phase 3) get redundancy from a replica on another node. local-path-provisioner isn't used because it ignores PVC sizes.
**Consequences:** Storage costs nothing extra; data is node-bound, so planned changes use in-place maintenance windows or evacuation, and node loss uses restore from R2. `tenantvg` exists only on tenant VMs; etcd lives on the control VMs and doesn't share a disk with tenant I/O (S2 still measures both).
**Revisit when:** TopoLVM problems appear (fallback: OpenEBS LVM LocalPV on the same VG), or a provider offers cheap local second disks.

### ADR-005 — Custom Rust pg-gateway for Postgres ingress
**Status:** Accepted (spike-gated: S3)
**Context:** Every tenant needs a stable hostname on port 5432, per-tenant allowlists, limits, suspension errors and correct handling of SSLRequest, direct TLS and cancel requests. Traefik's Postgres STARTTLS routing can't route plaintext CancelRequests (they carry no SNI) or answer with Postgres errors; PgDog is AGPL and doesn't route by SNI; pgcat is unmaintained.
**Decision:** Build `pg-gateway` in Rust (tokio, rustls, kube-rs), borrowing handshake patterns from Neon's Apache-2.0 proxy. It terminates TLS, resolves tenants by SNI (or `options=endpoint`), enforces access and limits, and relays the pooled hostname to the tenant's PgBouncer (transaction mode) and the `-direct` hostname to Postgres itself. It dials pod IPs from EndpointSlices and always strips `SCRAM-SHA-256-PLUS`. Cancel keys encode the issuing replica and an HMAC; a cancel that lands on another replica is forwarded over internal mTLS. Readiness requires a complete route list; a replica that starts during an API outage bootstraps from a peer's signed snapshot. It runs as a DaemonSet on every tenant node with `hostPort` 5432, and DNS returns one A record per node (ADR-013).
**Consequences:** We own a data-path component, so it gets fuzzing, a client test matrix, one-node-at-a-time rollout and per-tenant metrics. No load balancer to pay for; libpq, pgx and Node.js try each address, so a dead node is skipped. `channel_binding=require` isn't available because TLS ends at the gateway.
**Revisit when:** customers require end-to-end channel binding, or the provider offers a cheap floating IP (then one IP replaces the multi-record scheme).

### ADR-006 — Cloudflare Tunnel for all HTTP ingress and admin access
**Status:** Accepted (spike-gated: S9)
**Context:** HTTP services need TLS, WAF and DDoS protection without exposing an origin; admins need SSH and `kubectl` without public ports or a paid bastion.
**Decision:** One Cloudflare tunnel **per cell** for HTTP apps (cloudflared Deployment, 2 replicas). A second, host-level tunnel (`cloudflared` systemd service on each VM) exposes SSH and the K3s API as Cloudflare Access applications restricted to the owner with MFA. No public 443, 22 or 6443 on any VM; control VMs accept nothing from the internet. From Phase 4, **Cloudflare Load Balancing** spreads `api`, `mcp` and `oauth` across two cells.
**Consequences:** Origins are unreachable except through Cloudflare. Proxy read timeout is 125 s, so MCP streams send heartbeats. Customer Postgres traffic never uses Cloudflare's proxy. The provider console is the break-glass path when Cloudflare is down.
**Revisit when:** not expected.

### ADR-007 — Supabase as the admin/management database (only), Free first
**Status:** Accepted (spike-gated: S7)
**Context:** The control plane needs a managed Postgres for its own metadata and job queue. Supabase Free costs nothing but has no backups, 500 MB, 5 GB egress, pausing after a week of low activity, and IPv6-only direct connections.
**Decision:** Supabase **Free** in `ap-south-1` at launch (`dbcloud-prod` + `dbcloud-staging`), then **Pro** at the `03-…` §7 trigger. The admin DB stays on Supabase in the two-VM layout (ADR-025). All connections go through the Supavisor pooler over IPv4: transaction mode with pgx `QueryExecModeExec` for request handlers, session mode for the River coordinator, migrations and dumps. Per-service roles, network restrictions to the node IPs, SSL enforcement, Data API disabled, RLS on. Our own nightly `pg_dump` to R2 with weekly restore drills; migrations run as an in-cell Job that dumps first.
**Consequences:** No platform database to operate. Admin-DB RPO is 24 h on Free (bounded by pre-migration dumps, `RebuildCatalogFromCells` and Razorpay records); 6 h on Pro. Running databases don't depend on Supabase (static stability).
**Open owner option (not a decision):** host the admin DB on `ctl-1` as a CNPG cluster instead (≈ 1.3 GiB planning, inside `ctl-1`'s spare memory, `03-…` §6.1).
**Revisit when:** control-plane write volume or compliance needs exceed Supabase Pro/Team economics.

### ADR-008 — Supabase Auth for user identity
**Status:** Accepted
**Context:** Users need sign-up, social login, MFA and CAPTCHA.
**Decision:** Supabase Auth with Turnstile, TOTP MFA (required for org owners/admins), Google/GitHub, custom SMTP through the chosen transactional email service, ES256 JWTs verified by our Go services via JWKS. Orgs and roles stay in our schema. SAML SSO is added after the Pro upgrade when a customer needs it.
**Consequences:** No identity server to run. All launch features are included in the Free plan.
**Revisit when:** not expected.

### ADR-009 — Own OAuth 2.1 server (`mcp-auth`) for MCP clients
**Status:** Accepted (spike-gated: S5)
**Context:** MCP 2026-07-28 wants Client ID Metadata Documents, audience-bound tokens (RFC 8707) and loopback redirects on any port. Supabase's OAuth 2.1 server is beta, has no CIMD, fixes `aud` to `authenticated` and rejects ephemeral loopback ports.
**Decision:** `mcp-auth` in Go on zitadel/oidc (OpenID-certified): PKCE, CIMD (SSRF-guarded fetcher), DCR fallback. Thin middleware adds `resource` → `aud` (RFC 8707, kept across refresh), a default `mcp` scope and the RFC 9207 `iss` response parameter. The first-party CLI uses the device authorization grant (RFC 8628) with `aud` = `https://api.example.com`. Login is delegated to Supabase Auth; consent is our dashboard page. Signing keys are Kubernetes Secrets, encrypted at rest.
**Consequences:** A security-sensitive component we own, covered by the MCP conformance suite and an external review before public MCP launch.
**Revisit when:** Supabase Auth supports CIMD and RFC 8707.

### ADR-010 — River for outbox + job queue
**Status:** Accepted
**Context:** State changes and their follow-up jobs must commit atomically.
**Decision:** River jobs inserted in the same transaction as the desired-state change (works through the transaction pooler); unique jobs per idempotency key; the coordinator runs on session-mode connections.
**Consequences:** One component instead of outbox + poller + queue. River's `PollOnly` mode is the fallback if session connections become scarce.
**Revisit when:** measured job volume needs a streaming system.

### ADR-011 — Pull-based cluster agent + `TenantDatabase` operator
**Status:** Accepted
**Context:** The Kubernetes API must stay private; the design must work for one cell or many, on any provider.
**Decision:** Per-cell agent pulls desired specs over mTLS; operator renders all tenant objects from one CRD; pg-gateway reads routes from the same CRD. The agent deletes only on explicit tombstones, rejects generation rollbacks and quarantines unknown objects.
**Consequences:** Cells are reached outbound only; adding a cell (K3s or managed Kubernetes) = installing the agent. Control-plane outages freeze changes, never traffic.
**Revisit when:** not expected.

### ADR-012 — Cloudflare R2 for backups and etcd snapshots
**Status:** Accepted (spike-gated: S4, S11)
**Context:** Backups must be off-cell, provider-neutral, cheap to store and free to restore, and give every plan point-in-time recovery; etcd needs off-cell snapshots too.
**Decision:** One R2 bucket per cell for tenants (prefix per tenant) and one for etcd snapshots; Barman Cloud Plugin with checksum env vars and `AWS_DEFAULT_REGION=auto`. Continuous WAL (zstd) and **weekly** base backups in per-tenant slots (uncompressed: R2 rejects the uneven multipart parts compressed barman streams produce). Retention checks every 6 h (R2 bills LIST as Class A). One bucket-wide **8-day bucket lock**. K3s etcd snapshots every 6 h, 28 kept, via `--etcd-s3`.
**Consequences:** 7-day PITR on every plan. R2 is free up to 10 GB and 1 M Class A operations, then US$0.015/GB-month (`03-…` §8). No object versioning in R2, so the lock is the protection. No India jurisdiction; India-only customers get an India S3-compatible `ObjectStore`.
**Revisit when:** barman fixes uneven part sizes, or S4 fails.

### ADR-013 — Cloudflare DNS for database hostnames, one A record per node
**Status:** Accepted (spike-gated: S3, S10)
**Context:** Per-query-billed DNS could create an unbounded bill; there is no managed load balancer; tenants are routed by TLS SNI.
**Decision:** `*.db.example.com` is a **DNS-only** wildcard in the Cloudflare zone with one A record per tenant node, TTL 300 s. The worker adds and removes node records through the Cloudflare API. Tenants in a non-default cell get explicit records. cert-manager uses the Cloudflare DNS-01 solver. DNSSEC is Cloudflare's automatic signing.
**Consequences:** DNS is free and unmetered. Clients skip a dead node's address. A Cloudflare authoritative-DNS outage affects new lookups after the TTL. The Free zone allows 200 records.
**Revisit when:** explicit records approach the zone limit, or a floating IP replaces the multi-record scheme (ADR-005).

### ADR-014 — Cedar for authorization
**Status:** Accepted
**Decision:** cedar-go evaluated in-process (control-api, mcp-server) with entities from the admin DB; policies versioned and tested in `policies/cedar`.
**Revisit when:** cross-org sharing needs a relationship graph (then OpenFGA).

### ADR-015 — VictoriaMetrics, VictoriaLogs, Grafana per cell
**Status:** Accepted
**Decision:** VictoriaMetrics single-node + vmagent + vmalert + Alertmanager from Helm charts, without an operator; VictoriaLogs + Fluent Bit (including the K3s audit log); Grafana behind Cloudflare Access; an external Workers Cron probe for whole-cell outages.
**Revisit when:** multi-cell retention needs → VictoriaMetrics cluster version.

### ADR-016 — Flux for GitOps, SOPS for secrets; tenants are not in git
**Status:** Accepted
**Decision:** Flux reconciles platform components per cell and decrypts **SOPS**-encrypted secrets with an age key that exists only in the cell and the owner's password manager. K3s encrypts Secrets at rest. Tenant objects come only from the admin DB via agent/operator.
**Consequences:** No paid secret manager; rotating a secret is a commit. Losing the age key means re-creating secrets, so it is backed up offline.
**Revisit when:** dynamic credentials are needed (OpenBao, Phase 5).

### ADR-017 — Capacity controller with a budget guard and a provider adapter
**Status:** Accepted
**Context:** Placement depends on TopoLVM free space, pod slots, plan envelopes and headroom; spend must never run ahead of revenue; the provider may have no API.
**Decision:** A worker loop computes demand per cell and never plans beyond the owner-set `max_nodes`; at the limit, new databases are waitlisted. Nodes are requested through `ProviderAdapter`: the launch implementation `manual` alerts the owner with the exact node to create from the bootstrap; a provider-API implementation is added when used. Nodes are removed only after evacuation.
**Revisit when:** not expected.

### ADR-018 — No instance credentials in workloads
**Status:** Accepted (spike-gated: S1)
**Decision:** No workload uses provider instance credentials or metadata services. NetworkPolicy blocks 169.254.169.254 and private/link-local ranges for egress in every namespace except explicit allows. Each external API (Cloudflare, R2, Supabase, Razorpay, email) gets a scoped token held as a SOPS-encrypted Secret, readable only by the service that uses it.
**Revisit when:** a provider-API adapter needs credentials (then a dedicated, narrowly scoped token for the worker only).

### ADR-019 — Cell-based scaling
**Status:** Accepted
**Decision:** Planned limits per cell: 50 nodes, 1,500 tenants. At 70 % of any limit the next cell is provisioned; placement stops using a cell at 100 %. The same code serves one cell or many, on any provider. The Phase 2 scale gate (1,000 hibernated synthetic tenants, no compute cost) measures the limits.
**Revisit when:** load tests show a cell can safely hold more.

### ADR-020 — Language split: Rust for the data path, Go for everything else
**Status:** Superseded by ADR-027 (2026-10-07), which keeps the same assignments and states the rule and its evidence.
**Decision:** Rust for `pg-gateway` only. Go for control-api, worker, cluster-agent, tenant-operator, mcp-server, mcp-auth, CLI. TypeScript with Next.js for the web apps (ADR-021). Every backend component ships as a Docker-built multi-arch image (`amd64` + `arm64`) in ghcr.io.
**Revisit when:** see ADR-027.

### ADR-021 — Next.js static exports on Workers Static Assets
**Status:** Accepted
**Context:** We want Next.js for the public site and both dashboards, at no hosting cost. Full Next.js on Cloudflare needs the OpenNext adapter, and the Workers Free plan's 10 ms CPU per request is too tight for server rendering. Next.js's recent critical vulnerabilities are server-side.
**Decision:** Three Next.js 16 App Router apps (`web/site`, `web/customer`, `web/admin`) built with `output: 'export'` and deployed with wrangler to Workers Static Assets. They run fully in the browser: supabase-js for sign-in, the generated control-api client with TanStack Query for data. Resource pages take IDs as query parameters. The admin app sits behind Cloudflare Access. A post-build step writes a hash-based CSP into `_headers`.
**Consequences:** Hosting is free and unlimited; no server-side Next.js code runs in production. No route handlers, proxy, server actions, ISR or default image optimization.
**Revisit when:** a feature truly needs server rendering; then OpenNext on Workers Paid (US$5/month) from revenue.

### ADR-022 — Plans and QoS
**Status:** Accepted
**Decision:** Three launch plans (`03-…` §4): `base` 0.1 vCPU guaranteed / 0.5 burst, 512 MiB, 5 GB, ₹299 + GST; `plus` 0.25 / 1, 1 GiB, 10 GB; `premium` 0.5 / 1, 2 GiB, 20 GB. CPU request = the guaranteed vCPU, CPU limit = the burst; memory request = limit on every container.
**Consequences:** Small plans feel fast when a node is idle and still get their guaranteed share under contention; no plan can be memory-overcommitted.
**Revisit when:** noisy-neighbour metrics show tenants' CPU bursts harming others, or the price review changes a plan.

### ADR-023 — We build the DBaaS; we reuse building blocks
**Status:** Accepted
**Decision:** Reuse focused components (K3s, CloudNativePG, PgBouncer, TopoLVM, River, zitadel/oidc, Supabase for our own metadata and auth), not packaged DBaaS products.
**Consequences:** We own the control plane, operator, gateway and MCP stack, and with them the product and its economics.

### ADR-024 — Budget ≤ ₹9,000/month and provider choice by checklist
**Status:** Accepted (spike-gated: S2, S8)
**Context:** The whole platform must cost ≤ ₹9,000/month. The same two-VM starter cell (2 vCPU / 8 GB control VM + 4 vCPU / 16 GB tenant VM) costs ≈ ₹3,600 on Hostinger, ≈ ₹12,300 on AWS Lightsail and ≈ ₹14,100–17,900 on Azure before egress (`03-…` §2.2, checked 2026-10-14).
**Decision:** The starter cell must cost ≤ ₹3,600/month at renewal prices (control VM ≤ ₹1,200, tenant VM ≤ ₹2,400), which in practice means the value-VPS class; each growth tenant node ≤ ₹2,400 and each extra control VM ≤ ₹1,200. The provider is chosen at Step 1.1 from a shortlist, on the checklist in `03-…` §2.1 and spike S2's measurements, not on price alone. Supabase Pro is added at its trigger; everything else stays on free plans until revenue pays for more.
**Consequences:** Launch cost ≈ ₹3,670/month on the two-VM starter cell (ADR-025, renewal prices, incl. the domain); ≈ ₹6,070 with `node-2`; ≈ ₹8,470 with `node-2` + Supabase Pro; the HA control plane (+ ≈ ₹2,400) and further tenant nodes are paid from revenue (`03-…` §7). VM prices are budgeted at renewal, not at introductory rates. Value-VPS trade-offs (shared vCPU, weaker SLAs) are managed by measurement, alerts and the ability to rebuild a cell elsewhere. AWS and Azure stay options for a later, separate cell (ADR-002).
**Provider record (Step 0.0 / 1.1):** shortlist, prices, checklist results, S2 numbers and the final choice are recorded here.
- 2026-10-07, shortlist entry: **Hostinger** (India, Mumbai), as published on hostinger.in on 2026-09-14 (`03-…` §2.2). 2026-10-14: the two-VM layout uses **KVM 2** for `ctl-1` (2 vCPU / 8 GB / 100 GB NVMe / 8 TB; ₹799 intro, ₹1,199 renewal) and **KVM 4** for `node-1` (4 vCPU / 16 GB / 200 GB NVMe / 16 TB; ₹1,099 intro, ₹2,399 renewal); intro prices need a 24-month prepay; GST may be added. Open against the checklist: item 4 (private network, not found in Hostinger's docs; without it `node-1` joins over public IPs with nftables allow-lists + WireGuard, verified at Step 1.1), item 7 (prepay term), item 8 (S2 on both VMs), item 3 (`tenantvg` size). Not yet ordered.
- 2026-10-14, compared for the same layout (not shortlisted, over budget): AWS Lightsail US$44 + US$84 (≈ ₹12,288); AWS EC2 `t4g.large` + `t4g.xlarge` ≈ US$98 before EBS, IPv4 and egress; Azure Central India `B2as_v2` + `B4as_v2` with Premium SSD and IPv4 ≈ US$173 (≈ ₹16,586), `D2as_v5` + `D4as_v5` ≈ US$187, plus egress (`03-…` §2.2).
**Revisit when:** revenue makes a mainstream provider or a hyperscaler cell affordable, or the chosen provider fails the checklist.

### ADR-025 — Launch on a two-VM starter cell: one control VM, one tenant VM
**Status:** Accepted (spike-gated: S2 on the chosen VMs; local capacity test passed 2026-09-30). Updated in place on 2026-10-14: the starter cell is now two VMs (it was one VM before; see the history note below).
**Context:** The first paying customers must break even quickly, the K3s server, etcd and the platform must not compete with tenant databases for CPU and memory, and running databases must keep serving when management is down (invariant 3). K3s can start one server on embedded etcd (`--cluster-init`) and add servers later without a rebuild (https://docs.k3s.io/datastore/ha-embedded); agents join with a token and need only outbound access to the server's port 6443 plus the WireGuard and kubelet ports between nodes (https://docs.k3s.io/installation/requirements).
**Decision:** Phase 1 starts on **two VMs** in one region:
- **`ctl-1`, the control VM** (2 vCPU / 8 GB / 100 GB NVMe; planning example Hostinger KVM 2, ₹1,199/month at renewal): the single K3s server with embedded etcd (`cluster-init`), label `dbcloud.io/pool=control`. It runs every platform and control-plane workload (Flux, cert-manager, CNPG operator + Barman plugin, TopoLVM controller, observability, control-api, worker, tenant-operator, cluster-agent, mcp-server, mcp-auth, River UI, cloudflared) with one replica each and `nodeSelector: dbcloud.io/pool=control`. No tenant databases; no inbound from the internet.
- **`node-1`, the tenant VM** (4 vCPU / 16 GB / 200 GB NVMe; planning example Hostinger KVM 4, ₹2,399/month at renewal): a K3s agent, label `dbcloud.io/pool=tenant`, taint `dbcloud.io/pool=tenant:NoSchedule`. It runs only tenant databases (Postgres + Barman sidecar, PgBouncer) and the per-node DaemonSets (TopoLVM node plugin, pg-gateway on `hostPort` 5432, node-exporter, Fluent Bit) plus one CoreDNS replica; public TCP 5432 only here; `tenantvg` lives here. It holds ≈ 17 `base`, ≈ 10 `plus` or ≈ 5 `premium` (`03-…` §6.1).
- The admin DB and Auth stay on Supabase (ADR-007). All scope stays: the same images, operator, gateway, control plane, MCP, billing and backups.
- Growth: at 80 % of `node-1` (≈ 14 `base`) the owner adds `node-2`, `node-3`, … as tenant agents with the same config (capacity controller via `ProviderAdapter`, ADR-017) or waitlists new sign-ups. HA: at Step 1.13 `ctl-2` and `ctl-3` join the embedded etcd (`pool=control`) and platform Deployments go to 2 replicas with spread constraints and PDBs, when revenue covers it or a customer needs an availability commitment.
**Why two VMs:** (1) tenant load can't starve the K3s server, etcd or the platform, and etcd doesn't share a disk with tenant I/O; (2) the tenant VM's memory goes to databases (≈ 12.4 GiB of `node-1`'s 16 GiB, against ≈ 8.9 GiB if the same VM also carried the server and the platform); (3) static stability: if `ctl-1` is down, databases on `node-1` keep serving (the kubelet keeps running pods, the gateway keeps its route cache, tenant pods tolerate `unreachable`/`not-ready` with no time limit, a local CoreDNS replica keeps answering) and only management returns `503` + `Retry-After`; if `node-1` is lost, `ctl-1` stays up and restores its tenants from R2 onto a replacement VM (node-loss runbook); (4) a container escape on the tenant VM gets only that node's kubelet credentials (NodeRestriction) and the narrowly scoped tokens of the DaemonSet pods there, never the platform's secrets, because platform pods never tolerate the tenant taint and a kubelet can't change its node's taints.
**Consequences:** Cost ≈ ₹3,668/month at renewal prices with the domain, break-even at 13 `base` tenants ((₹1,199 + ₹2,399 + ₹70) ÷ ₹286.55 = 12.8), 7 on the intro prices ((₹799 + ₹1,099 + ₹70) ÷ ₹286.55 = 6.9); a full `node-1` nets ≈ ₹1,203/month (`03-…` §7). Until Step 1.13 the control plane is a single server: losing `ctl-1` stops changes (create, scale, restore, billing actions) until it is rebuilt from the latest etcd snapshot in R2, but not running databases. Tenants have **no failover** on one tenant VM: losing `node-1` stops its tenants until a replacement VM joins and they restore from R2 (RTO measured in the node-loss drill), and OS reboots of `node-1` interrupt its tenants in the announced window. Launch plans are sold as best-effort with 7-day PITR. The two VMs talk over the provider's private network or, without one, over public IPs with nftables allow-lists and WireGuard (`03-…` §2.1 item 4).
**History:** measured locally (2026-09-30): 10 `base` tenants were Ready in 79 s, all connected through PgBouncer, all archived WAL, cross-tenant access was blocked, idle memory ≈ 127 MiB per tenant against 656 MiB reserved (requests stay = limits; no overcommit); Step 0.3 then raised the backup sidecar from 96 to 160 Mi (720 Mi per `base` tenant). The record first chose one 8 vCPU / 24 GB VM, then (2026-10-07) one 4 vCPU / 16 GB VM running server, platform and tenants together, growing at Step 1.13 into a three-server cell (then named `cp-1`, `node-1`, `node-2`). On 2026-10-14 the owner chose the two-VM layout above; the earlier names apply only to those history entries.
**Revisit when:** a customer needs an availability commitment before Step 1.13 (then add `ctl-2`/`ctl-3` earlier), S2 shows `node-1` can't hold its planned tenants or `ctl-1` can't hold the platform, or the provider's network makes the public-IP join unreliable (S8).

### ADR-026 — SeaweedFS as the local S3; Flux reads the local cell from an OCI artifact
**Status:** Accepted (2026-09-30, Step 0.3)
**Context:** Phase 0 needs an S3-compatible store that stands in for R2, and a Flux source, on one PC with no cloud accounts. MinIO, planned for this, stopped publishing community-edition releases and images in 2025-10, so there is no supported, patched build to pin. For Flux, a git server inside the cell or pulling from GitHub on every change would add a component or a network dependency to every local edit.
**Decision:** (1) The local cell uses **SeaweedFS** 4.48 (Apache-2.0, single pod `weed server -s3`, digest-pinned) as its S3 endpoint, only in `deploy/local/s3`. Production keeps R2; nothing outside the local overlay references SeaweedFS. (2) Locally, `hack/cell-flux.sh` pushes `deploy/` as an OCI artifact (`flux push artifact`) to a localhost-only registry, and an `OCIRepository` feeds the same Kustomization tree production uses. Production Flux reads git (Step 1.3).
**Consequences:** Barman's S3 client is exercised against a real S3 API locally; R2-specific behaviour (multipart part sizes, checksum headers, `region auto`) is still proven only by spike S4 on R2. SeaweedFS needed an upload-buffer cap and a 512 Mi limit to survive a 1 GB base backup. Local rollouts go through Flux exactly as in production, but the local source is an artifact, not a commit, so git history is not what the local cell runs.
**Revisit when:** MinIO or another maintained, permissively licensed S3 server fits better, or the local cell moves to a git source.

### ADR-027 — Language split by workload
**Status:** Accepted (2026-10-07; supersedes ADR-020, same component assignments)
**Context:** The owner wants Rust wherever it makes the product faster, safer or lighter, and Go wherever Go is the stronger tool, without a blanket limit either way. The choice has to follow the workload and the maturity of the libraries each component depends on. Evidence checked 2026-10-07:
- kube-rs is a CNCF **Sandbox** project (since 2021-11-16); controller-runtime is the reference framework for Kubernetes operators, and CloudNativePG publishes its API types in Go, so a CNPG-facing operator in Go uses the upstream types directly (https://www.cncf.io/projects/kube-rs/ , https://github.com/kube-rs/kube).
- River's production implementation is Go. A Rust crate (`riverqueue`) exists but is at 0.3.0, with 0.2.0 and 0.3.0 both published on 2026-10-07 (https://crates.io/crates/riverqueue , https://riverqueue.com/docs).
- CNPG's `Pooler` manages only PgBouncer (https://cloudnative-pg.io/docs/1.30/connection_pooling). Of the Rust poolers, PgCat is reported abandoned since ~May 2025 (https://github.com/postgresml/pgcat/issues) and PgDog is a fast-moving 0.x under AGPL-3.0 (https://github.com/pgdogdev/pgdog).
- An official Rust MCP SDK exists (`rmcp`, https://github.com/modelcontextprotocol/rust-sdk), so MCP is not locked to Go; our mcp-server shares auth, store and `sqlsafety` code with the Go services.
- On a localhost benchmark (Feb 2026), PgBouncer added ≈ 0.05 ms and PgDog ≈ 0.07 ms per SELECT, while the two Go proxies tested added ≈ 0.1 and ≈ 0.4 ms at 4 clients (https://gitlab.com/postgres-ai/postgresql-consulting/tests-and-benchmarks/-/issues/72). It is one benchmark: per-query proxy cost is small and depends on the implementation as much as the language, so the gateway gets measured targets rather than assumptions.
**Decision:**
- **Rust** for everything on the customer's connection or data path, and for anything latency- or memory-critical that runs per connection or per packet: pg-gateway (TLS termination, SNI/`options` routing, handshake relay, SCRAM channel-binding handling, cancel forwarding, caps and rate limits, route cache), and any future data-path proxy, parser of untrusted wire input, or per-node agent with a tight memory or latency budget.
- **Go** for the Kubernetes control plane and business logic: tenant-operator (controller-runtime + CNPG Go types), cluster-agent (client-go), control-api (oapi-codegen strict server), worker (River), mcp-auth (zitadel/oidc, OpenID-certified), mcp-server (official Go SDK; `rmcp` is the recorded alternative if MCP becomes latency-critical), CLI.
- **PgBouncer** stays the per-tenant pooler through CNPG's `Pooler`.
- TypeScript (Next.js static exports, ADR-021) for the web apps. Every backend component ships as a Docker-built multi-arch image (`amd64` + `arm64`) in ghcr.io.
**Consequences:** No rewrite: the components keep their languages. Rust code follows `.kiro/steering/30-rust.md` (bounded buffers, no hot-path allocation, `forbid(unsafe_code)`), Go code `20-go.md`. pg-gateway gets measurable performance targets (`01-…` §8.2) that Step 0.5 and spike S3 must meet. A component that moves across the line needs a new ADR with a measured reason.
**Revisit when:** a Go component lands on the per-connection path or shows latency or memory problems a rewrite would fix (measured); kube-rs or a Rust River client reaches the maturity of their Go counterparts; MCP traffic becomes latency-critical; or CNPG supports another pooler.

---

## Superseded in Blueprint 4.0

| Earlier decision | Replaced by |
|---|---|
| OKE Basic cells on Oracle Cloud (Rev 3.1–3.3) | ADR-001, ADR-002 (K3s on provider-neutral VMs) |
| VCN-native networking + Calico policy-only | ADR-003 (flannel `wireguard-native` + K3s network-policy controller) |
| OCI NLB in front of the gateway | ADR-005, ADR-013 (hostPort DaemonSet + one A record per node) |
| OCI Vault + External Secrets | ADR-016 (SOPS + age, K3s secrets encryption) |
| OCI Bastion | ADR-006 (Cloudflare Tunnel + Access for SSH and `kubectl`) |
| Per-node-pool dynamic groups + IMDS policy | ADR-018 (no instance credentials) |
| OCIR, OCI Email Delivery | ghcr.io; a transactional email service (`02-…` §2) |
| Launch on Oracle's free A1 allowance | ADR-024 (budget and provider checklist) |
