# 03 — Cost Model, Budget and Capacity (Blueprint 4.0)

All prices checked 2026-09-29; Hostinger, block-volume and Supabase prices 2026-10-01 to 2026-10-07. USD prices are converted at **₹96 per US$** (USD/INR on 2026-09-29); re-convert at the day's rate. Prices exclude GST unless stated (18 % GST applies on top; recoverable only as input credit once GST-registered). Anything marked **verify** is re-checked when it is reached in the build plan. Capacity figures marked **planning value** are computed from the measurements named next to them and are re-measured in spike S12 (on the launch VMs) and Step 1.2 (on the live cell).

**Budget rule:** the whole platform costs **≤ ₹9,000/month**. Anything beyond that (more nodes, high availability, a second cell, HA plans) is added only when monthly revenue already covers it.

**Launch provider:** Hostinger India (Mumbai), three VMs in three roles (ADR-029), accepted only after spike S12 passes inside Hostinger's 30-day money-back window. Fallback: DigitalOcean BLR1; later big-database cell: AWS Mumbai (§2.2). The design itself stays provider-neutral (checklist §2.1).

---

## 1. What the platform needs, and what it costs

| Item | What | ₹ / month (renewal; intro in brackets) |
|---|---|---|
| **`control-1`** Hostinger KVM 2 | 2 vCPU / 8 GB / 100 GB NVMe / 8 TB transfer: the K3s server, platform, control plane, admin DB + Auth, WAL receiver | **1,199** (799) |
| **`storage-1`** Hostinger KVM 8 | 8 vCPU / 32 GB / 400 GB NVMe / 32 TB transfer: every tenant volume (LINSTOR/DRBD) | **4,399** (2,199) |
| **`compute-1`** Hostinger KVM 4 | 4 vCPU / 16 GB / 200 GB NVMe / 16 TB transfer: tenant Postgres + PgBouncer + pg-gateway | **2,399** (1,099) |
| Admin DB + Auth | self-hosted on `control-1` (ADR-028) | 0 |
| Cloudflare | DNS, Tunnel, Access, WAF, Turnstile, Workers Static Assets | 0 |
| Cloudflare R2 | backups: 10 GB free, then US$0.015/GB-month | ~0–200 typical (worst case in §8) |
| Transactional email | SMTP service with a free tier ≥ 3,000/month (§3) | 0 |
| GitHub Free + ghcr.io | code, CI, private container images | 0 |
| Domain | `.com` or `.in` | ~55–85 |
| **Total** | | **≈ ₹8,067 at renewal** (≈ ₹4,167 during the intro term), ex-GST |

- The intro prices need a **24-month prepay**: ≈ 24 × ₹4,097 = **₹98,328 upfront** for the three VMs. Shorter terms cost more per month (**verify** at order time). The term is an owner decision, recorded in ADR-024.
- The first growth step is `compute-2` (KVM 4, +₹2,399 renewal): it gives compute failover (`01-…` §7.5) and more tenants. It is bought when revenue covers it (§7).
- Supabase Pro (≈ ₹2,400/month, or ≈ ₹12,000 with PITR) is not needed: the admin DB and Auth run on `control-1` (ADR-028).
- **Paid from revenue, not the budget:** Razorpay takes ≈ **₹12.45** of each ₹352.82 `base` payment (2.99 % + GST on the fee). **One-time costs:** ₹199 Razorpay KYC and the domain's first year.
- **Local development costs ₹0:** Docker + k3d + QEMU/KVM test VMs inside WSL (build plan Phase 0).

---

## 2. VM checklist, launch shapes and fallbacks

### 2.1 Checklist (every item must pass, or its deviation is recorded in an ADR, before a provider is used; checked in spike S12)

| # | Requirement | Why |
|---|---|---|
| 1 | Region in India, or ≤ 70 ms RTT from Mumbai/Bengaluru (for example Singapore) | customer latency |
| 2 | KVM or equivalent full virtualization (AWS Nitro counts), root access, Ubuntu 24.04 LTS with the generic kernel and its headers, x86_64 or arm64 | K3s, WireGuard, LVM, the DRBD 9 module build |
| 3 | Local NVMe; storage nodes ≥ 400 GB with the space after a ~40 GB root filesystem available to LVM (repartitioning possible, or a second disk or a network block volume such as AWS gp3) | the LINSTOR pool `linstorvg` |
| 4 | **Free private network** between VMs in the same region. **Deviation allowed only with a record:** without one, every node-to-node path is encrypted (WireGuard, DRBD TLS, K3s TLS) and allow-listed by peer IP (Hostinger: ADR-029) | storage and K3s traffic between nodes |
| 5 | One public IPv4 per VM, included or priced into §7; inbound firewall (provider or host) | Postgres entry on port 5432 |
| 6 | Included transfer large enough for storage replication, backups and customer traffic, with internal traffic either free or measured and alerted; where egress is billed per GB (EC2), §8's uploads plus customer traffic are priced into §7 | DRBD traffic crosses the provider network |
| 7 | Flat monthly (or capped hourly) price; any prepay term is an explicit owner decision | predictable bill under the cap |
| 8 | Measured in spike S12 (`04-…` §8 spikes): node-to-node RTT p99 < 1 ms and ≥ 900 Mbps; local fsync p99 < 2 ms; DRBD fsync p99 < 3 ms; CPU steal < 5 % under load; on CPU-credit plans, capacity uses the sustained baseline | commit latency, noisy neighbours, CPU credits |
| 9 | Reinstall/rebuild and a console for break-glass access | recovery |
| 10 | API with power-off and power-state endpoints (for the fencer) and a Terraform provider; nice to have: snapshots, floating IP | automation (the design works without them, with the `manual` adapter) |

