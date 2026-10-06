# 04 — Build Plan (Blueprint 4.0, step by step)

Execution plan for `01-architecture.md`. Every step lists **Tasks**, **Done when** and, where relevant, the spike or gate it depends on. Code follows `07-engineering-standards.md` (also loaded automatically as Kiro steering from `.kiro/steering/`). Costs follow `03-cost-model.md`: the platform stays ≤ ₹9,000/month and nothing billable is created unless a step says so.

## How to work through this plan

- **Order:** top to bottom. A step starts only when the previous step's "Done when" passes in CI or on the real cell. Steps with a letter suffix (0.3b, 0.4b, 0.7b, 0.9b) follow the step they extend.
- **Build first, rent later:** all of Phase 0 is local development on your PC at ₹0 (Docker, k3d, and small QEMU/KVM VMs inside WSL for the storage engine). It builds the production components and tests them locally. The launch VMs are bought only at Step 1.0 (spike S12), after Gate G0, inside Hostinger's 30-day money-back window.
- **UI rule:** the web apps stay the Step 0.1 skeletons until Step 0.11. No UI work starts while an earlier step is open; the backend, storage, operator and gateway come first.
- **Delivery:** one branch + PR per step; CI green (lint, unit, integration, multi-arch images, scans).
- **Everything is code:** Terraform, node bootstrap, Flux manifests, Helm values, Dockerfiles, operator, gateway, tests. Console clicks are only for account sign-up and, where the provider has no API, creating the VM from the bootstrap file.
- **High availability** of the cell is designed but not scheduled ("Deferred (not scheduled): high availability" after Phase 1). No step may assume a single node of any role, so HA can be added without code changes.
- **Safety:** anything destructive or production-affecting (Terraform apply, admin-DB migrations, deletes, node replacement, fencing changes) needs a second reviewer or a signed checklist.
- **Spikes and cost:** a failed spike is recorded with its fallback in `06-decision-records.md` before work continues. A step that adds recurring cost names the cost and the `03-…` trigger it satisfies.

---

## 1. Repository layout (one monorepo)

```
dbcloud/
├── .github/workflows/                # CI: lint → test → build → scan → sign (actions pinned by SHA)
├── api/openapi.yaml                  # public /v1 + internal /internal/v1 contract (OpenAPI 3.1)
├── build/go.Dockerfile               # one multi-stage Dockerfile for every Go binary (ARG BINARY)
├── cmd/                              # Go binaries (thin main packages)
│   ├── control-api/  worker/  cluster-agent/  tenant-operator/
│   ├── compute-governor/  wal-receiver/          # compute-node memory governor; WAL receiver on control-1
│   ├── mcp-server/  mcp-auth/  dbcloud/          # dbcloud = CLI
├── internal/                         # Go packages, not importable from outside
│   ├── domain/        # entities, state machines, plan catalog (caps and ranges)
│   ├── store/         # sqlc output + transactions (pgx v5)
│   ├── jobs/          # River job args + workers
│   ├── placement/     # admission, scoring, waitlist, capacity controller, fencer
│   ├── authz/         # Cedar entities + policy evaluation
│   ├── identity/      # Auth JWT verification (JWKS), API keys, mTLS
│   ├── provider/      # ProviderAdapter interface + `manual` (+ `hostinger` if S12 passes, `aws` for an AWS cell)
│   ├── cloudflare/    # R2 temporary credentials, DNS records (compute-node A records, non-default cells)
│   ├── billing/       # plans, metering (CU-seconds, GB-months), subscriptions, Razorpay webhooks, invoices
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
├── supabase/config.toml              # local Supabase stack, used only until Step 0.6 replaces it
├── policies/cedar/                   # policies + schema + tests
├── web/ {site, customer, admin, ui}  # Next.js 16 static-export apps (+ shared shadcn/ui package), deployed with wrangler
├── sdk/ {go, ts, python}             # generated clients
├── deploy/
│   ├── terraform/ {modules/{cloudflare-edge,r2,vm-<provider>}, envs/{global,c1}}
│   ├── node-bootstrap/               # cloud-init + scripts by role (control, compute, storage): OS hardening, nftables,
│   │                                 # kernel pin + DRBD module, linstorvg, K3s install/config, admin cloudflared
│   ├── k3s/                          # K3s server/agent config templates, PSA and audit policy files, registries.yaml template
│   ├── flux/ {clusters/c1, infrastructure/, apps/}   # SOPS-encrypted secrets under */secrets/
│   ├── local/                        # local-only overlays: k3d profiles, SeaweedFS, QEMU/KVM test-bed seeds (vms/)
│   ├── helm/dbcloud/                 # chart for our services
│   └── upgrade-plans/                # system-upgrade-controller Plans (pinned K3s version)
├── hack/                             # install-tools.sh, local dev scripts (cell, local VMs)
├── test/ {e2e, gates, load, corpus, cell, storage}
├── docs/runbooks/
├── .sops.yaml  .dockerignore
├── package.json  pnpm-workspace.yaml # root of the pnpm workspace (web/*, sdk/ts)
├── tools.versions                    # pinned tool versions
└── Taskfile.yml
```

---

## 2. Phase 0 — Build everything locally (local development, ₹0)

Phase 0 builds and tests the production components on this PC. Nothing in it is called production-ready until its Phase 1 gate passes on the real cell.

### Step 0.0 — Accounts (no VMs yet)
**Tasks:**
- **GitHub:** organisation on Free with an Actions budget of US$0 and "stop usage" on; ghcr.io for private images.
- **Cloudflare:** account, zone `example.com` (DNSSEC on), R2 enabled (needs a payment method; usage stays in the free tier), R2 usage notifications, Zero Trust on the Free plan.
- **Domain:** registered (`.com` at Cloudflare Registrar, or `.in` at a NIXI-accredited registrar with eKYC) and pointed at Cloudflare.
- **Transactional email** account (free tier ≥ 3,000/month over SMTP; candidates in `03-…` §3), domain verified with SPF/DKIM in Cloudflare.
- **Razorpay** account and KYC started (one-time ₹199).
- **VM provider record**, not an order: Hostinger India (launch, ADR-029) and DigitalOcean BLR1 (fallback), checked on paper against `03-…` §2.1. The Hostinger account and order happen at Step 1.0 (spike S12); a DigitalOcean account only if S12 fails.
- No Supabase project: the admin DB and Auth are self-hosted (ADR-028).
- Order, instructions and links: `05-dev-environment-and-downloads.md` §1.

**Done when:** every account exists with MFA; the provider record with prices and checklist results is in ADR-024.

### Step 0.1 — Repo, tooling, CI
**Tasks:**
- **Repo and tooling:** layout above; `tools.versions` + `hack/install-tools.sh`; Go workspace (1.27), Cargo workspace (Rust stable, edition 2024).
- **Web skeletons:** pnpm workspace (Node 24) with three Next.js 16 apps scaffolded for static export (`web/site`, `web/customer`, `web/admin`) and the shared `web/ui` shadcn/ui package, as skeletons only (UI rule above).
- **Lint and config:** `golangci-lint` with gosec, clippy lints, ESLint flat config with `eslint-config-next`; `.kiro/steering/` files; `.sops.yaml` with the cell's age public key.
- **Images:** Dockerfiles (multi-stage, distroless, non-root numeric UID, base images pinned by digest).
- **CI:** GitHub Actions lint → test → build images natively on `ubuntu-24.04` + `ubuntu-24.04-arm` → push to ghcr.io by digest → Trivy → SBOM → cosign (main only). Workflow hardening: actions pinned by SHA, `permissions: contents: read`, `actionlint`, `zizmor`, `hadolint`, ShellCheck. Path filters per component; Renovate.

Compensating controls for GitHub Free, where private repos can't enforce branch rules, CODEOWNERS or deployment environments and have no secret scanning:
- Trivy secret scan in CI and in a pre-commit hook; a check that fails on any unencrypted Kubernetes `Secret` in git.
- Deploy and `terraform apply` workflows run only on `workflow_dispatch` by the owner, with repository secrets scoped to those workflows.
- Work lands through pull requests with green CI, by rule in `.kiro/steering/70-ci.md`.
- Move to GitHub Team, paid from revenue, when a second engineer joins.

**Done when:** CI is green on skeleton binaries/crate/apps within the free minutes budget (measured per run); each Next.js app exports and deploys to a preview Worker; a planted fake secret fails the secret scan; `task dev-up` works on a clean machine.

### Step 0.2 — API contract and admin schema v0
**Tasks:** `api/openapi.yaml` (PDF §22.1 + internal agent endpoints + plans/billing, RFC 9457 errors); oapi-codegen; goose migrations for §9; sqlc queries; River migrations; idempotency middleware; per-service database roles.
**Done when:** migrations apply and roll back on the local admin DB; contract tests pass; a repeated POST with the same `Idempotency-Key` returns the original response.
**Status (2026-09-30): done locally** (against the local Supabase stack, which Step 0.6 replaces):
- **Schema:** 10 goose migrations (35 tables incl. River, RLS on all, per-role grants, `retention_sweep`) with schema invariant tests.
- **Contract:** a 44-operation OpenAPI 3.1 contract with contract tests + vacuum lint, and generated Go types.
- **Idempotency:** sqlc idempotency queries and middleware (per-caller keys, replay, 409/422, one-time secrets never stored), tested in memory, against Postgres and through a local transaction pooler.
- **CI:** the admin-DB tests run against a throwaway Postgres 17.6 service.

