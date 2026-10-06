---
inclusion: fileMatch
fileMatchPattern: '**/*operator/**'
---

# Operator rules (tenant-operator, controller-runtime v0.24, on K3s)

- MUST: reconcile is level-based and idempotent. It reads the current state, computes the desired objects and applies them. Never branch on the event type.
- MUST: apply children with server-side apply and field owner `tenant-operator`; set controller owner references; watch children with `Owns()`.
- MUST: status uses `[]metav1.Condition` (`Ready` summary + specific types), each with `observedGeneration`, a CamelCase `Reason` and a human message; `status.observedGeneration` is updated. No `phase` enums.
- MUST: write status only when it changed; use `GenerationChangedPredicate` where only spec changes matter; never mutate the object's own spec.
- MUST: return an error for transient failures (rate-limited retry), `reconcile.TerminalError` for failures that won't succeed on retry, and `RequeueAfter` for polling external progress (CNPG readiness, backups).
- MUST: finalizers guard external cleanup (routes, R2 prefix retention, DNS). Cleanup is idempotent, and the finalizer is removed only after cleanup succeeds.
- MUST: leader election on, even with 1 replica: the launch cell runs 1, and HA (deferred) adds a second, spread across nodes, with no code change; `MaxConcurrentReconciles` set explicitly; each reconcile has a timeout.
- MUST: the operator never deletes a PVC or a backup unless the `TenantDatabase` is in `deletion_pending` past its grace period and the purge was authorized (PDF §9.1).
- MUST: static stability is rendered, not hoped for (plan/01 §9): `instances: 1` clusters get `spec.probes.liveness.isolationCheck.enabled: false`; tenant pods tolerate `node.kubernetes.io/unreachable` and `not-ready` without `tolerationSeconds`, so only the fencer moves them, after a confirmed power-off (plan/01 §7.5).
- MUST: node maintenance is rendered too (plan/01 §12.3): during an announced compute-node reboot window with no second compute node, the operator sets each affected single-instance cluster's `nodeMaintenanceWindow` (`inProgress: true`, `reusePVC: true`) and clears it afterwards; with a second compute node, tenants move there first. Node retirement never deletes a PVC; storage-node retirement uses online volume moves.
- MUST: render exactly the per-tenant footprint in `plan/03-cost-model.md` §5 from the user's range inside the plan caps (ADR-030):
  - **Postgres container:** CPU request = range min, limit = range max; memory request = range min, limit = range max (Burstable).
  - **Fixed containers:** the Barman sidecar and the `pgbouncer` container of the single `Pooler` (transaction mode) keep memory request = limit.
  - **`ObjectStore`:** sidecar resources and `retentionPolicyIntervalSeconds: 21600`.
  - **Range limits:** a range outside the plan caps is a `TerminalError`.
- MUST: never patch CNPG resources for autoscaling. Inside the range the compute governor steers memory; a range change is a rare, announced, restart-based reconcile (CNPG 1.30 restarts on any `.spec.resources` change).
- MUST: tenant pods are scheduled only to nodes labelled `dbcloud.io/pool=compute` (never `control-1` or a storage node), with priority class `dbcloud-tenant`; PVCs use storage class `tenant-network` only, with the size and the plan cap from the spec.
- MUST: render Postgres parameters from the plan catalog and the range, never from free-form input: `shared_buffers` from the range min, `effective_cache_size` from the max, `max_slot_wal_keep_size` per plan. Also render the WAL receiver's replication slot and access (plan/01 §7.4) and the weekly `ScheduledBackup` at the tenant's assigned slot.
- MUST: rendered pods meet Pod Security `restricted` (non-root, `allowPrivilegeEscalation: false`, capabilities dropped, `seccompProfile: RuntimeDefault`), don't automount service-account tokens unless CNPG needs them, and use images by digest from the `ClusterImageCatalog`.
- MUST: every tenant namespace gets default-deny ingress and egress NetworkPolicies plus the explicit allows in plan/01 §6.1 (including the WAL receiver's replication access and DNS egress); R2 egress uses `ipBlock` `0.0.0.0/0` with `except` for private, link-local and metadata ranges (169.254.0.0/16), so no tenant pod can reach a cloud metadata endpoint or another node's private services.
- MUST: RBAC markers grant only the verbs and resources the controller uses; no wildcards; no `list/watch` on Secrets cluster-wide (use namespaced caches for the tenant namespaces).
- MUST: nothing in the operator depends on a VM provider; node facts come from Kubernetes node objects and labels only.
- MUST: renderers are pure functions (`TenantDatabase` → objects) with golden-file unit tests; envtest covers reconcile loops; Chainsaw covers lifecycle e2e on k3d with the pinned K3s version; storage behaviour (expansion, reattach after fencing, moves) is tested on the QEMU/KVM test bed, because k3d's `tenant-network` is a local-path stand-in (plan/04 Step 0.3b).
- SHOULD: emit Kubernetes Events for user-visible transitions (Provisioned, Suspended, BackupFailed, MaintenanceScheduled) with stable reasons.