### 2.2 Launch shape and fallbacks (2026-10-07)

| Provider | `control-1` | `storage-1` | `compute-1` | ≈ ₹ / month | Within ₹9,000? |
|---|---|---|---|---|---|
| **Hostinger India** (renewal; intro on a 24-month prepay in brackets) | KVM 2: 2 vCPU / 8 GB / 100 GB, ₹1,199 (₹799) | KVM 8: 8 vCPU / 32 GB / 400 GB, ₹4,399 (₹2,199) | KVM 4: 4 vCPU / 16 GB / 200 GB, ₹2,399 (₹1,099) | **7,997** (4,097) | yes |
| **DigitalOcean BLR1** (fallback, 2-node design) | Basic 4 GiB / 2 vCPU, US$24 | none: a 200 GiB Volume (US$0.10/GiB, up to 16 TiB) attached to the compute Droplet | Basic 8 GiB / 4 vCPU, US$48 | ≈ 8,832 (US$92) | yes, only with ≤ 200 GiB of Volume |
| **AWS Mumbai** (`ap-south-1`, later big-database cell) | t4g.medium or t4g.large | m6a.large + gp3 (US$0.0912/GB-month ≈ ₹8.8, up to 64 TiB, online resize) | m6a.large (US$40.55 ≈ ₹3,893) | ≈ 13,900–15,500 before egress (3 public IPv4 at US$3.65 each included) | no |

**Hostinger facts the cost model and the checklist depend on** (`.local/reports/research-storage-split.md` §4, checked 2026-10-07):
- **No add-on disk:** disk and RAM can't be upgraded on their own, and KVM 8 (400 GB) is the top plan. More storage means more or bigger storage nodes (ADR-027).
- **One public IPv4 and no documented private network:** this fails checklist item 4 and is the recorded deviation in ADR-029. All node-to-node traffic is encrypted and allow-listed.
- **Monthly transfer cap** (KVM 2: 8 TB, KVM 4: 16 TB, KVM 8: 32 TB, i.e. ≈ 3.1 / 6.2 / 12.3 MB/s averaged over a month). Past the cap the port is **throttled to 10 Mbps** until the month ends. Whether VPS-to-VPS traffic counts is unknown: ask support in writing, measure it in S12, alert at 70 %.
- **CPU credits:**
  - **Baseline:** 30 % of the vCPUs, i.e. ≈ 0.6 vCPU sustained on KVM 2, ≈ 1.2 on KVM 4 and ≈ 2.4 on KVM 8.
  - **Balance:** a full balance lasts ≈ 3 h at 100 %; an empty one drops the VM to 20 %. A separate abuse throttle applies to long sustained high CPU.
  - **Consequence:** capacity and CU selling use the baseline (§6.1). CPU steal is the only in-VM signal of throttling.
- **Platform:** no nested virtualization. A 30-day money-back window covers the S12 spike. A public API (developers.hostinger.com) and an official Terraform provider exist; S12 checks that they cover VPS creation, power-off and power state.

**Cost per usable GB-month:**

| Option | ≈ ₹ per GB-month | Comment |
|---|---|---|
| Hostinger `storage-1` (₹4,399, ≈ 350 GB pool) | ≈ 12.6 (≈ 15.7 on the 280 GB placeable at 80 %) | also buys 8 vCPU / 32 GB that a storage node mostly leaves idle; 400 GB hard ceiling per VM |
| AWS gp3, Mumbai | ≈ 8.8 | grows to 64 TiB online; the VMs cost extra |
| DigitalOcean Volume, BLR1 | ≈ 9.6 | up to 16 TiB |
| Cloudflare R2 | ≈ 1.44 | the durable tier only (WAL + base backups), never under the commit path (ADR-031) |

Per GB, Hostinger isn't cheaper than managed block volumes. It wins at launch because its RAM and CPU come bundled, and the compute node needs that RAM as its cache.

### 2.3 Can the control plane run on one small VM and the rest on AWS or GCP?

Technically yes: K3s supports agents in other networks (flannel `wireguard-native` with `--node-external-ip`, or its experimental Tailscale integration). It isn't used here, for four reasons:
1. K3s supports embedded etcd only when all servers sit in one location. A split setup leaves a single control-plane VM, or a stretched datastore.
2. Every kubelet ↔ API-server byte leaving AWS or GCP is billed as internet egress (US$0.11–0.12/GB after the free allowance), and the tunnel adds latency. Storage replication across clouds would add a round trip of several milliseconds to every commit.
3. When the link breaks for more than ~50 s, nodes turn `Unknown`: nothing can be changed, restarted or failed over until it heals.
4. Hyperscaler VMs with the RAM a compute node needs cost 3–6× the value-VPS price, which alone breaks the budget (§2.2).

