---
inclusion: always
---

# Engineering standards (all code)

**MUST** = required for merge. **SHOULD** = default; deviations are explained in the PR.

## 1. Write only what is needed
- MUST: implement exactly what the current build-plan step asks for. No speculative features, flags, options, abstractions or "future" parameters.
- MUST: no dead code, commented-out code, unused functions, unused config keys or unused dependencies. Delete them; git keeps history.
- MUST: introduce an interface only at a real boundary (provider adapter, clock, external API) or when two implementations exist. Interfaces live with the consumer.
- SHOULD: prefer the standard library and the libraries already in `plan/02-technology-stack.md`. A new dependency needs a PR note: why, license, maintenance status, amd64 + arm64 support.

## 2. Comments and documentation
- MUST: every package/crate/module has a header comment stating its responsibility and what it deliberately does not do.
- MUST: every type, function and method, exported or not, has a doc comment: what it does, why it exists, its error/cancellation behaviour, and any concurrency or ordering expectations.
- MUST: inline comments explain **why**: invariants, protocol or security decisions, ordering constraints, non-obvious performance choices, units and limits behind every constant. Don't narrate obvious lines.
- MUST: every constant or magic number has a comment with its unit and source (for example, "125 s: Cloudflare proxy read timeout").
- MUST: TODOs reference an issue: `// TODO(#123): ...`. No bare TODOs; `todo!()`/`unimplemented!()` never ship.
- MUST: generated files keep their "Code generated … DO NOT EDIT." header and are never hand-edited.
- SHOULD: when code implements a plan decision, reference it (`// See plan/01-architecture.md §8.2`, `ADR-005`).

## 3. Scale without redesign
- MUST: services are stateless. Anything that must survive a restart lives in the admin DB (control plane) or in Kubernetes objects (cell).
- MUST: every record and log line that concerns a tenant carries `tenant_id` and `cell_id`. Code never assumes a single cell.
- MUST: list endpoints and list queries are paginated (cursor-based) with a max page size; no unbounded `SELECT`s or loops over all tenants in a request path. Fleet-wide work runs as batched River jobs.
- MUST: every new query is backed by an index; PRs that add queries include the `EXPLAIN` of the realistic case.
- MUST: bound all concurrency (worker pools, semaphores, pgx pool sizes, channel capacities) and apply backpressure instead of queueing without limit.
- MUST: respect the resource budgets in `plan/03-cost-model.md` §5–§6 (per-tenant pods and platform pods) and the Supabase connection budget in `plan/01-architecture.md` §10.2. A change that raises a request, adds a pod or adds a connection updates those tables in the same PR.
- MUST: IDs are UUIDv7 (time-ordered) unless a protocol dictates otherwise; tenant refs are opaque (`t-` + random base32).
- SHOULD: design every API and job so a second cell, a second region or ten times the tenants needs configuration, not code changes.

## 4. Reliability
- MUST: every network call has both a connect timeout and a request timeout; deadlines propagate (context in Go, `tokio::time::timeout` in Rust). Work whose deadline has already passed is dropped, not started.
- MUST: retries only for idempotent operations, with capped exponential backoff + jitter, at one layer only, and within a per-process retry budget. Permanent, malformed and permission errors are never retried.
- MUST: shed load before overload: bounded queues, `503` + `Retry-After` past the in-flight limit, and fail fast when a dependency is known to be down.
- MUST: mutating provider calls (VM provider API, Cloudflare, Razorpay) check current provider state before retrying (the `AMBIGUOUS` path in PDF §8.1).
- MUST: every mutating API accepts `Idempotency-Key`; every job is idempotent and unique by that key.
- MUST: graceful shutdown on SIGTERM: fail readiness → stop accepting → drain in-flight work → exit before `terminationGracePeriodSeconds`.
- MUST: readiness = "can serve now"; liveness only detects unrecoverable states (deadlock). Liveness never checks dependencies.
- MUST: dependencies (Supabase, the VM provider API, Cloudflare, the email service) are wrapped with timeouts and circuit breaking; when they are down, return `503` + `Retry-After` and keep the data plane untouched.
- MUST: time is UTC `time.Time`/`Duration` (Go) or `std::time`/`chrono` UTC (Rust). Never local time; never bare integers without a unit in the name.

## 5. Security (details in `15-security-and-reliability.md` and the per-language files)
- MUST: authorize every object access by tenant/org (OWASP API1). Loading an object by ID without checking the caller's org is a bug.
- MUST: validate all input at the boundary (OpenAPI schema + domain checks); reject unknown fields on mutating requests.
- MUST: SQL only through sqlc-generated queries (parameterized). The only exception is the MCP executor, which runs parsed, checked customer SQL under a constrained role.
- MUST: secrets never appear in git, logs, traces, errors, metrics labels or panic messages. Redact at the logger.
- MUST: outbound fetches of user-controlled URLs (CIMD documents, webhooks) go through the SSRF-guarded HTTP client: https only, public IPs only, size and time limits, no redirects to private ranges.
- MUST: least privilege everywhere (Kubernetes RBAC per service account, no instance credentials in workloads, scoped Cloudflare/R2/provider tokens, Supabase network restrictions).
- MUST: generate tokens and keys with a CSPRNG (`crypto/rand`, `rand::rngs::OsRng`); compare secrets in constant time.

## 6. Observability
- MUST: structured JSON logs with `request_id`, `trace_id`, `operation_id`, `tenant_id`, `cell_id` when known. Log an error once, where it is handled.
- MUST: RED metrics (rate, errors, duration) for every endpoint and job, labelled by bounded values only (never `tenant_id` on high-cardinality series in the control plane; the gateway's per-tenant series are capped and documented).
- MUST: OpenTelemetry spans around handlers, jobs and outbound calls; DB spans follow OTel DB semantic conventions (parameterized text only).
- MUST: every new failure mode has an alert or an explicit note saying why none is needed, and a runbook link.

## 7. Data and API
- MUST: admin-DB migrations are expand/contract and compatible with the previous release (N-1). Never rename or drop a column in the release that stops using it. Indexes are created `CONCURRENTLY`; migrations set `lock_timeout`.
- MUST: RLS stays enabled on every admin-DB table; the Supabase Data API roles get no grants in our schema.
- MUST: API changes start in `api/openapi.yaml`; `/v1` changes are additive only; errors use RFC 9457 problem details; async operations return `202` + `operation_id`.
- MUST: a state change and the River job that acts on it commit in the same transaction (`InsertTx`).

## 8. Testing
- MUST: unit tests for logic, integration tests against real dependencies (local Supabase stack / testcontainers, k3d running the pinned K3s version), e2e for user flows. Failure paths are tested, not just happy paths.
- MUST: parsers of untrusted input (Postgres startup packets, SQL, CIMD documents, webhooks) have fuzz targets.
- MUST: a bug fix ships with a test that fails without the fix.
- MUST: tests are deterministic: no sleeps for synchronization, injected clocks, isolated data per test.

## 9. Change hygiene
- MUST: Conventional Commits; small PRs; the PR description includes the build-plan step and its "Done when" checklist.
- MUST: CI green (lint, tests, race detector, clippy, cargo-deny, govulncheck, Trivy, hadolint, actionlint, zizmor, image builds for arm64 + amd64) before merge.
- MUST: a PR that adds recurring cost names the cost and the `plan/03-cost-model.md` §7 trigger it satisfies; otherwise it is rejected.
- MUST: when a version, limit or decision in `plan/` changes, update the plan file in the same PR.