### Step 0.3 — Local cell (k3d = K3s in Docker)
**Tasks:**
- **Admin DB:** `supabase start` (local Postgres + Auth) at the time.
- **k3d:** the pinned K3s release (`v1.36.4+k3s1`) in the single-server shape and a `reference` profile (3 servers + 1 agent), Traefik/ServiceLB/local-storage disabled, the same K3s config templates as production (`deploy/k3s/`).
- **Flux:** SOPS decryption (a local age key), fed from an OCI (Open Container Initiative) artifact of `deploy/` in a localhost registry (ADR-026).
- **Components:** TopoLVM on a loop-device VG (local only); CNPG 1.30.1 + Barman Cloud Plugin 0.15.0; SeaweedFS as the local S3 (ADR-026); cert-manager (self-signed); VictoriaMetrics single.
- **Image check:** whether the pinned PostgreSQL image is built with lz4 and zstd (`pg_config --configure`).

**Done when:** a hand-written CNPG Cluster archives WAL to the local S3 and restores; a NetworkPolicy default-deny between two namespaces is enforced by K3s's policy controller; stopping one k3d server of the `reference` profile keeps the API up (now the optional HA compatibility check); the image's compression support is recorded in the plan catalog.

**Status (2026-09-30): done locally.**
- **Shapes:** `task cell:up` builds the cell in the single-server shape (`PROFILE=starter`) or `PROFILE=reference` (3 servers + 1 agent, the deferred HA shape) from the shared `deploy/k3s/` config, then Flux applies every layer.
- **`task cell:test` (`test/cell`)** passes on both shapes:
  - PSA `restricted` by default, Metadata-only audit log, secrets encryption on, WireGuard link on every node;
  - tenant default-deny (own pooler, local S3 and 443 out allowed; other tenants, platform services and other ports blocked);
  - TopoLVM ENOSPC at the volume size and online expansion without a restart;
  - PITR to T1 returns exactly T1's rows;
  - stopping one server keeps the API and a tenant's writes going, with the server rejoining etcd.
- **TopoLVM setup:** its `lvmd` ran on the WSL host (one loop-file VG per tenant node, `hack/local-host.sh`), because k3d nodes have no LVM tools.
- **Image:** the pinned PostgreSQL 18.6 image is built `--with-lz4 --with-zstd` (`02-…` §5).
- **Findings that changed the plan:** the backup sidecar needs 160 Mi, not 96 Mi (`03-…` §5), and Flux, cert-manager (cainjector kept) and VictoriaMetrics need more than planned (`03-…` §6).
- **Superseded part:** the TopoLVM storage of this step is **replaced by Step 0.3b** (ADR-027). Everything else in it stands.

### Step 0.3b — Separated storage on the local cell
**Why a VM test bed:** the WSL kernel (6.18) has `nvme-tcp`, `nvmet`, `dm-thin` and `tls` but no DRBD. DRBD resources are kernel-global, and all k3d nodes share the WSL kernel, so a storage node's diskful resource and a compute node's diskless client of the same volume can't coexist in k3d. WSL exposes `/dev/kvm` (nested KVM verified on this PC). The storage engine is therefore tested on real VMs with stock Ubuntu kernels, as in production.

**Tasks:**
- **Test bed:**
  - **VMs:** three Ubuntu 24.04 cloud-image VMs under QEMU/KVM inside WSL, `control` (2 vCPU / 3 GB), `storage` (1 vCPU / 1.5 GB + a 20 GB data disk) and `compute` (2 vCPU / 2.5 GB), on a host-only bridge, created and destroyed by `hack/local-vms.sh` from cloud-init seeds in `deploy/local/vms/`.
  - **Kernel:** stock Ubuntu generic kernel; DRBD built by the Piraeus loader, as in production.
  - **Memory:** `.wslconfig` raised to 12 GB (the PC has 16 GB).
  - **Temporary fourth VM:** tests that need a second storage or compute node add `storage-2` or `compute-2` (1.5 GB) for their duration.
- **Bootstrap:** pull the minimum of Step 0.12 forward. The test-bed VMs are bootstrapped by the production node bootstrap: users and SSH keys, nftables (K3s, LINSTOR 3366–3367 and DRBD 7000–7999 only from peer IPs), K3s install by role (`control` server; `storage` and `compute` agents with their labels and the storage taint), the pinned generic kernel + headers, swap off on compute.
- **Storage layer (production manifests):**
  - Vendor the **Piraeus Operator v2.12.0** manifests (checksummed in `deploy/vendor/`, images by digest in `images.lock`).
  - Add a Flux layer `infrastructure/storage`:
    - `LinstorCluster` with TLS between controller and satellites (cert-manager);
    - `LinstorSatelliteConfiguration` for host networking and DRBD TLS (`tlshd` sidecar);
    - LVM-thin storage pool `tenant-pool` on `dbcloud.io/pool=storage` nodes and `platform-pool` on `dbcloud.io/pool=control`; compute nodes diskless;
    - StorageClasses `tenant-network` and `platform-local` with the parameters in architecture §7.1;
    - external-snapshotter + snapshot-controller if K3s doesn't bundle them.
  - In `deploy/k3s/server.yaml`: kube-controller-manager `disable-force-detach-on-timeout: true`.
  - Swap the `topolvm-system` PSA exemption for `piraeus-datastore`.
- **k3d** stays for the fast inner loop on everything that isn't the storage engine. Its `starter` profile becomes a control server (`pool=control`) + a storage agent (`pool=storage`, tainted) + a compute agent (`pool=compute`), so taints, nodeSelectors and priority classes are exercised.
  - **Local-only deviation:** in k3d, `tenant-network` and `platform-local` are stand-in StorageClasses on K3s's bundled local-path provisioner, re-enabled only in `deploy/local/k3s/`. Local-path ignores PVC sizes and can't expand, snapshot or attach remotely, so every storage test runs on the VM test bed, never in k3d.
- **Delete TopoLVM:** its Flux layer, vendored chart, PSA exemption, `topolvm-system` namespace, the loop-file volume groups and the host `lvmd` (the `lvm-up`/`lvm-down` parts of `hack/local-host.sh`, its sudo rule and module setup that only TopoLVM needed).
- **Acceptance tests** (`test/storage`, run against the VM test bed):
  - **Expansion:** online expansion of a tenant PVC with writes running, no pod restart; ENOSPC at the volume size, and the pool unaffected.
  - **Compute-node loss:** power off the `compute` VM; apply the `out-of-service` taint by hand (the fencer arrives in Step 0.7b); the tenant restarts on `compute-2` and reattaches diskless with zero data loss (pgbench with a consistency check, sentinel rows).
  - **Fencing safety:** block the DRBD ports between `compute` and `storage` without stopping the VM; writes freeze (`suspend-io`); no second Primary is possible before the old node is fenced.
  - **Online migration:** with pgbench running, move a volume to `storage-2` (`toggle-disk --migrate-from`) with no pod restart, and the old copy removed.
  - **PITR unchanged:** Barman → SeaweedFS in the test bed, PITR to T1 returns exactly T1's rows.
  - **Isolation unchanged:** the Step 0.3 default-deny and cross-tenant tests pass on the test bed.
  - **Encryption:** DRBD traffic on the wire is TLS; DRBD and LINSTOR ports refuse non-peer sources.
  - **Baselines:** fio fsync p99 and pgbench on the DRBD device vs a local disk in the same VMs, as a baseline for S12 (nested virtualization, so not production numbers).

**Done when:**
- `task cell:up` (k3d, three-role `starter`) and `task vms:up` (test bed) both come up from the repo.
- `test/storage` passes on the test bed and `test/cell` still passes on k3d.
- No TopoLVM object, chart, PSA exemption, host `lvmd` or loop-file VG remains in the repo or on the PC.
- The local-only deviation is documented in `deploy/local/`.
- The measured latency baseline is recorded in `03-…` §6.1 notes.

### Step 0.4 — `TenantDatabase` CRD + tenant-operator v0
**Tasks:**
- **CRD** (architecture §6.2) with `compute.cpu/memory {min, max}` validated against the plan caps and `storage.size/maxSize`.
- **Renderers** for the objects in architecture §6.1:
  - PVCs on `tenant-network`; nodeSelector `dbcloud.io/pool=compute`; priority class `dbcloud-tenant`; unreachable/not-ready tolerations without a time limit (only the fencer moves pods);
  - Burstable resources from the user's range (CPU and memory request = min, limit = max on the Postgres container; sidecar and PgBouncer request = limit);
  - Postgres parameters from the plan and the range (`shared_buffers` from the min, `effective_cache_size` from the max);
  - one Pooler; NetworkPolicies incl. the metadata-endpoint block; `isolationCheck` off for `instances: 1`;
  - sidecar resources and `retentionPolicyIntervalSeconds: 21600` on the `ObjectStore`; staggered weekly base slot; `nodeMaintenanceWindow` handling.
