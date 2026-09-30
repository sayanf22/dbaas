# 04 — Build Plan (Blueprint 4.0, step by step)

Execution plan for `01-architecture.md`. Every step lists **Tasks**, **Done when** and, where relevant, the spike or gate it depends on. Code follows `07-engineering-standards.md` (also loaded automatically as Kiro steering from `.kiro/steering/`). Costs follow `03-cost-model.md`: the platform stays ≤ ₹9,000/month and nothing billable is created unless a step says so.

## How to work through this plan

- Top to bottom. A step starts only when the previous step's "Done when" passes in CI or on the real cell.
- **Build first, rent later:** all of Phase 0 runs on your PC (Docker, k3d, local Supabase) at ₹0. VMs are rented only at Step 1.1, after Gate G0.
- One branch + PR per step; CI green (lint, unit, integration, multi-arch images, scans).
- Everything is code: Terraform, node bootstrap, Flux manifests, Helm values, Dockerfiles, operator, gateway, tests. Console clicks only for account sign-up and, where the provider has no API, creating the three VMs from the bootstrap file.
- Anything destructive or production-affecting (Terraform apply, admin-DB migrations, deletes, node replacement) needs a second reviewer or a signed checklist.
- A failed spike is recorded with its fallback in `06-decision-records.md` before work continues.
- A step that adds recurring cost names the cost and the `03-…` trigger it satisfies.

---

## 1. Repository layout (one monorepo)

```
dbcloud/
├── .github/workflows/                # CI: lint → test → build → scan → sign (actions pinned by SHA)
├── api/openapi.yaml                  # public /v1 + internal /internal/v1 contract (OpenAPI 3.1)
├── build/go.Dockerfile               # one multi-stage Dockerfile for every Go binary (ARG BINARY)
├── cmd/                              # Go binaries (thin main packages)
│   ├── control-api/  worker/  cluster-agent/  tenant-operator/
│   ├── mcp-server/  mcp-auth/  dbcloud/          # dbcloud = CLI
├── internal/                         # Go packages, not importable from outside
│   ├── domain/        # entities, state machines, plan catalog
│   ├── store/         # sqlc output + transactions (pgx v5)
│   ├── jobs/          # River job args + workers
│   ├── placement/     # admission, scoring, waitlist, capacity controller
│   ├── authz/         # Cedar entities + policy evaluation
│   ├── identity/      # Supabase JWT verification, API keys, mTLS
│   ├── provider/      # ProviderAdapter interface + `manual` implementation (a provider-API implementation when chosen)
│   ├── cloudflare/    # R2 temporary credentials, DNS records (node A records, non-default cells)
│   ├── billing/       # plans, subscriptions, Razorpay webhooks, invoices
│   ├── mcp/           # tools, grants, approvals
│   ├── oauth/         # mcp-auth storage + CIMD fetcher
│   ├── sqlsafety/     # pg_query_go checks
│   └── platform/      # config, logging, telemetry, idempotency, audit, rate limits, http server
├── operator/                         # tenant-operator packages (Go)
│   ├── api/v1alpha1/                 # TenantDatabase CRD types
│   └── internal/{controller,render}/ # reconcilers + pure renderers
├── gateway/                          # Rust crate: pg-gateway
│   ├── Cargo.toml  deny.toml  Dockerfile
│   └── src/ {main.rs, config.rs, listener.rs, negotiate.rs, tls.rs, resolve.rs,
│             routes.rs, snapshot.rs, limits.rs, upstream.rs, cancel.rs, errors.rs, metrics.rs, shutdown.rs}
├── db/migrations/  db/queries/       # goose migrations, sqlc queries (admin DB)
├── supabase/config.toml              # local Supabase stack (supabase start)
├── policies/cedar/                   # policies + schema + tests
├── web/ {site, customer, admin, ui}  # Next.js 16 static-export apps (+ shared shadcn/ui package), deployed with wrangler
├── sdk/ {go, ts, python}             # generated clients
├── deploy/
│   ├── terraform/ {modules/{cloudflare-edge,r2,vm-<provider>}, envs/{global,c1}}
│   ├── node-bootstrap/               # cloud-init + scripts: OS hardening, nftables, tenantvg, K3s install/config, admin cloudflared
│   ├── k3s/                          # K3s server/agent config templates, PSA and audit policy files, registries.yaml template
│   ├── flux/ {clusters/c1, infrastructure/, apps/}   # SOPS-encrypted secrets under */secrets/
│   ├── helm/dbcloud/                 # chart for our services
│   └── upgrade-plans/                # system-upgrade-controller Plans (pinned K3s version)
├── hack/                             # install-tools.sh, local dev scripts
├── test/ {e2e, gates, load, corpus}
├── docs/runbooks/
├── .sops.yaml  .dockerignore
├── package.json  pnpm-workspace.yaml # root of the pnpm workspace (web/*, sdk/ts)
├── tools.versions                    # pinned tool versions
└── Taskfile.yml
```

---

## 2. Phase 0 — Build everything locally (₹0)

### Step 0.0 — Accounts (no VMs yet)
**Tasks:**
- GitHub organisation on Free with an Actions budget of US$0 and "stop usage" on; ghcr.io for private images.
- Cloudflare: account, zone `example.com` (DNSSEC on), R2 enabled (needs a payment method; usage stays in the free tier), R2 usage notifications, Zero Trust on the Free plan.
- Domain registered (`.com` at Cloudflare Registrar, or `.in` at a NIXI-accredited registrar with eKYC) and pointed at Cloudflare.
- Supabase: organisation on **Free** with projects `dbcloud-prod` and `dbcloud-staging` in `ap-south-1`.
- Transactional email account (free tier ≥ 3,000/month over SMTP; candidates in `03-…` §3), domain verified with SPF/DKIM in Cloudflare.
- Razorpay account and KYC started (one-time ₹199).
- **VM provider shortlist**, not an order: 2–3 providers checked against `03-…` §2.1 on paper (region, private network, disk layout, transfer, price ≤ the ceilings, terms). The choice is made at Step 1.1 after spike S2's short benchmark.
- Order, instructions and links: `05-dev-environment-and-downloads.md` §1.

