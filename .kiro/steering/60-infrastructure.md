---
inclusion: fileMatch
fileMatchPattern: 'deploy/**'
---

# Infrastructure rules (node bootstrap, K3s, Terraform, Flux, Helm, Kubernetes manifests)

## Spend cap and provider neutrality first
- MUST: no resource that bills is added unless the build-plan step and `plan/03-cost-model.md` name it; the PR states the monthly cost. The platform stays ≤ ₹9,000/month; the reference cell ≤ ₹5,600/month.
- MUST: provider-specific code lives only in `deploy/terraform/modules/vm-<provider>`; everything else (node bootstrap, K3s config, Flux, Helm) works unchanged on any provider that passes `plan/03-cost-model.md` §2.1. No managed load balancer, block storage, secret manager or IAM of the VM provider is used.
- MUST: a cell never spans providers or regions; all K3s servers share one private network in one location.

## Node bootstrap (`deploy/node-bootstrap/`, cloud-init + scripts)
- MUST: one bootstrap for every node role (`server-cp`, `server-worker`, `agent`), selected by a variable; idempotent (every step checks before acting); failures exit non-zero so a node never joins half-configured; scripts follow `75-shell.md`.
- MUST: Ubuntu 24.04 LTS; unattended security updates on, automatic reboots off (reboots happen only in the announced maintenance window, plan/01 §12.3); the sysctls from the K3s CIS hardening guide (`vm.panic_on_oom=0`, `vm.overcommit_memory=1`, `kernel.panic=10`, `kernel.panic_on_oops=1`).
- MUST: SSH: key-only, no passwords, no root login, listens only on the private interface and localhost (reached through the admin `cloudflared`); an admin user with sudo, keys from the bootstrap variables.
- MUST: nftables default-deny inbound. Public interface: TCP 5432 on tenant nodes only (nothing on `cp-1`). Private interface: only 6443, 2379–2380, 10250, 51820–51821/udp (WireGuard) and 5001 (Spegel) from the cell's node IPs. The provider firewall mirrors this where available.
- MUST: `tenantvg` is created only from the disk space left after the root filesystem (≈ 50 GB), never from a disk that holds data; the script refuses to run if the target device has a filesystem signature.
- MUST: K3s is installed from the pinned release (`INSTALL_K3S_VERSION`) with its checksum verified; never `curl | sh` with a floating version.
- MUST: the admin `cloudflared` runs as a systemd service with a token from the bootstrap secrets; it exposes only SSH and the K3s API to Cloudflare Access.

## K3s configuration (`deploy/k3s/`)
- MUST: exact pinned version (for example `v1.36.4+k3s1`) in one place, used by the bootstrap, k3d, system-upgrade-controller Plans and CI.
- MUST: servers: `cluster-init` on the first server, embedded etcd with 3 servers; `node-ip`/`advertise-address` on the private network; `tls-san` for the admin hostname; `flannel-backend: wireguard-native`; `disable: [traefik, servicelb, local-storage]`; `embedded-registry: true`; `secrets-encryption: true`; `protect-kernel-defaults: true`; PSA admission config file (restricted default, exemptions only as listed in plan/01 §6.3); API audit log with policy file and rotation; `NodeRestriction` and `EventRateLimit` admission plugins; `cp-1` gets `node-taint: CriticalAddonsOnly=true:NoExecute`.
- MUST: kubelet args on every node: `max-pods=110`, `kube-reserved=cpu=500m,memory=2Gi` on servers (agents: `cpu=200m,memory=512Mi`), `system-reserved=cpu=200m,memory=512Mi`, `eviction-hard=memory.available<100Mi`; changes update `plan/03-cost-model.md` §6.
- MUST: etcd snapshots every 6 h, 28 kept, compressed, uploaded to the cell's R2 bucket (`etcd-s3-endpoint`, region `auto`, credentials from a file readable only by root). The K3s server token and agent token are separate, stored in the password manager, and rotated with `k3s token rotate` per the runbook.
- MUST: `registries.yaml` points `ghcr.io` at a read-only token; images are referenced by digest.
- MUST: upgrades only through system-upgrade-controller Plans committed to git: servers one at a time with `concurrency: 1`, then agents; never skip a minor; the target is the pinned version, not a channel.

