# 02 — Technology Stack (Blueprint 4.0)

Research date **2026-09-29**. This file lists only what we use. Pin exact versions and image digests in code; Renovate proposes upgrades; an upgrade merges only after its tests and gates pass. "Pin at install" means: take the newest stable release on the day the component is first added, record it in `tools.versions` / Helm values, and let Renovate track it.

Selection rules:
1. Production-proven, actively maintained, clear security process.
2. Official **amd64 and arm64** builds, because the VM provider isn't chosen yet and most value-VPS providers are x86_64.
3. Small footprint: a cell is three VMs (`03-cost-model.md` §6), so every pod earns its place.
4. Provider-neutral: runs on any Linux VM that passes the checklist in `03-…` §2.1; no managed service of one VM provider.
5. Free at launch, or paid only from revenue; total platform cost ≤ ₹9,000/month.
6. The same component still works at 100× scale, so growth means adding capacity, not replacing parts.
7. We build the DBaaS ourselves; we reuse focused building blocks, not a packaged DBaaS.

---

## 1. Language split

| Language | Where | Why |
|---|---|---|
| **Rust** (stable toolchain, edition 2024) | `pg-gateway` only — the Postgres TLS/SNI router every customer connection passes through | No GC pauses on the data path, low and predictable memory per connection, memory safety for an internet-facing TLS terminator, byte-level control of the Postgres handshake |
| **Go 1.27.x** | control-api, worker, cluster-agent, tenant-operator, mcp-server, mcp-auth, CLI, provider adapters | Kubernetes, CNPG, River and MCP SDK are Go-native; one toolchain for the whole control plane; small static binaries |
| **TypeScript** (Next.js 16, Node 24 LTS for builds) | public site, customer and admin dashboards | Static exports served free from Cloudflare |
| **SQL** | goose migrations, sqlc queries | Admin DB schema |
| **HCL** | Terraform | Cloudflare resources, and the VM provider's resources once it is chosen |
| **YAML** | Flux, Helm values, Kubernetes manifests, K3s config, cloud-init | Cell configuration |
| **Dockerfile** | one per image | Multi-stage builds to distroless images |
| **Bash** | `hack/` and `deploy/node-bootstrap/` scripts under 100 lines each | Glue only |

---

## 2. Infrastructure (per cell)

