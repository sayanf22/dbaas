# 06 — Architecture Decision Records (Blueprint 4.0)

Format: context → decision → consequences → revisit when.
Status: **Accepted**, or **Accepted (spike-gated: Sx)** = accepted unless the named spike in `04-build-plan.md` fails, in which case the listed fallback is adopted and this record is updated. Records superseded in 4.0 are listed at the end.

---

### ADR-001 — Cells are K3s clusters on plain VMs
**Status:** Accepted (spike-gated: S1, S11)
**Context:** Oracle Cloud can't be used, and managed Kubernetes on mainstream clouds breaks the ₹9,000/month budget. The original blueprint used K3s.
**Decision:** Every cell is one **K3s v1.36** cluster on three Linux VMs in one region: three servers with embedded etcd (`cp-1` control-plane only, `node-1`/`node-2` also running workloads); growth nodes join as agents. Cells are independent failure and scale units registered with the control plane.
**Consequences:** No control-plane fee; we run etcd ourselves (snapshots to R2, quorum runbooks). K3s is a CNCF-certified distribution; CNPG officially targets vanilla Kubernetes, so conformance and CNPG smoke tests run on every K3s bump. The same code runs on any conformant Kubernetes, so a managed Kubernetes cell can be added later.
**Revisit when:** a contract requires an SLA-backed Kubernetes control plane, or revenue makes a managed control plane cheaper than operating etcd.

### ADR-002 — Provider-neutral, one provider and region per cell
**Status:** Accepted (spike-gated: S8)
**Context:** No provider is chosen yet. K3s supports embedded etcd only with all servers on one private network in one location; cross-provider nodes add latency, egress fees and partition risk (`03-…` §2.3).
**Decision:** A cell uses VMs from exactly one provider in one region with a free private network. Providers are chosen against the checklist in `03-…` §2.1. Provider-specific code lives only in `deploy/terraform/modules/vm-<provider>` and a `ProviderAdapter` implementation. A second region or provider (including AWS or GCP) is a separate cell.
**Consequences:** Moving providers means building a new cell and migrating tenants, not editing the cell. No managed load balancer, block storage or secret manager is relied on.
**Revisit when:** not expected.

### ADR-003 — flannel `wireguard-native` + K3s's network-policy controller
**Status:** Accepted (spike-gated: S1)
**Context:** Tenant isolation needs enforced NetworkPolicy; node-to-node pod traffic carries customer data over the provider's private network.
**Decision:** K3s's bundled flannel with the `wireguard-native` backend over the private network, and K3s's embedded network-policy controller (kube-router) for NetworkPolicy. No extra CNI.
**Consequences:** Pod traffic between nodes is encrypted; no extra pods. The CNI is fixed at cluster creation; if S1 finds a policy gap, the fallback is Calico in a new cell.
**Revisit when:** S1 finds a NetworkPolicy semantics gap we rely on.

### ADR-004 — Storage: TopoLVM on local NVMe; redundancy by Postgres replication
**Status:** Accepted (spike-gated: S2)
**Context:** Network block storage costs extra and isn't guaranteed across providers; CNPG recommends shared-nothing local storage.
**Decision:** `tenant-local` TopoLVM thick LVs in `tenantvg` on each node's NVMe for every plan. HA plans (Phase 3) get redundancy from a replica on another node. local-path-provisioner isn't used because it ignores PVC sizes.
**Consequences:** Storage costs nothing extra; data is node-bound, so planned changes use in-place maintenance windows or evacuation, and node loss uses restore from R2. etcd shares the disk on `node-*`, measured in S2.
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
**Decision:** One Cloudflare tunnel **per cell** for HTTP apps (cloudflared Deployment, 2 replicas). A second, host-level tunnel (`cloudflared` systemd service on each VM) exposes SSH and the K3s API as Cloudflare Access applications restricted to the owner with MFA. No public 443, 22 or 6443 on any VM. From Phase 4, **Cloudflare Load Balancing** spreads `api`, `mcp` and `oauth` across two cells.
**Consequences:** Origins are unreachable except through Cloudflare. Proxy read timeout is 125 s, so MCP streams send heartbeats. Customer Postgres traffic never uses Cloudflare's proxy. The provider console is the break-glass path when Cloudflare is down.
**Revisit when:** not expected.

### ADR-007 — Supabase as the admin/management database (only), Free first
**Status:** Accepted (spike-gated: S7)
**Context:** The control plane needs a managed Postgres for its own metadata and job queue. Supabase Free costs nothing but has no backups, 500 MB, 5 GB egress, pausing after a week of low activity, and IPv6-only direct connections.
**Decision:** Supabase **Free** in `ap-south-1` at launch (`dbcloud-prod` + `dbcloud-staging`), then **Pro** at the `03-…` §7 trigger. All connections go through the Supavisor pooler over IPv4: transaction mode with pgx `QueryExecModeExec` for request handlers, session mode for the River coordinator, migrations and dumps. Per-service roles, network restrictions to the node IPs, SSL enforcement, Data API disabled, RLS on. Our own nightly `pg_dump` to R2 with weekly restore drills; migrations run as an in-cell Job that dumps first.
**Consequences:** No platform database to operate. Admin-DB RPO is 24 h on Free (bounded by pre-migration dumps, `RebuildCatalogFromCells` and Razorpay records); 6 h on Pro. Running databases don't depend on Supabase (static stability).
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
**Status:** Accepted
**Decision:** Rust for `pg-gateway` only. Go for control-api, worker, cluster-agent, tenant-operator, mcp-server, mcp-auth, CLI. TypeScript with Next.js for the web apps (ADR-021). Every backend component ships as a Docker-built multi-arch image (`amd64` + `arm64`) in ghcr.io.
**Revisit when:** another component lands on the per-connection data path.

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
**Revisit when:** noisy-neighbour metrics show burstable tenants harming others, or the price review changes a plan.

