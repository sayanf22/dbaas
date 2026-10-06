---
inclusion: fileMatch
fileMatchPattern: 'deploy/**'
---

# Infrastructure rules (node bootstrap, K3s, Terraform, Flux, Helm, Kubernetes manifests)

## Spend cap and provider neutrality first
- MUST: no resource that bills is added unless the build-plan step and `plan/03-cost-model.md` name it; the PR states the monthly cost. The platform stays ≤ ₹9,000/month; the launch cell is three VMs, `control-1`, `storage-1` and `compute-1` (plan/03-cost-model.md §1, §7; ADR-029).
- MUST: provider-specific code lives only in `deploy/terraform/modules/vm-<provider>`; everything else (node bootstrap, K3s config, Flux, Helm) works unchanged on any provider that passes `plan/03-cost-model.md` §2.1.
  - No managed load balancer, block-storage CSI, secret manager or IAM of the VM provider is used. A provider disk or volume attached by Terraform under a storage node's LINSTOR pool is allowed (for example AWS gp3 in `vm-aws`).
  - On AWS (`vm-aws`, plan/01 §4.3): no ELB/NLB, EBS CSI, Secrets Manager/KMS for workloads or instance profile; IMDSv2 required with hop limit 1.
- MUST: a cell never spans providers or regions; all nodes sit in one location. Where the provider has no private network (Hostinger, the recorded deviation in ADR-029), node-to-node traffic uses the public addresses and is encrypted and allow-listed by peer IP.

## Node roles
- MUST: every node has exactly one role label: `dbcloud.io/pool=control` (K3s servers), `dbcloud.io/pool=compute` or `dbcloud.io/pool=storage` (agents). Storage nodes carry the taint `dbcloud.io/pool=storage:NoSchedule`.
- MUST: platform workloads select `dbcloud.io/pool=control` and use priority class `dbcloud-platform`; tenant pods select `dbcloud.io/pool=compute` and use `dbcloud-tenant` (both classes in `deploy/flux/infrastructure/priority-classes`).
- MUST: only these DaemonSets tolerate the storage taint: the LINSTOR satellite, node-exporter and Fluent Bit. pg-gateway and the compute governor run only on compute nodes.
- MUST: compute nodes hold no tenant data on their disks (diskless DRBD clients only) and run with swap off.

## Node bootstrap (`deploy/node-bootstrap/`, cloud-init + scripts)
- MUST: one bootstrap for every node role (`control`, `compute`, `storage`; `control` also joins as an extra server once HA is scheduled), selected by a variable; idempotent (every step checks before acting); failures exit non-zero so a node never joins half-configured; scripts follow `75-shell.md`.
- MUST: Ubuntu 24.04 LTS with the generic kernel and its headers; unattended security updates on, automatic reboots off (reboots happen only in the announced maintenance window, plan/01 §12.3); the sysctls from the K3s CIS hardening guide (`vm.panic_on_oom=0`, `vm.overcommit_memory=1`, `kernel.panic=10`, `kernel.panic_on_oops=1`).
- MUST: the kernel package is pinned (held). The DRBD 9 module is built for the running kernel by the pinned Piraeus loader image. A kernel update is applied only after a module build test against the new kernel passes (on the QEMU/KVM test bed or a node in its maintenance window); a failing build holds the update.
- MUST: SSH: key-only, no passwords, no root login, reached only through the admin `cloudflared` (listening on localhost, and on the private interface where one exists); an admin user with sudo, keys from the bootstrap variables.
- MUST: nftables default-deny inbound.
  - **From the internet:** TCP 5432 on compute nodes only; nothing on control and storage nodes.
  - **Between nodes, from the cell's peer node IPs only:** 6443 (to servers), 10250, 51820–51821/udp (WireGuard), 5001 (Spegel), 3366–3367 (LINSTOR satellite API) and 7000–7999 (DRBD); 2379–2380 only between servers.
  - The provider firewall mirrors this where available. Changes to these rules need a second reviewer.
- MUST: the volume group `linstorvg` (with one LVM-thin pool) is created only on control and storage nodes, only from the disk space left after the root filesystem (sizes recorded in `plan/03-cost-model.md` §6.1), never from a disk that holds data; the script refuses to run if the target device has a filesystem signature. Compute nodes get no LVM.
- MUST: K3s is installed from the pinned release (`INSTALL_K3S_VERSION`) with its checksum verified; never `curl | sh` with a floating version.
- MUST: the admin `cloudflared` runs as a systemd service with a token from the bootstrap secrets; it exposes only SSH and the K3s API to Cloudflare Access.