So a cell stays in **one provider and one region**. A whole cell can run on AWS later (§2.2); a presence on another provider or region is a **separate cell** registered with the same control plane (architecture §5.3).

---

## 3. Other services and their free limits

| Service | Free limits that matter | Paid step |
|---|---|---|
| **Supabase** (fallback for the admin DB only, ADR-028; `supabase start` for local development until Step 0.6) | Free: 500 MB database, **no backups or PITR**, pauses after ~7 days of low activity | Pro US$25 ≈ ₹2,400/month; PITR + US$100 ≈ ₹9,600/month (supabase.com/pricing, 2026-10-01) |
| **Cloudflare Free** | DNS (200 records per zone created after 2024-09-01) + DNSSEC; Tunnel (1,000 tunnels, 25 replicas each); Access up to 50 users; WAF Free Managed Ruleset; **1 rate-limiting rule**; Turnstile unlimited; Workers Static Assets requests free and unlimited; Workers 100,000 requests/day incl. Cron Triggers; Email Routing inbound | Pro US$25/month: OWASP/managed rulesets, 3,500 DNS records |
| **Cloudflare R2** | 10 GB-month storage; 1 M Class A ops (PUT, LIST, multipart); 10 M Class B ops; deletes free; **egress free**; a payment method is needed to enable R2 | US$0.015/GB-month, US$4.50 per M Class A, US$0.36 per M Class B |
| **Transactional email** (SMTP; chosen in Step 0.0) | candidates: Resend (3,000/month, 100/day), Brevo (300/day); Postmark (100/month) and Mailgun (100/day) are too small | Cloudflare Email Service needs Workers Paid (US$5 + US$0.35 per 1,000 beyond 3,000); Amazon SES US$0.16 per 1,000 |
| **GitHub Free** (organization) | unlimited private repos; 2,000 Actions minutes/month (Linux x64 and arm64 runners); **ghcr.io container storage and transfer currently free** (GitHub gives a month's notice before any change); a US$0 Actions budget with "stop usage". Not on private repos under Free: branch rules, CODEOWNERS enforcement, deployment environments, secret scanning | GitHub Team when a second engineer joins |
| **Razorpay** | no setup or annual fee; one-time KYC ₹199 | 2 % per domestic card/UPI payment + 0.99 % for Subscriptions, plus 18 % GST on the fee |
| **Docker** | Engine and Desktop need no account (Desktop free under 250 employees and US$10 M revenue); Docker Hub pulls 100 per 6 h anonymous, 200 with a free account | — (third-party images are mirrored into ghcr.io, so the cell never depends on Docker Hub limits) |
| **Domain** | — | `.in` ≈ ₹575 first year, ≈ ₹625 renewal + GST, from a NIXI-accredited registrar (eKYC); `.com` ≈ US$10.46/year at Cloudflare Registrar (**verify**) |

---

## 4. Customer plans

Each plan is a fixed monthly price with **caps**. Inside them the user picks a compute range (min ≤ max); CNPG CPU request = min, limit = max, memory request = min, limit = max, and the compute governor scales memory inside the range (ADR-030). The defaults below are the plan's own range.

| Plan | vCPU min / max (cap) | RAM min / max (cap) | Storage included / cap | Postgres `max_connections` | Pooled clients | PITR window | RPO | Price / month |
|---|---|---|---|---|---|---|---|---|
| `base` | 0.1 / 0.5 | 256 MiB / 512 MiB | 5 GB / 20 GB | 25 | 100 | 7 days | < 1 s typical; ≤ 5 min worst case | **₹299 + GST** |
| `plus` | 0.25 / 1 | 512 MiB / 1 GiB | 10 GB / 50 GB | 50 | 200 | 7 days | < 1 s typical; ≤ 1 min worst case | ₹699 + GST (proposed) |
| `premium` | 0.5 / 1 | 1 GiB / 2 GiB | 20 GB / 200 GB | 100 | 400 | 7 days | < 1 s typical; ≤ 1 min worst case | ₹1,399 + GST (proposed) |

- **CPU:**
  - **Min** is the CPU request: the scheduler reserves it and the kernel's CPU weights protect it under contention. **Max** is the CPU limit: spare node CPU up to that cap.
  - On credit-based VMs (Hostinger) the guarantee is real under full contention only while the node's sustained load stays within its CPU baseline (§6.1).
- **Memory:**
  - **Min** is the request and sizes `shared_buffers` (≈ 25 %).
  - **Max** is the hard cap and sizes `effective_cache_size`.
  - The governor grows the tenant's page cache towards the max when the node has room and reclaims it when it doesn't. `base` keeps 512 MiB as its cap because Postgres, the CNPG instance manager and the buffers leave too little for connections at a 256 MiB hard limit; 256 MiB is safe as a floor because idle tenants measured ≈ 104–127 MiB (§5).
- **Storage:**
  - The plan includes the first size. The database grows online in 5 GB steps up to the cap; the price per extra GB-month is an **owner decision**.
  - The launch maximum for any single database is ≈ 200 GB on a 400 GB storage node (ADR-027). Larger databases need a bigger storage class (`01-…` §7.3).
- **RPO:** < 1 s with the WAL receiver on `control-1` (`01-…` §7.4). The worst case is the plan's `archive_timeout` (300 s / 60 s) if `control-1` and the storage node are lost together.
- **Compute units (CU, ADR-030):** proposed **1 CU = 1 vCPU + 2 GiB, in 0.25 steps**. Usage is metered per second from Step 0.4b. The unit and the price per CU-hour are **owner decisions**; until they are set, the fixed plan prices above apply.
- **Price parity:** the proposed `plus` and `premium` prices keep revenue per guaranteed vCPU about equal across plans (≈ ₹2,605, ₹2,577 and ₹2,629 net per guaranteed vCPU-month, using the per-tenant CPU in §5).
- With GST at 18 %, `base` costs the customer ₹352.82.

---

## 5. Per-tenant footprint

Each tenant runs **two pods** on a compute node: the Postgres pod (Postgres container + the Barman Cloud backup sidecar) and one PgBouncer pod for the pooled hostname. The `-direct` hostname goes straight to Postgres (architecture §8.2). The Postgres container is Burstable (request = range min, limit = range max); the sidecar and PgBouncer keep memory request = limit. The volume lives on a storage node (`tenant-network`), not on the compute node.

| Plan | Postgres container (CPU req / limit, memory req / limit) | Backup sidecar | PgBouncer pod | Tenant total at range min (CPU req / memory / pods / disk) |
|---|---|---|---|---|
| `base` | 100m / 500m, 256 Mi / 512 Mi | 5m / 200m, 160 Mi | 5m / 100m, 48 Mi | **110m / 464 Mi / 2 / 5 GB** (720 Mi at range max) |
| `plus` | 250m / 1000m, 512 Mi / 1,024 Mi | 5m / 200m, 160 Mi | 5m / 100m, 48 Mi | **260m / 720 Mi / 2 / 10 GB** (1,232 Mi at max) |
| `premium` | 500m / 1000m, 1,024 Mi / 2,048 Mi | 5m / 200m, 160 Mi | 5m / 100m, 48 Mi | **510m / 1,232 Mi / 2 / 20 GB** (2,256 Mi at max) |

**Backup sidecar, measured (spike S4, local part, 2026-09-30):**
- **Memory:** during a base backup of a 1 GB database with WAL archived concurrently, the sidecar's peak non-reclaimable memory is 142–145 MiB with one upload worker (`ObjectStore.spec.configuration.data.jobs: 1`) and 177 MiB with the default two, for the same duration. The 200m CPU limit bounds throughput, not parallelism.
- **Sizing:** the planning value of 96 Mi was OOMKilled, so the plans use 160 Mi with `jobs: 1`.
- **Throughput:** at 200m CPU a base backup runs at ≈ 4 MB/s (1 GB in ≈ 4 min; a full 20 GB `premium` disk in ≈ 80 min), well inside the weekly slot.
- **Re-measurement:** S4 on R2 re-measures both. PgBouncer's 48 Mi stays a planning value until S12.

**Step 0.3 measurement (2026-09-30, k3d):** real use at idle ≈ 104–127 MiB per tenant (Postgres + backup sidecar + PgBouncer) against 720 Mi reserved when memory request = limit; 10 `base` tenants were Ready in 79 s. That gap is what Burstable memory with the governor recovers.

---

## 6. Capacity

**Method:**
- **Compute nodes:** tenants per compute node = what is left after the kubelet reservations, the platform's requests and 15 % headroom on CPU and memory, divided by the per-tenant footprint at its range minimum (§5). It is bounded by pod slots (max pods 110) and, on credit-based VMs, by the CPU baseline (§6.1).
- **Storage:** tenants per storage node = the LINSTOR pool's placement limit (80 % of the pool provisioned) divided by each plan's included storage.
- **Admission:** placement uses each node's real Allocatable (architecture §6.3); Step 1.2 writes the measured values into §6.1.

**Platform footprint** (requests on the launch cell, one replica per component). Platform pods have memory limits = requests and no CPU limits.
- **Measured** = sizes measured on the local cell (Step 0.3, `deploy/flux/…` comments).
- **Planning value** = to be measured in Step 0.3b / S12 and confirmed at Step 1.2.

| Component (node; source) | Pods | CPU | Memory |
|---|---|---|---|
| K3s add-on kept: CoreDNS (`control-1`; planning value: Step 1.3's sized copy, limit = request; metrics-server is disabled in `deploy/k3s/server.yaml`) | 1 | 100m | 170 Mi |
| Flux: source, kustomize, helm controllers (`control-1`; measured) | 3 | 30m | 448 Mi |
| cert-manager: controller, webhook, cainjector (`control-1`; measured) | 3 | 20m | 224 Mi |
| CNPG operator + Barman Cloud Plugin (`control-1`; measured) | 2 | 30m | 288 Mi |
| Piraeus operator, LINSTOR controller, LINSTOR CSI controller, affinity controller (`control-1`; planning value) | 4 | 90m | 736 Mi |
| system-upgrade-controller (`control-1`; planning value) | 1 | 10m | 64 Mi |
| VictoriaMetrics single (measured 512 Mi; 256 Mi was OOMKilled in Step 0.3), vmagent, vmalert, Alertmanager, kube-state-metrics (`control-1`; others planning values) | 5 | 70m | 800 Mi |
| VictoriaLogs (`control-1`; planning value) | 1 | 20m | 192 Mi |
| Grafana (`control-1`; planning value) | 1 | 10m | 128 Mi |
| cloudflared (`control-1`; planning value) | 1 | 10m | 64 Mi |
| Admin DB `dbcloud-admin`: Postgres 100m / 512 Mi + backup sidecar 5m / 160 Mi; Pooler 5m / 48 Mi (`control-1`; planning value, the `base` tenant footprint) | 2 | 110m | 720 Mi |
| Supabase Auth (`control-1`; planning value) | 1 | 10m | 96 Mi |
| WAL receiver (`control-1`; planning value for the base process, per-tenant cost measured in Step 0.9b) | 1 | 20m | 128 Mi |
| control-api, worker, tenant-operator, mcp-server, mcp-auth, cluster-agent, River UI (`control-1`; planning value) | 7 | 90m | 640 Mi |
| LINSTOR satellite with `tlshd` + CSI node plugin (every node except storage nodes, which run only the satellite: 25m / 256 Mi; planning value) | 2 per node | 35m | 320 Mi |
| pg-gateway + compute governor (each compute node; planning value; the gateway stays under 32 Mi idle, Step 0.5) | 2 per node | 60m | 96 Mi |
| node-exporter + Fluent Bit (every node; planning value) | 2 per node | 20m | 80 Mi |
| **Total on the launch cell** (`control-1` 37 pods / 675m / 5,098 Mi; `storage-1` 3 / 45m / 336 Mi; `compute-1` 6 / 115m / 496 Mi) | **46** | **835m** | **5,930 Mi** |

- **Per-node arithmetic:**
  - `control-1` = the 14 control rows + satellite and CSI node (2 / 35m / 320 Mi) + node-exporter and Fluent Bit (2 / 20m / 80 Mi).
  - `storage-1` = satellite only (1 / 25m / 256 Mi) + node-exporter and Fluent Bit.
  - `compute-1` = satellite and CSI node + gateway and governor (2 / 60m / 96 Mi) + node-exporter and Fluent Bit.
- **Measured inputs behind the planning values:** the K3s server process measured ≈ 483 MiB idle (Step 0.3), inside the 2 GiB `kube-reserved` on `control-1`. K3s's packaged CoreDNS (70 Mi request, 170 Mi limit) doesn't yet have limit = request; Step 1.3 replaces its packaged manifest with a sized copy.
- **With high availability (deferred):** the stateless platform Deployments and CoreDNS go to 2 replicas on `control-1..3`; that footprint is sized when HA is scheduled.

**Growth nodes** (K3s agents, `04-…` Phase 2): reservations as set in `deploy/k3s/agent.yaml`.
- **Compute node:** runs only the per-node DaemonSets and tenants; a second KVM 4 holds the same tenant count as `compute-1` (§6.1).
- **Storage node:** runs only the satellite and the node DaemonSets; a second KVM 8 adds ≈ 350 GB of pool and a target for online moves.

**Node loss:**
- **Compute node:** with a second compute node, the fencer moves its tenants in ≈ 1–2 min.
- **Storage node:** its tenants restore from R2 + the WAL receiver.
- **`control-1`:** databases keep serving; the platform is restored from the etcd snapshot and R2.

Published RTOs come from the drills (`04-…` Step 1.10).

### 6.1 Launch cell per node role (Hostinger, ADR-029)

All values are **planning values** from the measurements above; S12 and Step 1.2 replace them with measured Allocatable and usage.

| Item | `control-1` (KVM 2) | `storage-1` (KVM 8) | `compute-1` (KVM 4) |
|---|---|---|---|
| VM | 2 vCPU / 8 GB / 100 GB NVMe | 8 vCPU / 32 GB / 400 GB NVMe | 4 vCPU / 16 GB / 200 GB NVMe |
| Sustained CPU at the 30 % baseline | 0.6 vCPU | 2.4 vCPU | 1.2 vCPU |
| `kube-reserved` / `system-reserved` / eviction (as set in `deploy/k3s/server.yaml` and `agent.yaml`) | 500m + 2 GiB / 200m + 512 Mi / 100 Mi | 200m + 512 Mi / 200m + 512 Mi / 100 Mi | 200m + 512 Mi / 200m + 512 Mi / 100 Mi |
| Allocatable (8 GB = 8,192 Mi, etc.) | 1,300m / 5,532 Mi / 110 pods | 7,600m / 31,644 Mi / 110 pods | 3,600m / 15,260 Mi / 110 pods |
| Platform requests (§6 table) | 675m / 5,098 Mi / 37 pods | 45m / 336 Mi / 3 pods | 115m / 496 Mi / 6 pods |
| Left after the platform | 625m / 434 Mi (≈ 8 %): no tenants here | 7,555m / 31,308 Mi: unused, the node is bought for its disk | 3,485m / 14,764 Mi / 104 pod slots |
| Left for tenants after 15 % headroom | — | — | **2,962m / 12,549 Mi / 104 pod slots**; CPU against the baseline: (1,200m − 115m) × 0.85 = **922m** |
| Disk | root ≈ 40 GB; `platform-pool` ≈ 55 GB (admin DB 8 GB, VictoriaMetrics 15 GB, VictoriaLogs 8 GB, WAL receiver spool 10 GB = 41 GB) | root ≈ 40 GB; `tenant-pool` ≈ 350 GB, placement stops at 80 % = **280 GB** | root only (OS, images, `emptyDir`); no tenant data |
| Monthly transfer cap (average rate) | 8 TB (≈ 3.1 MB/s) | 32 TB (≈ 12.3 MB/s) | 16 TB (≈ 6.2 MB/s) |

- **`control-1` is tight.** Its platform requests (675m) exceed its 0.6 vCPU baseline as requests, though idle usage is far lower, and ≈ 434 Mi of memory stays free. S12 measures sustained CPU. Step 1.2 decides whether to lower `kube-reserved` on `control-1` (the 2 GiB was sized when the server also hosted tenants; the K3s server measured ≈ 483 MiB idle) or move `control-1` to KVM 4 (+₹1,200/month renewal).
- **Disk layout:** whether Hostinger's template disk can be repartitioned for `linstorvg` is an S12 check.

**Tenants per compute node (KVM 4)**, at each plan's range minimum (Burstable), with the old guaranteed-memory model for comparison:

| Plan | Footprint at range min (CPU / memory) | CPU, strict baseline rule (922m) | CPU, usage-gated rule (2,962m) | Memory at min (12,549 Mi) | Memory if request = max (guaranteed) | Pods (104) | Disk on `storage-1` (280 GB) | **Tenants: strict / usage-gated** |
|---|---|---|---|---|---|---|---|---|
| `base` | 110m / 464 Mi | 8 | 26 | 27 | 17 | 52 | 56 | **8 / 26** |
| `plus` | 260m / 720 Mi | 3 | 11 | 17 | 10 | 52 | 28 | **3 / 11** |
| `premium` | 510m / 1,232 Mi | 1 | 5 | 10 | 5 | 52 | 14 | **1 / 5** |

**Arithmetic (`base`):**
- **CPU, strict:** 922 / 110 = 8.4 → 8.
- **CPU, usage-gated:** 2,962 / 110 = 26.9 → 26.
- **Memory at min:** 12,549 / 464 = 27.0 → 27.
- **Memory guaranteed:** 12,549 / 720 = 17.4 → 17.
- **Pods:** 104 / 2 = 52.
- **Disk:** 280 / 5 = 56.

`plus` and `premium` use 260m / 720 Mi / 10 GB and 510m / 1,232 Mi / 20 GB the same way. The binding resource is CPU on every plan; memory autoscaling helps only once CPU isn't the limit.

**The two CPU rules (owner decision, ADR-030):**
- **Strict:** Σ range minimums ≤ 85 % of (baseline − platform). The CPU guarantee holds even if every tenant runs flat out, but a KVM 4 holds only 8 `base` tenants.
- **Usage-gated:** Σ range minimums ≤ 85 % of Allocatable, **and** the node's measured sustained CPU (1-hour average, p95 over 7 days) stays ≤ 70 % of its baseline; placement stops on the node when either is exceeded. While sustained load stays under the baseline, Hostinger's credit balance stays positive and the node can always burst to its full vCPUs, so the guarantees hold. The risk is a correlated load spike lasting longer than the ≈ 3 h a full balance covers; CPU steal and sustained-CPU alerts catch it (§9).
- **Until the owner decides,** the capacity controller uses the strict rule (the safe default).

**Storage node (KVM 8):** ≈ 350 GB pool, 280 GB placeable. That is 56 `base`, 28 `plus` or 14 `premium` tenants at their included sizes, or a mix. Growth past included sizes counts against the same 280 GB. One database is capped at ≈ 200 GB at launch (§4).

**Growth triggers:**
- **`compute-2`** joins when `compute-1` reaches 80 % of its tenant capacity under the active CPU rule (usage-gated: 21 `base` tenants; strict: 6) and monthly net covers it.
- **`storage-2`** joins at 70 % of the pool provisioned or when a database needs a move.

High availability is deferred (`04-…` "Deferred (not scheduled): high availability").

---

## 7. Break-even and when money is spent

**Launch cell (ADR-029), `base` tenants only:** cost = the three VMs + domain ≈ ₹70 (R2, email, Cloudflare and GitHub inside free tiers). Each `base` tenant nets ₹299 − ₹12.45 fee = **₹286.55**. Break-even = ⌈cost ÷ 286.55⌉.

| Cell | Cost ₹/month (VMs + domain) | Break-even (`base` tenants) | Full, strict rule (`base`) | Full, usage-gated (`base`) | Net when full ₹/month: strict / usage-gated |
|---|---|---|---|---|---|
| Launch (3 VMs), renewal prices | 7,997 + 70 = 8,067 | 29 | 8 | 26 | −5,775 / −617 |
| Launch (3 VMs), intro prices (24-month prepay) | 4,097 + 70 = 4,167 | 15 | 8 | 26 | −1,875 / +3,283 |
| + `compute-2`, renewal prices | 10,396 + 70 = 10,466 | 37 | 16 | 52 | −5,881 / +4,435 |
| + `compute-2`, intro prices (if bought on the intro term) | 5,196 + 70 = 5,266 | 19 | 16 | 52 | −681 / +9,635 |

Arithmetic, first row: 8,067 ÷ 286.55 = 28.2 → 29. Net when full = tenants × 286.55 − cost: 8 × 286.55 − 8,067 = −5,775; 26 × 286.55 − 8,067 = −617. The other rows work the same way.

**What the numbers say:**
- **Strict rule:** each KVM 4 compute node earns 8 × ₹286.55 = ₹2,292 against its ₹2,399 renewal price. At ₹299 with 0.1 vCPU guaranteed, Hostinger's 30 % baseline never pays for the control and storage nodes. The cell never breaks even on `base`, and `plus` or `premium` don't change that, because revenue per guaranteed vCPU is about equal across plans (§4).
- **Usage-gated rule:**
  - **Intro prices:** `compute-1` alone breaks even at 15 tenants.
  - **Renewal prices:** `compute-1` (26 tenants) falls just short; the cell breaks even at 37 tenants with `compute-2`.
  - **Margin per compute node:** a second compute node earns 26 × ₹286.55 = ₹7,450 for ₹2,399 (≈ 68 % margin before fees).

**Owner decisions needed before Step 1.12** (recorded in ADR-024 / ADR-030):
1. **The CPU rule:** strict or usage-gated.
2. **Prices:** the `base` guaranteed CPU or price, or CU-based pricing (§4).
3. **The Hostinger term:** prepay or monthly.

The capacity controller enforces whichever rule is chosen.

**Spending rules:**
- **Out-of-pocket runway:** before break-even the platform costs the three VMs + domain (≈ ₹8,067 at renewal, ≈ ₹4,167 on the intro term), inside the budget.
- **High availability (deferred):** `control-2`, `control-3`, `storage-2` and the doubled storage add their VM prices. HA is scheduled when monthly net covers them or a customer needs an availability commitment (`04-…` "Deferred (not scheduled): high availability").
- **Growth:** a node is added only when monthly net already covers it, or when the part of the bill above ₹9,000 is covered by revenue. The capacity controller never plans beyond the owner-set `max_nodes`. When capacity is full, sign-ups join a waitlist instead of spending money.

---

## 8. Backup storage formula

- **Retention:** 7 days, weekly base backup, continuous WAL (architecture §14.1).
- **Stored in R2:** ≈ `2 × db + 10.5 × wal_gb_day`. With the planning assumption `wal_gb_day = 2 % of db`, that is **≈ 2.21 × db**. Base backups are uncompressed (R2 multipart rule, spike S4); WAL is zstd-compressed. The admin DB adds a daily base backup of a few hundred MB plus its WAL and nightly dumps: well under 10 GB.
- **Uploads leaving the cell:** ≈ **4.96 × db per month** (4.35 weekly bases + WAL), from the compute nodes. They count against each VM's included transfer (checklist item 6); on AWS they are internet egress beyond the free 100 GB/month.
- **Traffic inside the cell, which may also count on Hostinger** (measured in S12):
  - every tenant write crosses compute → storage once (DRBD);
  - every cache-miss read crosses storage → compute;
  - WAL also streams compute → `control-1` (WAL receiver);
  - base backups read from the storage node before uploading.
- **R2 cost:** `(2.21 × data − 10 GB) × US$0.015`. At a full `storage-1` (280 GB provisioned), R2 holds at most ≈ 2.21 × 280 ≈ 619 GB ≈ US$9.1 ≈ ₹877/month; real data is usually far below provisioned sizes.
- **WAL PUTs per tenant per month:** ≤ 8,640 at `archive_timeout` 300 s (`base`) and ≤ 43,200 at 60 s (`plus`, `premium`). These are upper bounds: an idle database doesn't switch WAL segments. Retention checks run every 6 h to keep LIST operations low.
- **etcd snapshots** (every 6 h, 28 kept, compressed) add well under 1 GB.

## 9. Risks to the budget

| Risk | Effect | Guard |
|---|---|---|
| Chosen provider raises prices or adds location fees | ceiling exceeded | checklist item 7; re-check at every renewal; the design moves to another provider as a new cell |
| CPU credits (Hostinger 30 % baseline) | throttling to 20 % when the balance empties; tenant latency | capacity against the baseline (§6.1); CPU steal and sustained-CPU alerts; placement stops on a hot node |
| Transfer cap with a 10 Mbps throttle (Hostinger) | a throttled node stalls every tenant's storage path | S12 measures internal-traffic accounting; resync and backup shaping; alert at 70 % of each VM's allowance; add a node or move volumes before the cap |
| No private network (Hostinger) | storage and K3s traffic on the public interface | DRBD TLS, WireGuard, K3s TLS; nftables allow-lists by peer IP; recorded deviation (ADR-029) |
| Noisy neighbours / slow disks on value VPS | etcd, DRBD or tenant latency | S12 measurements before launch; weekly fsync/steal alerts; move volumes online or nodes if they degrade |
| Single storage node | its loss stops every volume until restore | WAL receiver (RPO < 1 s), R2 restores, storage-loss drill; `storage-2` and HA when revenue allows |
| Thin pool runs out of space | pool damage | oversubscription 1.0, placement stop at 80 %, alerts at 70/80 % data and metadata |
| 24-month prepay | ≈ ₹98,328 tied up, provider lock for the term | owner decision on the term; 30-day money-back covers S12 |
| R2 growth | bounded by the storage pools (§8) | R2 usage notifications |
| GitHub minutes or ghcr policy change | CI pauses / registry cost | US$0 budget; path filters; images can move to any OCI (Open Container Initiative) registry by changing `registries.yaml` |

## 10. Cost guardrails

- **Fixed and usage-billed costs:** the VM bill is fixed per month. The only usage-billed items are R2, email beyond its free tier and, on an AWS cell, egress beyond 100 GB/month.
- **Alerts:** provider billing alerts (where offered) at ₹6,000 and ₹8,500; R2 usage notifications; GitHub budget US$0; monthly transfer per VM at 70 %.
- **Node limit:** capacity controller `max_nodes`, set by the owner from the weekly net figure.
- **Weekly cost report job:** VM invoice figures are entered once per month; with R2 usage, transfer per VM and revenue from Razorpay it produces "net" and "next node affordable: yes/no" on the admin dashboard.

## Sources (checked 2026-09-29; Hostinger, block volumes and Supabase 2026-10-01 to 2026-10-07)

- K3s requirements, HA, multicloud limits, profiling: https://docs.k3s.io/installation/requirements , https://docs.k3s.io/datastore/ha-embedded , https://docs.k3s.io/networking/distributed-multicloud , https://docs.k3s.io/reference/resource-profiling
- Node heartbeat and eviction defaults: https://kubernetes.io/docs/reference/command-line-tools-reference/kube-controller-manager/ , https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/
- Hostinger: plans https://www.hostinger.com/in/vps-hosting ; upgrades https://www.hostinger.com/support/1583229-how-to-upgrade-a-vps-server-at-hostinger/ ; IP https://www.hostinger.com/support/5139756-how-to-find-your-vps-ip-address-at-hostinger/ ; bandwidth https://www.hostinger.com/support/8789965-what-happens-if-your-vps-bandwidth-resource-limits-are-exceeded/ ; CPU credits https://www.hostinger.com/support/how-cpu-credits-work-on-hostinger-vps/ ; nested virtualization https://support.hostinger.com/en/articles/10429687-is-nested-virtualization-supported-in-hostinger ; API https://developers.hostinger.com/ ; Terraform provider https://github.com/hostinger/terraform-provider-hostinger
- DigitalOcean: https://www.digitalocean.com/pricing/droplets , https://docs.digitalocean.com/products/volumes/details/pricing/ , https://docs.digitalocean.com/products/volumes/details/limits/
- AWS: EBS price file https://b0.p.awsstatic.com/pricing/2.0/meteredUnitMaps/ec2/USD/current/ebs.json ; EC2 Mumbai price file https://b0.p.awsstatic.com/pricing/2.0/meteredUnitMaps/ec2/USD/current/ec2-ondemand-without-sec-sel/Asia%20Pacific%20(Mumbai)/Linux/index.json ; gp3 https://docs.aws.amazon.com/ebs/latest/userguide/general-purpose.html ; public IPv4 charge https://aws.amazon.com/blogs/aws/new-aws-public-ipv4-address-charge-public-ip-insights/ ; 100 GB free egress https://aws.amazon.com/blogs/aws/aws-free-tier-data-transfer-expansion-100-gb-from-regions-and-1-tb-from-amazon-cloudfront-per-month/
- Full provider and engine comparison: `.local/reports/research-storage-split.md` §4–§6
- Supabase pricing and pausing: https://supabase.com/pricing , https://supabase.com/docs/guides/platform/free-project-pausing
- Cloudflare R2 pricing: https://developers.cloudflare.com/r2/pricing/ ; Workers static assets: https://developers.cloudflare.com/workers/static-assets/billing-and-limitations/ ; rate limiting: https://developers.cloudflare.com/waf/rate-limiting-rules/ ; DNS records: https://developers.cloudflare.com/dns/manage-dns-records/ ; Email Service pricing: https://developers.cloudflare.com/email-service/platform/pricing/
- Email free tiers: https://resend.com/docs/knowledge-base/account-quotas-and-limits , https://www.brevo.com/pricing/ , https://aws.amazon.com/ses/pricing/
- GitHub Actions and Packages billing: https://docs.github.com/en/billing/concepts/product-billing/github-actions , https://docs.github.com/en/billing/concepts/product-billing/github-packages
- Razorpay pricing: https://razorpay.com/blog/razorpay-payment-gateway-pricing-explained/
- USD/INR: https://tradingeconomics.com/india/currency
