# 03 — Cost Model, Budget and Capacity (Blueprint 4.0)

All prices checked 2026-09-29. USD prices are converted at **₹96 per US$** (USD/INR on 2026-09-29); re-convert at the day's rate. Prices exclude GST unless stated. Anything marked **verify** is re-checked when it is reached in the build plan.

**Budget rule:** the whole platform costs **≤ ₹9,000/month**, target ₹8,000–8,500 once the admin DB is on Supabase Pro. Anything beyond that (more nodes, a second cell, HA plans) is added only when monthly revenue already covers it.

**No provider is chosen yet.** The design runs on any provider that meets the VM checklist in §2. Prices below are reference bands from official price lists, not a selection.

---

## 1. What the platform needs, and what it costs

| Item | What | ₹ / month |
|---|---|---|
| **Cell `c1`: 3 Linux VMs** in one region (§2) | `cp-1` 2 vCPU / 4 GB; `node-1`, `node-2` 8 vCPU / 24 GB / 200 GB NVMe | **≤ 5,600** (ceiling: cp-1 ≤ 1,000, each node ≤ 2,300) |
| Admin DB + Auth | Supabase Free at launch → Pro at the §7 trigger | 0 → **2,400** |
| Cloudflare | DNS, Tunnel, Access, WAF, Turnstile, Workers Static Assets | 0 |
| Cloudflare R2 | backups: 10 GB free, then US$0.015/GB-month | ~0–200 typical (≤ 940 with every tenant disk full) |
| Transactional email | SMTP service with a free tier ≥ 3,000/month (§3) | 0 |
| GitHub Free + ghcr.io | code, CI, private container images | 0 |
| Domain | `.com` or `.in` | ~55–85 |
| **Total** | | **≈ ₹5,700–5,900 at launch; ≈ ₹8,100–8,300 with Supabase Pro** |

**Launch runs on the starter cell (ADR-025):** only `node-1` (≤ ₹2,300), ≈ ₹2,370/month in total, break-even at 9 `base` tenants (§6.1, §7). The table above is the reference cell it grows into at ~18 tenants.

Worst case (every tenant disk full on both nodes, i.e. ~47 paying tenants) is ≈ ₹9,025 with Supabase Pro, and by then revenue is ≈ ₹14,050/month.

Paid from revenue, not the budget: Razorpay takes ≈ **₹12.45** of each ₹352.82 `base` payment (2.99 % + GST on the fee). One-time costs: ₹199 Razorpay KYC and the domain's first year.

Local development costs ₹0: Docker + k3d + `supabase start` (build plan Phase 0).

---

## 2. VM checklist and reference price bands

### 2.1 Checklist (every item must pass before a provider is used; shortlist in build plan Step 0.0, choice in Step 1.1 after spike S2)

| # | Requirement | Why |
|---|---|---|
| 1 | Region in India, or ≤ 70 ms RTT from Mumbai/Bengaluru (for example Singapore) | customer latency |
| 2 | KVM (or equivalent full virtualization), root access, Ubuntu 24.04 LTS image, x86_64 or arm64 | K3s, WireGuard kernel module, LVM |
| 3 | Local SSD/NVMe; a disk layout with ≥ 150 GB left for LVM on each node (custom partitioning at install, or a second local/attached disk) | TopoLVM `tenantvg` |
| 4 | **Free private network** between the three VMs in the same region | K3s embedded etcd needs servers on a private network in one location |
| 5 | One public IPv4 per VM included; inbound firewall (provider or host) | Postgres entry on port 5432 |
| 6 | Included outbound transfer ≥ 3 TB/month per node, no throttling below that | backup uploads + customer traffic |
| 7 | Flat monthly (or capped hourly) price, no long lock-in required | predictable bill under the cap |
| 8 | Measured in spike S2: fio 4k random-write p99 latency and fsync p99 < 10 ms, CPU steal < 5 % under load | etcd health and noisy neighbours |
| 9 | Reinstall/rebuild and a console for break-glass access | recovery |
| 10 | Nice to have: API + Terraform provider, snapshots, floating IP | automation (the design works without them) |

