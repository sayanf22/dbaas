# 05 — Developer Environment, Accounts and Downloads (Blueprint 4.0)

Target machine: Windows. Recommended: **WSL2 Ubuntu 24.04** for all build and cluster tooling, Windows for the editor and browser. Reasons: `pg_query_go` needs cgo and a C toolchain, the operator tooling and scripts assume Linux, and Docker runs the local K3s cell (k3d) and the local Supabase stack.

**Status of this PC (2026-09-30):**
- WSL2 Ubuntu 24.04.5 LTS (default user `dev`, 8 GB / 4 CPUs via `.wslconfig`) and Docker Desktop 29.8.1 with WSL integration. Both virtual disks live on D: (`D:\Offload\WSL\Ubuntu-24.04`, `D:\Offload\DockerDesktopWSL`).
- Installed in Ubuntu, versions from `tools.versions`: Go 1.27.1, Rust 1.98.1 (clippy, rustfmt), Node 24.21.0 + pnpm, Supabase CLI, kubectl 1.36, Helm, k3d, psql/pgbench 18.6, Terraform, cloudflared, Trivy, gh, SOPS, and the lint, test and release tools. A k3d cluster on K3s v1.36.4+k3s1 was created, ran a pod and was deleted.
- Not installed yet (added at the step that needs them): age, Flux CLI, kubectl-cnpg, yq, the cargo supply-chain tools, the Go codegen tools (sqlc, goose, oapi-codegen, govulncheck; installed by `hack/install-tools.sh` in Step 0.1), the K3s VM bundle, and the AWS CLI.
- 2026-09-30 update: the remaining tools (age, yq, Flux, kubectl-cnpg, sqlc, goose, oapi-codegen, govulncheck, cargo-deny, cargo-nextest) come from `hack/install-tools.sh` into `~/.local/bin`. Upstream manifests are vendored in `deploy/vendor/` (SHA256SUMS + `images.lock` digests); source clones for reference live in `third_party/` (git-ignored). `task cell:up` starts the local starter cell; `hack/capacity-test.sh 10 base` passed.
- Next: Step 0.0 (accounts, incl. the GitHub org and the GitHub MCP token), then the rest of Step 0.1 (push, first green CI run).

**Docker's role:** every backend component is packaged as a container image built with Docker Buildx (locally and in CI) and stored in ghcr.io. Local development runs on Docker: **k3d** runs real K3s servers and agents as Docker containers, and `supabase start` runs the Supabase stack in Docker. The production VMs don't run Docker; K3s runs the same images with its bundled containerd.

---

## 1. Accounts (build plan Step 0.0)

Checked 2026-09-29. Everything below is free to create; the "card" column says where a card must be on file. **No VM provider account is needed until Step 1.1**: all of Phase 0 is built on this PC.

