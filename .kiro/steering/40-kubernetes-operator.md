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
- MUST: leader election on (2 replicas, spread across nodes); `MaxConcurrentReconciles` set explicitly; each reconcile has a timeout.
- MUST: the operator never deletes a PVC or a backup unless the `TenantDatabase` is in `deletion_pending` past its grace period and the purge was authorized (PDF §9.1).
- MUST: static stability is rendered, not hoped for (plan/01 §9): `instances: 1` clusters get `spec.probes.liveness.isolationCheck.enabled: false`; `tenant-local` pods tolerate `node.kubernetes.io/unreachable` and `not-ready` without `tolerationSeconds`.
- MUST: node maintenance is rendered too (plan/01 §12.3): during an announced OS-reboot window the operator sets each affected single-instance cluster's `nodeMaintenanceWindow` (`inProgress: true`, `reusePVC: true`) and clears it afterwards; node retirement uses evacuation (replica → switchover), never PVC deletion.
- MUST: render exactly the per-tenant footprint in `plan/03-cost-model.md` §5: Postgres container CPU request = the plan's guaranteed vCPU and limit = its burst, one `Pooler` (transaction mode) with resources on its `pgbouncer` container, and the Barman sidecar's resources and `retentionPolicyIntervalSeconds: 21600` on the `ObjectStore`. Memory request = limit on every container.
- MUST: tenant pods are scheduled only to nodes labelled `dbcloud.io/pool=tenant` (never `cp-1`); storage class `tenant-local` (TopoLVM) only.
- MUST: render Postgres parameters from the plan catalog (never from free-form input) and the weekly `ScheduledBackup` at the tenant's assigned slot.
- MUST: rendered pods meet Pod Security `restricted` (non-root, `allowPrivilegeEscalation: false`, capabilities dropped, `seccompProfile: RuntimeDefault`), don't automount service-account tokens unless CNPG needs them, and use images by digest from the `ClusterImageCatalog`.
- MUST: every tenant namespace gets default-deny ingress and egress NetworkPolicies plus the explicit allows in plan/01 §6.1, including DNS egress; R2 egress uses `ipBlock` `0.0.0.0/0` with `except` for private, link-local and metadata ranges (169.254.0.0/16), so no tenant pod can reach a cloud metadata endpoint or another node's private services.
- MUST: RBAC markers grant only the verbs and resources the controller uses; no wildcards; no `list/watch` on Secrets cluster-wide (use namespaced caches for the tenant namespaces).
- MUST: nothing in the operator depends on a VM provider; node facts come from Kubernetes node objects and labels only.
- MUST: renderers are pure functions (`TenantDatabase` → objects) with golden-file unit tests; envtest covers reconcile loops; Chainsaw covers lifecycle e2e on k3d with the pinned K3s version.
- SHOULD: emit Kubernetes Events for user-visible transitions (Provisioned, Suspended, BackupFailed, MaintenanceScheduled) with stable reasons.
