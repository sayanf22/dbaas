# 03 — Cost Model, Budget and Capacity (Blueprint 4.0)

All prices checked 2026-09-29; the Hostinger prices (§2.2) were added 2026-10-07 and the two-VM layout with its AWS and Azure prices on 2026-10-14. USD prices are converted at **₹96 per US$** (USD/INR on 2026-09-29); re-convert at the day's rate. Prices exclude GST unless stated. Anything marked **verify** is re-checked when it is reached in the build plan.

**Budget rule:** the whole platform costs **≤ ₹9,000/month**, target ₹8,000–8,500 once the admin DB is on Supabase Pro. Anything beyond that (more nodes, a second cell, HA plans) is added only when monthly revenue already covers it. VM costs are budgeted at the **renewal** price, never at an introductory price.

**No provider is chosen yet.** The design runs on any provider that meets the VM checklist in §2. Prices below come from official price lists; Hostinger KVM 2 + KVM 4 is the shortlisted planning example for the two-VM starter cell (§2.2), and AWS and Azure are priced for the same layout. None of them is a selection.

---

## 1. What the platform needs, and what it costs

| Item | What | ₹ / month |
|---|---|---|
| **Starter cell `c1` at launch: 2 Linux VMs** in one region (ADR-025, §6.1) | control VM `ctl-1` 2 vCPU / 8 GB / 100 GB NVMe (planning example: Hostinger KVM 2) + tenant VM `node-1` 4 vCPU / 16 GB / 200 GB NVMe (Hostinger KVM 4) | **≤ 3,600** (₹1,199 + ₹2,399 at renewal; the ₹799 + ₹1,099 intro prices need a 24-month prepay) |
| Growth tenant nodes `node-2`, `node-3`, … (§7) | 4 vCPU / 16 GB / 200 GB NVMe each | + ≤ 2,400 each, added as §7 describes |
| HA control plane from Step 1.13 (§6) | `ctl-2`, `ctl-3` 2 vCPU / 8 GB each | + ≤ 2,400 (2 × ₹1,199), when revenue covers it |
| Admin DB + Auth | Supabase Free at launch → Pro at the §7 trigger | 0 → **2,400** |
| Cloudflare | DNS, Tunnel, Access, WAF, Turnstile, Workers Static Assets | 0 |
| Cloudflare R2 | backups: 10 GB free, then US$0.015/GB-month | ~0–260 per full tenant node (§8) |
| Transactional email | SMTP service with a free tier ≥ 3,000/month (§3) | 0 |
| GitHub Free + ghcr.io | code, CI, private container images | 0 |
| Domain | `.com` or `.in` | ~55–85 |
| **Total** | | **≈ ₹3,670 at launch (starter cell, renewal prices; ≈ ₹1,970 during a prepaid intro term); ≈ ₹6,070 with `node-2`; ≈ ₹8,470 with `node-2` + Supabase Pro** |

**Launch runs on the two-VM starter cell (ADR-025):** `ctl-1` runs K3s's server and the whole platform, `node-1` runs only tenant databases. At renewal prices it costs ₹1,199 + ₹2,399 + domain ≈ ₹70 = **≈ ₹3,668/month** and breaks even at **13 `base` tenants**; on the intro prices (₹799 + ₹1,099 + ₹70 = ₹1,968) at 7 (§6.1, §7). It holds ≈ 17 `base` tenants; at 80 % (≈ 14) the owner decides whether to add `node-2` or waitlist new sign-ups. GST may be added to the VM invoices at checkout (**verify** at Step 1.1).

Inside the ≤ ₹9,000 cap the platform reaches `ctl-1` + `node-1` + `node-2` + Supabase Pro ≈ ₹8,470 (+ R2 ≈ ₹510 with both tenant nodes full of `base` tenants, 2 × ₹256, §8: ≈ ₹8,980), holding ≈ 34 `base` tenants with ≈ ₹10,170/month revenue ex-GST. Anything beyond that (a third tenant node, the HA control plane) is paid from revenue (§7).

Paid from revenue, not the budget: Razorpay takes ≈ **₹12.45** of each ₹352.82 `base` payment (2.99 % + GST on the fee). One-time costs: ₹199 Razorpay KYC and the domain's first year.

Local development costs ₹0: Docker + k3d + `supabase start` (build plan Phase 0).

---

## 2. VM checklist and reference price bands

### 2.1 Checklist (every item must pass before a provider is used; shortlist in build plan Step 0.0, choice in Step 1.1 after spike S2)

| # | Requirement | Why |
|---|---|---|
| 1 | Region in India, or ≤ 70 ms RTT from Mumbai/Bengaluru (for example Singapore) | customer latency |
| 2 | KVM (or equivalent full virtualization), root access, Ubuntu 24.04 LTS image, x86_64 or arm64 | K3s, WireGuard kernel module, LVM |
| 3 | Local SSD/NVMe; on each tenant VM a disk layout with ≥ 150 GB left for LVM after a ≈ 30 GB root (custom partitioning at install, or a second local/attached disk); control VMs ≥ 80 GB SSD for etcd, images and logs | TopoLVM `tenantvg`; etcd fsync latency |
| 4 | **Free private network** between the cell's VMs in the same region: preferred, verified at Step 1.1 (spike S8), not a blocker. Without one, the tenant VM joins over public IPs with nftables allow-lists (6443, 10250, 51820–51821/udp and 5001 only from the other node's IP) and flannel `wireguard-native` encrypts all pod traffic; the K3s API is TLS | node-to-node traffic from day one (`ctl-1` ↔ `node-1`); K3s embedded etcd needs its servers in one location (Step 1.13) |
| 5 | One public IPv4 per VM included; inbound firewall (provider or host) | Postgres entry on port 5432 |
| 6 | Included outbound transfer ≥ 3 TB/month per node, no throttling below that | backup uploads + customer traffic |
| 7 | Flat monthly (or capped hourly) price, no long lock-in required | predictable bill under the cap |
| 8 | Measured in spike S2: fio 4k random-write p99 latency and fsync p99 < 10 ms, CPU steal < 5 % under load | etcd health and noisy neighbours |
| 9 | Reinstall/rebuild and a console for break-glass access | recovery |
| 10 | Nice to have: API + Terraform provider, snapshots, floating IP | automation (the design works without them) |