| Layer | Choice | Version | Why |
|---|---|---|---|
| VMs | Any provider passing `03-…` §2.1 (shortlisted in Step 0.0, chosen in Step 1.1; value-VPS class to meet the cap) | reference cell: 1 × 2 vCPU / 4 GB + 2 × 8 vCPU / 24 GB NVMe | Fixed monthly price; no provider-specific services used |
| OS | **Ubuntu 24.04 LTS** | latest point release, unattended security updates | Long support, WireGuard in the kernel, lvm2, supported by K3s |
| Kubernetes | **K3s**, 3 servers with **embedded etcd** | **v1.36.x** stable channel, pinned exactly (e.g. `v1.36.4+k3s1`) | CNCF-certified single-binary Kubernetes; HA control plane on our own VMs at no fee; the original blueprint's choice |
| Container runtime | containerd (bundled with K3s) | bundled | Runs our Docker-built OCI images |
| Pod networking | flannel **`wireguard-native`** (bundled) | bundled | Encrypts node-to-node pod traffic over the provider's private network |
| NetworkPolicy | K3s embedded network-policy controller (kube-router, bundled) | bundled | Enforces Kubernetes NetworkPolicy without an extra CNI |
| DNS in cluster | CoreDNS (bundled, 2 replicas) | bundled | Standard |
| Metrics API | metrics-server (bundled) | bundled | `kubectl top`, capacity reports |
| Disabled K3s add-ons | Traefik, ServiceLB, local-path-provisioner | — | HTTP enters through Cloudflare Tunnel; Postgres through the gateway's hostPort; local-path ignores PVC sizes |
| Registry mirror | K3s embedded registry mirror (**Spegel**) | bundled | Nodes share pulled images; restarts don't depend on ghcr.io |
| Upgrades | **system-upgrade-controller** | pin at install | One-node-at-a-time K3s upgrades from git |
| Secrets | **SOPS** + **age**, decrypted by Flux's kustomize-controller; K3s `secrets-encryption` at rest | SOPS pin at install | No paid secret manager; secrets live encrypted in git and only decrypted inside the cell |
| Admin access | **Cloudflare Tunnel + Access** for SSH and `kubectl` (`cloudflared` as a systemd service on each VM) | cloudflared pinned | No public SSH or API port; MFA-protected access at no cost |
| Host firewall | nftables (rules from the node bootstrap) + the provider's firewall where offered | OS package | Only TCP 5432 public on tenant nodes |
| Registry | **ghcr.io** (private images, GitHub Free) | service | Container storage and transfer currently free; our images plus third-party images mirrored by digest (`docker buildx imagetools create`), so Docker Hub limits never matter |
| Transactional email | SMTP service with a free tier ≥ 3,000/month (candidates: Resend, Brevo; chosen in Step 0.0) | service | Supabase Auth custom SMTP, Alertmanager, our notifications |
| IaC | **Terraform** + `cloudflare/cloudflare` provider; the chosen VM provider's Terraform provider in `modules/vm-<provider>` if it has one | providers pinned to exact versions at install | One IaC tool; node bootstrap is identical with or without it |
| Terraform state | R2 bucket via the S3 backend; applies run only from CI in one concurrency group (test `use_lockfile` against R2 before relying on it) | — | Free tier, outside the cell, bucket-lock protected |
| GitOps | **Flux** | **v2.9.5** | Pull-based; no inbound access to the K3s API; native SOPS decryption |
| Containers | **Docker** Buildx for images (CI and local); Docker Desktop (WSL2) or Docker Engine for local dev | pin at install | One image format everywhere; k3d and `supabase start` run on Docker |
| Dependency updates | Renovate | hosted app (free) | Helm charts, images, Go modules, Cargo crates, pnpm, GitHub Actions SHAs, K3s version |

## 3. Cloudflare

| Product | Plan | Use |
|---|---|---|
| DNS + DNSSEC | Free | `example.com` zone, including `*.db.example.com` as a **DNS-only** wildcard with one A record per tenant node (architecture §8.1); SPF/DKIM for email |
| WAF, DDoS | Free (Pro US$25/month when OWASP/managed rulesets are funded) | HTTP hostnames |
| Rate limiting | Free (1 rule) | `POST api.example.com/v1/databases`; all other limits are in control-api and the gateway |
| Turnstile | Free | Sign-up, sign-in, password reset (native in Supabase Auth), database creation |
| Zero Trust **Access** | Free (≤ 50 users) | Admin app, Grafana, River UI, SSH, `kubectl` |
| **Tunnel** (`cloudflared`) | Free | HTTP ingress (in-cluster Deployment) and admin access (host systemd service), one of each per cell |
| **Workers Static Assets** (deployed with `wrangler`) | Free (static asset requests are unlimited; Workers Caching stays off because it bills cached requests) | Next.js static exports: public site, customer and admin dashboards; `_headers` for security headers |
| Workers Cron Trigger | Free (100,000 requests/day) | external uptime probe of the API and each node's gateway every minute |
| **R2** | 10 GB-month, 1 M Class A, 10 M Class B free; then pay as you go | WAL, base backups, etcd snapshots, admin-DB dumps, audit exports, Terraform state |
| Email Routing | Free | Inbound `support@` / `security@` |
| Load Balancing | paid add-on, Phase 4 | One pool per cell tunnel with health monitors, for the active-active control plane |

## 4. Supabase (admin/management layer only)