| # | Account | Why | Plan | Card needed | Link |
|---|---|---|---|---|---|
| 0 | Password manager + authenticator app | one strong password and 2FA for every account below; also stores the K3s token, SOPS age key and recovery codes | Bitwarden Free (or any TOTP app) | no | https://bitwarden.com/help/password-manager-plans/ |
| 1 | A dedicated work email (Gmail or similar) | owner of every account; later replaced by `@example.com` addresses via Cloudflare Email Routing | free | no | https://accounts.google.com/signup |
| 2 | **GitHub** personal account + a **free organization** | the org owns the monorepo, Actions minutes, ghcr.io images, OAuth app and Renovate | GitHub Free (org) | no | https://github.com/signup , https://github.com/organizations/plan |
| 3 | **Docker** | no account needed (already installed); a free account only raises Docker Hub pulls from 100 to 200 per 6 h | Docker Personal (optional) | no | https://docs.docker.com/subscription/desktop-license/ |
| 4 | **Cloudflare** | DNS, Tunnel, Access (incl. SSH/`kubectl`), WAF, Turnstile, web apps, R2 | Free | **yes, for R2 only** (card or PayPal, charged ₹0 inside the free tier) | https://dash.cloudflare.com/sign-up |
| 5 | **Domain** | `example.com` for the product | `.com` at Cloudflare Registrar (at cost), or `.in` at a NIXI-accredited registrar (Cloudflare doesn't sell `.in`; eKYC via DigiLocker) | yes (purchase) | https://domains.cloudflare.com/ , https://www.registry.in/accredited-registrars |
| 6 | **Supabase** (sign in with GitHub) | admin DB + Auth: projects `dbcloud-prod` and `dbcloud-staging` in `ap-south-1` | Free | no | https://supabase.com/dashboard/sign-up |
| 7 | **Transactional email** (SMTP) | Supabase Auth emails, alerts, notifications | free tier ≥ 3,000/month (for example Resend or Brevo; compare in `03-…` §3) | no | https://resend.com/ , https://www.brevo.com/ |
| 8 | **Google Cloud** project (OAuth client only) | "Sign in with Google" in Supabase Auth | free, no billing account needed for sign-in scopes | no | https://console.cloud.google.com/ , https://supabase.com/docs/guides/auth/social-login/auth-google |
| 9 | **GitHub OAuth App** (in the org) | "Sign in with GitHub" in Supabase Auth | free | no | `https://github.com/organizations/<org>/settings/applications` , https://supabase.com/docs/guides/auth/social-login/auth-github |
| 10 | **Renovate** GitHub app | dependency, image-digest, K3s-version and action-SHA updates | free for private repos | no | https://github.com/apps/renovate |
| 11 | **Razorpay** | subscriptions (UPI AutoPay, cards) | standard, ₹199 one-time KYC | no (PAN + bank account instead) | https://dashboard.razorpay.com/signup |
| 12 | **VM provider** (Step 1.1, not now) | the three VMs of cell `c1` | chosen against the checklist in `03-…` §2.1, within ₹5,600/month | yes (at Step 1.1) | shortlist recorded in ADR-024 |

You don't need: a Docker Hub paid plan, a GitHub paid plan, a Supabase paid plan (until its trigger), a Cloudflare paid plan, a Vercel account, or an AWS/GCP account.

### 1.1 The accounts, in order

1. **Password manager + authenticator** → store every password and recovery code there; use TOTP 2FA everywhere (GitHub makes 2FA mandatory).
2. **GitHub:** create your personal account → enable 2FA → create the organization on the Free plan (https://github.com/organizations/plan) → Billing → *Budgets and alerts* → an Actions budget of **US$0** with "stop usage when the budget is reached". Free private repos don't enforce branch rules, CODEOWNERS or deployment environments and have no secret scanning; `04-…` Step 0.1 lists the compensating controls. ghcr.io needs nothing extra: images are pushed from Actions with `GITHUB_TOKEN`, and the cell pulls with a read-only fine-grained token.
3. **Domain + Cloudflare:** create the Cloudflare account → buy `.com` in Cloudflare Registrar, or buy `.in` from an accredited registrar, complete its eKYC, then change its nameservers to the two Cloudflare gives you → enable DNSSEC → enable R2 (adds the card, ₹0 inside the free tier) → turn on R2 usage notifications → enable Zero Trust on the Free plan (team name, your email as the only allowed identity).
4. **Supabase:** sign in with GitHub → create an organization on Free → create `dbcloud-prod` and `dbcloud-staging` in *South Asia (Mumbai)*; store the database passwords in the password manager.
5. **Transactional email:** sign up → verify `example.com` (SPF and DKIM records in Cloudflare DNS) → create SMTP credentials for Supabase Auth and Alertmanager.
6. **Google OAuth client:** Google Cloud Console → new project → Google Auth Platform → Audience *External* → Branding (app name, support email) → Clients → *Web application* with redirect URI `https://<project-ref>.supabase.co/auth/v1/callback`. Sign-in scopes (openid, email, profile) need no Google verification; showing your logo needs brand verification (a few days).
7. **GitHub OAuth App:** in the org's settings → OAuth Apps → New, callback `https://<project-ref>.supabase.co/auth/v1/callback`.
8. **Renovate:** install the app on the monorepo only.
9. **Razorpay:** sign up → KYC as an individual (PAN, Aadhaar/DigiLocker, savings or current bank account; GST optional). Live payments need an approved website with About, Contact, Pricing, Terms, Privacy, Cancellation/Refund and Shipping pages, a sample invoice and a test login; these pages ship with the public site in Step 1.11. Master KYC guide: https://razorpay.com/docs/payments/business-types-kyc-documents/ , website rules: https://razorpay.com/docs/payments/dashboard/account-settings/business-website-details/ . Tax registration (GST) and the wording of legal pages are decisions for you and your accountant or lawyer.
10. **VM provider shortlist (paper only):** list 2–3 providers that seem to meet `03-…` §2.1 with their prices; no account yet. The choice and sign-up happen at Step 1.1, after Phase 0.

---

## 2. Windows side (done on this PC)

```powershell
wsl --install -d Ubuntu-24.04
winget install --id Git.Git -e
winget install --id Docker.DockerDesktop -e   # WSL 2 engine + Ubuntu-24.04 integration
```

`$env:USERPROFILE\.wslconfig` gives WSL2 8 GB RAM and 4 CPUs (`[wsl2]`, `memory=8GB`, `processors=4`); a 3-server + 1-agent k3d cell plus the local Supabase stack fit in that.

Editor extensions (Kiro / VS Code): Go, rust-analyzer, YAML, Kubernetes, HashiCorp Terraform, ESLint, Prettier, Tailwind CSS IntelliSense, Even Better TOML, Markdown Mermaid preview.

---

## 3. Pinned tool versions (`tools.versions`)

`hack/install-tools.sh` reads this file so every developer and CI job uses the same versions. "Latest stable" = newest stable on the day the file is created; Renovate bumps it afterwards.

| Tool | Version | Purpose |
|---|---|---|
| Go | **1.27.x** | control plane, operator, CLI |
| Rust (rustup) | stable, edition 2024; components `clippy`, `rustfmt` | pg-gateway |
| cargo-deny, cargo-audit, cargo-nextest, cargo-auditable | latest stable | Rust supply chain + tests |
| Node.js + pnpm (corepack) | **24 LTS** (Next.js 16 needs ≥ 20.9) | Next.js apps, TS SDK, wrangler, Supabase CLI |
| Next.js, React | pinned per app in `package.json` (Next.js **16.3.x**, ≥ 16.3.7; React ≥ 19.2.6) | web apps |
| Supabase CLI | pinned as a pnpm dev dependency | local Supabase stack, type checks |
| wrangler | pinned as a pnpm dev dependency | deploy Workers Static Assets |
| **K3s** | **v1.36.x** exact release (same as production) | production cells; also the image k3d runs |
| **k3d** | latest stable | local K3s cells in Docker |
| kubectl | **1.36.x** | match the K3s minor |
| Helm | latest stable | charts |
| Flux CLI | **2.9.5** | GitOps bootstrap |
| **SOPS**, **age** | latest stable | encrypted secrets in git |
| kubectl-cnpg plugin | **1.30.1** | CNPG operations |
| Terraform | latest stable; providers pinned to exact versions (`cloudflare/cloudflare`, and the VM provider's when chosen) | infrastructure |
| Docker Buildx | bundled with Docker | multi-arch image builds |
| hadolint, ShellCheck, actionlint, zizmor, kubeconform | latest stable | Dockerfile, shell, workflow and manifest checks |
| sonobuoy | latest stable | Kubernetes conformance on K3s (spike S1) |
| fio | Ubuntu package | disk benchmarks (spike S2) |
| cloudflared | latest stable | admin access (SSH, `kubectl`) and tunnel debugging |
| kubebuilder | latest v4.x | operator scaffolding |
| sqlc, goose, oapi-codegen (v2) | latest stable | codegen + migrations |
| golangci-lint, govulncheck | latest stable | Go checks |
| Chainsaw | latest stable | operator e2e |
| cosign, syft, Trivy | latest stable | sign, SBOM, scan, secret scan |
| GoReleaser | latest stable | CLI releases |
| psql + pgbench | **18.x** (PGDG apt repo) | client + load tests |
| k6, k9s, jq, yq, openssl | latest stable | load tests, daily ops |

---

## 4. WSL2 (Ubuntu 24.04) install

Replace `<ver>` with the value pinned in `tools.versions`. Prefer release archives with published checksums; the vendor scripts shown are the documented installers.

```bash
# Base packages (build-essential gives cgo a C compiler for pg_query_go)
sudo apt-get update && sudo apt-get install -y build-essential git curl unzip jq ca-certificates gnupg lsb-release openssl pkg-config fio

# Go
curl -LO https://go.dev/dl/go<ver>.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go<ver>.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' >> ~/.bashrc && source ~/.bashrc

# Rust
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --profile minimal
source "$HOME/.cargo/env" && rustup component add clippy rustfmt
cargo install --locked cargo-deny cargo-audit cargo-nextest cargo-auditable

# Node 24 LTS + pnpm (Supabase CLI and wrangler come from the repo's package.json)
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/master/install.sh | bash
source ~/.bashrc && nvm install 24 && corepack enable

# kubectl 1.36
curl -fsSL https://pkgs.k8s.io/core:/stable:/v1.36/deb/Release.key | sudo gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
echo 'deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v1.36/deb/ /' | sudo tee /etc/apt/sources.list.d/kubernetes.list
sudo apt-get update && sudo apt-get install -y kubectl

# Helm and k3d (release archives; verify checksums from the release page)
curl -LO https://get.helm.sh/helm-v<ver>-linux-amd64.tar.gz && tar -xzf helm-v<ver>-linux-amd64.tar.gz && sudo mv linux-amd64/helm /usr/local/bin/
curl -Lo k3d https://github.com/k3d-io/k3d/releases/download/v<ver>/k3d-linux-amd64 && chmod +x k3d && sudo mv k3d /usr/local/bin/

# SOPS and age (release archives)
curl -Lo sops https://github.com/getsops/sops/releases/download/v<ver>/sops-v<ver>.linux.amd64 && chmod +x sops && sudo mv sops /usr/local/bin/
curl -LO https://github.com/FiloSottile/age/releases/download/v<ver>/age-v<ver>-linux-amd64.tar.gz && tar -xzf age-v<ver>-linux-amd64.tar.gz && sudo mv age/age age/age-keygen /usr/local/bin/

# Flux CLI and CNPG plugin (pinned)
curl -s https://fluxcd.io/install.sh | sudo FLUX_VERSION=2.9.5 bash
curl -sSfL https://github.com/cloudnative-pg/cloudnative-pg/raw/main/hack/install-cnpg-plugin.sh | sudo sh -s -- -b /usr/local/bin v1.30.1

# Terraform (HashiCorp apt repo)
wget -O- https://apt.releases.hashicorp.com/gpg | sudo gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/hashicorp.list
sudo apt-get update && sudo apt-get install -y terraform

# cloudflared (Cloudflare apt repo)
sudo mkdir -p --mode=0755 /usr/share/keyrings
curl -fsSL https://pkg.cloudflare.com/cloudflare-main.gpg | sudo tee /usr/share/keyrings/cloudflare-main.gpg >/dev/null
echo "deb [signed-by=/usr/share/keyrings/cloudflare-main.gpg] https://pkg.cloudflare.com/cloudflared any main" | sudo tee /etc/apt/sources.list.d/cloudflared.list
sudo apt-get update && sudo apt-get install -y cloudflared

# PostgreSQL 18 client (PGDG)
sudo install -d /usr/share/postgresql-common/pgdg
sudo curl -o /usr/share/postgresql-common/pgdg/apt.postgresql.org.asc --fail https://www.postgresql.org/media/keys/ACCC4CF8.asc
echo "deb [signed-by=/usr/share/postgresql-common/pgdg/apt.postgresql.org.asc] https://apt.postgresql.org/pub/repos/apt $(lsb_release -cs)-pgdg main" | sudo tee /etc/apt/sources.list.d/pgdg.list
sudo apt-get update && sudo apt-get install -y postgresql-client-18
# pgbench: if not on PATH, install postgresql-18 (leave the service disabled) or use `kubectl cnpg pgbench`

# Go tools (pinned)
go install github.com/sqlc-dev/sqlc/cmd/sqlc@<ver>
go install github.com/pressly/goose/v3/cmd/goose@<ver>
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@<ver>
go install golang.org/x/vuln/cmd/govulncheck@<ver>
go install github.com/kyverno/chainsaw@<ver>

# kubebuilder
curl -L -o kubebuilder "https://go.kubebuilder.io/dl/latest/$(go env GOOS)/$(go env GOARCH)" && chmod +x kubebuilder && sudo mv kubebuilder /usr/local/bin/
```

Install cosign, syft, Trivy, golangci-lint, GoReleaser, sonobuoy, k6, k9s and yq from their release pages (checksum-verified) or official apt repos.

---

## 5. Access to the real cell (from Step 1.2)

```bash
# SSH to a node through Cloudflare Access (no public SSH port). One-time ~/.ssh/config entry:
#   Host node-1
#     HostName ssh-node-1.admin.example.com
#     ProxyCommand cloudflared access ssh --hostname %h
ssh node-1

# kubectl through Cloudflare Access (no public K3s API port)
cloudflared access tcp --hostname k8s-c1.admin.example.com --url 127.0.0.1:1234 &
HTTPS_PROXY=socks5://127.0.0.1:1234 kubectl --context c1 get nodes

# Cloudflare (wrangler and Terraform use API tokens with only the permissions they need)
export CLOUDFLARE_API_TOKEN=...          # never committed; stored in the password manager

# Supabase (local stack for development)
pnpm supabase start                      # local Postgres + Auth in Docker
pnpm supabase status                     # prints local DB URL and JWT/JWKS settings
```

The kubeconfig for `c1` points at `https://k8s-c1.admin.example.com` with the cluster's CA; the admin client certificate lives only in the password manager. The provider's web console is the break-glass path if Cloudflare is down. CI never talks to the K3s API or to Supabase: it pushes images and commits manifests, and Flux inside the cell pulls them; admin-DB migrations run as an in-cell Job. Terraform applies run from a manually triggered workflow.

---

## 6. Local development loop

One-time host preparation (root, from Windows): `wsl -d Ubuntu-24.04 -u root -- bash hack/local-host.sh setup dev`. It loads the WireGuard and thin-pool modules at boot, installs lvm2, xfsprogs and the pinned `lvmd`, and adds a sudo rule that lets `dev` run only the root-owned copy's `lvm-up`, `lvm-down` and `status` commands, which `hack/cell.sh` uses for the TopoLVM loop-file volume groups.

```bash
task cell:up    # k3d cell "local" (pinned K3s v1.36, same deploy/k3s config as production; PROFILE=starter: 1 server,
                # default; PROFILE=reference: 3 servers + 1 agent) + Flux from a local OCI artifact with a local age key
                # + TopoLVM (host lvmd, loop VG per tenant node) + CNPG + Barman plugin + SeaweedFS (local S3, ADR-026)
                # + cert-manager (self-signed) + VictoriaMetrics single
task cell:sync  # roll out changes under deploy/ through Flux, as production does
task cell:test  # Step 0.3 acceptance suite (test/cell) against the running cell
task cell:down  # delete the cell, its registry and the loop-file volume groups
task db:up      # supabase start (admin DB + Auth)
task gen        # sqlc, oapi-codegen, controller-gen, SDKs
task test       # Go unit + integration (testcontainers), cargo nextest, Vitest
task e2e        # Chainsaw suites against k3d, gateway client matrix (from Step 0.4)
```

Local hostnames: `*.localtest.me` resolves to 127.0.0.1, so SNI routing is tested with real names, e.g.
`psql "host=t-demo.db.localtest.me port=5432 dbname=app user=app sslmode=verify-full sslrootcert=./hack/dev-ca.crt"`.

Local images build for `linux/amd64` and are imported into k3d (`k3d image import`); CI builds `linux/amd64` and `linux/arm64` natively on GitHub's runners and merges them into one multi-arch manifest in ghcr.io. Go binaries are built in the builder stage of `build/go.Dockerfile` and copied into a distroless image; the gateway uses `gateway/Dockerfile` the same way.

---

## 7. Conventions

The coding and review rules live in `07-engineering-standards.md` and are loaded into Kiro automatically from `.kiro/steering/`. Commits follow Conventional Commits; every PR description carries the "Done when" checklist of its build-plan step.

## Sources (checked 2026-09-29)

- k3d: https://k3d.io/ ; K3s install and config: https://docs.k3s.io/installation/configuration
- Cloudflare SSH and kubectl through Access: https://developers.cloudflare.com/cloudflare-one/tutorials/kubectl/ , https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/use-cases/ssh/
- SOPS and age: https://github.com/getsops/sops , https://github.com/FiloSottile/age ; Flux SOPS guide: https://fluxcd.io/flux/guides/mozilla-sops/
- Docker Desktop licence: https://docs.docker.com/subscription/desktop-license/
- GitHub Packages billing: https://docs.github.com/en/billing/concepts/product-billing/github-packages