- **Reconcile mechanics:** finalizers; conditions + `observedGeneration`; staged deletion.
- **Tests:** envtest unit tests; Chainsaw e2e (create → Ready → change range → expand storage → suspend → delete).

**Done when:**
- e2e is green on k3d, and expansion is also green on the Step 0.3b test bed.
- The operator killed mid-reconcile converges; reapplying the same spec changes nothing.
- A pod in tenant A can't reach tenant B.
- Rendered requests and limits equal the range (min, max) and never exceed the plan caps; tenant pods land only on compute nodes.
- The rendered liveness-probe fields match the pinned CNPG version.

### Step 0.4b — Compute governor + usage metering
**Tasks:**
- **Governor** (`cmd/compute-governor`): a DaemonSet on compute nodes in `dbcloud-governor` (PSA exemption, second reviewer; write access only to tenant pod cgroups).
  - Every 1–5 s it reads PSI (`memory.pressure`, `cpu.pressure`) and `memory.stat` per tenant Postgres container.
  - It sets `memory.high` inside [floor, max], running `memory.reclaim` before lowering, and sets `memory.oom.group = 1`.
  - It keeps the node's sum of targets ≤ allocatable − headroom, denying growth when the node is full.
  - It never patches CNPG objects or pods (`research-building-blocks.md` §10.1).
- **Metrics:** target, usage and pressure per tenant.
- **Metering:** per-second CPU (`cpu.stat usage_usec`) and memory target samples, aggregated by the worker into `usage_hourly` as CU-seconds (proposed 1 CU = 1 vCPU + 2 GiB, ADR-030) and storage GB-months from the PVC sizes.
- **Tests:** the control law in table and property tests; e2e on k3d (cgroup v2, pinned K3s).

**Done when:**
- 100 scale cycles under a synthetic workload give zero restarts and zero OOM kills.
- `memory.high` survives a container restart and a kubelet resync (or the governor restores it within one loop).
- A synthetic burst of every tenant on a node never pushes the sum of targets over the budget, and kubelet eviction never fires.
- A governor crash leaves tenants running at their last targets.
- Metered totals match the cgroup counters within 1 %.

### Step 0.5 — pg-gateway v0 (Rust)
**Tasks:**
- **Listener:** graceful drain; SSLRequest + direct TLS (ALPN); SNI and `options=endpoint` resolution.
- **Routes:** a route table from a kube-rs watch of `TenantDatabase` and EndpointSlices, readiness on a complete list, signed peer snapshots.
- **Limits:** IP allowlist; per-tenant pooled and direct caps, per-node global cap and per-source-IP rate limit.
- **Protocol:** ErrorResponse builder; upstream relay (pooled → PgBouncer pod, direct → Postgres primary pod); `SCRAM-SHA-256-PLUS` stripping; cancel keys (replica ID + HMAC) with mTLS forwarding between replicas.
- **Operations:** metrics; fuzz targets for the startup-packet parser.
- **Deployment:** a DaemonSet on compute nodes (`dbcloud.io/pool=compute`) with `hostPort` 5432 (architecture §8.2).

**Done when:**
- psql 14/16/18, pgx v5, node-postgres, pgjdbc, psycopg 3 and asyncpg connect by hostname through the gateway on k3d, with default libpq settings (`channel_binding=prefer`), on both the pooled and the `-direct` hostname.
- With the hostname resolving to two node addresses and one node's gateway stopped, libpq and pgx connect through the other address.
- A suspended tenant gets SQLSTATE `57P03`; unknown SNI gets `08004`.
- A protocol-level CancelRequest (psql Ctrl-C, pgx context cancel, JDBC `Statement.cancel()`) cancels the running query on both hostnames, including during a rolling update.
- A replica restarted while the Kubernetes API is unreachable becomes ready from a peer snapshot and serves every existing route.
- Fuzzing runs 30 min without a crash; a gateway pod stays under 32 Mi at idle.

### Step 0.6 — Admin DB + self-hosted Auth in the local cell; control-api v0 + identity + authz
**Tasks:**
- **Admin DB** (ADR-028):
  - CNPG `dbcloud-admin` in `dbcloud-control`, on the k3d stand-in class and on `platform-local` in the VM test bed;
  - Barman → the local S3, `archive_timeout` 60 s, daily base backups;
  - a PgBouncer `Pooler` in transaction mode, per-service roles, TLS `verify-full` against the CNPG CA;
  - goose migrations as an in-cell Job that dumps first; the Step 0.2 migrations move here unchanged.