| Item | Choice | Why |
|---|---|---|
| Plan | **Free** for launch (`dbcloud-prod` + `dbcloud-staging`, region `ap-south-1`); **Pro** at the trigger in `03-…` §7 | ₹0 at launch; Pro adds daily backups, no pausing, 8 GB, 250 GB egress |
| Connection path | **Supavisor pooler over IPv4**: transaction mode (6543) for request handlers, session mode (5432) for the River coordinator, migrations and dumps | Direct connections are IPv6-only on Free, and not every VM provider routes IPv6. No IPv4 add-on needed, even on Pro |
| Database | Postgres (our `dbcloud` schema only; Data API disabled; RLS on; per-service roles) | Admin metadata + River job tables |
| Auth | **Supabase Auth**: email/password, magic link, Google, GitHub, TOTP MFA, Turnstile CAPTCHA, custom SMTP, ES256 signing keys + JWKS | Managed identity for dashboard users; all included on Free |
| Client libraries | `@supabase/supabase-js` (web apps, auth only); Go services use pgx + JWKS verification | Minimal surface |

## 5. Data plane (per cell)

| Layer | Choice | Version | Why |
|---|---|---|---|
| Postgres operator | **CloudNativePG** | **1.30.1** (supports Kubernetes 1.34–1.36; adopt 1.31 after its release and a soak) | Declarative clusters, roles, databases, poolers, CNPG-I plugins, Lease-based primary election |
| PostgreSQL | via CNPG `ClusterImageCatalog` | **18.6** (`ghcr.io/cloudnative-pg/postgresql:18.6-standard-trixie`, by digest) | Current major and minor. Built `--with-lz4 --with-zstd` (plus ICU, LLVM, libxml), checked with `pg_config --configure` in Step 0.3, so `wal_compression = lz4` is available |
| Backups | **Barman Cloud Plugin** | **0.15.0** (Barman 3.20) | Official CNPG backup plugin; S3-compatible targets; sidecar resources and retention interval set per `ObjectStore` |
| Pooler | PgBouncer via CNPG `Pooler` (one per tenant) | bundled | Per-tenant, operator-managed |
| Local storage | **TopoLVM** on the NVMe volume group `tenantvg` | Helm chart **17.2.0** | Capacity-aware LVM CSI, thick volumes, online expansion |
| Certificates | **cert-manager** + Let's Encrypt (DNS-01, Cloudflare solver) | **1.21.2** (cainjector kept: it injects the webhook's CA bundle) | Wildcard `*.db.example.com` |
| Metrics | **VictoriaMetrics** single-node + vmagent + vmalert + Alertmanager, from Helm charts (no operator) | pin at install | Small footprint; vmagent discovers CNPG pods by label and scrapes etcd |
| Logs | **VictoriaLogs** + **Fluent Bit** | pin at install | Small footprint; also ships the K3s API audit log |
| Dashboards | **Grafana** OSS + CNPG dashboard (grafana.com 20417) | pin at install | Standard |
| Image and config scanning | Trivy in CI and a weekly `trivy k8s` Job | pin at install | Covers image and misconfiguration scanning without a resident operator |
| Admission guardrails | Pod Security Admission (K3s PSA config) + ValidatingAdmissionPolicy (CEL) | built-in | No extra pods |

## 6. pg-gateway (Rust) crates

| Crate | Purpose |
|---|---|
| `tokio` | async runtime, TCP listener, timeouts |
| `rustls` + `tokio-rustls` | TLS 1.2/1.3 termination, SNI, ALPN `postgresql` |
| `postgres-protocol` | Postgres message encoding/decoding (startup, ErrorResponse, BackendKeyData) |
| `kube` + `k8s-openapi` (kube-rs, CNCF) | watch `TenantDatabase` routes and EndpointSlices in the cell |
| `arc-swap` | lock-free route table swaps |
| `prometheus-client` | metrics endpoint |
| `tracing` + `tracing-subscriber` | structured logs and spans |
| `thiserror` (library modules), `anyhow` (binary edge) | errors |
| `tokio-util` (`CancellationToken`, `TaskTracker`) | graceful shutdown and drain |

