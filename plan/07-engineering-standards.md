# 07 — Engineering and Security Standards (Blueprint 4.0)

These standards keep the codebase small, secure, reliable, provider-neutral and able to grow from one cell to many without rewrites. The detailed rules are **Kiro steering files**, so they are applied automatically whenever code is written or reviewed in this workspace. Every rule was checked against the sources listed at the end (2026-09-29).

| Steering file | Loaded | Covers |
|---|---|---|
| `.kiro/steering/00-project-context.md` | always | what runs where, components, languages, invariants (incl. the spend cap), steering index |
| `.kiro/steering/10-engineering-standards.md` | always | minimal code, comments, scale, resource budgets, reliability, security summary, observability, data/API, testing, change hygiene |
| `.kiro/steering/15-security-and-reliability.md` | always | threat model, tenant isolation, tokens, secrets, input handling, crypto/TLS, deadlines/retries/idempotency/static stability, logging and audit, supply chain, review gates |
| `.kiro/steering/20-go.md` | `**/*.go` | layout, errors, context, concurrency, pgx through Supavisor, HTTP timeouts, gosec rules, tests, doc-comment template |
| `.kiro/steering/30-rust.md` | `gateway/**` | crate hygiene, no-panic and checked arithmetic, cancel safety, bounded resources, protocol correctness (cancel keys, SCRAM, readiness), rustls, tests, rustdoc template |
| `.kiro/steering/35-sql.md` | `db/**` | parameterized queries, per-service roles, `search_path`, RLS, `SECURITY DEFINER`, timeouts, safe goose migrations |
| `.kiro/steering/40-kubernetes-operator.md` | `**/*operator/**` (operator packages and `cmd/tenant-operator`) | reconcile rules, SSA, conditions, finalizers, rendered static stability, per-tenant footprint, restricted pods, RBAC, tests |
| `.kiro/steering/50-web.md` | `web/**` | Next.js 16 static export rules (no server features, query-param resource pages, webpack production builds), generated client only, no secrets, XSS-safe rendering, hash-based CSP in `_headers` + Trusted Types, session handling, CORS, accessibility, pnpm supply chain |
| `.kiro/steering/55-containers.md` | `**/*Dockerfile*` | multi-stage, distroless, digest pins, non-root, BuildKit secrets, native multi-arch builds, SBOM and signing |
| `.kiro/steering/60-infrastructure.md` | `deploy/**` | spend cap, provider neutrality, node bootstrap and host hardening, K3s configuration (CIS hardening, etcd snapshots, upgrades), Terraform modules/state/pinning, SOPS secrets, Kubernetes manifest hardening, Flux/Helm |
| `.kiro/steering/70-ci.md` | `.github/**` | SHA-pinned actions, least-privilege tokens, injection-safe workflows, GitHub Free compensating controls (manual deploy workflows, secret scanning with Trivy), free-minute budget, pipeline content |
| `.kiro/steering/75-shell.md` | `**/*.sh` | strict mode, ShellCheck, quoting, no `eval`, idempotent scripts |

## Principles

1. **Only what the step needs.** No speculative features, flags or abstractions; no dead code.
2. **Everything commented for the next engineer.** Every package, type and function has a doc comment; inline comments explain why, invariants and limits.
3. **Stateless services, bounded resources.** State lives in the admin DB or Kubernetes; every pool, queue and fan-out has a limit; every pod fits the footprint in `03-…`.
4. **Idempotent and retry-safe.** `Idempotency-Key` on writes, unique River jobs, provider state checked before retries, one retry layer with backoff and jitter.
5. **Statically stable.** Control-plane, Supabase or Cloudflare outages never touch running databases.
6. **Secure by default.** Tenants are hostile to each other; authorization on every object, least privilege, no secrets in logs, SSRF-guarded outbound fetches, pinned and scanned dependencies.
7. **Observable.** Structured logs with tenant/cell/operation IDs, RED metrics, traces, an alert and runbook for every failure mode.
8. **Tested where it breaks.** Failure paths, fuzzed parsers, cross-tenant tests, real dependencies in integration tests.
9. **Spend follows revenue.** Nothing billable is created unless the plan names it; the platform stays ≤ ₹9,000/month.
10. **Provider-neutral.** No code or manifest depends on one VM provider; provider specifics live only in `deploy/terraform/modules/vm-<provider>` and the `ProviderAdapter`.

## Pull-request review checklist

- [ ] Implements the named build-plan step and nothing extra; its "Done when" is demonstrated.
- [ ] No dead code, unused config or new dependency without a justification note.
- [ ] Doc comments on every new package/type/function; constants have units and sources.
- [ ] Tenant/org authorization checked before any object is returned or changed; cross-tenant tests added.
- [ ] Input decoded into typed, size-bounded structures; unknown fields rejected on mutations.
- [ ] Every network call has a timeout; retries only for idempotent operations; degraded path tested.
- [ ] New queries are sqlc, parameterized, indexed, paginated; `EXPLAIN` attached; transaction-pooler rules respected.
- [ ] Migrations are expand/contract, N-1 compatible, with `lock_timeout`.
- [ ] Logs/metrics/traces added; no secrets, bodies or unbounded labels; alert + runbook for new failure modes.
- [ ] Tests for success and failure paths; fuzz target for new parsers; bug fixes include a regression test.
- [ ] Linters and scanners clean (golangci-lint/gosec, clippy, cargo-deny, govulncheck, ESLint, hadolint, ShellCheck, actionlint, zizmor, Trivy); suppressions carry a reason.
- [ ] Resource requests, Supabase connections and recurring cost unchanged, or the tables in `01-…`/`03-…` are updated in this PR.
- [ ] Security-sensitive change (auth, gateway handshake, SQL safety, webhooks, crypto, IAM/RBAC) has a second reviewer.
- [ ] `plan/` updated if a version, limit or decision changed.