### 2.2 The two-VM starter cell on Hostinger, AWS and Azure (checked 2026-10-14, all **verify**)

The same layout on each provider: a control VM (2 vCPU / 8 GB) + a tenant VM (4 vCPU / 16 GB), both in one region, 730 hours a month, Linux, on-demand or monthly term. Break-even uses ₹286.55 net per `base` tenant (§7) and the ≈ 17 `base` a 4 vCPU / 16 GB tenant VM holds (§6.1).

| Provider (region) | Control VM | Tenant VM | Disks, IPv4, transfer | ₹/month for the cell | Within ≤ ₹9,000? | Breaks even on one tenant VM? |
|---|---|---|---|---|---|---|
| **Hostinger** (India, Mumbai) | KVM 2: 2 vCPU / 8 GB / 100 GB NVMe, ₹1,199 renewal (₹799 intro) | KVM 4: 4 vCPU / 16 GB / 200 GB NVMe, ₹2,399 renewal (₹1,099 intro) | NVMe, one IPv4 and 8 + 16 TB transfer included | **₹3,598** renewal (₹1,898 intro) | **yes** | **yes: 13 of ≈ 17** at renewal, 7 on intro prices |
| **AWS Lightsail** (`ap-south-1`, Mumbai) | US$44: 2 vCPU / 8 GB / 160 GB SSD | US$84: 4 vCPU / 16 GB / 320 GB SSD | static IPv4 and SSD included; transfer 5 + 6 TB per the bundles, but Mumbai bundles include half (2.5 + 3 TB) | **≈ ₹12,288** (US$128) | no | no: needs (12,288 + 70) ÷ 286.55 = 44 tenants |
| AWS EC2 `t4g` (`ap-south-1`) | `t4g.large` 2 vCPU / 8 GiB, ≈ US$0.0448/h ≈ US$32.70 | `t4g.xlarge` 4 vCPU / 16 GiB, ≈ US$0.0896/h ≈ US$65.41 | plus EBS volumes, US$0.005/h per public IPv4 and internet egress (US$0.11–0.12/GB after the free allowance); burstable CPU credits: sustained load above the baseline is throttled (standard mode) or billed (unlimited mode) | **≈ ₹9,419** for the two instances alone (US$98.11), before disks, IPv4 and egress | no | no |
| **Azure** B-series (Central India) | `B2as_v2` 2 vCPU / 8 GiB, US$0.0492/h = US$35.92 | `B4as_v2` 4 vCPU / 16 GiB, US$0.0984/h = US$71.83 | no local data disk: Premium SSD managed disks P10 128 GiB US$19.71 + P15 256 GiB US$38.01; 2 static IPv4 × US$3.65; egress: first 100 GB/month free, then US$0.12/GB (Microsoft network) or US$0.11/GB (routing preference Internet). Burstable: CPU credits, sustained use above the baseline is throttled | **≈ ₹16,586** (US$172.77) + egress; ≈ ₹14,086 with Standard SSD E10 + E15 (US$10.56 + US$21.12) | no | no |
| Azure D-series (Central India) | `D2as_v5` 2 vCPU / 8 GiB, US$0.0556/h = US$40.59 | `D4as_v5` 4 vCPU / 16 GiB, US$0.111/h = US$81.03 | same disks and IPv4 as above; no CPU credits | **≈ ₹17,917** (US$186.64) + egress | no | no |
| Azure (South India) | `B2as_v2` US$0.106/h, `D2as_v5` US$0.121/h | `B4as_v2` US$0.212/h, `D4as_v5` US$0.243/h | as above | about twice Central India | no | no |