**Done when:** every account exists with MFA; the shortlist with prices and checklist results is recorded in ADR-024.

### Step 0.1 — Repo, tooling, CI
**Tasks:** layout above; `tools.versions` + `hack/install-tools.sh`; Go workspace (1.27), Cargo workspace (Rust stable, edition 2024), pnpm workspace (Node 24) with three Next.js 16 apps scaffolded for static export (`web/site`, `web/customer`, `web/admin`) and the shared `web/ui` shadcn/ui package; lint configs (`golangci-lint` with gosec, clippy lints, ESLint flat config with `eslint-config-next`); `.kiro/steering/` files; `.sops.yaml` with the cell's age public key; Dockerfiles (multi-stage, distroless, non-root numeric UID, base images pinned by digest); GitHub Actions: lint → test → build images natively on `ubuntu-24.04` + `ubuntu-24.04-arm` → push to ghcr.io by digest → Trivy → SBOM → cosign (main only); workflow hardening (actions pinned by SHA, `permissions: contents: read`, `actionlint`, `zizmor`, `hadolint`, ShellCheck); path filters per component; Renovate.
Compensating controls for GitHub Free, where private repos can't enforce branch rules, CODEOWNERS or deployment environments and have no secret scanning:
- Trivy secret scan in CI and in a pre-commit hook; a check that fails on any unencrypted Kubernetes `Secret` in git.
- Deploy and `terraform apply` workflows run only on `workflow_dispatch` by the owner, with repository secrets scoped to those workflows.
- Work lands through pull requests with green CI, by rule in `.kiro/steering/70-ci.md`.
- Move to GitHub Team, paid from revenue, when a second engineer joins.
**Done when:** CI is green on skeleton binaries/crate/apps within the free minutes budget (measured per run); each Next.js app exports and deploys to a preview Worker; a planted fake secret fails the secret scan; `task dev-up` works on a clean machine.

### Step 0.2 — API contract and admin schema v0
**Tasks:** `api/openapi.yaml` (PDF §22.1 + internal agent endpoints + plans/billing, RFC 9457 errors); oapi-codegen; goose migrations for §9; sqlc queries; River migrations; idempotency middleware; per-service database roles.
**Done when:** migrations apply and roll back on the local Supabase stack; contract tests pass; a repeated POST with the same `Idempotency-Key` returns the original response.

### Step 0.3 — Local cell (k3d = K3s in Docker)
**Tasks:** `supabase start` (local Postgres + Auth) for the admin DB; **k3d** with the pinned K3s v1.36 release, 3 servers (embedded etcd) + 1 agent, Traefik/ServiceLB/local-storage disabled, the same K3s config templates as production (`deploy/k3s/`); Flux against a local branch with SOPS decryption (a local age key); TopoLVM on a loop-device VG (local only); CNPG 1.30.1 + Barman Cloud Plugin 0.15.0; MinIO as the local S3; cert-manager (self-signed); VictoriaMetrics single; check whether the pinned PostgreSQL image is built with lz4 and zstd (`pg_config --configure`).
**Done when:** a hand-written CNPG Cluster archives WAL to MinIO and restores; a NetworkPolicy default-deny between two namespaces is enforced by K3s's policy controller; stopping one k3d server keeps the API up; the image's compression support is recorded in the plan catalog.

### Step 0.4 — `TenantDatabase` CRD + tenant-operator v0
**Tasks:** CRD (architecture §6.2); renderers for the objects in architecture §6.1 (one Pooler, NetworkPolicies incl. the metadata-endpoint block, `isolationCheck` off for `instances: 1`, unreachable/not-ready tolerations for `tenant-local`, per-plan Postgres parameters, sidecar resources and `retentionPolicyIntervalSeconds: 21600` on the `ObjectStore`, staggered weekly base slot, `nodeMaintenanceWindow` handling); finalizers; conditions + `observedGeneration`; staged deletion; envtest unit tests; Chainsaw e2e (create → Ready → resize → expand storage → suspend → delete).
**Done when:** e2e green on k3d; operator killed mid-reconcile converges; reapplying the same spec changes nothing; a pod in tenant A can't reach tenant B; every container of a tenant pod has memory request = limit; the rendered liveness-probe fields match the pinned CNPG version.

### Step 0.5 — pg-gateway v0 (Rust)
**Tasks:** listener with graceful drain; SSLRequest + direct TLS (ALPN); SNI and `options=endpoint` resolution; route table from a kube-rs watch of `TenantDatabase` and EndpointSlices, readiness on a complete list, signed peer snapshots; IP allowlist; per-tenant pooled and direct caps, per-node global cap and per-source-IP rate limit; ErrorResponse builder; upstream relay (pooled → PgBouncer pod, direct → Postgres primary pod); `SCRAM-SHA-256-PLUS` stripping; cancel keys (replica ID + HMAC) with mTLS forwarding between replicas; metrics; fuzz targets for the startup-packet parser; DaemonSet with `hostPort` 5432 (architecture §8.2).
**Done when:**
- psql 14/16/18, pgx v5, node-postgres, pgjdbc, psycopg 3 and asyncpg connect by hostname through the gateway on k3d, with default libpq settings (`channel_binding=prefer`), on both the pooled and the `-direct` hostname.
- With the hostname resolving to two node addresses and one node's gateway stopped, libpq and pgx connect through the other address.
- A suspended tenant gets SQLSTATE `57P03`; unknown SNI gets `08004`.
- A protocol-level CancelRequest (psql Ctrl-C, pgx context cancel, JDBC `Statement.cancel()`) cancels the running query on both hostnames, including during a rolling update.
- A replica restarted while the Kubernetes API is unreachable becomes ready from a peer snapshot and serves every existing route.
- Fuzzing runs 30 min without a crash; a gateway pod stays under 32 Mi at idle.