## Sources used for these rules (checked 2026-09-29)

- Go: https://go.dev/doc/security/best-practices , https://pkg.go.dev/crypto/rand , https://pkg.go.dev/context , https://github.com/securego/gosec/blob/master/RULES.md , https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/ , https://go.dev/doc/modules/layout , https://go.dev/wiki/CodeReviewComments , https://go.dev/doc/comment , https://google.github.io/styleguide/go/best-practices , https://github.com/uber-go/guide/blob/master/style.md , https://pkg.go.dev/github.com/jackc/pgx/v5
- Rust: https://anssi-fr.github.io/rust-guide/checklist.html , https://anssi-fr.github.io/rust-guide/integer.html , https://rustsec.org/ , https://embarkstudios.github.io/cargo-deny/checks/index.html , https://doc.rust-lang.org/clippy/lints.html , https://docs.rs/tokio/latest/tokio/macro.select.html , https://tokio.rs/tokio/topics/shutdown , https://docs.rs/rustls/latest/rustls/client/danger/index.html , https://rust-lang.github.io/api-guidelines/checklist.html
- Web: https://cheatsheetseries.owasp.org/cheatsheets/Cross_Site_Scripting_Prevention_Cheat_Sheet.html , https://cheatsheetseries.owasp.org/cheatsheets/Content_Security_Policy_Cheat_Sheet.html , https://cheatsheetseries.owasp.org/cheatsheets/HTML5_Security_Cheat_Sheet.html , https://react.dev/reference/react-dom/components/common , https://www.rfc-editor.org/rfc/rfc10017.html , https://pnpm.io/supply-chain-security
- SQL/PostgreSQL: https://cheatsheetseries.owasp.org/cheatsheets/SQL_Injection_Prevention_Cheat_Sheet.html , https://www.postgresql.org/docs/current/ddl-schemas.html , https://wiki.postgresql.org/wiki/A_Guide_to_CVE-2018-1058:_Protect_Your_Search_Path , https://www.postgresql.org/docs/current/sql-createfunction.html , https://www.postgresql.org/docs/current/ddl-rowsecurity.html , https://www.postgresql.org/docs/current/runtime-config-client.html , https://www.postgresql.org/docs/current/sql-createindex.html
- Terraform: https://developer.hashicorp.com/terraform/language/manage-sensitive-data , https://developer.hashicorp.com/terraform/language/files/dependency-lock , https://developer.hashicorp.com/terraform/cli/commands/providers/lock , https://github.com/aquasecurity/tfsec
- Kubernetes: https://kubernetes.io/docs/concepts/security/pod-security-standards/ , https://kubernetes.io/docs/concepts/security/rbac-good-practices/ , https://kubernetes.io/docs/concepts/services-networking/network-policies/ , https://kubernetes.io/docs/concepts/security/secrets-good-practices/ , https://media.defense.gov/2022/Aug/29/2003066362/-1/-1/0/CTR_KUBERNETES_HARDENING_GUIDANCE_1.2_20220829.PDF , https://book.kubebuilder.io/reference/good-practices , https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md
- K3s and hosts: https://docs.k3s.io/security/hardening-guide , https://docs.k3s.io/security/self-assessment-1.12 , https://docs.k3s.io/security/secrets-encryption , https://docs.k3s.io/cli/token , https://fluxcd.io/flux/guides/mozilla-sops/
- Containers: https://docs.docker.com/build/building/best-practices/ , https://docs.docker.com/build/building/secrets/ , https://github.com/hadolint/hadolint , https://github.com/docker/docker-bench-security
- CI: https://docs.github.com/en/actions/reference/security/secure-use , https://docs.github.com/en/actions/concepts/security/openid-connect , https://github.com/ossf/scorecard/blob/main/docs/checks.md , https://docs.zizmor.sh/audits/ , https://slsa.dev/spec/
- Shell: https://google.github.io/styleguide/shellguide.html , https://github.com/koalaman/shellcheck/wiki/SC2086
- Reliability: https://sre.google/sre-book/addressing-cascading-failures/ , https://docs.aws.amazon.com/wellarchitected/latest/reliability-pillar/rel_mitigate_interaction_failure_limit_retries.html , https://docs.aws.amazon.com/wellarchitected/latest/reliability-pillar/rel_mitigate_interaction_failure_client_timeouts.html , https://docs.aws.amazon.com/wellarchitected/latest/reliability-pillar/rel_prevent_interaction_failure_idempotent.html , https://kubernetes.io/docs/concepts/configuration/liveness-readiness-startup-probes/ , https://opentelemetry.io/docs/specs/semconv/database/database-spans/
- API and security: https://www.rfc-editor.org/rfc/rfc9457 , https://owasp.org/API-Security/editions/2023/en/0x11-t10/ , https://github.com/OWASP/ASVS

Content from these sources was rephrased for compliance with licensing restrictions.