- **Auth:** self-hosted Supabase Auth (image by digest) on the `auth` schema, with ES256 signing keys in SOPS Secrets (key variable names checked against Supabase's self-hosting docs), Turnstile test keys, TOTP MFA, SMTP to a local mail sink and rate limits.
- **Retire the local Supabase stack:** remove `supabase start` from the Taskfile and delete `supabase/config.toml`.
- **control-api v0:**
  - Auth JWT verification via JWKS (ES256; `iss`, `aud`, `exp`, `nbf`); org API keys;
  - orgs/memberships/tenants/databases/operations handlers;
  - plan catalog (`base`, `plus`, `premium` with caps and default ranges) + entitlement and range checks;
  - waitlist when capacity or `max_nodes` is reached; per-org and per-IP rate limits;
  - Cedar policies + table-driven tests; audit events on every mutation.

**Done when:**
- A user signs up and signs in with MFA against the local Auth, and the token verifies in control-api.
- A member of org A can't read or change org B's objects (tests).
- Every mutation returns 202 + `operation_id`; a create request beyond capacity returns an operation in state `WAITLISTED`; a range outside the plan caps is rejected with a problem-details error.
- Admin-DB PITR to a timestamp restores exactly that state.
- Nothing in the repo uses `supabase start` any more.

### Step 0.7 — worker v0 (River) + placement
**Tasks:**
- **Jobs:** `ProvisionTenant`, `ScaleTenant`, `ExpandStorage`, `MoveVolume` (online `toggle-disk --migrate-from`, shaped), `BackupNow`, `RestoreTenant`, `SuspendTenant`, `DeleteTenant`, `PurgeTenant`, `RotateCredentials`, `ExportAdminDB`, `RetentionSweep`, `SyncNodeDNS` (compute-node A records), `MeterUsage`.
- **River mechanics:** per-kind timeouts, snooze-polling for long operations, `RescueStuckJobsAfter` 10 min; state change and job insert in one transaction (`InsertTx`); River coordinator on a direct session connection; unique jobs per idempotency key.
- **Placement v0:** one cell; compute nodes by range minimum under the active CPU rule (`03-…` §6.1); storage pools by the 80 % rule; backup slots; admission.
- **Provider:** `ProviderAdapter` with the `manual` implementation.

**Done when:**
- The worker killed mid-job completes the job exactly once; two identical requests make one tenant.
- Insufficient capacity ends `WAITLISTED` with a clear reason and a "node needed: compute | storage" alert.
- A storage pool past 80 % produces a planned move instead of a new volume.
- The same code passes against the local admin DB in both connection modes (direct session for the worker, Pooler transaction mode with `QueryExecModeExec` for control-api).

### Step 0.7b — Compute-node fencer
**Tasks:**
- **Detection:** a capacity-controller loop in the worker. When a compute node has been NotReady past its threshold (planning value 60 s), it starts `FenceComputeNode`.
- **Fencing job:**
  1. `ProviderAdapter.PowerOff`, then confirm with `PowerState`. An ambiguous answer means: re-read the provider state, never taint without a confirmed power-off.
  2. Apply `node.kubernetes.io/out-of-service:NoExecute`.
  3. Wait for CNPG to recreate the pods on another compute node.
  4. `UnfenceComputeNode` removes the taint after repair or replacement.
- **Safety limits:**
  - At most one compute node is fenced at a time.
  - If every compute node turns NotReady together, the fault is assumed on the control side: the fencer fences nothing and alerts.
  - Storage nodes and `control-1` are never fenced.
- **`manual` adapter:** the fencer pages the owner and waits for a confirmed power-off through an admin API endpoint (its admin page arrives in Step 0.11).
- **Tests:** a test-only adapter that powers local QEMU VMs off (test code, not shipped). Every step is an audited River job.

**Done when** (on the Step 0.3b test bed with `compute-2`):
- Powering off `compute` brings its tenants Ready on `compute-2` within 2 min with zero data loss.
- `compute` partitioned from `control` only (DRBD to `storage` still up) is powered off before the taint, and pgbench shows no divergence.
- All compute nodes NotReady at once → no fencing and an alert.
- An ambiguous provider answer → no taint, a retry with backoff.
- The `manual` path waits for confirmation.
- Every step is visible in `audit_events`.

### Step 0.8 — cluster-agent v0
**Tasks:**
- **Transport:** mTLS client cert per cell; long-poll `GET /internal/v1/cells/{id}/desired`; server-side apply.
- **Reports:** status + capacity per node and role (Allocatable, requests, LINSTOR pool free and provisioned bytes, free pod slots).
- **Resilience:** backoff while control-api is down.
- **Agent safety rules** (architecture §10.5): deletes only on tombstones, generation rollbacks rejected, unknown objects quarantined, deletes rate-limited.

**Done when:** control-api stopped for 10 min → cell unchanged, then converges; cell X's certificate can't read cell Y's specs; a desired state from an older admin-DB backup changes nothing in the cell and raises an alert.

### Step 0.9 — Backup and restore v0
**Tasks:**
- **Backups:** per-tenant `ObjectStore` prefix; weekly base backups in per-tenant slots and `archive_timeout` by plan (architecture §14.1); zstd WAL compression; base-backup upload shaping on compute nodes.
- **Restores:** PITR into a new `TenantDatabase`; sentinel-table verification; `RestoreDrill` job.
- **Storage-loss path:** parallel restore of all tenants of a storage node onto another storage node, with bounded concurrency.
- **Cell restore:** K3s etcd snapshots to the local S3, and a restore of the local cell from one (including LINSTOR's state, compared with the satellites' resources afterwards).

**Done when:**
- PITR to T1 returns exactly T1's rows; an archive gap is detected and reported.
- Restoring 25 small tenants in parallel completes and is timed (k3d for the logic, the VM test bed for the storage-loss path).
- A cell rebuilt from an etcd snapshot comes back with every `TenantDatabase` object and a consistent LINSTOR resource list.

### Step 0.9b — Streaming WAL receiver on `control-1`
**Tasks:**
- **Controller:** `cmd/wal-receiver` on `control-1` supervises one async receiver per tenant (`pg_receivewal --synchronous --slot`, or pgrwl after a review; the choice and its reason go into ADR-031). It watches `TenantDatabase` objects and keeps a spool on `platform-local`.
- **Operator side:** it renders the replication slot, the `streaming_replica` access (NetworkPolicy from the receiver only) and `max_slot_wal_keep_size` per plan.
- **Restore integration:** `RestoreTenant` and the storage-loss path replay R2 base + WAL first, then the receiver's newer segments.
- **Monitoring:** alerts on receiver lag and slot retention; spool retention is bounded (segments already in R2 are pruned).

**Done when:**
- Commit latency with the receiver on is within noise of the receiver off.
- Receiver lag p99 < 1 s at the target write rate.
- CNPG accepts the configuration on `instances: 1`.
- On the VM test bed, a simulated storage-node loss restored with the receiver loses < 1 s of committed transactions (sentinel timestamps).
- A receiver stopped for 1 h can't fill a tenant's volume (the slot cap holds) and the alerts fire.

### Step 0.10 — MCP v0 + mcp-auth v0
**Tasks:**
- **mcp-server:** Go SDK; 2026-07-28 stateless + 2025-11-25; RFC 9728 metadata; read tools `list_projects`, `list_tables`, `get_schema`, `query_read`, `explain`, `get_health`.
- **mcp-auth** on zitadel/oidc: PKCE S256, CIMD with an SSRF-guarded fetcher, DCR fallback, `resource` → `aud`, RFC 9207, loopback any port, login via the local self-hosted Auth, consent page in the customer app (its UI lands in Step 0.11; until then an API-level test client).
- **`sqlsafety` v0.**

**Done when:** MCP Inspector and one desktop client (Claude Code or VS Code) connect via CIMD; wrong `aud`/`iss`/expired tokens are rejected; a read-only grant can't write (`WITH … INSERT`, `pg_read_file`, multi-statement); the MCP conformance authorization scenarios pass.

### Step 0.11 — CLI and web apps v0
UI work starts here (UI rule).

**Tasks:**
- **CLI:** `dbcloud login` (device grant via mcp-auth's CLI client, `aud` = `https://api.example.com`), `db create/list/info/credentials`.
- **Customer app** (Next.js static export): sign-in (the Supabase Auth client against the local Auth + Turnstile), org, plan picker with the compute range inside the plan caps, create DB, connection strings, usage charts (CPU, memory target, storage), operations, MCP consent page, resource pages by query parameter (`/databases/detail?id=…`).
- **Admin app:** foundation (layout and navigation), plus the fencer's power-off confirmation page; its other feature pages arrive with the steps that need them.
- **Hardening and checks:** strict CSP generated into `_headers`; Playwright + axe checks.

**Done when:** on the local stack, a new user signs up, creates a DB and connects with psql using only the CLI or the dashboard.

### Step 0.12 — Node bootstrap (tested locally)
**Tasks:** `deploy/node-bootstrap/` cloud-init + scripts for Ubuntu 24.04, one bootstrap with a role variable `control`, `compute` or `storage`:
- **Base:** users and SSH keys (no passwords, no root login); unattended security updates; K3s CIS sysctls.
- **Kernel and DRBD:** the generic kernel pinned (held), with headers; the DRBD 9 module built for it through the pinned Piraeus loader image. A kernel update is applied only after a module build test against the new kernel passes.
- **Firewall:** nftables. From the internet, TCP 5432 on compute nodes only. Between nodes, only the cell's peer IPs, on the K3s ports, 3366–3367 (LINSTOR) and 7000–7999 (DRBD).
- **Disks:** LVM `linstorvg` with a thin pool on `control` and `storage` (refusing any device with a filesystem signature); swap off on `compute`.
- **K3s:** install from the pinned release with checksum verification. Config by role (architecture §5.1): embedded etcd and `cluster-init` on `control`, agents with the role labels, the storage taint, wireguard-native, disabled add-ons, hardening flags, kubelet reservations, Spegel, etcd snapshots to R2.
- **Registry and admin access:** `registries.yaml` for ghcr.io; the admin `cloudflared` systemd service.
- **Testing:** on the Step 0.3b QEMU/KVM VMs.

**Done when:**
- The bootstrap forms the three-role cell on local VMs and joins a second compute and a second storage agent.
- It is idempotent (a second run changes nothing).
- A kernel update whose DRBD build fails is held back.
- ShellCheck passes.
- Joining two more control servers belongs to the optional HA compatibility check ("Deferred (not scheduled): high availability").

**Gate G0:** steps 0.0–0.12 (including 0.3b, 0.4b, 0.7b, 0.9b) done locally; CI green; ready for the storage spike S12.

---

## 3. Phase 1 — Launch on cell `c1` (three VMs, paying customers)

**Launch shape (ADR-029):** cell `c1` is three Hostinger India VMs (`03-…` §1, §6.1):
- **`control-1`** (KVM 2): the single K3s server with `cluster-init`, every platform workload with 1 replica, the admin DB and Auth;
- **`storage-1`** (KVM 8): every tenant volume;
- **`compute-1`** (KVM 4): tenants and the gateway.

High availability is not scheduled ("Deferred (not scheduled): high availability" after Gate G1); nothing in Steps 1.0–1.12 assumes one node of any role.

### Step 1.0 — Storage spike S12 on Hostinger (gate before Step 1.1)
**Tasks:**
- **Buy** the three plans (term per the owner's decision, ADR-024) inside the 30-day money-back window, plus a temporary fourth VM (KVM 4) for the failover test, refunded or kept as `compute-2`.
- **Ask Hostinger support in writing** whether VPS-to-VPS traffic counts toward the transfer allowance, whether two VPS can be placed on different physical hosts, and which API endpoints power a VPS off and report its state.
- **Run S12 within one week:**

| Test | How | Pass |
|---|---|---|
| Network | `ping -c 1000`, `sockperf`, `iperf3 -P4` both ways between the VMs | RTT p99 < 1 ms; ≥ 900 Mbps |
| Disk | `fio` 4k randwrite QD1 + `pg_test_fsync` on `storage-1` | local fsync p99 < 2 ms |
| DRBD path | Piraeus v2.12 on K3s; diskless client on `compute-1`; `fio` + `pgbench` with and without DRBD TLS | DRBD fsync p99 < 3 ms; pgbench TPS within 50 % of local |
| Transfer accounting | push 200 GB VPS-to-VPS, read hPanel graphs; the support answer | internal traffic not counted, or projected use < 50 % of the allowance |
| CPU credits | sustained pgbench at 40 % and 80 % of vCPUs for 4 h; watch steal | behaviour matches the documented model; capacity uses the 30 % baseline |
| Kernel | `uname -r`; DRBD loader build; `modinfo tls`; Secure Boot state | DRBD 9 loads on the template kernel |
| Disk layout | repartition the template disk (or rescue mode) for `linstorvg` | ≥ 350 GB pool on `storage-1` |
| Failover | hard-stop `compute-1` through the API with `compute-2` present; time to `Ready` | ≤ 2 min, no data loss (pgbench consistency) |
| Fencing safety | block the DRBD port between `compute-1` and `storage-1` without stopping it | old primary freezes (`suspend-io`); the new pod promotes only after the old node is fenced |
| API and Terraform | power off / power state through the API; `hostinger` Terraform provider creates a VPS | both work (otherwise `manual` adapter and hand-made VMs) |
| Platform sizes | run the platform on `control-1` | measured usage within ±20 % of `03-…` §6; sustained CPU vs the 0.6 vCPU baseline recorded |

- **Reject Hostinger if** internal traffic counts *and* projected storage traffic exceeds ≈ 50 % of the allowance, *or* RTT p99 > 1 ms, *or* the DRBD module can't be built and loaded. Then refund inside the window and switch to the fallback: DigitalOcean BLR1, 2-node design (`03-…` §2.2). Update ADR-024 and ADR-029 before continuing.
- **Record** all numbers, the support answers and the decision in ADR-024; feed the measured sizes into `03-…` §6 and §6.1.

**Done when:** S12 passed (or the fallback is adopted and recorded); the owner has decided the CPU rule and the term (`03-…` §7).

### Step 1.1 — Form the launch cell
**Tasks:**
- **VMs:** create the three VMs with Terraform `modules/vm-hostinger` if the Hostinger provider covers it, otherwise by hand with the bootstrap as cloud-init user data. Run the bootstrap by role (`control`, `storage`, `compute`).
- **Cloudflare:** API tokens scoped per use.
- **Terraform state:** the bucket `dbcloud-tfstate` is created once with `wrangler r2 bucket create` and imported into `envs/global`.

**Done when:** the three VMs pass the checklist (with the recorded item-4 deviation); their monthly cost matches `03-…` §1; the bootstrap has formed the three-role cell; `terraform plan` (where used) shows no changes.

### Step 1.2 — Terraform: global + `c1`
**`envs/global`:**
- **Edge:** Cloudflare zone settings, WAF rules, the rate-limiting rule, Access apps/policies (admin app, Grafana, River UI, SSH per node, K3s API), Turnstile widget.
- **R2:** buckets `dbcloud-c1-pg`, `dbcloud-c1-etcd`, `dbcloud-admin-pg`, `dbcloud-admin-exports` and the imported `dbcloud-tfstate`, plus bucket-lock rules.
- **DNS:** `*.db.example.com` as DNS-only A records → each compute node's public IPv4, `compute-1` at launch, TTL 300; `auth.example.com` on the tunnel; SPF/DKIM for email.

**`envs/c1`:** the cell's HTTP tunnel and admin tunnel with their ingress hostnames; the provider's VMs and firewall where Terraform can manage them.

The Next.js web apps are not in Terraform: they deploy with wrangler (Step 1.5).

**Done when:** a second `terraform plan` shows no changes; SSH and the K3s API are reachable only through Cloudflare Access; a port scan from outside shows only TCP 5432 on compute nodes and nothing on `control-1` and `storage-1`; each node's real Allocatable is written into `03-…` §6.1.

### Step 1.3 — Cell bootstrap
**Tasks:**
- **Flux bootstrap:** read-only deploy key; SOPS age key created on the cell and stored in the password manager.
- **Infrastructure layer:**
  - NetworkPolicy baseline (default deny + DNS + metadata block) for every platform namespace;
  - Piraeus/LINSTOR with DRBD TLS and the StorageClasses `tenant-network` and `platform-local`;
  - cert-manager + ClusterIssuer (Cloudflare DNS-01); CNPG + Barman plugin + `ClusterImageCatalog`;
  - system-upgrade-controller + Plans;
  - VictoriaMetrics single + vmagent + vmalert + Alertmanager, VictoriaLogs + Fluent Bit (incl. the K3s audit log), Grafana;
  - cloudflared (1 replica); PSA labels and ValidatingAdmissionPolicies; priority classes; weekly `trivy k8s` Job.
- **Images:** third-party images mirrored by digest into ghcr.io.
- **CoreDNS:** K3s's packaged CoreDNS manifest replaced with a copy whose memory limit = request (K3s keeps a replaced packaged manifest when a `.skip` file sits next to it).

**Done when:** all HelmReleases are Ready; measured platform usage is within ±20 % of the platform requests in `03-…` §6; a chart bump reconciles with no manual step; an etcd snapshot lands in R2; a test PVC on `tenant-network` attaches diskless on `compute-1` with DRBD TLS on.

### Step 1.4 — Admin DB + Auth on `control-1`
**Tasks:**
- **Admin DB:** `dbcloud-admin` with Barman → `dbcloud-admin-pg` (bucket lock), the Pooler, and per-service roles (`dbcloud_app`, `dbcloud_worker`, `dbcloud_migrator`, `dbcloud_dump`, `supabase_auth_admin`) with no `VALID UNTIL`; TLS `verify-full`; NetworkPolicy to the listed pods only.
- **Migrations:** goose migrations as an **in-cell Kubernetes Job** that dumps to R2 first (the release's migration image, applied by Flux before the app rollout; expand/contract, `lock_timeout` set). CI never connects to the admin DB.
- **Data hygiene and drills:** RLS on every table; River migrations; nightly `ExportAdminDB` to `dbcloud-admin-exports`; weekly restore drill (PITR + dump) on `control-1`; `RetentionSweep` for data older than 90 days; alerts at 70 % of the volume and on backup age.
- **Auth:** at `auth.example.com` with production ES256 keys, Turnstile, TOTP, providers, custom SMTP = the chosen email service, redirect URLs and rate limits.

**Done when:** the drill restores and a checksum query matches production; a failed migration Job blocks the app rollout; a connection from any pod outside the allowed list is refused; sign-up with MFA works through `auth.example.com`.

### Step 1.5 — Platform services
**Tasks:** deploy control-api, worker (with the fencer and the `manual` or `hostinger` adapter), cluster-agent, tenant-operator, compute-governor, wal-receiver, mcp-server, mcp-auth, River UI (`jobs.example.com`, behind Access) and the pg-gateway DaemonSet on compute nodes (architecture §8.2); deploy the Next.js apps with wrangler; add the external uptime probe (Workers Cron Trigger).

**Done when:** the smoke suite is green against `https://api.example.com`, `https://mcp.example.com/mcp`, `https://auth.example.com` and `t-smoke.db.example.com` through each compute node's address.

### Step 1.6 — Tenant lifecycle on `c1`
**Tasks:** create → connect (pooled + direct) → write/read → change range (memory follows the governor without a restart inside the range) → expand storage online → backup → PITR restore → rotate credentials (create-before-revoke) → suspend (gateway returns `57P03`) → staged delete → purge with second authorization.

**Done when:** gates **Tenant isolation, Provisioning idempotency, Pooler, Backup/PITR, R2 compatibility, Gateway, Storage** pass on `c1`.

### Step 1.7 — Observability, usage tracking and cost report
**Tasks:**
- **Dashboards:** tenant, node per role, storage (LINSTOR/DRBD), etcd, gateway, governor, platform, SLO, cost.
- **Alert rules:** PDF §21 plus the list in architecture §17 (etcd fsync p99 and leader changes, CPU steal and sustained CPU vs baseline, LINSTOR pool data/metadata, DRBD connection and resync, DRBD module vs kernel, WAL receiver lag and slot retention, WAL archive age, free pod slots, gateway rejections, tunnel health, admin-DB backup age, Auth errors, restore-drill failures, etcd snapshot age, certificate expiry, R2 credential lifetime, R2 monthly usage, transfer vs allowance at 70 %).
- **Routing:** email + chat.
- **Customer usage:** `GET /v1/databases/{id}/metrics` for usage charts.
- **Cost report:** the weekly cost report job (architecture §17, `03-…` §10).

**Done when:** each "Immediate" alert fired in a test reaches on-call within 2 minutes; the external probe alerts when every gateway of the cell is stopped; a customer can see only their own usage.

### Step 1.8 — Security gate
**Tasks:**
- **Tenant boundaries:** cross-tenant attempts (pod-to-pod, wrong SNI, `options` spoofing); metadata endpoint unreachable from tenant pods.
- **Exposure:** external port scan (only 5432 on compute nodes); DRBD and LINSTOR ports closed to non-peer sources; DRBD traffic verified as TLS on the wire; SSH and K3s API reachable only through Access with MFA.
- **Kubernetes:** `kubectl auth can-i --list` matrix per service account; PSA/VAP rejections (incl. tenant pods off compute nodes, governor cgroup scope); K3s CIS self-assessment (benchmark v1.12) with findings fixed or justified.
- **Secrets and scanning:** secrets encrypted at rest and absent from logs and git (incl. Auth signing keys); Trivy clean; WAF/rate-limit rules tested; CI hardening checks (`zizmor`, `actionlint`) clean.

**Done when:** **Security** gate passes; findings fixed or risk-accepted in writing.

### Step 1.9 — MCP and AI-safety gates
**Tasks:** write tools (`query_write`, `migrate` with approval), `backup.create`, `operations.get`; approval binding; negative suites (wrong tenant/scope/issuer/audience, replayed approval, expired token); unsafe SQL + prompt-injection corpus stored in tenant data.

**Done when:** **MCP authorization** and **AI safety** gates pass.

### Step 1.10 — Runbooks v1 + static stability + node-loss drills
**Tasks:**
- **Write runbooks for:**
  - provisioning failure, WAL archive failure, PITR restore, credential rotation failure;
  - node loss per role: **compute** (fencer, or waiting for the VM with one compute node), **storage** (`RestoreStorageNode`: tenants from R2 + the WAL receiver), **control** (`RestoreControlNode`: etcd snapshot with the saved K3s token, LINSTOR state check, admin DB from R2);
  - `RebuildCell` (fresh K3s + Flux when no usable snapshot exists);
  - K3s upgrade, OS reboot window per role, kernel update with DRBD module gating, storage pool full / volume move;
  - transfer cap near, CPU credits draining;
  - admin DB outage, Auth outage, Cloudflare outage, Kubernetes API outage;
  - gateway overload, certificate expiry, capacity full, provider price change, MCP security incident, `RebuildCatalogFromCells`.
- **Rehearse each runbook once.** The drills run before customers are invited, so they may power real nodes off.

**Done when:**
- Every runbook is rehearsed.
- **Static stability** gate passes: `control-1`'s K3s API, admin DB and Auth blocked and the tunnel stopped for 30 min, then the K3s API stopped for 15 min. Running databases keep serving on existing routes, zero primary restarts, mutations return `503` + `Retry-After`, everything converges afterwards.
- **Control-node loss drill:** power off `control-1`; databases keep serving; rebuild it with `RestoreControlNode`; record the RTO.
- **Storage-node loss drill:** with synthetic tenants only, power off `storage-1` and restore every tenant from R2 + the WAL receiver onto a rebuilt `storage-1`; record RTO and the measured RPO, and set the published values from them.
- **Compute-node loss:** with one compute node, its tenants come back when the VM returns (time recorded). The fencer drill with two compute nodes ran in Step 1.0 and repeats on `c1` when `compute-2` joins (Step 2.1).
- The server-loss and quorum-loss drills belong to "Deferred (not scheduled): high availability".

### Step 1.11 — Public site and billing
**Tasks:**
- **Public site** `web/site` (Next.js static export at `example.com`): product, pricing (the `03-…` §4 plans with their caps), docs, About, Contact, Terms, Privacy, Cancellation/Refund and Shipping pages. Razorpay requires these before live keys; the legal wording is reviewed by the owner's lawyer or accountant.
- **Razorpay review:** website review with a sample invoice and a test login.
- **Billing:** Razorpay Subscriptions (UPI AutoPay, cards) per plan; signed, idempotent webhooks; GST invoice data (GSTIN, place of supply, HSN/SAC as advised by the company's accountant); proration on plan change; dunning emails → suspend → staged delete; reconciliation job against Razorpay.
- **Metering:** usage (CU-seconds, GB-months) shown on invoices for transparency; CU pricing only if the owner has set it (ADR-030).

**Done when:** Razorpay has approved the website and issued live keys; a test subscription is charged, invoiced and reconciled; a failed renewal suspends the database after the grace period and a payment restores it; replayed webhooks change nothing.

### Step 1.12 — Launch
**Tasks:** invited customers first, then public sign-up up to the compute and storage capacity under the chosen CPU rule (`03-…` §6.1), then the waitlist. Weekly capacity/error/cost review.

**Done when:** Gate G1 passes.

**Gate G1:**
- Phase 1 gates green for 14 consecutive days on the three-role launch cell.
- Weekly restore drills green.
- The node-loss drills' RTOs are recorded and published.
- **Cost** gate green: the first full month is within `03-…` §1 and §7.
- No open Sev-1/Sev-2.

---

## Deferred (not scheduled): high availability

High availability of the cell is **not part of the step-by-step plan**. The design is kept here so it can be added later without a rebuild or code changes.

**Trigger:** monthly net revenue covers the extra VMs and the doubled storage, or a customer needs an availability commitment.

**Design:**
- **Control plane:** rent `control-2` and `control-3` (the shape of `control-1`) from the same provider and region. Run the node bootstrap with role `control` in server-join mode; both join the existing embedded etcd through `control-1` (no rebuild: `cluster-init` was set at launch). Three etcd members keep quorum when any one VM is lost. Without a private network, etcd peer traffic (2379–2380) is TLS and allow-listed between the three servers; it is accepted only if their RTT stays < 1 ms.
- **Platform:** Deployments and CoreDNS go to 2 replicas with `topologySpreadConstraints` across the control nodes and PodDisruptionBudgets `minAvailable: 1`.
- **Storage:** placement count 2 on `tenant-network` across two storage nodes (`storage-1`, `storage-2`) plus a diskless tie-breaker on a control node, which gives DRBD quorum. The Piraeus HA controller then fails a volume over in ≈ 10 s, and losing a storage node loses nothing (RPO 0). Existing volumes gain their second replica online. Raw storage doubles (`03-…` §7). The WAL receiver gets a second instance on another control node, so synchronous RPO 0 (`ANY 1 (r1, r2)`) becomes possible for plans that want it.
- **Compute and gateway:** at least two compute nodes, so DNS returns a second gateway address and the fencer always has a target.
- **Runbooks and drills:** **server loss** (power off a control node: the API stays up; replace the etcd member), **quorum loss** (two servers down: running pods keep serving; restore quorum from the surviving member or the latest etcd snapshot in R2), **storage-node loss with replicas** (power off a storage node: volumes keep serving, zero data loss).
- **Done when (at that time):** `etcdctl member list` shows three healthy members; stopping any one VM keeps the API, the platform and every volume up; a fresh etcd snapshot restores; no tenant was restarted during the join.

**Compatibility kept now:** code and manifests make no single-node assumption:
- leader election stays on with 1 replica;
- placement handles N nodes per role;
- one gateway and one A record per compute node;
- StorageClass parameters carry the placement count;
- the bootstrap keeps its role variable.

The local `reference` k3d profile (3 servers + 1 agent) with `TestControlPlaneSurvivesServerLoss` is an **optional** compatibility check, run when the K3s configuration changes, not on every step.

---

## 4. Phase 2 — Growth paid from revenue

Each step starts only when its `03-…` §6.1/§7 trigger is met.

| Step | Tasks | Done when |
|---|---|---|
| 2.1 Second compute node | `compute-2` (KVM 4 or larger) joins as a K3s agent with role `compute`; the worker adds its A record; placement spreads tenants; the fencer drill runs on `c1` | powering off `compute-1` moves its tenants to `compute-2` within 2 min with zero data loss; clients reconnect through the other A record |
| 2.2 Storage growth + provider adapter | `storage-2` (or a bigger storage node) joins with role `storage`; `MoveVolume` jobs rebalance by free space; if the provider has an API, a `ProviderAdapter` implementation creates and removes nodes within `max_nodes`; removal only after evacuation (compute) or online moves (storage) | a synthetic load triggers a node request of the right role before admission fails; a 100 GB volume moves online with writes running and no pod restart; nothing is added past `max_nodes` |
| 2.3 Maintenance automation | K3s upgrades through system-upgrade-controller, kernel updates gated by the DRBD module build, and OS reboot windows per role (architecture §12.3) run on a schedule with tenant notices | an upgrade and a reboot cycle per role complete with only the announced interruptions |
| 2.4 Per-tenant R2 credentials | worker mints prefix-scoped temporary credentials (TTL ≥ 72 h), refreshes at 25 % of lifetime | tenant A's credentials can't read tenant B's prefix; a refresh failure alerts before 24 h remain |
| 2.5 Capacity envelope | pgbench/k6 replay per plan, range and node shape; governor tuning; measured sustained CPU per tenant feeds the CPU rule and CU pricing (ADR-030) | **Capacity** and **Resource limits** gates pass; limits written into the plan catalog |
| 2.6 Scale gate (₹0) | 1,000 synthetic `TenantDatabase` objects in `c1` whose CNPG clusters use declarative hibernation (no pods; volumes thin and empty) | **Scale** gate passes; architecture §5.3 limits updated from the measurement |
| 2.7 Hardening | Cloudflare Pro decision (OWASP rules); status page; external review of mcp-auth before public MCP launch | go/no-go checklist signed |

## 5. Phase 3 — HA plans (when revenue funds them)

HA plans need the cell's high availability first ("Deferred (not scheduled): high availability"), because CNPG failover goes through the Kubernetes API.

| Step | Tasks | Done when |
|---|---|---|
| 3.1 HA plans | primary + replica on different compute nodes with volumes on different storage nodes (3 instances once the cell has ≥ 3 compute nodes), optional synchronous replication, 2 PgBouncer pods; priced from their cost | provisioned end to end |
| 3.2 Failure drills | kill primary, isolate primary, partition, lose replica, drain node, lose a storage node, delete primary PVC, kill gateway | **HA** gate: exactly one writer in every drill; measured RPO/RTO recorded |
| 3.3 Dedicated plans | per-tenant compute nodes (taint) + promotion workflow | shared → dedicated with only the documented reconnect |
| 3.4 Publish HA SLO | from drill results only | terms reviewed |

## 6. Phase 4 — Second cell (DR + active-active control plane)

| Step | Tasks | Done when |
|---|---|---|
| 4.1 Cell `c2` | another region (same or another provider, chosen against the same checklist); `envs/c2` with the same modules and bootstrap; own tunnels and R2 buckets; register cell | cell accepts tenants; tenants placed there get explicit DNS records |
| 4.2 Active-active control plane | control-plane Deployments in both cells, each behind its own tunnel; **Cloudflare Load Balancing** pools (one per cell tunnel) with health monitors for `api`, `mcp`, `oauth`; the admin DB gets a promotable CNPG replica cluster in `c2` and a cross-cell connection design (decided at this step) | losing `c1` entirely keeps the API, MCP and dashboards up after promotion |
| 4.3 DR tier | CNPG replica clusters in `c2` following `c1` WAL from R2 | replica lag per tenant visible |
| 4.4 Promotion workflow | PDF §15.3 as a River workflow with manual gates; break-glass runbook (admin tunnel or provider console + `kubectl` + direct Cloudflare DNS change) | **DR** gate: region-failure drill records real RPO/RTO, once through the workflow and once through break-glass |
| 4.5 Cross-cell migration | import + logical replication paths | **Migration** gate passes |

## 7. Phase 5 — Scale-out (only when measurements and revenue call for it)

Phase 5 is a list of options, each taken only when measurements and revenue call for it:
- **More cells** on any provider, including an **AWS cell** (K3s on EC2, LINSTOR pools on gp3 volumes up to 64 TiB for very large databases; the cluster-agent and operator are the same), when customers or compliance need it and revenue covers its higher price.
- **Ceph RBD** for striped volumes once a cell has ≥ 3–5 storage nodes on ≥ 10 GbE (ADR-027).
- The **object-store page tier** if a trigger in ADR-031 fires (spike S13 first).
- **Observability and control-plane scale:** VictoriaMetrics cluster; a larger admin-DB instance and a read replica for dashboards.
- **Protocol and security:** gateway-level SCRAM termination (enables `channel_binding=require`); OpenBao for dynamic credentials; dual-stack cells for IPv6 clients.
- **Other engines:** a MySQL adapter (PDF §28).

---

## 8. Acceptance gates

| Gate | Test | Phase |
|---|---|---|
| Tenant isolation | cross-tenant DB/secret/PVC/network/SNI access fails | 1 |
| Resource limits | stress can't exceed range caps or starve neighbours; the governor's node budget holds | 2 |
| Capacity | supported envelope per plan, range and node shape from replay | 2 |
| Scale | 1,000 synthetic (hibernated) tenants in one cell: reconcile latency, route propagation, API-server latency, etcd latency and agent loop time within targets | 2 |
| Provisioning idempotency | duplicates and crashes converge to one resource | 0/1 |
| Pooler | connection storms and PgBouncer restarts stay under `max_connections` | 1 |
| Storage | online expansion; ENOSPC at the volume size; DRBD TLS on; fencing safety (`suspend-io`); online volume move (locally in Step 0.3b, on `c1` once `storage-2` exists) | 0/1 |
| Backup/PITR | exact known-point restore; gaps reported; WAL receiver RPO < 1 s on a simulated storage loss | 0/1 |
| R2 compatibility | upload, multipart, restore, PITR, retention with locks, re-archive inside lock, interruption; etcd snapshot upload and restore | 0/1 |
| Gateway | client matrix on both hostnames, direct TLS, wrong SNI rejected, allowlists, protocol cancel, failover between node addresses, 10k conns per node | 0/1 |
| Static stability | `control-1`'s API, admin DB and Auth blocked + tunnel stopped 30 min, Kubernetes API stopped 15 min: running DBs unaffected, zero primary restarts | 1 |
| Compute-node loss | fencer + `out-of-service`: tenants Ready on another compute node ≤ 2 min, zero data loss (Step 1.0 on Hostinger, Step 2.1 on `c1`); one compute node: tenants return with the VM | 1/2 |
| Storage-node loss | every tenant restored from R2 + WAL receiver onto a rebuilt storage node; RTO and RPO measured | 1 |
| Control-node loss | `control-1` powered off: databases keep serving; `RestoreControlNode` rebuilds it; RTO measured (server-loss and quorum drills: deferred HA section) | 1 |
| Cost | first full month within `03-…` §1; no unplanned billable resource | 1 |
| Billing | charge, invoice, dunning, suspend, restore, webhook replay | 1 |
| HA | exactly one writer in all drills | 3 |
| Migration | concurrent writes, DDL, sequences, large objects, cutover, rollback | 4 |
| MCP authorization | wrong tenant/scope/token/issuer/audience/replay rejected; conformance suite | 1 |
| AI safety | unsafe SQL, unbounded exports, prompt injection blocked or escalated | 1 |
| Security | RBAC, NetworkPolicy, metadata block, PSA, VAP, secrets at rest and in git, TLS (incl. DRBD TLS), audit, WAF, port scan (DRBD/LINSTOR ports closed to non-peers), K3s CIS self-assessment, CI hardening | 1 |
| DR | region failure drill records RPO/RTO (workflow and break-glass) | 4 |

### Spikes (on your PC in Phase 0 where possible; on Hostinger at Step 1.0)

| ID | Question | Pass condition | Fallback |
|---|---|---|---|
| S1 | K3s v1.36 + CNPG + NetworkPolicy | Kubernetes conformance (sonobuoy quick mode) and CNPG smoke tests pass on the pinned K3s; K3s's policy controller enforces default deny, namespace selectors, `ipBlock` with `except` and egress rules; WireGuard pod traffic between nodes works | Calico in place of flannel + kube-router (a new cell, since the CNI is fixed at creation) |
| S2 | Provider VM quality at launch density | runs as part of S12 on Hostinger: fio and fsync, CPU steal < 5 % under load, etcd fsync p99 < 10 ms on `control-1` next to the admin DB, sustained CPU within the baseline, PgBouncer memory measured | the fallback provider in S12 |
| S3 | pg-gateway on `hostPort` with two A records | client IPs visible (allowlists work); cancel works; 10k concurrent connections per node; stopping one node's gateway moves new connections to the other address (proven locally in Step 0.5, on Hostinger in Step 1.0 with the temporary fourth VM, on `c1` once `compute-2` exists) | NodePort + host nftables DNAT; PROXY protocol v2 if a provider TCP load balancer is used later |
| S4 | Barman Cloud Plugin ↔ **R2** | WAL archive (zstd); uncompressed base backup > 5 GB (multipart); restore; PITR; retention delete under the 8-day bucket lock, with deletes refused by the lock retried by a later run; re-archiving a WAL segment that already exists inside the lock; killed upload recovers; compressed base backup retested (barman #954); sidecar peak memory and R2 operations per tenant measured | another S3-compatible store; revise ADR-012 |
| S5 | mcp-auth with real MCP clients | Claude Code, VS Code and Cursor complete OAuth; tokens have the MCP URL as `aud`; conformance scenarios pass | DCR-only for affected clients |
| S6 | Multi-arch images | every pinned image has `linux/amd64` and `linux/arm64` (incl. Piraeus, LINSTOR, DRBD loader, Supabase Auth) | build it in CI |
| S7 | Admin DB + self-hosted Auth in the cell | River coordinator over a direct session (LISTEN/NOTIFY); control-api over the CNPG Pooler with `QueryExecModeExec`; TLS `verify-full` against the CNPG CA; NetworkPolicy blocks other sources; worker recovers after an admin-DB restart; PITR to a timestamp; dump + restore of `dbcloud` + `auth`; Auth issues ES256 tokens verified via JWKS, with Turnstile and TOTP | River `PollOnly`; managed Postgres or Supabase as the admin DB (ADR-028 fallback) |
| S8 | Provider facts | runs as part of S12: transfer accounting (support answer in writing), overage behaviour, first invoice equals the quote, console and rebuild work, API power-off/state | the fallback provider in S12 |
| S9 | Cloudflare edge | Tunnel streams MCP SSE without buffering; heartbeats keep sessions past the 125 s read timeout; Access protects Grafana, River UI, SSH and `kubectl`; Workers Static Assets deploy; Turnstile works with the self-hosted Auth at `auth.example.com`; the one rate-limiting rule applies | — |
| S10 | Database DNS and certificates | `*.db.example.com` resolves to every compute node's IP; cert-manager issues `*.db.example.com` via the Cloudflare DNS-01 solver; an explicit tenant record overrides the wildcard within its TTL | — |
| S11 | K3s operations | etcd snapshot to R2 (region `auto`) and a full restore on a new VM with the saved token, including LINSTOR's state (the control-node loss path); a K3s patch upgrade via system-upgrade-controller with tenants running and zero Postgres restarts | manual upgrade runbook; snapshots copied to R2 by a script |
| S12 | **Storage split on Hostinger** (Step 1.0) | RTT p99 < 1 ms and ≥ 900 Mbps; local fsync p99 < 2 ms; DRBD fsync p99 < 3 ms; pgbench within 50 % of local; internal traffic not counted or < 50 % of the allowance; CPU credits as documented; DRBD module builds on the template kernel; ≥ 350 GB pool; compute failover ≤ 2 min with no data loss; fencing safety. **Reject** if internal traffic counts and storage traffic would exceed ≈ 50 % of the allowance, or RTT p99 > 1 ms, or DRBD can't be built | DigitalOcean BLR1, 2-node design (`03-…` §2.2) |
| S13 | **Neon-lite** (optional, non-launch; ADR-031) | WAL on the `tenant-network` volume, PGDATA on a ZeroFS NBD device over R2 with a large local cache: pgbench p99 within 2× of local for a cached working set; no data loss under kill -9 of ZeroFS and of the node; R2 operations per tenant-month inside `03-…` §8 | drop the idea; keep ADR-031's deferred track |

**Gate G0** covers the spikes that run locally: S1 partly, S4 against the local S3 and then R2, S5, S6, S7, S9, S10, and S11 on k3d and the VM test bed. S12 (with S2, S8 and S3's provider part) passes on Hostinger at Step 1.0, before Step 1.1. S13 is never a launch gate.

## 9. Admin DB schema outline (CNPG `dbcloud-admin`, schema `dbcloud`)

Columns added by this revision (`plans` ranges and caps, `nodes.role`, pool bytes, placement storage pool, `cu_seconds`) arrive as expand-only migrations in the steps that use them (0.4, 0.4b, 0.7).

```
organizations(id, name, slug, billing_state, gstin, created_at)
users(id, auth_user_id, email, created_at)                -- auth_user_id = Supabase Auth (self-hosted) user id
memberships(org_id, user_id, role)                         -- owner | admin | developer | viewer
api_keys(id, org_id, prefix, hash, scopes, expires_at, revoked_at)
plans(id, name, cpu_min_m, cpu_max_m, mem_min_mi, mem_max_mi, storage_gb, storage_cap_gb, storage_class, instances,
      max_connections, pooled_clients, archive_timeout_s, retention_days, pg_parameters jsonb, price_paise, active)
cells(id, provider, region, name, state, k8s_version, agent_cert_fp, max_nodes, capacity jsonb)
nodes(id, cell_id, name, provider_ref, role, k3s_role, vcpu, mem_gb, disk_gb, cpu_baseline_m, public_ipv4, state, joined_at)
      -- role: control | compute | storage; k3s_role: server | agent
capacity_snapshots(cell_id, node_id, taken_at, allocatable jsonb, requested jsonb, pool_free_bytes, pool_provisioned_bytes, free_pod_slots)
tenants(id, ref, org_id, plan_id, lifecycle, created_at, deleted_at)   -- lifecycle: waitlisted | active | suspended | deletion_pending | purged
databases(id, tenant_id, cell_id, pg_major, backup_slot, desired_generation, desired_spec jsonb, observed_generation, observed_status jsonb)
placements(tenant_id, cell_id, compute_node_id, storage_node_id, storage_pool, decided_at, reason jsonb)
routes(tenant_id, hostname_pooled, hostname_direct, cell_id, route_generation, state, checked_at)
tombstones(id, tenant_id, cell_id, generation, requested_by, approved_by, created_at, applied_at)   -- the only thing that lets an agent delete
operations(id, org_id, tenant_id, cell_id, kind, state, idempotency_key, requested_by, provider_request_id, error, created_at, finished_at)
operation_events(operation_id, seq, state, message, at)
backups(id, tenant_id, cell_id, kind, started_at, finished_at, size_bytes, begin_lsn, end_lsn, status)
restore_points(tenant_id, cell_id, first_recoverability_point, last_archived_wal_at, last_received_wal_at)
credentials_meta(id, tenant_id, role, version, created_at, revoked_at)     -- never the secret
subscriptions(id, org_id, tenant_id, plan_id, razorpay_subscription_id, state, current_period_start, current_period_end, grace_until)
payment_events(razorpay_event_id, received_at, kind, payload_hash, processed_at)   -- webhook idempotency
invoices(id, org_id, period, amount_paise, gst_paise, currency, razorpay_ref, state)
usage_hourly(tenant_id, cell_id, hour, cpu_m_hours, mem_mi_hours, cu_seconds, storage_gb_hours, backup_gb_hours)
oauth_clients(id, client_id, kind, metadata jsonb, fetched_at, blocked)    -- CIMD cache + DCR clients
oauth_codes(code_hash, client_id, user_id, grant_id, pkce_challenge, resource, expires_at)
oauth_refresh_tokens(token_hash, client_id, user_id, grant_id, family_id, expires_at, revoked_at)
oauth_revoked_tokens(jti, expires_at)                                      -- access-token revocation list, pruned after expiry
mcp_grants(id, user_id, client_id, org_id, tenant_ids uuid[], mode, capabilities text[], expires_at, revoked_at)
approvals(id, grant_id, op_hash, tenant_id, database_id, actor, scope, expires_at, used_at)
audit_events(id, at, actor, actor_type, org_id, tenant_id, cell_id, action, target, request_id, details jsonb)   -- append-only; exported then swept after 90 days
idempotency_keys(principal, key, request_hash, status_code, response jsonb, created_at, completed_at)   -- scoped to the caller (user:/api_key:), 24 h; one-time secrets never stored
-- plus River tables; Supabase Auth owns schema auth
```

## 10. API surface

Public (PDF §22.1): `GET /v1/plans`, `POST/GET /v1/databases`, `GET /v1/databases/{id}`, `DELETE /v1/databases/{id}` (staged delete; cancel the operation to undo within the grace period), `POST /v1/databases/{id}/purge` (irreversible, second authorization), `POST /v1/databases/{id}/{scale,storage,backup,restore,export,migrate,credentials/rotate,suspend,resume}`, `GET /v1/databases/{id}/{metrics,logs,health}`, `GET /v1/operations/{id}`, `POST /v1/operations/{id}/cancel`, `/v1/orgs`, `/v1/members`, `/v1/api-keys`, `/v1/mcp/grants`, `/v1/approvals/{id}/{approve,deny}`, `/v1/billing/{subscriptions,invoices}`, `POST /v1/webhooks/razorpay`. `scale` sets the compute range inside the plan caps.

Internal (mTLS, cell agents): `GET /internal/v1/cells/{id}/desired`, `POST /internal/v1/cells/{id}/{status,capacity,events}`.

## 11. Risk register

| # | Risk | Mitigation |
|---|---|---|
| R1 | VM quality (CPU steal, CPU credits at a 30 % baseline, slow or noisy disks) | spike S12 before launch; capacity against the baseline; steal, sustained-CPU and fsync alerts; move volumes, tenants or the cell to the fallback provider (provider-neutral design) |
| R2 | Provider price rises or policy change | fixed ceilings per VM (`03-…` §1); cells are rebuildable elsewhere from git + R2 |
| R3 | etcd shares `control-1`'s NVMe with the admin DB and observability | S12 measures etcd under load; alerts at 10 ms fsync p99; observability retention trimmed first |
| R4 | K3s is a distribution; CNPG officially targets vanilla Kubernetes | conformance + CNPG smoke tests on every K3s bump (S1); pinned versions |
| R5 | LINSTOR/DRBD defect, or the DRBD module fails to build on a new kernel | Step 0.3b and S12 suites; kernel updates gated on the module build; pinned Piraeus versions; Cozystack's production use as a reference; restore from R2 as the last resort |
| R6 | R2 + barman incompatibility after an upgrade | S4 suite on every plugin/Barman bump; WAL archive alert; another S3-compatible store |
| R7 | Self-hosted admin DB or Auth fails | static stability; CNPG PITR from `dbcloud-admin-pg`; nightly dump; weekly drills; fallback to managed Postgres or Supabase (ADR-028) |
| R8 | Gateway bug (new code on the data path) | fuzzing, client matrix, one-node-at-a-time rollouts, per-tenant metrics |
| R9 | mcp-auth security defect | certified protocol library, conformance suite, external review before public MCP launch |
| R10 | Losing `control-1` (the single etcd member) | data path keeps serving; etcd snapshots every 6 h to R2; K3s token in the password manager; `RestoreControlNode` rehearsed; three servers once HA is scheduled (deferred section) |
| R11 | Public Postgres port attacked | TLS-only gateway, per-IP and per-node caps, unknown tenants get `08004`, provider network DDoS protection, alerts on rejection spikes |
| R12 | Clients without SNI or TLS | `options=endpoint` fallback; TLS required and documented |
| R13 | pg_query_go PG 17 grammar vs PG 18 syntax | unknown syntax rejected in AI tools |
| R14 | One storage node and one compute node at launch | honest best-effort tier; WAL receiver (RPO < 1 s) + R2 PITR; storage- and compute-loss drills with measured RTO; `compute-2` first, then `storage-2` and HA as revenue allows |
| R15 | Cloudflare authoritative-DNS outage | TTL 300 on the A records; node IPs shown in the dashboard |
| R16 | Admin DB restored to an older point | agent safety rules (architecture §10.5), `RebuildCatalogFromCells` runbook, Razorpay reconciliation; admin-DB RPO ≤ 60 s |
| R17 | Single provider region | Phase 4 second cell; `RebuildCell` from git + R2 meanwhile |
| R18 | Hostinger transfer cap with a 10 Mbps throttle; no private network | S12 measures internal-traffic accounting; shaping; alerts at 70 %; encrypted, allow-listed node traffic (ADR-029) |
| R19 | Thin-pool exhaustion on a storage node | oversubscription 1.0, placement stop at 80 %, alerts at 70/80 % data and metadata, online moves |
| R20 | Fencer misfires (fencing a healthy node, or a cascade during a control-side partition) | power-off confirmed before the taint; one node at a time; no fencing when every compute node is NotReady; audited jobs; DRBD single-primary as the last line |

## 12. Definition of done (every release)

Tests green (unit, integration, e2e, affected gates); images multi-arch, signed, scanned; Flux reconciles from git; dashboards and alerts cover new behaviour; runbooks updated; changelog entry; rollback tested for anything touching data; code meets `07-engineering-standards.md` and the steering files; no new recurring cost unless the step says so.