### 2.2 Reference price bands (official price lists, 2026-09-29)

| Provider class | 2 vCPU / 4 GB | 8 vCPU / 24–32 GB | Reference cell (1 × small + 2 × large) | Fits the cap? |
|---|---|---|---|---|
| **Value VPS** (for example OVHcloud VPS, Contabo Cloud VPS) | ≈ ₹420–600 | ≈ ₹1,200–2,300 (8/24, 200–300 GB NVMe/SSD) | **≈ ₹2,800–5,600** | **yes** |
| Mainstream VPS (for example DigitalOcean, Vultr, Akamai/Linode, Hetzner) | ≈ ₹1,900–3,000 (US$20–31) | ≈ ₹15,400–29,500 (US$160–307, 8/32) | ≈ ₹33,000+ | no |
| Hyperscalers (for example AWS `ap-south-1`, GCP `asia-south1`) | ≈ ₹1,600–3,100 (US$16–33) + disk | ≈ ₹16,300–28,300 (US$170–295) + disk + egress (US$0.11–0.12/GB) | ≈ ₹35,000+ | no |

- Only the value-VPS class meets the cap with a useful cell. Its trade-offs are shared vCPUs, weaker SLAs, and discounts tied to longer terms. That is why checklist items 7–8 and spike S2 decide, not the price alone.
- Some value providers serve India from nearby regions (checklist item 1 applies). Location surcharges and India-specific prices must be read at order time (**verify**).
- Hyperscalers stay the Phase 5 option (a second cell on AWS or GCP) once revenue pays for them (`04-…` §7).

### 2.3 Can the control plane run on one small VM and the rest on AWS or GCP?

Technically yes: K3s supports agents in other networks (flannel `wireguard-native` with `--node-external-ip`, or its experimental Tailscale integration). It isn't used here, for four reasons:
1. K3s supports embedded etcd only when all servers share a private network in one location. A split setup leaves a single control-plane VM, or a stretched datastore.
2. Every kubelet ↔ API-server byte leaving AWS or GCP is billed as internet egress (US$0.11–0.12/GB after the free allowance), and the tunnel adds latency.
3. When the link breaks for more than ~50 s, nodes turn `Unknown`: nothing can be changed, restarted or failed over until it heals.
4. The worker VMs on a hyperscaler cost 5–6× the value-VPS price, which alone breaks the budget.

So a cell stays in **one provider and one region**. A future AWS or GCP presence is a **separate cell** registered with the same control plane (architecture §5.3).

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

## 6. Capacity of the reference cell

**Node reservations** (each node is also a K3s server with embedded etcd): `kube-reserved` 500m / 2 GiB (the K3s server process: API server, etcd, controllers; K3s profiling measured ~1.6 GB) + `system-reserved` 200m / 512 Mi + eviction threshold 100 Mi. `cp-1` carries the taint `CriticalAddonsOnly=true:NoExecute`, so it runs only the control plane and holds no tenants.

**Platform footprint** (requests, spread over `node-1` and `node-2`; platform pods have memory limits = requests and no CPU limits):

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

**Left for tenants on the two nodes** after reservations, the platform and 15 % headroom: CPU ≈ **11,690m**, memory ≈ **33.4 GiB**, pod slots ≈ **90** tenants (max pods 110 per node), disk ≈ **300 GB** of `tenantvg` (200 GB NVMe − ~50 GB root and etcd per node).

| Plan | Bound by CPU | memory | pods | disk | **Tenants per reference cell** | Revenue when full (ex-GST) |
|---|---|---|---|---|---|---|
| `base` | 106 | 47 | 90 | 60 | **~47** | ₹14,053 |
| `plus` | 45 | 27 | 90 | 30 | **~27** | ₹18,873 |
| `premium` | 22 | 15 | 90 | 15 | **~15** | ₹20,985 |