### Step 0.6 — control-api v0 + identity + authz
**Tasks:** Supabase JWT verification via JWKS (ES256; `iss`, `aud`, `exp`); org API keys; orgs/memberships/tenants/databases/operations handlers; plan catalog (`base`, `plus`, `premium`) + entitlement checks; waitlist when capacity or `max_nodes` is reached; per-org and per-IP rate limits; Cedar policies + table-driven tests; audit events on every mutation.
**Done when:** a member of org A can't read or change org B's objects (tests); every mutation returns 202 + `operation_id`; a create request beyond capacity returns an operation in state `WAITLISTED`.

### Step 0.7 — worker v0 (River) + placement
**Tasks:** jobs `ProvisionTenant`, `ScaleTenant`, `ExpandStorage`, `BackupNow`, `RestoreTenant`, `SuspendTenant`, `DeleteTenant`, `PurgeTenant`, `RotateCredentials`, `ExportAdminDB`, `RetentionSweep`, `SyncNodeDNS` (node A records); per-kind timeouts, snooze-polling for long operations, `RescueStuckJobsAfter` 10 min; state change and job insert in one transaction (`InsertTx`); River coordinator on a session-mode connection; placement v0 (one cell, nodes, backup slots); admission; `ProviderAdapter` with the `manual` implementation; unique jobs per idempotency key.
**Done when:** worker killed mid-job → job completes exactly once; two identical requests → one tenant; insufficient capacity ends `WAITLISTED` with a clear reason and a "node needed" alert; the same code passes against the local stack in both pooler modes (session for the worker, transaction with `QueryExecModeExec` for control-api).

### Step 0.8 — cluster-agent v0
**Tasks:** mTLS client cert per cell; long-poll `GET /internal/v1/cells/{id}/desired`; server-side apply; status + capacity reports (Allocatable, requests, TopoLVM free bytes, free pod slots, per node); backoff while control-api is down; agent safety rules (architecture §10.5): deletes only on tombstones, generation rollbacks rejected, unknown objects quarantined, deletes rate-limited.
**Done when:** control-api stopped for 10 min → cell unchanged, then converges; cell X's certificate can't read cell Y's specs; a desired state from an older admin-DB dump changes nothing in the cell and raises an alert.

### Step 0.9 — Backup and restore v0
**Tasks:** per-tenant `ObjectStore` prefix; weekly base backups in per-tenant slots and `archive_timeout` by plan (architecture §14.1); zstd WAL compression; PITR into a new `TenantDatabase`; sentinel-table verification; `RestoreDrill` job; parallel restore of all tenants of a node (node-loss path) with bounded concurrency; K3s etcd snapshots to MinIO and a restore of the local cell from one.
**Done when:** PITR to T1 returns exactly T1's rows; an archive gap is detected and reported; restoring 25 small tenants in parallel on k3d completes and is timed; a k3d cell rebuilt from an etcd snapshot comes back with every `TenantDatabase` object.

### Step 0.10 — MCP v0 + mcp-auth v0
**Tasks:** mcp-server (Go SDK; 2026-07-28 stateless + 2025-11-25; RFC 9728 metadata; read tools: `list_projects`, `list_tables`, `get_schema`, `query_read`, `explain`, `get_health`); mcp-auth on zitadel/oidc (PKCE S256, CIMD with SSRF-guarded fetcher, DCR fallback, `resource` → `aud`, RFC 9207, loopback any port, login via local Supabase Auth, consent page in the customer app); `sqlsafety` v0.
**Done when:** MCP Inspector and one desktop client (Claude Code or VS Code) connect via CIMD; wrong `aud`/`iss`/expired tokens are rejected; a read-only grant can't write (`WITH … INSERT`, `pg_read_file`, multi-statement); the MCP conformance authorization scenarios pass.

