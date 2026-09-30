---
inclusion: always
---

# Security and reliability baseline (all code, all languages)

These rules apply everywhere. Language files (`20-go.md`, `30-rust.md`, `35-sql.md`, `50-web.md`, `55-containers.md`, `60-infrastructure.md`, `70-ci.md`, `75-shell.md`) add the language-specific checks. If a rule here can't be met, stop and raise it; never weaken it quietly.

## 1. Threat model in one paragraph
Tenants are mutually untrusted and share nodes; the VMs sit on a provider network we don't control, with a public Postgres port. Anything a customer sends (SQL, startup packets, API bodies, webhook payloads, CIMD documents, MCP tool arguments, data stored in their database) is hostile input. The gateway, control-api, mcp-server and mcp-auth are internet-facing. A bug must not let one tenant read, change, slow down or bill another tenant, and must not expose VM, R2, Supabase, Cloudflare or Kubernetes credentials.

## 2. Tenant isolation
- MUST: every query, API handler, job and MCP tool scopes by `org_id`/`tenant_id` taken from the authenticated principal, never from the request body alone.
- MUST: cross-tenant tests accompany every new endpoint or tool (read, write, list, delete, metrics).
- MUST: per-tenant limits (connections, rate, CPU, memory, storage, backup size) are enforced server-side; clients are never trusted to self-limit.
- MUST: metrics and logs queries on behalf of a customer fix the tenant label server-side.

## 3. Authentication and tokens
- MUST: verify JWTs fully: signature against the pinned JWKS, `alg` from an allow-list (ES256), `iss`, `aud`, `exp`, `nbf`, and clock skew ≤ 60 s. Reject `none` and unexpected key IDs. Cache JWKS with a bounded TTL and refresh on unknown `kid` at most once per minute.
- MUST: API keys and refresh tokens are stored only as hashes (argon2id for API keys, SHA-256 for high-entropy random tokens); compare in constant time.
- MUST: webhooks (Razorpay) are verified with HMAC-SHA256 over the raw body in constant time before parsing, then deduplicated by event ID.
- MUST: tokens, passwords, connection strings and keys never appear in URLs, logs, errors, metrics or traces.

## 4. Secrets and keys
- MUST: secrets reach the cell only as SOPS-encrypted files in git, decrypted by Flux with the cell's age key (K3s encrypts them at rest), and reach CI only as GitHub Actions secrets used by the workflows that need them (deployment environments aren't available for private repos on GitHub Free). Never plaintext in git, images, Terraform variables with defaults, ConfigMaps or VM user data beyond the bootstrap tokens.
- MUST: no workload uses a provider's instance metadata or instance credentials; NetworkPolicy blocks 169.254.0.0/16 for every pod.
- MUST: each secret has one owner component and the narrowest scope (R2 token per bucket/prefix, Cloudflare token per API, Supabase role per service).
- MUST: rotation is create-before-revoke and has a runbook; a leaked secret is rotated, not just deleted.
- MUST: generate secrets with a CSPRNG and at least 128 bits of entropy.

## 5. Input handling
- MUST: parse, don't validate-and-pass: decode into typed structures with size limits (HTTP body ≤ 1 MiB unless the endpoint documents more; Postgres startup packet ≤ 10 KiB), reject unknown fields on mutating requests, and bound every string, list and number.
- MUST: identifiers used in SQL, shell commands, file paths or URLs come from allow-lists or typed enums, never from raw input.
- MUST: outbound requests to user-controlled URLs go through the SSRF-guarded client (https only, public IPs only after DNS resolution, no redirects to private ranges, size and time limits).
- MUST: data read from tenant databases is returned to AI tools as untrusted data, never as instructions.

## 6. Cryptography and TLS
- MUST: use the standard library or the pinned libraries (Go `crypto/*`, `rustls`); no custom crypto, no MD5/SHA-1/DES/RC4 for security purposes.
- MUST: traffic between nodes is encrypted (flannel `wireguard-native`); nothing assumes the provider's private network is trusted.
- MUST: TLS 1.2+ everywhere; certificate verification is never disabled (no `InsecureSkipVerify`, no custom `ServerCertVerifier` without a security review note).
- MUST: connections to Supabase use `sslmode=verify-full` with Supabase's CA.

## 7. Reliability patterns
- MUST: set a deadline at the edge (HTTP request, job, connection handshake) and pass it down; every hop subtracts its own budget.
- MUST: retries: idempotent operations only, exponential backoff with full jitter, max attempts, one layer only, inside a retry budget.
- MUST: idempotency: every mutating API takes `Idempotency-Key`; every River job is unique by its key and safe to re-run after a crash.
- MUST: static stability: data-plane paths (gateway, Postgres, PgBouncer, backups) never call Supabase, Cloudflare APIs or the control plane synchronously.
- MUST: degrade explicitly: when a dependency is down, return `503` + `Retry-After`, alert, and keep serving what doesn't need it. The degraded path is covered by a test.
- MUST: graceful shutdown and bounded drain on SIGTERM; readiness reflects the ability to serve now; liveness never depends on another service.

## 8. Logging, audit and privacy
- MUST: structured logs with redaction at the logger for known secret fields; no request/response bodies, SQL text with literals, or customer row data in logs.
- MUST: every privileged or mutating action writes an `audit_events` row (who, what, target, request ID) in the same transaction as the change.
- MUST: personal data (emails, names, IPs) is logged only where needed for security, and never in metrics labels.

## 9. Dependencies and supply chain
- MUST: every dependency is pinned (lockfiles committed, images by digest, actions by commit SHA) and scanned in CI (govulncheck, cargo-audit/cargo-deny, pnpm audit via Trivy, Trivy for images and IaC).
- MUST: Trivy's secret scanner runs in CI and as a pre-commit hook; a finding blocks the change and the secret is rotated (GitHub secret scanning isn't available for private repos on the Free plan).
- MUST: a new dependency has a PR note: purpose, licence, maintenance status, amd64 + arm64 support, and why the standard library or an existing dependency isn't enough.
- MUST: no code or binaries fetched at runtime; no `curl | sh` in images or CI except the documented tool installers in `hack/install-tools.sh`, which verify checksums.

## 10. Review gates
- MUST: changes to authentication, authorization, the gateway handshake, SQL safety, webhook verification, crypto, RBAC, NetworkPolicy, host firewall rules or K3s hardening flags get a second reviewer and a note on what was tested.
- MUST: security findings are fixed or risk-accepted in writing with an owner and a date; nothing is silenced without a reason (`#nosec G### -- reason`, `#[allow(...)] // reason:`, `// eslint-disable-next-line <rule> -- reason`).