Memory binds first for every plan, so a node with more RAM (8 vCPU / 32 GB, 300 GB) raises capacity to ~66 `base` / ~39 `plus` / ~21 `premium` if the chosen provider offers it within the ceiling. The real allocatable values are read in Step 1.3 and replace these planning numbers.

**Node loss:** the two nodes run near full, so the survivor can't absorb the other node's tenants. A lost node is replaced (a new VM joins with the same config) and its tenants restore from R2; the published RTO comes from the node-loss drill. The platform itself survives the loss of any one VM (architecture §1).

### 6.1 Starter cell: one VM (ADR-025)

One 8 vCPU / 24 GB / 200 GB NVMe VM (≤ ₹2,300/month) runs K3s as a single server with embedded etcd, the platform with **one replica** per component (≈ 720m CPU, ≈ 2.9 GiB memory, ≈ 35 pods) and the tenants. Reservations as above (2.6 GiB); 15 % headroom on CPU and memory.

| Plan | Bound by CPU | memory | pods | disk (150 GB `tenantvg`) | **Tenants on the starter VM** |
|---|---|---|---|---|---|
| `base` | 50 | 22 | 37 | 30 | **~22** |
| `plus` | 21 | 13 | 37 | 15 | **~13** |
| `premium` | 10 | 7 | 37 | 7 | **~7** |

- **Minimum VM** for exactly 10 `base` tenants: 4–6 vCPU / 16 GB (memory: 10 × 720 Mi ÷ 0.85 ≈ 8.3 GiB + 2.6 + 2.9 GiB ≈ 13.8 GiB, so 12 GB no longer fits). It leaves little room to grow, so the 24 GB VM is the launch choice.
- **Local measurements (2026-09-30, k3d on 4 CPUs / 8 GB):** with the earlier 96 Mi sidecar, 10 `base` tenants scheduled at 88 % of node memory *requests*; with the measured 160 Mi sidecar the same 8 GB node holds 7 (92 % of requests), which matches this table's arithmetic. Real use at idle ≈ 104–127 MiB per tenant (Postgres + backup sidecar + PgBouncer). Requests stay equal to limits, so the planning numbers above don't count on that slack.
- **Growth:** at ~18 `base` tenants (80 %), `cp-1` and `node-2` join and the cell becomes the reference cell of §6 (Step 1.13).

---

## 7. Break-even and when money is spent

**Starter cell (ADR-025), `base` tenants, Supabase Free:** cost ≈ ₹2,370/month (VM ≤ ₹2,300 + domain ≈ ₹70; R2, email, Cloudflare and GitHub inside free tiers). Each `base` tenant nets ₹299 − ₹12.45 fee = ₹286.55.

| `base` tenants | Revenue ex-GST | Fees | Cost | Net ₹/month |
|---|---|---|---|---|
| 5 | 1,495 | 62 | ~2,370 | −937 |
| **9 (break-even)** | 2,691 | 112 | ~2,370 | **+209** |
| 10 | 2,990 | 125 | ~2,370 | +495 |
| 18 (grow trigger) | 5,382 | 224 | ~2,370 | +2,788 |
| 22 (starter full) | 6,578 | 274 | ~2,370 | +3,934 |

**Reference cell (after Step 1.13):**

| Stage | `base` tenants | Revenue ₹/month | Cost ₹/month | Net ₹/month |
|---|---|---|---|---|
| Launch, Supabase Free | 1–19 | 299–5,681 | ~5,700–6,000 | −5,400 → −300 |
| Break-even | ~20 | ~5,980 | ~6,000 | ≈ 0 |
| Supabase Pro | from ~30 (trigger below) | 8,970+ | ~8,400–8,600 | positive |
| Cell full | ~47 | ~14,050 | ~8,900 | ≈ +5,150 |
| 3rd node (8 vCPU / 24 GB) | 47+ | | + ≤ 2,300 | added only when net ≥ its cost |