### ADR-023 — We build the DBaaS; we reuse building blocks
**Status:** Accepted
**Decision:** Reuse focused components (K3s, CloudNativePG, PgBouncer, TopoLVM, River, zitadel/oidc, Supabase for our own metadata and auth), not packaged DBaaS products.
**Consequences:** We own the control plane, operator, gateway and MCP stack, and with them the product and its economics.

### ADR-024 — Budget ≤ ₹9,000/month and provider choice by checklist
**Status:** Accepted (spike-gated: S2, S8)
**Context:** The whole platform must cost ≤ ₹9,000/month. Mainstream VPS and hyperscaler prices for a useful three-VM cell are ₹33,000+ (`03-…` §2.2).
**Decision:** The reference cell (1 × 2 vCPU / 4 GB + 2 × 8 vCPU / 24 GB NVMe) must cost ≤ ₹5,600/month, which in practice means the value-VPS class. The provider is chosen at Step 1.1 from a shortlist, on the checklist in `03-…` §2.1 and spike S2's measurements, not on price alone. Supabase Pro is added at its trigger; everything else stays on free plans until revenue pays for more.
**Consequences:** Launch cost ≈ ₹5,700–5,900/month; ≈ ₹8,100–8,300 with Supabase Pro. Value-VPS trade-offs (shared vCPU, weaker SLAs) are managed by measurement, alerts and the ability to rebuild a cell elsewhere.
**Provider record (Step 0.0 / 1.1):** shortlist, prices, checklist results, S2 numbers and the final choice are recorded here.
**Revisit when:** revenue makes a mainstream provider or a hyperscaler cell affordable, or the chosen provider fails the checklist.

### ADR-025 — Launch on a one-VM starter cell that grows into the reference cell
**Status:** Accepted (spike-gated: S2 on the chosen VM; local capacity test passed 2026-09-30)
**Context:** The first 10–20 paying customers must break even quickly. The three-VM reference cell costs ≤ ₹5,600/month and breaks even at ~20 `base` tenants (`03-…` §7). K3s can start one server on embedded etcd (`--cluster-init`) and add servers later without a rebuild (https://docs.k3s.io/datastore/ha-embedded).
**Decision:** Phase 1 starts on **one VM**, 8 vCPU / 24 GB / 200 GB NVMe (≤ ₹2,300/month), running K3s as a single server with embedded etcd, the whole platform with one replica per component, and up to ~22 `base` tenants (`03-…` §6.1). All scope stays: the same images, operator, gateway, control plane, MCP, billing and backups run on it. When the cell reaches 80 % of its capacity (~18 `base`), `cp-1` and `node-2` join as K3s servers and the starter VM becomes `node-1` of the reference cell (Step 1.13); platform Deployments go back to 2 replicas.
**Consequences:** Break-even at ~9 `base` tenants instead of ~20. Until Step 1.13 there is **no failover**: losing the VM stops every tenant until a replacement VM is bootstrapped and tenants restore from R2 (RTO measured in the node-loss drill, hours), and OS reboots interrupt everyone in the announced window. Launch plans are sold as best-effort with 7-day PITR, as before. Measured locally: 10 `base` tenants were Ready in 79 s, all connected through PgBouncer, all archived WAL, cross-tenant access was blocked, idle memory ≈ 127 MiB per tenant against 656 MiB reserved (requests stay = limits; no overcommit). Step 0.3 then measured the backup sidecar's peak during a base backup and raised it from 96 to 160 Mi (720 Mi per `base` tenant), which lowers the starter VM from ~25 to ~22 `base`; break-even stays at 9.
**Revisit when:** a customer needs an availability commitment before the reference cell exists (then grow earlier), or S2 shows the chosen VM can't hold 22 tenants.

### ADR-026 — SeaweedFS as the local S3; Flux reads the local cell from an OCI artifact
**Status:** Accepted (2026-09-30, Step 0.3)
**Context:** Phase 0 needs an S3-compatible store that stands in for R2, and a Flux source, on one PC with no cloud accounts. MinIO, planned for this, stopped publishing community-edition releases and images in 2025-10, so there is no supported, patched build to pin. For Flux, a git server inside the cell or pulling from GitHub on every change would add a component or a network dependency to every local edit.
**Decision:** (1) The local cell uses **SeaweedFS** 4.48 (Apache-2.0, single pod `weed server -s3`, digest-pinned) as its S3 endpoint, only in `deploy/local/s3`. Production keeps R2; nothing outside the local overlay references SeaweedFS. (2) Locally, `hack/cell-flux.sh` pushes `deploy/` as an OCI artifact (`flux push artifact`) to a localhost-only registry, and an `OCIRepository` feeds the same Kustomization tree production uses. Production Flux reads git (Step 1.3).
**Consequences:** Barman's S3 client is exercised against a real S3 API locally; R2-specific behaviour (multipart part sizes, checksum headers, `region auto`) is still proven only by spike S4 on R2. SeaweedFS needed an upload-buffer cap and a 512 Mi limit to survive a 1 GB base backup. Local rollouts go through Flux exactly as in production, but the local source is an artifact, not a commit, so git history is not what the local cell runs.
**Revisit when:** MinIO or another maintained, permissively licensed S3 server fits better, or the local cell moves to a git source.

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