Versions: pin at install in `Cargo.toml`, `Cargo.lock` committed, `cargo-deny` + `cargo-audit` in CI, binaries built with `cargo-auditable`. Neon's Apache-2.0 proxy is the reference design for handshake details (attribute any copied code).

## 7. Control plane (Go) libraries

| Library | Version | Purpose |
|---|---|---|
| stdlib `net/http` + **oapi-codegen** v2 | pin at install | OpenAPI 3.1 contract → server interfaces and Go client |
| **pgx v5** + **sqlc** | pin at install | Typed SQL against the admin DB; `QueryExecModeExec` on transaction-mode pooler connections |
| **goose** | pin at install | Expand/contract SQL migrations |
| **River** (`riverqueue/river`) | pin at install | Transactional job queue (the outbox) |
| **River UI** (`riverqueue/riverui`) | pin at install | Staff job inspection at `jobs.example.com`, behind Cloudflare Access |
| **controller-runtime v0.24.x** (kubebuilder v4 layout) | v0.24.x | tenant-operator |
| Cloudflare Go SDK | pin at install | R2 temporary credentials and DNS records (node A records, tenants outside the default cell); everything else is Terraform |
| VM provider SDK | pin when the provider is chosen | the provider-API implementation of `ProviderAdapter` (the launch implementation is `manual`) |
| **zitadel/oidc** v3 | pin at install | mcp-auth protocol engine (OpenID-certified) |
| **MCP Go SDK** (`modelcontextprotocol/go-sdk`) | **v1.8.0** | mcp-server (spec 2026-07-28 + 2025-11-25) |
| **cedar-go** | v1.x | Authorization policies |
| **pg_query_go v6** | v6 | SQL parsing for AI tools (cgo) |
| `log/slog`, OpenTelemetry Go SDK | stdlib / pin | Logs, traces, metrics |
| cobra + GoReleaser | pin at install | CLI and releases |
| testcontainers-go, envtest, Chainsaw | pin at install | Integration, operator and e2e tests |

## 8. Web

| Layer | Choice | Version | Why |
|---|---|---|---|
| Framework | **Next.js** App Router with `output: 'export'` (static export), three apps: `web/site`, `web/customer`, `web/admin` | **16.3.x**, pin ≥ 16.3.7; React ≥ 19.2.6 | File-based routing, layouts and build-time rendering for the public site; the export is plain files, so hosting is free and no server-side Next.js code runs in production |
| Hosting | Cloudflare Workers Static Assets via wrangler (`assets.directory = "out"`, `not_found_handling = "404-page"`, `_headers` from `public/`) | wrangler pinned | Free, unlimited static requests |
| UI | **shadcn/ui** (Radix primitives, accessible) + **Tailwind CSS v4** | pin at install | Accessible components we own as source |
| Data | **TanStack Query v5** in client components; `openapi-fetch` + `openapi-typescript` generated from `api/openapi.yaml` | pin at install | Typed calls to control-api, caching, polling operations |
| Auth | `@supabase/supabase-js` browser client, sign-in only | pin at install | Works without a server; `@supabase/ssr` isn't used because there is no server |
| Sanitizing | DOMPurify, only where HTML must be rendered | pin at install | XSS defense |
| Quality | TypeScript strict, ESLint flat config with `eslint-config-next` (`next lint` was removed in 16), Vitest, Playwright + axe-core | pin at install | Checks in CI |
| Tooling | Node 24 LTS + pnpm workspace (lockfile committed, dependency build scripts off by default, `minimumReleaseAge` set) | — | One workspace for web apps and the TS SDK |

Build: Turbopack is Next.js 16's default, but production builds use `next build --webpack` while Turbopack's chunk loading breaks Trusted Types (`.kiro/steering/50-web.md`). A post-build script hashes every inline script in the exported HTML and writes the strict CSP into `_headers`.