## K3s configuration (`deploy/k3s/`)
- MUST: exact pinned version (`v1.36.4+k3s1` today) in one place, used by the bootstrap, k3d, system-upgrade-controller Plans and CI.
- MUST: servers use embedded etcd, never SQLite (ADR-029; the measurements are kept in superseded ADR-025). The launch cell is one server (`control-1`) with `cluster-init`, and HA (deferred) joins two more servers to it without a rebuild.
- MUST: server flags:
  - `node-ip`/`advertise-address` on the private network where one exists, otherwise the public IPv4; `tls-san` for the admin hostname;
  - `flannel-backend: wireguard-native`; `disable: [traefik, servicelb, local-storage, metrics-server]`; `embedded-registry: true`;
  - `secrets-encryption: true`; `protect-kernel-defaults: true`; PSA admission config file (restricted default, exemptions only as listed in plan/01 §6.3); API audit log with policy file and rotation; `NodeRestriction` and `EventRateLimit` admission plugins;
  - kube-controller-manager `disable-force-detach-on-timeout: true`, because only the fencer detaches volumes from a lost compute node.
- MUST: agents join with their role label from the bootstrap; storage agents also get `node-taint: dbcloud.io/pool=storage:NoSchedule`.
- MUST: kubelet args on every node: `max-pods=110`, and `kube-reserved`, `system-reserved` and `eviction-hard` as set in `deploy/k3s/server.yaml` (server) and `deploy/k3s/agent.yaml` (agents), recorded in `plan/03-cost-model.md` §6.1; changing one updates that table.

## Storage (LINSTOR + DRBD 9 through the Piraeus Operator, ADR-027)
- MUST: Piraeus Operator, LINSTOR, LINSTOR CSI and the DRBD loader are pinned together (Piraeus v2.12.0: LINSTOR 1.35.2, CSI 1.13.1, DRBD 9.3.4), vendored with checksums, images by digest.
- MUST: satellites use host networking; DRBD uses TLS (`DrbdOptions/Net/tls: yes`, `tlshd` sidecar) from the first volume; controller ↔ satellite traffic uses TLS with cert-manager certificates.
- MUST: StorageClasses are exactly `tenant-network` and `platform-local`, with the parameters in plan/01 §7.1:
  - placement count 1 at launch; LVM-thin; online expansion; snapshots;
  - `on-no-data-accessible: suspend-io`, `on-suspended-primary-outdated: force-secondary`, `rr-conflict: retry-connect`;
  - `tenant-network` pools only on storage nodes, diskless access from compute nodes; `platform-local` only on control nodes.
  - Any other class needs an ADR.
- MUST: thin-pool safety: oversubscription ratio 1.0 at launch; placement stops at 80 % provisioned; alerts at 70/80 % data and metadata; DRBD resync rate (`c-max-rate`) capped as measured in S12.
- MUST: no TopoLVM, local-path (outside the local k3d overlay) or NVMe-oF target in any cell; `nvmet-tcp` is never exposed.
- MUST: etcd snapshots every 6 h, 28 kept, compressed, uploaded to the cell's R2 bucket (`etcd-s3-endpoint`, region `auto`, credentials from a file readable only by root). The K3s server token and agent token are separate, stored in the password manager, and rotated with `k3s token rotate` per the runbook.
- MUST: `registries.yaml` points `ghcr.io` at a read-only token; images are referenced by digest.
- MUST: upgrades only through system-upgrade-controller Plans committed to git: servers one at a time with `concurrency: 1`, then agents; never skip a minor; the target is the pinned version, not a channel.