## Terraform
- MUST: all Cloudflare resources, and the VM provider's resources when it has a Terraform provider, come from Terraform in `deploy/terraform`; no console changes (drift is a bug). Runtime-owned exceptions, each managed by exactly one component: node count (capacity controller via `ProviderAdapter`), DNS A records for tenant nodes and for tenants outside the default cell (worker), R2 temporary credentials (worker), Next.js web app deploys (wrangler, from a `workflow_dispatch` workflow). The Terraform state bucket is created once with `wrangler r2 bucket create` and then imported.
- MUST: providers pinned to an exact version; modules pinned to an exact version (the lock file doesn't cover modules); `.terraform.lock.hcl` committed with hashes for `linux_amd64`, `linux_arm64` and `windows_amd64` (`terraform providers lock -platform=…`), and its diffs reviewed.
- MUST: one reusable module per concern (`cloudflare-edge`, `r2`, `vm-<provider>`); environments (`global`, `c1`, …) only compose modules with variables. A new cell = a new env folder, no module changes.
- MUST: `terraform plan` output in the PR; applies run from a manually triggered workflow; state in R2; one apply at a time.
- MUST: state holds secrets in plaintext, so it lives only in the private R2 bucket; `sensitive = true` hides values from output but not from state. Prefer write-only arguments and ephemeral values for secrets; otherwise create the secret outside Terraform and keep it SOPS-encrypted in git.
- MUST: VM resources have `prevent_destroy`; CI fails any plan that would replace a tenant node, because that destroys `tenant-local` volumes. Nodes are replaced only through the node-loss or evacuation runbooks.
- MUST: every variable has a description, a type and validation where values are constrained; no defaults for secrets; every module has a header comment with inputs, outputs and what it doesn't manage; every resource that supports tags/labels carries `dbcloud-env`, `dbcloud-cell`.
- MUST: `terraform fmt -check`, `terraform validate` and Trivy's misconfiguration scan pass in CI; findings are fixed or ignored inline with a reason.

## Secrets (SOPS + age)
- MUST: every Kubernetes `Secret` in git is SOPS-encrypted with the cell's age public key (`.sops.yaml` rules by path); CI fails on any unencrypted `Secret` or `stringData` in `deploy/`.
- MUST: the cell's age private key exists only as the `sops-age` Secret in `flux-system` and in the password manager (offline backup); it is never in git, CI, or a developer's shell history.
- MUST: each secret is readable only by the ServiceAccount that uses it (RBAC `get` on that name only).

## Kubernetes manifests (Flux, Helm values, platform objects)
- MUST: HelmReleases pin chart versions and image digests; Renovate proposes bumps. Every Helm value that differs from the chart default has a comment explaining why.
- MUST: every container has CPU and memory requests, a memory limit equal to its request, and matches the platform footprint table in `plan/03-cost-model.md` §6; changing one updates the table.
- MUST: every stateless platform Deployment has 2 replicas with `topologySpreadConstraints` across `node-1`/`node-2` and a PodDisruptionBudget `minAvailable: 1`; nothing but K3s itself tolerates `CriticalAddonsOnly`.
- MUST: Pod Security `restricted` for everything except the documented exemptions (K3s components in `kube-system`, TopoLVM node plugin, node-exporter and Fluent Bit, pg-gateway's `hostPort`): `runAsNonRoot`, `allowPrivilegeEscalation: false`, `capabilities: drop: [ALL]`, `seccompProfile: RuntimeDefault`, `readOnlyRootFilesystem: true` where the app allows it. A ValidatingAdmissionPolicy allows `hostPort` only for the pg-gateway DaemonSet.
- MUST: `automountServiceAccountToken: false` unless the pod calls the Kubernetes API; each workload has its own ServiceAccount.
- MUST: RBAC: namespaced Roles over ClusterRoles; no wildcards; treat `list/watch` on Secrets, workload creation, `nodes/proxy`, `escalate`, `bind`, `impersonate`, `serviceaccounts/token` and webhook configuration as privileged and justify each in a comment.
- MUST: every namespace has default-deny ingress and egress NetworkPolicies with explicit allows, including DNS egress; internet egress uses `ipBlock` `0.0.0.0/0` with `except` for private, link-local and metadata ranges.
- MUST: images referenced by digest only; `imagePullPolicy: IfNotPresent`.
- MUST: per-cell overlays in `deploy/flux/clusters/<cell>`; shared bases in `infrastructure/` and `apps/`.
- MUST: tenant resources are never in git; they come only from the control plane.
- MUST: manifests pass `kubeconform` against the pinned Kubernetes version and CRD schemas.