## 9. Payments

**Razorpay** Subscriptions (UPI AutoPay, cards) with GST invoice data from our billing module. No fixed fee; 2 % + 0.99 % per payment plus GST on the fee.

## 10. CI/CD and supply chain

| Concern | Choice |
|---|---|
| CI | GitHub Actions on the Free plan: 2,000 minutes/month, native `ubuntu-24.04` (amd64) and `ubuntu-24.04-arm` (arm64) runners (no QEMU); path filters so only changed components build; Actions budget US$0; a self-hosted runner in WSL2 for k3d e2e if minutes run short |
| Workflow hardening | actions pinned to full commit SHAs; top-level `permissions: contents: read`; `persist-credentials: false`; no `pull_request_target`; `actionlint` + `zizmor` in CI |
| Images | multi-arch (`linux/amd64` + `linux/arm64`), distroless bases pinned by digest, non-root numeric UID; pushed to ghcr.io by digest; `hadolint` in CI |
| Signing / SBOM / scan | cosign (keyless), syft, Trivy (fail on critical) |
| Go checks | `go vet`, `golangci-lint` (incl. gosec), `go test -race`, `govulncheck`, fuzz targets for parsers |
| Rust checks | `cargo fmt --check`, `cargo clippy -D warnings` (pedantic + no-unwrap lints), `cargo nextest`, `cargo-deny`, `cargo-audit` |
| Web checks | ESLint (`eslint-config-next`), TypeScript strict, Vitest, Playwright + axe, CSP check on the exported HTML (no inline script without a hash in `_headers`) |
| Secret scanning | Trivy secret scanner in CI and as a pre-commit hook (GitHub's secret scanning isn't available for private repos on the Free plan); SOPS lint: no unencrypted `Secret` in git |
| IaC and manifests | `terraform validate`, Trivy config scan, `kubeconform`, ShellCheck for scripts |
| MCP checks | `modelcontextprotocol/conformance` suites for mcp-server and mcp-auth |

## 11. Build vs reuse

| We build | We operate (reuse) |
|---|---|
| control-api, worker jobs, placement + capacity controller, billing | K3s (etcd, flannel, kube-router, CoreDNS, Spegel), system-upgrade-controller |
| `TenantDatabase` CRD, tenant-operator, cluster-agent | CloudNativePG, Barman Cloud Plugin, PgBouncer, TopoLVM |
| **pg-gateway** (Rust) | cert-manager, Flux + SOPS |
| mcp-server tools, grant model, approvals, SQL safety | MCP Go SDK, pg_query_go, cedar-go |
| **mcp-auth** on zitadel/oidc (CIMD, RFC 8707 audience, consent) | Supabase Auth (user identity), Supabase Postgres (admin DB) |
| Next.js web apps (site, customer, admin), CLI, SDK generation | Cloudflare DNS, Tunnel, Access, WAF, Turnstile, Workers Static Assets, R2 |
| Node bootstrap (cloud-init + scripts), runbooks, acceptance-gate suites | Ubuntu, VictoriaMetrics/Logs, Grafana, ghcr.io |

## 12. Multi-arch check (build plan spike S6)

```bash
docker buildx imagetools inspect <image>:<tag> | grep -E "linux/(amd64|arm64)"
```

Must list both platforms for every image in the cell: CNPG operator + PostgreSQL 18.6 catalog image, Barman Cloud Plugin + sidecar, PgBouncer, TopoLVM, cert-manager, Flux, system-upgrade-controller, VictoriaMetrics single/vmagent/vmalert, Alertmanager, kube-state-metrics, node-exporter, VictoriaLogs, Fluent Bit, Grafana, cloudflared, Trivy, and all our images. (K3s itself ships binaries for both.)

## Sources (checked 2026-09-29)