## Terraform
- MUST: all Cloudflare resources, and the VM provider's resources when it has a Terraform provider, come from Terraform in `deploy/terraform`; no console changes (drift is a bug). Runtime-owned exceptions, each managed by exactly one component: node count (capacity controller via `ProviderAdapter`), DNS A records for compute nodes and for tenants outside the default cell (worker), R2 temporary credentials (worker), Next.js web app deploys (wrangler, from a `workflow_dispatch` workflow). The Terraform state bucket is created once with `wrangler r2 bucket create` and then imported.
- MUST: providers pinned to an exact version; modules pinned to an exact version (the lock file doesn't cover modules); `.terraform.lock.hcl` committed with hashes for `linux_amd64`, `linux_arm64` and `windows_amd64` (`terraform providers lock -platform=…`), and its diffs reviewed.
- MUST: one reusable module per concern (`cloudflare-edge`, `r2`, `vm-<provider>`); environments (`global`, `c1`, …) only compose modules with variables. A new cell = a new env folder, no module changes.
- MUST: `terraform plan` output in the PR; applies run from a manually triggered workflow; state in R2; one apply at a time.
- MUST: state holds secrets in plaintext, so it lives only in the private R2 bucket; `sensitive = true` hides values from output but not from state. Prefer write-only arguments and ephemeral values for secrets; otherwise create the secret outside Terraform and keep it SOPS-encrypted in git.
- MUST: VM resources have `prevent_destroy`; CI fails any plan that would replace a storage node (that destroys its tenant volumes) or `control-1` (etcd, admin DB). Nodes are replaced only through the node-loss, evacuation or online-move runbooks.
- MUST: every variable has a description, a type and validation where values are constrained; no defaults for secrets; every module has a header comment with inputs, outputs and what it doesn't manage; every resource that supports tags/labels carries `dbcloud-env`, `dbcloud-cell`.
- MUST: `terraform fmt -check`, `terraform validate` and Trivy's misconfiguration scan pass in CI; findings are fixed or ignored inline with a reason.

## Secrets (SOPS + age)
- MUST: every Kubernetes `Secret` in git is SOPS-encrypted with the cell's age public key (`.sops.yaml` rules by path); CI fails on any unencrypted `Secret` or `stringData` in `deploy/`.
- MUST: the cell's age private key exists only as the `sops-age` Secret in `flux-system` and in the password manager (offline backup); it is never in git, CI, or a developer's shell history.
- MUST: each secret is readable only by the ServiceAccount that uses it (RBAC `get` on that name only).

## Kubernetes manifests (Flux, Helm values, platform objects)
- MUST: HelmReleases pin chart versions and image digests; Renovate proposes bumps. Every Helm value that differs from the chart default has a comment explaining why.
- MUST: every platform container has CPU and memory requests, a memory limit equal to its request, no CPU limit, and matches the platform footprint table in `plan/03-cost-model.md` §6; changing one updates the table. (Tenant Postgres containers are Burstable inside their range, `40-kubernetes-operator.md`.)
- MUST: every stateless platform Deployment runs 1 replica on the launch cell; when HA arrives (deferred, `plan/04-build-plan.md`), 2 replicas with `topologySpreadConstraints` across the control nodes and a PodDisruptionBudget `minAvailable: 1`.
- MUST: Pod Security `restricted` for everything except the documented exemptions:
  - **Exempt:** K3s components in `kube-system`; the LINSTOR satellites and CSI node plugin in `piraeus-datastore`; pg-gateway's `hostPort` in `dbcloud-edge`; the compute governor's cgroup access in `dbcloud-governor`; node-exporter and Fluent Bit in `observability`.
  - **Everything else:** `runAsNonRoot`, `allowPrivilegeEscalation: false`, `capabilities: drop: [ALL]`, `seccompProfile: RuntimeDefault`, `readOnlyRootFilesystem: true` where the app allows it.
  - **Admission policies:** a ValidatingAdmissionPolicy allows `hostPort` only for the pg-gateway DaemonSet, and keeps tenant pods on `dbcloud.io/pool=compute`.
- MUST: `automountServiceAccountToken: false` unless the pod calls the Kubernetes API; each workload has its own ServiceAccount.
- MUST: RBAC: namespaced Roles over ClusterRoles; no wildcards; treat `list/watch` on Secrets, workload creation, `nodes/proxy`, `escalate`, `bind`, `impersonate`, `serviceaccounts/token` and webhook configuration as privileged and justify each in a comment.
- MUST: every namespace has default-deny ingress and egress NetworkPolicies with explicit allows, including DNS egress; internet egress uses `ipBlock` `0.0.0.0/0` with `except` for private, link-local and metadata ranges.
- MUST: images referenced by digest only; `imagePullPolicy: IfNotPresent`.
- MUST: per-cell overlays in `deploy/flux/clusters/<cell>`; shared bases in `infrastructure/` and `apps/`.
- MUST: tenant resources are never in git; they come only from the control plane.
- MUST: manifests pass `kubeconform` against the pinned Kubernetes version and CRD schemas.