- **Out-of-pocket runway:** until ~20 tenants the platform costs at most ≈ ₹6,000/month, inside the budget.
- **Supabase Pro trigger:** ~30 paying tenants, or admin DB > 300 MB, or Supabase egress > 3.5 GB/month, or any Supabase restriction notice, whichever comes first.
- **Growth:** a node is added only when monthly net already covers it. The capacity controller never plans beyond the owner-set `max_nodes`; when capacity is full, sign-ups join a waitlist instead of spending money.
- Paid-capacity margins on an added 8 vCPU / 24 GB node (~25 `base` per node at ₹2,300): ≈ 69 % before payment fees, far better than hyperscaler pricing because the node is fixed-price.

---

## 8. Backup storage formula

- Retention 7 days, weekly base backup, continuous WAL (architecture §14.1).
- Stored in R2 ≈ `2 × db + 10.5 × wal_gb_day`. With the planning assumption `wal_gb_day = 2 % of db`, that is **≈ 2.21 × db**. Base backups are uncompressed (R2 multipart rule, spike S4); WAL is zstd-compressed.
- Uploads leaving the cell ≈ **4.96 × db per month** (4.35 weekly bases + WAL); they count against the VMs' included transfer (checklist item 6).
- R2 cost = `(2.21 × data − 10 GB) × US$0.015`. At full disks the reference cell can't exceed ~663 GB in R2 (≈ ₹940/month).
- WAL PUTs per tenant per month ≤ 8,640 at `archive_timeout` 300 s (`base`) and ≤ 43,200 at 60 s (`plus`, `premium`). Upper bounds: an idle database doesn't switch WAL segments. Retention checks run every 6 h to keep LIST operations low.
- etcd snapshots (every 6 h, 28 kept, compressed) add well under 1 GB.

## 9. Risks to the budget

| Risk | Effect | Guard |
|---|---|---|
| Chosen provider raises prices or adds location fees | ceiling exceeded | checklist item 7; re-check at every renewal; the design moves to another provider as a new cell |
| Noisy neighbours / slow disks on value VPS | etcd or tenant latency | spike S2 measurements before launch; weekly fsync/steal alerts; move nodes if they degrade |
| R2 growth | ≤ ₹940 per full cell | bounded by node disks; R2 usage notifications |
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
- VPS price lists: https://www.digitalocean.com/pricing/droplets , https://api.vultr.com/v2/plans , https://api.linode.com/v4/linode/types , https://www.ovhcloud.com/en-in/vps/vps-india/ , https://contabo.com/en/vps/ , https://docs.hetzner.com/general/infrastructure-and-availability/price-adjustment/
- Supabase pricing, pausing, compute, connections: https://supabase.com/pricing , https://supabase.com/docs/guides/platform/free-project-pausing , https://supabase.com/docs/guides/platform/compute-and-disk , https://supabase.com/docs/guides/database/connecting-to-postgres
- Cloudflare R2 pricing: https://developers.cloudflare.com/r2/pricing/ ; Workers static assets: https://developers.cloudflare.com/workers/static-assets/billing-and-limitations/ ; rate limiting: https://developers.cloudflare.com/waf/rate-limiting-rules/ ; DNS records: https://developers.cloudflare.com/dns/manage-dns-records/ ; Email Service pricing: https://developers.cloudflare.com/email-service/platform/pricing/
- Email free tiers: https://resend.com/docs/knowledge-base/account-quotas-and-limits , https://www.brevo.com/pricing/ , https://aws.amazon.com/ses/pricing/
- GitHub Actions and Packages billing: https://docs.github.com/en/billing/concepts/product-billing/github-actions , https://docs.github.com/en/billing/concepts/product-billing/github-packages
- Razorpay pricing: https://razorpay.com/blog/razorpay-payment-gateway-pricing-explained/
- USD/INR: https://tradingeconomics.com/india/currency