- K3s: https://docs.k3s.io/ , releases https://update.k3s.io/v1-release/channels , packaged components https://docs.k3s.io/installation/packaged-components , networking https://docs.k3s.io/networking/basic-network-options , registry mirror https://docs.k3s.io/installation/registry-mirror , hardening https://docs.k3s.io/security/hardening-guide , upgrades https://docs.k3s.io/upgrades/automated
- Flux + SOPS: https://fluxcd.io/flux/guides/mozilla-sops/ ; SOPS: https://github.com/getsops/sops
- Cloudflare kubectl/SSH through Access: https://developers.cloudflare.com/cloudflare-one/tutorials/kubectl/
- CNPG support matrix: https://cloudnative-pg.io/docs/1.30/supported_releases
- Barman Cloud Plugin changelog and usage: https://github.com/cloudnative-pg/plugin-barman-cloud/blob/main/CHANGELOG.md , https://cloudnative-pg.io/plugin-barman-cloud/docs/usage/
- PostgreSQL 18.6: https://www.postgresql.org/about/news/postgresql-186-1711-1615-1519-1424-and-19-beta-3-released-3365/
- TopoLVM: https://github.com/topolvm/topolvm
- Supabase pricing, compute, connections: https://supabase.com/pricing , https://supabase.com/docs/guides/platform/compute-and-disk , https://supabase.com/docs/guides/database/connecting-to-postgres
- Supabase Auth signing keys + CAPTCHA + SMTP: https://supabase.com/docs/guides/auth/signing-keys , https://supabase.com/docs/guides/auth/auth-captcha , https://supabase.com/docs/guides/auth/auth-smtp
- River and PgBouncer: https://riverqueue.com/docs/pgbouncer ; pgx: https://pkg.go.dev/github.com/jackc/pgx/v5
- Cloudflare R2 pricing/limits: https://developers.cloudflare.com/r2/pricing/ , https://developers.cloudflare.com/r2/platform/limits/
- Cloudflare Tunnel on Kubernetes: https://developers.cloudflare.com/tunnel/guides/kubernetes/
- Workers Static Assets billing: https://developers.cloudflare.com/workers/static-assets/billing-and-limitations/
- GitHub Actions and Packages billing: https://docs.github.com/en/billing/concepts/product-billing/github-actions , https://docs.github.com/en/billing/concepts/product-billing/github-packages
- zitadel/oidc: https://github.com/zitadel/oidc
- MCP Go SDK: https://github.com/modelcontextprotocol/go-sdk/releases ; MCP conformance: https://github.com/modelcontextprotocol/conformance
- cedar-go: https://github.com/cedar-policy/cedar-go ; pg_query_go: https://github.com/pganalyze/pg_query_go
- kube-rs: https://kube.rs ; Neon proxy: https://github.com/neondatabase/neon/tree/main/proxy
- cert-manager releases: https://cert-manager.io/docs/releases/ ; Flux: https://fluxcd.io
- pnpm supply-chain settings: https://pnpm.io/supply-chain-security
- Next.js 16 and static exports: https://nextjs.org/blog/next-16 , https://nextjs.org/docs/app/guides/static-exports , https://nextjs.org/docs/app/guides/content-security-policy , https://nextjs.org/docs/app/api-reference/config/eslint , https://nextjs.org/blog/next-security-release-program
- Workers static assets routing and headers: https://developers.cloudflare.com/workers/static-assets/routing/static-site-generation/ , https://developers.cloudflare.com/workers/static-assets/headers/
- OpenNext for Cloudflare (upgrade path only): https://opennext.js.org/cloudflare
- shadcn/ui with Next.js: https://ui.shadcn.com/docs/installation/next ; TanStack Query: https://tanstack.com/query/latest ; openapi-fetch: https://openapi-ts.dev/openapi-fetch/
- Email free tiers: https://resend.com/docs/knowledge-base/account-quotas-and-limits , https://www.brevo.com/pricing/