What the table says:
- **Only the value-VPS class fits the budget and breaks even.** On Hostinger the cell costs ≈ ₹3,670 with the domain, breaks even at 13 `base` tenants and nets ≈ ₹1,200/month when full (§7). AWS Lightsail costs ≈ 3.4× as much and AWS EC2 or Azure ≈ 2.6–5× before egress; none of them breaks even on one tenant VM, and each alone is above or close to the whole ≤ ₹9,000 budget.
- **Egress is the hidden cost on AWS EC2 and Azure:** every backup upload to R2 leaves the provider (≈ 4.96 × the stored data per month, §8). A tenant VM holding 85 GB of `base` data uploads ≈ 420 GB/month, ≈ US$35–39 on Azure after the free 100 GB (320 GB × US$0.11–0.12). Lightsail and Hostinger include the transfer.
- **Disks:** Azure `Basv2`/`Dasv5` and EC2 have no local NVMe for data; `tenantvg` and etcd would sit on network-attached managed disks (the VM's own disk, not an extra storage service). That is allowed by ADR-004 only if spike S2's fsync and latency limits pass on them (checklist items 3 and 8).
- **Burstable CPU** (Azure B-series, AWS `t4g`, Lightsail bundles): the cell's steady load (Postgres, etcd, the platform) must stay within the baseline, which spike S2 measures as sustained CPU and steal; otherwise use a non-burstable size.
- **A cell never spans providers** (ADR-002): AWS or Azure become a separate cell, with their own control VM, when revenue pays for it (`04-…` §7). The mainstream-VPS class (for example DigitalOcean, Vultr, Akamai/Linode, Hetzner) and other value-VPS providers (for example OVHcloud, Contabo) were priced only for the earlier VM shapes on 2026-09-29 (mainstream 2 vCPU / 4 GB at ≈ US$20–31, value 8 vCPU / 24 GB at ≈ ₹1,200–2,300); they are re-priced for these two shapes in the Step 0.0 shortlist.
- Prices exclude GST; the US$ rows are on-demand list prices before any reservation or savings plan, converted at ₹96 per US$.

**Hostinger KVM plans (India, Mumbai data center).** Prices as published on hostinger.in on 2026-09-14 (re-read on hostinger.in at Step 1.1, **verify**); taxes may be added at checkout. Hostinger sells only KVM VPS now (OpenVZ renewals ended 2026-03-01). Per-role capacity from §6.1.

| Plan | vCPU / RAM / NVMe / transfer | Intro ₹/month (24-month prepay) | Renewal ₹/month | Fit in the cell (§6.1) |
|---|---|---|---|---|
| KVM 1 | 1 / 4 GB / 50 GB / 4 TB | 599 | 999 | **rejected for both roles:** 1 vCPU is below K3s's 2-core server minimum, and as a tenant VM it holds ≈ 3 `base` |
| **KVM 2** | **2 / 8 GB / 100 GB / 8 TB** | **799** | **1,199** | **control VM `ctl-1`** (and `ctl-2`, `ctl-3` at Step 1.13). As a cheaper first tenant VM it holds ≈ 7 `base` and breaks even only on intro prices (§6.1) |
| **KVM 4** | **4 / 16 GB / 200 GB / 16 TB** | **1,099** | **2,399** | **tenant VM `node-1`** and growth nodes: ≈ 17 `base` |
| KVM 8 | 8 / 32 GB / 400 GB / 32 TB | 2,199 | 4,399 | a larger tenant VM later: ≈ 36 `base` ((32 − 1.1 − 0.33) GiB × 0.85 ≈ 26,600 Mi ÷ 720 = 36.9); with `ctl-1` it breaks even at 20 ((₹1,199 + ₹4,399 + ₹70) ÷ ₹286.55 = 19.8) |

Open points for Step 1.1 / spike S2 / S8 (checklist §2.1): **item 4**, whether Hostinger offers a free private network between VPSs (not found in its documentation on 2026-10-07). The two-VM cell uses node-to-node traffic from day one; without a private network `node-1` joins `ctl-1` over public IPs with nftables allow-lists and WireGuard after S8 measures latency and loss. This is recorded at Step 1.1, not a blocker. **Item 7**, the intro prices need a 24-month prepay; the monthly-term price is checked at order time and the budget uses the renewal price. **Item 8**, sustained CPU, steal and fio/fsync are measured in S2 on both VMs. **Item 3**, how much of `node-1`'s 200 GB disk can become `tenantvg` (§6.1 plans 170 GB).

- Value-VPS trade-offs are shared vCPUs, weaker SLAs and discounts tied to longer terms. That is why checklist items 7–8 and spike S2 decide, not the price alone.
- Some value providers serve India from nearby regions (checklist item 1 applies). Location surcharges and India-specific prices must be read at order time (**verify**).
- Hyperscalers (AWS, Azure, GCP) stay the Phase 5 option for a second cell once revenue pays for them (`04-…` §7).

### 2.3 Can the control VM run at one provider and the tenant VMs on AWS, Azure or GCP?

Technically yes: K3s supports agents in other networks (flannel `wireguard-native` with `--node-external-ip`, or its experimental Tailscale integration). It isn't used here, for four reasons:
1. K3s supports embedded etcd only when all servers share one location. A split setup leaves the control plane at one provider for good, or a stretched datastore once `ctl-2`/`ctl-3` join.
2. Every kubelet ↔ API-server byte and every backup upload leaving AWS, Azure or GCP is billed as internet egress (US$0.11–0.12/GB after the free allowance), and the long path adds latency.
3. When the link breaks for more than ~50 s, nodes turn `Unknown`: nothing can be changed, restarted or failed over until it heals.
4. A tenant VM on a hyperscaler costs ≈ 3.4–4.5× the value-VPS price (Lightsail US$84 ≈ ₹8,064, Azure `B4as_v2` + P15 + IPv4 ≈ US$113.49 ≈ ₹10,895, vs Hostinger KVM 4 at ₹2,399), which alone breaks the budget.

So a cell stays in **one provider and one region**. A future AWS, Azure or GCP presence is a **separate cell** registered with the same control plane (architecture §5.3).

---

## 3. Other services and their free limits

| Service | Free limits that matter | Paid step |
|---|---|---|
| **Supabase Free** (admin DB + Auth) | 500 MB database; 5 GB egress; 50,000 MAU; 2 active projects; **no backups**; pauses after ~7 days of low database activity; Nano compute: 60 DB connections, 200 pooler clients; direct connection IPv6-only, the Supavisor pooler is IPv4 (session 5432, transaction 6543); network restrictions and SSL enforcement available; TOTP MFA, Turnstile CAPTCHA, custom SMTP, ES256 signing keys included | Pro US$25 ≈ ₹2,400/month incl. US$10 compute credit (covers Micro); daily backups (7 days), no pausing, 8 GB disk, 250 GB egress |
| **Cloudflare Free** | DNS (200 records per zone created after 2024-09-01) + DNSSEC; Tunnel (1,000 tunnels, 25 replicas each); Access up to 50 users; WAF Free Managed Ruleset; **1 rate-limiting rule**; Turnstile unlimited; Workers Static Assets requests free and unlimited; Workers 100,000 requests/day incl. Cron Triggers; Email Routing inbound | Pro US$25/month: OWASP/managed rulesets, 3,500 DNS records |
| **Cloudflare R2** | 10 GB-month storage; 1 M Class A ops (PUT, LIST, multipart); 10 M Class B ops; deletes free; **egress free**; a payment method is needed to enable R2 | US$0.015/GB-month, US$4.50 per M Class A, US$0.36 per M Class B |
| **Transactional email** (SMTP; chosen in Step 0.0) | candidates: Resend (3,000/month, 100/day), Brevo (300/day); Postmark (100/month) and Mailgun (100/day) are too small | Cloudflare Email Service needs Workers Paid (US$5 + US$0.35 per 1,000 beyond 3,000); Amazon SES US$0.16 per 1,000 |
| **GitHub Free** (organization) | unlimited private repos; 2,000 Actions minutes/month (Linux x64 and arm64 runners); **ghcr.io container storage and transfer currently free** (GitHub gives a month's notice before any change); a US$0 Actions budget with "stop usage". Not on private repos under Free: branch rules, CODEOWNERS enforcement, deployment environments, secret scanning | GitHub Team when a second engineer joins |
| **Razorpay** | no setup or annual fee; one-time KYC ₹199 | 2 % per domestic card/UPI payment + 0.99 % for Subscriptions, plus 18 % GST on the fee |
| **Docker** | Engine and Desktop need no account (Desktop free under 250 employees and US$10 M revenue); Docker Hub pulls 100 per 6 h anonymous, 200 with a free account | — (third-party images are mirrored into ghcr.io, so the cell never depends on Docker Hub limits) |
| **Domain** | — | `.in` ≈ ₹575 first year, ≈ ₹625 renewal + GST, from a NIXI-accredited registrar (eKYC); `.com` ≈ US$10.46/year at Cloudflare Registrar (**verify**) |

---

## 4. Customer plans

| Plan | vCPU guaranteed / burst | RAM | Storage | Postgres `max_connections` | Pooled clients | PITR window | RPO | Price / month |
|---|---|---|---|---|---|---|---|---|
| `base` | 0.1 / 0.5 | 512 MiB | 5 GB | 25 | 100 | 7 days | ≤ 5 min | **₹299 + GST** |
| `plus` | 0.25 / 1 | 1 GiB | 10 GB | 50 | 200 | 7 days | ≤ 1 min | ₹699 + GST (proposed) |
| `premium` | 0.5 / 1 | 2 GiB | 20 GB | 100 | 400 | 7 days | ≤ 1 min | ₹1,399 + GST (proposed) |

- **Guaranteed** is the CPU request: the scheduler reserves it and the kernel's CPU weights protect it under contention. **Burst** is the CPU limit: spare node CPU up to that cap.
- `base` gets 512 MiB, not 256 MiB: Postgres, the CNPG instance manager and 128 MB of shared buffers leave too little for connections at 256 MiB.
- The proposed `plus` and `premium` prices keep revenue per reserved vCPU about equal across plans (₹2,718, ₹2,688 and ₹2,743 per vCPU including the per-tenant overhead in §5).
- With GST at 18 %, `base` costs the customer ₹352.82.

---

## 5. Per-tenant footprint

Each tenant runs **two pods**: the Postgres pod (Postgres container + the Barman Cloud backup sidecar) and one PgBouncer pod for the pooled hostname. The `-direct` hostname goes straight to Postgres (architecture §8.2). Memory request = limit on every container.

| Plan | Postgres container (CPU req / limit, memory) | Backup sidecar | PgBouncer pod | Tenant total (CPU req / memory / pods / disk) |
|---|---|---|---|---|
| `base` | 100m / 500m, 512 Mi | 5m / 200m, 160 Mi | 5m / 100m, 48 Mi | **110m / 720 Mi / 2 / 5 GB** |
| `plus` | 250m / 1000m, 1,024 Mi | 5m / 200m, 160 Mi | 5m / 100m, 48 Mi | **260m / 1,232 Mi / 2 / 10 GB** |
| `premium` | 500m / 1000m, 2,048 Mi | 5m / 200m, 160 Mi | 5m / 100m, 48 Mi | **510m / 2,256 Mi / 2 / 20 GB** |

**Backup sidecar, measured (spike S4, local part, 2026-09-30):** during a base backup of a 1 GB database with WAL archived concurrently, the sidecar's peak non-reclaimable memory is 142–145 MiB with one upload worker (`ObjectStore.spec.configuration.data.jobs: 1`) and 177 MiB with the default two, for the same duration: the 200m CPU limit bounds throughput, not parallelism. The planning value of 96 Mi was OOMKilled, so the plans use 160 Mi with `jobs: 1`. At 200m CPU a base backup runs at ≈ 4 MB/s (1 GB in ≈ 4 min; a full 20 GB `premium` disk in ≈ 80 min), well inside the weekly slot. S4 on R2 re-measures both. PgBouncer's 48 Mi stays a planning value until S2.

---

## 6. Capacity: reservations, platform footprint, growth and HA

**Node pools** (architecture §5.2): control VMs (`ctl-1`; `ctl-2`, `ctl-3` from Step 1.13) are K3s servers labelled `dbcloud.io/pool=control` and run the platform, never tenants. Tenant VMs (`node-1`, `node-2`, …) are K3s agents labelled `dbcloud.io/pool=tenant` and tainted `dbcloud.io/pool=tenant:NoSchedule`; they run tenant pods, the per-node DaemonSets and one CoreDNS replica.

**Node reservations per role** (kubelet flags, `60-infrastructure.md`):
- Control VM (K3s server with embedded etcd): `kube-reserved` 500m / 2 GiB (the K3s server process: API server, etcd, controllers; K3s profiling measured ~1.6 GB, so 2 GiB is a planning figure re-measured at Step 1.3) + `system-reserved` 200m / 512 Mi + eviction threshold 100 Mi = **700m / 2.6 GiB**.
- Tenant VM (K3s agent): `kube-reserved` 250m / 640 Mi (K3s agent + containerd measured on the local tenant node 2026-10-14: p95 479 MiB / 193m over a busy period, + 25 %) + `system-reserved` 200m / 512 Mi + eviction threshold 100 Mi = **450m / 1,252 Mi ≈ 1.2 GiB**.

**Platform footprint with 2 replicas** (the HA control plane from Step 1.13; requests; platform pods have memory limits = requests and no CPU limits). The starter cell runs 1 replica of each (§6.1). The per-node rows (TopoLVM node plugin and pg-gateway on tenant VMs; node-exporter and Fluent Bit on every VM) were planned for two nodes and scale with the node count:

| Component | Pods | CPU | Memory |
|---|---|---|---|
| K3s add-ons kept: CoreDNS ×2, metrics-server | 3 | 300m | 210 Mi |
| TopoLVM (controller 192 Mi, node ×2 at 128 Mi) | 3 | 65m | 448 Mi |
| cert-manager (controller, webhook, cainjector: the webhook's CA bundle needs it) | 3 | 20m | 224 Mi |
| CNPG operator (192 Mi) + Barman Cloud Plugin (96 Mi) | 2 | 30m | 288 Mi |
| Flux (source 128 Mi, kustomize 192 Mi, helm 128 Mi) | 3 | 30m | 448 Mi |
| system-upgrade-controller | 1 | 10m | 32 Mi |
| VictoriaMetrics single (512 Mi, measured: 256 Mi was OOMKilled), vmagent, vmalert, Alertmanager, kube-state-metrics, node-exporter ×2 | 7 | 70m | 800 Mi |
| VictoriaLogs + Fluent Bit ×2 | 3 | 30m | 160 Mi |
| Grafana | 1 | 10m | 128 Mi |
| cloudflared ×2 | 2 | 20m | 96 Mi |
| pg-gateway (one per node) | 2 | 40m | 64 Mi |
| control-api ×2, worker ×2, tenant-operator ×2, mcp-server ×2, mcp-auth ×2, cluster-agent, River UI | 13 | 220m | 704 Mi |
| **Total** | **43** | **≈ 850m** | **≈ 3.5 GiB** |

The rows for components that already run on the local cell (K3s add-ons, TopoLVM, cert-manager, CNPG + plugin, Flux, VictoriaMetrics single) are the requests measured and set in Step 0.3 (2026-09-30); the others stay planning values until their step. K3s's packaged CoreDNS (70 Mi request, 170 Mi limit) and metrics-server (70 Mi, no limit) don't yet have limit = request; Step 1.3 replaces their packaged manifests with sized copies.

**Growth tenant nodes:** every added tenant VM of the `node-1` shape (4 vCPU / 16 GB / 200 GB) brings the capacity in §6.1 Table B (≈ 17 `base`, ≈ 10 `plus` or ≈ 5 `premium`), its own pg-gateway and its own A record. A larger tenant VM (8 vCPU / 32 GB, Hostinger KVM 8) holds ≈ 36 `base` (§2.2). The real allocatable values are read in Step 1.3 and replace these planning numbers.

**HA control plane (Step 1.13):** `ctl-2` and `ctl-3` (same shape as `ctl-1`) join the embedded etcd. The three control VMs have 3 × (8 − 2.6) = 16.2 GiB and 3 × 1,300m allocatable for the ≈ 3.5 GiB / 850m platform above, so any two of them still hold the whole platform (10.8 GiB, 2,600m) when one is lost. Tenant capacity doesn't change: tenants never run on control VMs.

**Node loss:** tenant VMs run near full, so the other tenant VMs can't absorb a lost node's tenants. A lost tenant VM is replaced (a new VM joins with the same config) and its tenants restore from R2; the published RTO comes from the node-loss drill. Losing `ctl-1` on the starter cell stops management (API, dashboards, provisioning return `503` + `Retry-After`) while running databases keep serving (architecture §1); from Step 1.13 the platform survives the loss of any one control VM.

### 6.1 Starter cell: two VMs (ADR-025)

Two VMs in one region: **`ctl-1`**, the control VM (planning example: Hostinger KVM 2, 2 vCPU / 8 GB / 100 GB NVMe, ₹1,199/month at renewal, §2.2), runs K3s's single server with embedded etcd and the platform with **one replica** per component; **`node-1`**, the tenant VM (Hostinger KVM 4, 4 vCPU / 16 GB / 200 GB NVMe, ₹2,399/month at renewal), runs only the tenants and the per-node pods. 15 % headroom on the tenant VM's CPU and memory. These are planning values: "8 GB" and "16 GB" are taken as GiB, and the real allocatable is read at Step 1.3 and replaces them.

**Table A, control VM `ctl-1`** (holds no tenants):

| Item | CPU | Memory | Pods |
|---|---|---|---|
| VM (2 vCPU / 8 GiB) | 2,000m | 8 GiB | 110 |
| Server reservations: `kube-reserved` 500m / 2 GiB + `system-reserved` 200m / 512 Mi + eviction 100 Mi | 700m | 2.6 GiB | — |
| Platform, 1 replica each: the starter footprint (≈ 720m, ≈ 2.9 GiB, ≈ 35 pods) minus the TopoLVM node plugin (128 Mi) and pg-gateway (64 Mi), ≈ 20m together, which run on `node-1` (node-exporter and Fluent Bit run on both VMs; Fluent Bit on `ctl-1` also ships the K3s audit log) | ≈ 700m | ≈ 2.7 GiB | ≈ 33 |
| **Left** | **≈ 600m** | **≈ 2.7 GiB** (≈ 2.5 GiB if the full 2.9 GiB is kept as a margin) | **≈ 77** |

The spare ≈ 2.7 GiB (≈ 34 % of the VM) absorbs request growth measured at Step 1.3 and the 2 GiB `kube-reserved` estimate (the K3s profiling figure, re-measured at Step 1.3). It is not tenant capacity.

**Table B, tenant VM `node-1`:**

| Item | CPU | Memory | Pods | Disk |
|---|---|---|---|---|
| VM (4 vCPU / 16 GiB / 200 GB) | 4,000m | 16,384 Mi | 110 | 200 GB |
| Agent reservations: `kube-reserved` 250m / 640 Mi + `system-reserved` 200m / 512 Mi + eviction 100 Mi | 450m | 1,252 Mi | — | root ≈ 30 GB |
| Per-node pods: TopoLVM node plugin 128 Mi, pg-gateway 64 Mi, node-exporter 32 Mi, Fluent Bit 48 Mi (≈ 40m together) + one CoreDNS replica 100m / 70 Mi, so name resolution on the tenant VM survives a `ctl-1` outage | 140m | 342 Mi | 5 | — |
| **Left for tenants** after 15 % headroom | (4,000 − 450 − 140) × 0.85 = **2,898m** | (16,384 − 1,252 − 342) × 0.85 = **12,571 Mi** (≈ 12.3 GiB) | (110 − 5) ÷ 2 per tenant = **52 tenants** | 200 − 30 = **170 GB** `tenantvg` |

| Plan | Bound by CPU | memory | pods | disk (170 GB) | **Tenants on `node-1`** |
|---|---|---|---|---|---|
| `base` (110m / 720 Mi / 5 GB) | 26 | 17 | 52 | 34 | **~17** |
| `plus` (260m / 1,232 Mi / 10 GB) | 11 | 10 | 52 | 17 | **~10** |
| `premium` (510m / 2,256 Mi / 20 GB) | 5 | 5 | 52 | 8 | **~5** |

(CPU: 2,941 ÷ 110 = 26.7, ÷ 260 = 11.3, ÷ 510 = 5.8; memory: 12,680 ÷ 720 = 17.6, ÷ 1,232 = 10.3, ÷ 2,256 = 5.6; rounded down.) Memory binds first for `base` and `plus`; for `premium` CPU and memory tie.

- **Why two VMs, not one:** tenant load can't starve the K3s server, etcd or the platform; the tenant VM's memory goes to databases (a single 16 GB VM would also carry the 2.6 GiB server reservation and the 2.9 GiB platform, leaving (16 − 2.6 − 2.9) × 0.85 ≈ 8.9 GiB for tenants instead of `node-1`'s 12.4 GiB); `ctl-1` down leaves the databases serving; a container escape on `node-1` reaches only that node's credentials (ADR-025, architecture §1).
- **A cheaper first tenant VM:** `node-1` as a 2 vCPU / 8 GB VM (Hostinger KVM 2) leaves (8,192 − 1,124 − 342) × 0.85 = 5,717 Mi, ≈ 7 `base` (5,717 ÷ 720 = 7.9; CPU (2,000 − 400 − 140) × 0.85 = 1,241m ÷ 110 = 11; disk 70 GB ÷ 5 = 14). The cell then costs ₹1,199 + ₹1,199 + ₹70 = ₹2,468 at renewal and needs 2,468 ÷ 286.55 = 8.6, so 9 tenants: it never breaks even at renewal. On intro prices (₹799 + ₹799 + ₹70 = ₹1,668, break-even 5.8, so 6) it does, and a full one nets ≈ +₹338. It is an option only for the prepaid term, resized to KVM 4 before the intro price ends.
- **Local measurements (2026-09-30, k3d on 4 CPUs / 8 GB):** with the earlier 96 Mi sidecar, 10 `base` tenants scheduled at 88 % of node memory *requests*; with the measured 160 Mi sidecar the same 8 GB node holds 7 (92 % of requests), which matches this arithmetic. Real use at idle ≈ 104–127 MiB per tenant (Postgres + backup sidecar + PgBouncer). Requests stay equal to limits, so the planning numbers above don't count on that slack.
- **Growth:** at 80 % of `node-1` (0.8 × 17 = 13.6, so ≈ 14 `base` tenants) the owner chooses: `node-2` joins as a second tenant agent (+ ₹2,399/month, the cell then costs ≈ ₹6,067 and holds ≈ 34 `base`, inside the ≤ ₹9,000 budget), or new sign-ups are waitlisted until growth is funded (`max_nodes`, §7). A full `node-1` nets ≈ ₹1,203/month (§7), less than `node-2` costs, so the step is an owner decision paid partly from the budget until `node-2` fills.

---

## 7. Break-even and when money is spent

**Starter cell (ADR-025), `base` tenants, Supabase Free:** cost ≈ ₹3,668/month at renewal prices (`ctl-1` KVM 2 ₹1,199 + `node-1` KVM 4 ₹2,399 + domain ≈ ₹70; R2, email, Cloudflare and GitHub inside free tiers; GST on the VM invoices, if charged, comes on top). Each `base` tenant nets ₹299 − ₹12.45 fee = ₹286.55. Break-even = 3,668 ÷ 286.55 = 12.8, so **13 tenants**.

| `base` tenants | Revenue ex-GST | Fees | Cost | Net ₹/month |
|---|---|---|---|---|
| 7 | 2,093 | 87 | ~3,668 | −1,662 |
| 9 | 2,691 | 112 | ~3,668 | −1,089 |
| **13 (break-even)** | 3,887 | 162 | ~3,668 | **+57** |
| 14 (grow trigger, 80 %) | 4,186 | 174 | ~3,668 | +344 |
| 17 (`node-1` full) | 5,083 | 212 | ~3,668 | +1,203 |

On the intro prices (₹799 + ₹1,099 + ₹70 = ₹1,968/month while the prepaid 24-month term lasts; (₹799 + ₹1,099) × 24 = ₹45,552 paid up front) break-even is 1,968 ÷ 286.55 = 6.9, so **7 tenants** (+38/month), and a full `node-1` nets ≈ +₹2,903. Plans are made on the renewal rows above.

**Growth stages (renewal prices, `base` tenants, R2 excluded):**

| Stage | VMs | `base` capacity | Cost ₹/month | Break-even | Net when full |
|---|---|---|---|---|---|
| Launch | `ctl-1` + `node-1` | 17 | 3,668 | 13 | +1,203 |
| + `node-2` | `ctl-1` + `node-1`, `node-2` | 34 | 6,067 | 22 | +3,676 |
| + Supabase Pro (trigger below) | same | 34 | 8,467 | 30 | +1,276 |
| + `node-3` | `ctl-1` + `node-1`…`node-3` | 51 | 10,866 | 38 | +3,748 |
| + HA control plane (`ctl-2`, `ctl-3`, Step 1.13) | 3 control + 3 tenant VMs | 51 | 13,264 | 47 | +1,350 |

(Cost = 1,199 per control VM + 2,399 per tenant VM + 70 domain + 2,400 Supabase Pro; break-even = cost ÷ 286.55 rounded up; net when full = capacity × 286.55 − cost.)

- **Out-of-pocket runway:** at launch the platform costs ≈ ₹3,670/month and is net positive from 13 tenants. Stages up to Supabase Pro stay within the ≤ ₹9,000 cap. A new tenant node starts empty, so each growth step is a dip the budget absorbs until it fills: adding `node-2` at 14 tenants moves the month from +₹344 to −₹2,055 (14 × 286.55 − 6,067) until 22 tenants; this is why it is an owner decision, not an automatic trigger.
- **HA control plane:** it adds ≈ ₹2,398/month and no tenant capacity. With `node-1` alone (cost ≈ ₹6,066) even a full `node-1` (≈ ₹4,871 net revenue) can't pay for it. It is bought when revenue covers it (in this table, from three tenant nodes) or when a customer pays for an availability commitment.
- **Supabase Pro trigger:** ~30 paying tenants, or admin DB > 300 MB, or Supabase egress > 3.5 GB/month, or any Supabase restriction notice, whichever comes first.
- **Growth:** beyond the cap a node is added only when monthly revenue already covers it. The capacity controller never plans beyond the owner-set `max_nodes`; when capacity is full, sign-ups join a waitlist instead of spending money.
- Paid-capacity margin on an added 4 vCPU / 16 GB tenant node (≈ 17 `base` at ₹2,399): (5,083 − 2,399) ÷ 5,083 ≈ 53 % before payment fees, far better than hyperscaler pricing because the node is fixed-price; it pays for itself at 9 `base` tenants (2,399 ÷ 286.55 = 8.4).

---

## 8. Backup storage formula

- Retention 7 days, weekly base backup, continuous WAL (architecture §14.1).
- Stored in R2 ≈ `2 × db + 10.5 × wal_gb_day`. With the planning assumption `wal_gb_day = 2 % of db`, that is **≈ 2.21 × db**. Base backups are uncompressed (R2 multipart rule, spike S4); WAL is zstd-compressed.
- Uploads leaving the cell ≈ **4.96 × db per month** (4.35 weekly bases + WAL); they count against the VMs' included transfer (checklist item 6).
- R2 cost = `(2.21 × data − 10 GB) × US$0.015`. A tenant VM full of `base` tenants (17 × 5 GB = 85 GB) stores ≈ 188 GB in R2: (187.9 − 10) × US$0.015 ≈ US$2.67 ≈ ₹256/month. Memory caps the data a tenant VM can hold at ≈ 112 GB with any plan mix (12,680 Mi of `premium` tenants at 20 GB per 2,256 Mi), ≈ ₹340/month in R2.
- WAL PUTs per tenant per month ≤ 8,640 at `archive_timeout` 300 s (`base`) and ≤ 43,200 at 60 s (`plus`, `premium`). Upper bounds: an idle database doesn't switch WAL segments. Retention checks run every 6 h to keep LIST operations low.
- etcd snapshots (every 6 h, 28 kept, compressed) add well under 1 GB.

## 9. Risks to the budget

| Risk | Effect | Guard |
|---|---|---|
| Chosen provider raises prices or adds location fees | ceiling exceeded | checklist item 7; re-check at every renewal; the design moves to another provider as a new cell |
| Intro prices end (Hostinger KVM 2 ₹799 → ₹1,199, KVM 4 ₹1,099 → ₹2,399 after the prepaid term) | starter cost almost doubles (₹1,968 → ₹3,668) | budget and break-even use the renewal prices only (§1, §7) |
| Launch provider has no private network (§2.2) | `ctl-1` ↔ `node-1` traffic crosses the public network | S8 at Step 1.1; public-IP join with nftables allow-lists limited to the other node's IP, WireGuard for pod traffic, TLS for the K3s API; or another shortlisted provider |
| Noisy neighbours / slow disks on value VPS | etcd or tenant latency | spike S2 measurements on both VMs before launch; weekly fsync/steal alerts; move nodes if they degrade |
| R2 growth | ≈ ₹260–340 per full tenant node | bounded by tenant-node memory and disks (§8); R2 usage notifications |
| Transfer overage | provider-specific | checklist item 6; monthly egress alert at 70 % of the included transfer |
| Supabase Free limit or pause | admin writes stop | usage alerts at 60 %; Pro trigger; static stability keeps databases serving |
| GitHub minutes or ghcr policy change | CI pauses / registry cost | US$0 budget; path filters; images can move to any OCI registry by changing `registries.yaml` |

## 10. Cost guardrails

- The VM bill is fixed per month; the only usage-billed items are R2, email beyond its free tier and Supabase beyond Pro's inclusions.
- Provider billing alerts (where offered) at ₹6,000 and ₹8,500; R2 usage notifications; GitHub budget US$0.
- Capacity controller `max_nodes`, set by the owner from the weekly net figure.
- Weekly cost report job: VM invoice figures entered once per month, R2 usage, Supabase size/egress, revenue from Razorpay → "net" and "next node affordable: yes/no" on the admin dashboard.

## Sources (checked 2026-09-29)

- K3s requirements, HA, multicloud limits, profiling: https://docs.k3s.io/installation/requirements , https://docs.k3s.io/datastore/ha-embedded , https://docs.k3s.io/networking/distributed-multicloud , https://docs.k3s.io/reference/resource-profiling
- Node heartbeat and eviction defaults: https://kubernetes.io/docs/reference/command-line-tools-reference/kube-controller-manager/ , https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/
- AWS EC2 Mumbai prices and data transfer: https://aws.amazon.com/ec2/pricing/on-demand/ ; GCP network pricing: https://cloud.google.com/vpc/network-pricing
- Added 2026-10-14 (two-VM layout, §2.2): AWS Lightsail bundles and the Mumbai half-transfer rule: https://aws.amazon.com/lightsail/pricing/ ; EC2 `t4g.xlarge` in `ap-south-1` at US$0.0896/h from third-party price indexes (https://instances.cloudchipr.com/aws/ec2/t4g.xlarge , https://www.devzero.io/instances/aws/t4g.xlarge; `t4g.large` taken as half), to be confirmed on the EC2 page; burstable credits: https://aws.amazon.com/ec2/instance-types/t4/ . Azure Linux pay-as-you-go prices for `B2as_v2`, `B4as_v2`, `D2as_v5`, `D4as_v5` (Central India and South India), Premium/Standard SSD managed disks, Standard static public IPv4 and internet egress, from Microsoft's retail prices API (https://prices.azure.com/api/retail/prices) and the pricing pages https://azure.microsoft.com/en-in/pricing/details/virtual-machines/linux/ , https://azure.microsoft.com/en-in/pricing/details/bandwidth/ ; cross-checked with https://www.azurespeed.com/AzureVmPricing/Standard_B2as_v2 and https://www.azurespeed.com/AzureVmPricing/Standard_D2as_v5 (same US$0.0492/h and US$0.0556/h).
- Hostinger India VPS prices (checked 2026-10-07; hostinger.in page as read 2026-09-14, re-verify at Step 1.1): https://zoutons.com/news/hostinger-vps-hosting-price-india-2026 , https://www.hostinger.com/in/vps-hosting ; Mumbai location: https://www.hostinger.com/support/1583267-where-are-hostinger-servers-located/ ; OpenVZ discontinued: https://www.hostinger.com/support/10469992-how-to-migrate-your-vps-from-openvz-to-kvm-at-hostinger/ ; VPS help center (no private-network article found): https://www.hostinger.com/support/vps/
- VPS price lists: https://www.digitalocean.com/pricing/droplets , https://api.vultr.com/v2/plans , https://api.linode.com/v4/linode/types , https://www.ovhcloud.com/en-in/vps/vps-india/ , https://contabo.com/en/vps/ , https://docs.hetzner.com/general/infrastructure-and-availability/price-adjustment/
- Supabase pricing, pausing, compute, connections: https://supabase.com/pricing , https://supabase.com/docs/guides/platform/free-project-pausing , https://supabase.com/docs/guides/platform/compute-and-disk , https://supabase.com/docs/guides/database/connecting-to-postgres
- Cloudflare R2 pricing: https://developers.cloudflare.com/r2/pricing/ ; Workers static assets: https://developers.cloudflare.com/workers/static-assets/billing-and-limitations/ ; rate limiting: https://developers.cloudflare.com/waf/rate-limiting-rules/ ; DNS records: https://developers.cloudflare.com/dns/manage-dns-records/ ; Email Service pricing: https://developers.cloudflare.com/email-service/platform/pricing/
- Email free tiers: https://resend.com/docs/knowledge-base/account-quotas-and-limits , https://www.brevo.com/pricing/ , https://aws.amazon.com/ses/pricing/
- GitHub Actions and Packages billing: https://docs.github.com/en/billing/concepts/product-billing/github-actions , https://docs.github.com/en/billing/concepts/product-billing/github-packages
- Razorpay pricing: https://razorpay.com/blog/razorpay-payment-gateway-pricing-explained/
- USD/INR: https://tradingeconomics.com/india/currency