### Step 0.11 — CLI and web apps v0
**Tasks:** `dbcloud login` (device grant via mcp-auth's CLI client, `aud` = `https://api.example.com`), `db create/list/info/credentials`; customer app (Next.js static export): sign-in (Supabase Auth + Turnstile), org, plan picker, create DB, connection strings, usage charts, operations, resource pages by query parameter (`/databases/detail?id=…`); admin app skeleton; strict CSP generated into `_headers`; Playwright + axe checks.
**Done when:** on the local stack, a new user signs up, creates a DB and connects with psql using only the CLI or the dashboard.

### Step 0.12 — Node bootstrap (tested locally)
**Tasks:** `deploy/node-bootstrap/`: cloud-init + scripts for Ubuntu 24.04: users and SSH keys (no passwords, no root login), unattended security updates, K3s CIS sysctls, nftables rules (public TCP 5432 on tenant nodes only; K3s ports on the private interface only), LVM `tenantvg` from the spare disk space, K3s install from the pinned release with checksum verification, K3s config for server/agent roles (§5.1 of the architecture: embedded etcd, wireguard-native, disabled add-ons, hardening flags, kubelet reservations, Spegel, etcd snapshots to R2), `registries.yaml` for ghcr.io, and the admin `cloudflared` systemd service. Tested on local Ubuntu 24.04 VMs (Multipass or WSL-hosted VMs) or containers where possible.
**Done when:** the bootstrap creates a 3-server cluster on local VMs, is idempotent (a second run changes nothing), and ShellCheck passes.

**Gate G0:** steps 0.0–0.12 done locally; CI green; ready to rent VMs.

---

## 3. Phase 1 — Launch on cell `c1` (3 VMs, paying customers)

**Launch profile (ADR-025):** Phase 1 runs on the **starter cell**: one 8 vCPU / 24 GB VM that becomes `node-1`. Steps 1.1–1.12 apply to it unchanged except that there is one node, K3s runs as a single server with `cluster-init` (embedded etcd), and platform Deployments run 1 replica. `cp-1` and `node-2` are rented at Step 1.13.

### Step 1.1 — Choose the provider and rent the cell
**Tasks:** from the Step 0.0 shortlist, rent one small VM per candidate for a day where the provider bills hourly (or use its trial) and run spike **S2**'s short benchmark; pick the provider that passes `03-…` §2.1 with the best numbers within the ceilings; record the decision, prices and benchmark in ADR-024. Then create `node-1` (starter cell, ADR-025); `cp-1` and `node-2` follow at Step 1.13 in the same region on the same private network (Terraform `modules/vm-c1-<provider>` if the provider has a Terraform provider, otherwise by hand with the bootstrap as cloud-init user data). Cloudflare API tokens scoped per use; the Terraform state bucket `dbcloud-tfstate` is created once with `wrangler r2 bucket create` and imported into `envs/global`.
**Done when:** three VMs pass the checklist, including the full S2 run; the total monthly bill is ≤ ₹5,600; the bootstrap has formed the K3s cluster; `terraform plan` (where used) shows no changes.

### Step 1.2 — Terraform: global + `c1`
`envs/global`: Cloudflare zone settings, WAF rules, the rate-limiting rule, Access apps/policies (admin app, Grafana, River UI, SSH per node, K3s API), Turnstile widget, R2 buckets (`dbcloud-c1-pg`, `dbcloud-c1-etcd`, `dbcloud-admin-exports`, imported `dbcloud-tfstate`) + bucket-lock rules, DNS records (`*.db.example.com` DNS-only A records → `node-1` and `node-2`, TTL 300; SPF/DKIM for email).
`envs/c1`: the cell's HTTP tunnel and admin tunnel with their ingress hostnames; the provider's VMs, private network and firewall where Terraform can manage them.
The Next.js web apps are not in Terraform: they deploy with wrangler (Step 1.5).
**Done when:** a second `terraform plan` shows no changes; SSH and the K3s API are reachable only through Cloudflare Access (a port scan from outside shows only TCP 5432 on the tenant nodes); each node's real allocatable CPU and memory are written into `03-…` §6.

### Step 1.3 — Cell bootstrap
Flux bootstrap (read-only deploy key, SOPS age key created on the cell and stored in the password manager); infrastructure layer: NetworkPolicy baseline (default deny + DNS + metadata block) for every platform namespace, TopoLVM, cert-manager + ClusterIssuer (Cloudflare DNS-01), CNPG + Barman plugin + `ClusterImageCatalog`, system-upgrade-controller + Plans, VictoriaMetrics single + vmagent + vmalert + Alertmanager, VictoriaLogs + Fluent Bit (incl. the K3s audit log), Grafana, cloudflared (2 replicas), PSA labels and ValidatingAdmissionPolicies, weekly `trivy k8s` Job; third-party images mirrored by digest into ghcr.io.
**Done when:** all HelmReleases Ready; measured platform usage within ±20 % of `03-…` §6 (820m CPU, 2.4 GiB, 42 pods); a chart bump reconciles with no manual step; an etcd snapshot lands in R2.

### Step 1.4 — Admin DB on Supabase Free
Per-service roles (`dbcloud_app`, `dbcloud_worker`, `dbcloud_migrator`), no `VALID UNTIL`; Supavisor pool size set; network restrictions to the three node IPs; SSL enforcement on; Data API disabled; goose migrations as an **in-cell Kubernetes Job** that dumps to R2 first (the release's migration image, applied by Flux before the app rollout; expand/contract, `lock_timeout` set). CI never connects to Supabase. RLS enabled on every table with no policies for `anon`/`authenticated`; River migrations; nightly `ExportAdminDB` to R2; weekly restore drill of the dump into `dbcloud-staging`; `RetentionSweep` for data older than 90 days; alerts at 60 % of database size and egress.
**Done when:** the drill restores and a checksum query matches production; a failed migration Job blocks the app rollout; a connection from any IP other than the node IPs is refused.

### Step 1.5 — Platform services
Deploy control-api, worker, cluster-agent, tenant-operator, mcp-server, mcp-auth, River UI (`jobs.example.com`, behind Access), pg-gateway DaemonSet (architecture §8.2); Next.js apps deployed with wrangler; Supabase Auth settings (Turnstile, TOTP, providers, custom SMTP = the chosen email service, redirect URLs, rate limits); the external uptime probe (Workers Cron Trigger).
**Done when:** smoke suite green against `https://api.example.com`, `https://mcp.example.com/mcp` and `t-smoke.db.example.com` through each node's address.

### Step 1.6 — Tenant lifecycle on `c1`
Create → connect (pooled + direct) → write/read → expand storage → backup → PITR restore → rotate credentials (create-before-revoke) → suspend (gateway returns `57P03`) → staged delete → purge with second authorization.
**Done when:** gates **Tenant isolation, Provisioning idempotency, Pooler, Backup/PITR, R2 compatibility, Gateway** pass on `c1`.

### Step 1.7 — Observability, usage tracking and cost report
Dashboards (tenant, node, etcd, gateway, platform, SLO, cost); alert rules (PDF §21 + etcd fsync p99 and leader changes, CPU steal, WAL archive age, TopoLVM free space, free pod slots, gateway rejections, tunnel health, Supabase size/egress/connections, restore drill failures, etcd snapshot age, certificate expiry, R2 credential lifetime, R2 monthly usage, transfer vs included allowance); routing to email + chat; `GET /v1/databases/{id}/metrics` for customer usage charts; weekly cost report job (architecture §17, `03-…` §10).
**Done when:** each "Immediate" alert fired in a test reaches on-call within 2 minutes; the external probe alerts when both gateways are stopped; a customer can see only their own usage.

### Step 1.8 — Security gate
Cross-tenant attempts (pod-to-pod, wrong SNI, `options` spoofing); metadata endpoint unreachable from tenant pods; external port scan (only 5432 open); SSH and K3s API reachable only through Access with MFA; `kubectl auth can-i --list` matrix per service account; PSA/VAP rejections; K3s CIS self-assessment (benchmark v1.12) with findings fixed or justified; secrets encrypted at rest and absent from logs and git; Trivy clean; WAF/rate-limit rules tested; CI hardening checks (`zizmor`, `actionlint`) clean.
**Done when:** **Security** gate passes; findings fixed or risk-accepted in writing.

### Step 1.9 — MCP and AI-safety gates
Write tools (`query_write`, `migrate` with approval), `backup.create`, `operations.get`; approval binding; negative suites (wrong tenant/scope/issuer/audience, replayed approval, expired token); unsafe SQL + prompt-injection corpus stored in tenant data.
**Done when:** **MCP authorization** and **AI safety** gates pass.

### Step 1.10 — Runbooks v1 + static stability + node loss
Runbooks: provisioning failure, WAL archive failure, PITR restore, credential rotation failure, **node loss** (replace VM, rejoin, restore tenants), **`cp-1` loss** (replace etcd member), **quorum loss** (restore from etcd snapshot), `RebuildCell`, K3s upgrade, OS reboot window, Supabase outage, Supabase Free limit reached, Cloudflare outage, Kubernetes API outage, gateway overload, certificate expiry, capacity full, provider price change, MCP security incident, `RebuildCatalogFromCells`. Each rehearsed once.
**Done when:** every runbook is rehearsed; the **Static stability** gate passes (Supabase blocked and the tunnel stopped for 30 min, then the Kubernetes API stopped on all servers for 15 min: running databases keep serving on existing routes, zero primary restarts, mutations return `503` + `Retry-After`, everything converges afterwards); the **node-loss drill** (power off `node-2`, replace it, restore its tenants from R2) and the **server-loss drill** (power off `cp-1`: API stays up) record real numbers, and the published RTO is set from them.

### Step 1.11 — Public site and billing
Public site `web/site` (Next.js static export at `example.com`): product, pricing (the `03-…` §4 plans), docs, About, Contact, Terms, Privacy, Cancellation/Refund and Shipping pages, which Razorpay requires before live keys (legal wording reviewed by the owner's lawyer or accountant); Razorpay website review with a sample invoice and a test login. Razorpay Subscriptions (UPI AutoPay, cards) per plan; signed, idempotent webhooks; GST invoice data (GSTIN, place of supply, HSN/SAC as advised by the company's accountant); proration on plan change; dunning emails → suspend → staged delete; reconciliation job against Razorpay.
**Done when:** Razorpay has approved the website and issued live keys; a test subscription is charged, invoiced and reconciled; a failed renewal suspends the database after the grace period and a payment restores it; replayed webhooks change nothing.

### Step 1.12 — Launch
Invited customers first, then public sign-up up to the cell's capacity (`03-…` §6), then the waitlist. Weekly capacity/error/cost review.
**Done when:** Gate G1 passes.

### Step 1.13 — Grow the starter cell into the reference cell (ADR-025)
**Trigger:** the starter VM reaches 80 % of its capacity (~20 `base` tenants, `03-…` §6.1) or a customer needs failover, and monthly net covers ≈ ₹3,300 more.
**Tasks:** rent `cp-1` (2 vCPU / 4 GB) and `node-2` (8 vCPU / 24 GB) from the same provider, region and private network; run the node bootstrap as `server-cp` and `server-worker`, joining the existing etcd through `node-1` (no rebuild: `cluster-init` was set at launch); taint `cp-1`; platform Deployments back to 2 replicas with spread constraints and PDBs; add `node-2`'s A record; re-place new tenants across both nodes.
**Done when:** `etcdctl member list` shows three healthy members; stopping any one VM keeps the API and platform up (Gate G1 availability drill); a fresh etcd snapshot restores; no tenant was restarted during the join.

**Gate G1:** Phase 1 gates green for 14 consecutive days; weekly restore drills green; **Cost** gate green (first full month total within `03-…` §1); no open Sev-1/Sev-2.

---

## 4. Phase 2 — Growth paid from revenue

Each step starts only when its `03-…` §7 trigger is met.

| Step | Tasks | Done when |
|---|---|---|
| 2.1 Supabase Pro | upgrade `dbcloud-prod` at the trigger (~30 tenants, 300 MB, or 3.5 GB/month egress); admin-DB dumps every 6 h | daily backups visible; pause and quota warnings gone |
| 2.2 Growth node + provider adapter | `node-3` (same shape) joins as a K3s agent; if the provider has an API, a `ProviderAdapter` implementation creates and removes nodes within `max_nodes`; the worker adds its A record; removal only after evacuation | a synthetic load triggers a node request before admission fails; nothing is added past `max_nodes`; an emptied node is removed after evacuation |
| 2.3 Maintenance automation | K3s upgrades through system-upgrade-controller and OS reboot windows (architecture §12.3) run on a schedule with tenant notices | an upgrade and a reboot cycle complete with only the announced interruptions |
| 2.4 Per-tenant R2 credentials | worker mints prefix-scoped temporary credentials (TTL ≥ 72 h), refreshes at 25 % of lifetime | tenant A's credentials can't read tenant B's prefix; a refresh failure alerts before 24 h remain |
| 2.5 Capacity envelope | pgbench/k6 replay per plan and node shape | **Capacity** and **Resource limits** gates pass; limits written into the plan catalog |
| 2.6 Scale gate (₹0) | 1,000 synthetic `TenantDatabase` objects in `c1` whose CNPG clusters use declarative hibernation (no pods) | **Scale** gate passes; architecture §5.3 limits updated from the measurement |
| 2.7 Hardening | Cloudflare Pro decision (OWASP rules); status page; external review of mcp-auth before public MCP launch | go/no-go checklist signed |

## 5. Phase 3 — HA plans (when revenue funds them)

| Step | Tasks | Done when |
|---|---|---|
| 3.1 HA plans | primary + replica on different nodes (3 instances once the cell has ≥ 3 tenant nodes), optional synchronous replication, 2 PgBouncer pods; priced from their cost | provisioned end to end |
| 3.2 Failure drills | kill primary, isolate primary, partition, lose replica, drain node, delete primary PVC, kill gateway | **HA** gate: exactly one writer in every drill; measured RPO/RTO recorded |
| 3.3 Dedicated plans | per-tenant nodes (taint) + promotion workflow | shared → dedicated with only the documented reconnect |
| 3.4 Publish HA SLO | from drill results only | terms reviewed |

## 6. Phase 4 — Second cell (DR + active-active control plane)

| Step | Tasks | Done when |
|---|---|---|
| 4.1 Cell `c2` | another region (same or another provider, chosen against the same checklist); `envs/c2` with the same modules and bootstrap; own tunnels and R2 buckets; node IPs added to Supabase network restrictions; register cell | cell accepts tenants; tenants placed there get explicit DNS records |
| 4.2 Active-active control plane | control-plane Deployments in both cells, each behind its own tunnel; **Cloudflare Load Balancing** pools (one per cell tunnel) with health monitors for `api`, `mcp`, `oauth` | losing `c1` entirely keeps the API, MCP and dashboards up |
| 4.3 DR tier | CNPG replica clusters in `c2` following `c1` WAL from R2 | replica lag per tenant visible |
| 4.4 Promotion workflow | PDF §15.3 as a River workflow with manual gates; break-glass runbook (admin tunnel or provider console + `kubectl` + direct Cloudflare DNS change) | **DR** gate: region-failure drill records real RPO/RTO, once through the workflow and once through break-glass |
| 4.5 Cross-cell migration | import + logical replication paths | **Migration** gate passes |

## 7. Phase 5 — Scale-out (only when measurements and revenue call for it)

More cells on any provider, including an **AWS or GCP cell** (K3s on their VMs, or their managed Kubernetes; the cluster-agent and operator are the same) when customers or compliance need it and revenue covers its higher price; VictoriaMetrics cluster; larger Supabase compute and a read replica for dashboards; gateway-level SCRAM termination (enables `channel_binding=require`); OpenBao for dynamic credentials; dual-stack cells for IPv6 clients; MySQL adapter (PDF §28).

---

## 8. Acceptance gates

| Gate | Test | Phase |
|---|---|---|
| Tenant isolation | cross-tenant DB/secret/PVC/network/SNI access fails | 1 |
| Resource limits | stress can't exceed limits or starve neighbours | 2 |
| Capacity | supported envelope per shape from replay | 2 |
| Scale | 1,000 synthetic (hibernated) tenants in one cell: reconcile latency, route propagation, API-server latency, etcd latency and agent loop time within targets | 2 |
| Provisioning idempotency | duplicates and crashes converge to one resource | 0/1 |
| Pooler | connection storms and PgBouncer restarts stay under `max_connections` | 1 |
| Backup/PITR | exact known-point restore; gaps reported | 0/1 |
| R2 compatibility | upload, multipart, restore, PITR, retention with locks, re-archive inside lock, interruption; etcd snapshot upload and restore | 0/1 |
| Gateway | client matrix on both hostnames, direct TLS, wrong SNI rejected, allowlists, protocol cancel, failover between node addresses, 10k conns per node | 0/1 |
| Static stability | Supabase blocked + tunnel stopped 30 min, Kubernetes API stopped 15 min: running DBs unaffected, zero primary restarts | 1 |
| Node loss | tenant node powered off: API stays up, clients fail over, node replaced, its tenants restored from R2, RTO measured; `cp-1` powered off: API stays up | 1 |
| Cost | first full month within `03-…` §1; no unplanned billable resource | 1 |
| Billing | charge, invoice, dunning, suspend, restore, webhook replay | 1 |
| HA | exactly one writer in all drills | 3 |
| Migration | concurrent writes, DDL, sequences, large objects, cutover, rollback | 4 |
| MCP authorization | wrong tenant/scope/token/issuer/audience/replay rejected; conformance suite | 1 |
| AI safety | unsafe SQL, unbounded exports, prompt injection blocked or escalated | 1 |
| Security | RBAC, NetworkPolicy, metadata block, PSA, VAP, secrets at rest and in git, TLS, audit, WAF, port scan, K3s CIS self-assessment, CI hardening | 1 |
| DR | region failure drill records RPO/RTO (workflow and break-glass) | 4 |

### Spikes (on your PC in Phase 0 where possible; on `c1` right after Step 1.1)

| ID | Question | Pass condition | Fallback |
|---|---|---|---|
| S1 | K3s v1.36 + CNPG + NetworkPolicy | Kubernetes conformance (sonobuoy quick mode) and CNPG smoke tests pass on the pinned K3s; K3s's policy controller enforces default deny, namespace selectors, `ipBlock` with `except` and egress rules; WireGuard pod traffic between nodes works | Calico in place of flannel + kube-router (a new cell, since the CNI is fixed at creation) |
| S2 | Provider VM quality at launch density | fio 4k random write p99 and fsync p99 < 10 ms, CPU steal < 5 % under load, etcd fsync p99 < 10 ms while 25 `base` tenants run pgbench on the same NVMe; PVC binds, online expansion works, reboot survives; PgBouncer memory measured | another shortlisted provider; lower density per node |
| S3 | pg-gateway on `hostPort` with two A records | client IPs visible (allowlists work); stopping one node's gateway moves new connections to the other address; cancel works; 10k concurrent connections per node | NodePort + host nftables DNAT; PROXY protocol v2 if a provider TCP load balancer is used later |
| S4 | Barman Cloud Plugin ↔ **R2** | WAL archive (zstd); uncompressed base backup > 5 GB (multipart); restore; PITR; retention delete under the 8-day bucket lock, with deletes refused by the lock retried by a later run; re-archiving a WAL segment that already exists inside the lock; killed upload recovers; compressed base backup retested (barman #954); sidecar peak memory and R2 operations per tenant measured | another S3-compatible store; revise ADR-012 |
| S5 | mcp-auth with real MCP clients | Claude Code, VS Code and Cursor complete OAuth; tokens have the MCP URL as `aud`; conformance scenarios pass | DCR-only for affected clients |
| S6 | Multi-arch images | every pinned image has `linux/amd64` and `linux/arm64` | build it in CI |
| S7 | **Supabase Free** from the cell | River coordinator over the session pooler (LISTEN/NOTIFY) from a node IP; control-api over the transaction pooler with `QueryExecModeExec`; network restrictions and SSL enforcement block other sources; worker recovers after a Supabase restart; `pg_dump` of `dbcloud` + `auth` to R2 and restore into `dbcloud-staging` work | River `PollOnly`; upgrade to Pro earlier |
| S8 | Provider facts | private network speed and isolation between the three VMs; included transfer and overage price confirmed; first invoice equals the quote; console and rebuild work | another shortlisted provider |
| S9 | Cloudflare edge | Tunnel streams MCP SSE without buffering; heartbeats keep sessions past the 125 s read timeout; Access protects Grafana, River UI, SSH and `kubectl`; Workers Static Assets deploy; Turnstile works with Supabase Auth; the one rate-limiting rule applies | — |
| S10 | Database DNS and certificates | `*.db.example.com` resolves to both node IPs; cert-manager issues `*.db.example.com` via the Cloudflare DNS-01 solver; an explicit tenant record overrides the wildcard within its TTL | — |
| S11 | K3s operations | etcd snapshot to R2 (region `auto`) and a full restore on new VMs with the saved token; server replacement; a K3s patch upgrade via system-upgrade-controller with tenants running and zero Postgres restarts | manual upgrade runbook; snapshots copied to R2 by a script |

**Gate G0** covers the spikes that run locally (S1 partly, S4 against MinIO and then R2, S5, S6, S7, S9, S10, S11 on k3d); S2, S3, S8 and the rest of S1/S11 pass on `c1` before Step 1.3.

## 9. Admin DB schema outline (Supabase, schema `dbcloud`)

```
organizations(id, name, slug, billing_state, gstin, created_at)
users(id, auth_user_id, email, created_at)                -- auth_user_id = Supabase Auth user id
memberships(org_id, user_id, role)                         -- owner | admin | developer | viewer
api_keys(id, org_id, prefix, hash, scopes, expires_at, revoked_at)
plans(id, name, cpu_request_m, cpu_limit_m, mem_mi, storage_gb, storage_class, instances, max_connections, pooled_clients,
      archive_timeout_s, retention_days, pg_parameters jsonb, price_paise, active)
cells(id, provider, region, name, state, k8s_version, agent_cert_fp, max_nodes, capacity jsonb)
nodes(id, cell_id, name, provider_ref, role, vcpu, mem_gb, disk_gb, public_ipv4, state, joined_at)   -- role: server | agent
capacity_snapshots(cell_id, node_id, taken_at, allocatable jsonb, requested jsonb, lvm_free_bytes, free_pod_slots)
tenants(id, ref, org_id, plan_id, lifecycle, created_at, deleted_at)   -- lifecycle: waitlisted | active | suspended | deletion_pending | purged
databases(id, tenant_id, cell_id, pg_major, backup_slot, desired_generation, desired_spec jsonb, observed_generation, observed_status jsonb)
placements(tenant_id, cell_id, node_id, decided_at, reason jsonb)
routes(tenant_id, hostname_pooled, hostname_direct, cell_id, route_generation, state, checked_at)
tombstones(id, tenant_id, cell_id, generation, requested_by, approved_by, created_at, applied_at)   -- the only thing that lets an agent delete
operations(id, org_id, tenant_id, cell_id, kind, state, idempotency_key, requested_by, provider_request_id, error, created_at, finished_at)
operation_events(operation_id, seq, state, message, at)
backups(id, tenant_id, cell_id, kind, started_at, finished_at, size_bytes, begin_lsn, end_lsn, status)
restore_points(tenant_id, cell_id, first_recoverability_point, last_archived_wal_at)
credentials_meta(id, tenant_id, role, version, created_at, revoked_at)     -- never the secret
subscriptions(id, org_id, tenant_id, plan_id, razorpay_subscription_id, state, current_period_start, current_period_end, grace_until)
payment_events(razorpay_event_id, received_at, kind, payload_hash, processed_at)   -- webhook idempotency
invoices(id, org_id, period, amount_paise, gst_paise, currency, razorpay_ref, state)
usage_hourly(tenant_id, cell_id, hour, cpu_m_hours, mem_mi_hours, storage_gb_hours, backup_gb_hours)
oauth_clients(id, client_id, kind, metadata jsonb, fetched_at, blocked)    -- CIMD cache + DCR clients
oauth_codes(code_hash, client_id, user_id, grant_id, pkce_challenge, resource, expires_at)
oauth_refresh_tokens(token_hash, client_id, user_id, grant_id, family_id, expires_at, revoked_at)
oauth_revoked_tokens(jti, expires_at)                                      -- access-token revocation list, pruned after expiry
mcp_grants(id, user_id, client_id, org_id, tenant_ids uuid[], mode, capabilities text[], expires_at, revoked_at)
approvals(id, grant_id, op_hash, tenant_id, database_id, actor, scope, expires_at, used_at)
audit_events(id, at, actor, actor_type, org_id, tenant_id, cell_id, action, target, request_id, details jsonb)   -- append-only; exported then swept after 90 days
idempotency_keys(key, org_id, request_hash, response jsonb, created_at)
-- plus River tables
```

## 10. API surface

Public (PDF §22.1): `GET /v1/plans`, `POST/GET /v1/databases`, `GET /v1/databases/{id}`, `DELETE /v1/databases/{id}` (staged delete; cancel the operation to undo within the grace period), `POST /v1/databases/{id}/purge` (irreversible, second authorization), `POST /v1/databases/{id}/{scale,storage,backup,restore,export,migrate,credentials/rotate,suspend,resume}`, `GET /v1/databases/{id}/{metrics,logs,health}`, `GET /v1/operations/{id}`, `POST /v1/operations/{id}/cancel`, `/v1/orgs`, `/v1/members`, `/v1/api-keys`, `/v1/mcp/grants`, `/v1/approvals/{id}/{approve,deny}`, `/v1/billing/{subscriptions,invoices}`, `POST /v1/webhooks/razorpay`.

Internal (mTLS, cell agents): `GET /internal/v1/cells/{id}/desired`, `POST /internal/v1/cells/{id}/{status,capacity,events}`.

## 11. Risk register

| # | Risk | Mitigation |
|---|---|---|
| R1 | Value-VPS quality (CPU steal, slow or noisy disks) | spike S2 before launch; steal and fsync alerts; move nodes or cells to another shortlisted provider (provider-neutral design) |
| R2 | Provider price rises or policy change | fixed ceilings per VM (`03-…` §1); cells are rebuildable elsewhere from git + R2 |
| R3 | etcd shares the NVMe with tenants | S2 measures etcd under load; alerts at 10 ms fsync p99; density reduced before anything else |
| R4 | K3s is a distribution; CNPG officially targets vanilla Kubernetes | conformance + CNPG smoke tests on every K3s bump (S1); pinned versions |
| R5 | TopoLVM defect or disk layout not possible on the provider | S2; checklist item 3; OpenEBS LVM LocalPV on the same VG as fallback |
| R6 | R2 + barman incompatibility after an upgrade | S4 suite on every plugin/Barman bump; WAL archive alert; another S3-compatible store |
| R7 | Supabase outage, Free-plan limit or pause | static stability; alerts at 60 %; Pro upgrade trigger; nightly R2 dump + weekly drill |
| R8 | Gateway bug (new code on the data path) | fuzzing, client matrix, one-node-at-a-time rollouts, per-tenant metrics |
| R9 | mcp-auth security defect | certified protocol library, conformance suite, external review before public MCP launch |
| R10 | Losing etcd quorum | 3 servers; etcd snapshots every 6 h to R2; K3s token in the password manager; quorum-loss runbook rehearsed |
| R11 | Public Postgres port attacked | TLS-only gateway, per-IP and per-node caps, unknown tenants get `08004`, provider network DDoS protection, alerts on rejection spikes |
| R12 | Clients without SNI or TLS | `options=endpoint` fallback; TLS required and documented |
| R13 | pg_query_go PG 17 grammar vs PG 18 syntax | unknown syntax rejected in AI tools |
| R14 | Both tenant nodes near full; node loss needs a replacement VM | honest best-effort tier, R2 PITR for every plan, node-loss drill with measured RTO, HA plans in Phase 3 |
| R15 | Cloudflare authoritative-DNS outage | TTL 300 on the A records; node IPs shown in the dashboard |
| R16 | Admin DB restored from an older state (24 h RPO on Free) | agent safety rules (architecture §10.5), `RebuildCatalogFromCells` runbook, Razorpay reconciliation |
| R17 | Single provider region | Phase 4 second cell; `RebuildCell` from git + R2 meanwhile |

## 12. Definition of done (every release)

Tests green (unit, integration, e2e, affected gates); images multi-arch, signed, scanned; Flux reconciles from git; dashboards and alerts cover new behaviour; runbooks updated; changelog entry; rollback tested for anything touching data; code meets `07-engineering-standards.md` and the steering files; no new recurring cost unless the step says so.
