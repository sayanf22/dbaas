---
inclusion: fileMatch
fileMatchPattern: 'gateway/**'
---

# Rust rules (pg-gateway)

`pg-gateway` sits on every customer connection. Correctness, bounded resources and graceful behaviour under load matter more than cleverness.

## Crate hygiene
- MUST: `#![forbid(unsafe_code)]` in every crate root (this also rejects `no_mangle`, `export_name` and `link_section`).
- MUST: `cargo clippy -- -D warnings` with `clippy::pedantic` enabled and these denied: `unwrap_used`, `expect_used`, `panic`, `indexing_slicing`, `arithmetic_side_effects`, `todo`, `unimplemented`, `dbg_macro`, `print_stdout`, `print_stderr`. A local `#[allow(...)]` needs a `// reason:` comment.
- MUST: `Cargo.lock` committed; `cargo-deny` (advisories incl. unmaintained and yanked, licenses, bans incl. duplicates, sources) and `cargo-audit` pass; MSRV pinned in `Cargo.toml` (`rust-version`); release binaries built with `cargo auditable`.
- MUST: stable toolchain and tier-1 targets only (`aarch64-unknown-linux-gnu`, `x86_64-unknown-linux-gnu`); no overridden compiler flags or env vars beyond those in `Cargo.toml`.
- MUST: edition 2024, `cargo fmt` clean; automatic clippy fixes are reviewed by hand.
- MUST: `[profile.release] overflow-checks = true`, so an arithmetic bug fails loudly instead of wrapping.

## Errors and panics
- MUST: library modules define typed errors with `thiserror`; `anyhow` only in `main.rs`/startup.
- MUST: every error that reaches a client becomes a proper Postgres `ErrorResponse` with the documented SQLSTATE (`57P03`, `53300`, `28000`, `08004`, `08P01`). Internal details go to logs, not to the client.
- MUST: no panics on input: use `.get()` and explicit bounds checks instead of indexing, `checked_*`/`saturating_*` arithmetic for lengths, counters and offsets, and `TryFrom` for integer conversions.
- MUST: never panic in `Drop`; don't rely on `Drop` alone for security-relevant cleanup; no `mem::forget` or `Box::leak`.
- MUST: a panic in one connection task can't take down the process: tasks are spawned under the `TaskTracker`, and the panic is logged and counted.

## Async and resources
- MUST: never block the runtime: no `std::fs`, blocking DNS or CPU-heavy work in async code; use `spawn_blocking` for the rare exception.
- MUST: every accept, TLS handshake, upstream connect and idle period has a timeout (`tokio::time::timeout`); limits are constants with a unit and a source comment.
- MUST: bounded everything: a global `Semaphore` for connections, per-tenant permits, bounded `mpsc` channels, capped buffers when parsing startup packets (reject packets over 10 KiB with `08P01`).
- MUST: `select!` loops use only cancel-safe futures (`mpsc::Receiver::recv`, `TcpListener::accept`, `read`, `write`, `StreamExt::next`). `read_exact`, `read_to_end`, `write_all`, `Mutex::lock` and `Semaphore::acquire` are not cancel-safe: run them outside `select!` or in their own task. With `biased;`, the shutdown branch comes first; a timeout `sleep` is pinned outside the loop.
- MUST: graceful shutdown: signal → `CancellationToken` → stop accepting → `TaskTracker::wait` with a drain deadline → exit.
- MUST: after the handshake, relay with `tokio::io::copy_bidirectional` (no per-byte allocation, no parsing of the data stream).
- MUST: shared route state is read lock-free (`arc_swap::ArcSwap`); updates replace the whole map atomically.
- MUST: every `Send`/`Sync` bound we rely on is satisfied by the types themselves; no manual `unsafe impl`.

## Protocol correctness
- MUST: after answering `SSLRequest` with a single `S`, reject any bytes the client sent before the TLS handshake (CVE-2021-23222 class of bugs).
- MUST: direct TLS connections must negotiate ALPN `postgresql`; others are rejected.
- MUST: plaintext is refused except `CancelRequest`, which libpq sends unencrypted.
- MUST: tenant resolution order is SNI, then `options=endpoint=<ref>`; a missing or unknown tenant yields `08004` without revealing which tenants exist.
- MUST: cancel keys handed to clients encode the issuing replica and an HMAC (plan/01 §8.2); they never expose the upstream PID/secret. A cancel for another replica is verified (constant-time HMAC check) before it is forwarded over internal mTLS.
- MUST: always remove `SCRAM-SHA-256-PLUS` from the upstream mechanism list; channel binding can't survive TLS termination at the gateway.
- MUST: pooled hostnames go to the tenant's PgBouncer pod, `-direct` hostnames to the Postgres primary pod, both taken from EndpointSlices.
- MUST: readiness requires a complete route list (from the watch or a verified peer snapshot); liveness never checks the Kubernetes API.

## TLS (rustls)
- MUST: server config uses rustls defaults (TLS 1.2 + 1.3, safe cipher suites) with the `aws-lc-rs` or `ring` provider pinned in `Cargo.toml`.
- MUST: anything under `rustls::client::danger` or a custom certificate verifier is a security-review gate and needs a `// reason:` comment linking the review.
- MUST: certificates are hot-reloaded from the mounted secret without dropping existing connections.

## Observability
- MUST: `tracing` spans per connection with `tenant_id`, `cell_id`, `client_ip`, `phase`; never log passwords, startup `options` values beyond the endpoint, or query text.
- MUST: Prometheus metrics: active/total connections, handshake durations, rejections by reason, upstream connect errors. Per-tenant series are capped and documented.

## Testing
- MUST: unit tests for every parser and state transition; `cargo-fuzz` targets for the startup/SSLRequest/cancel parsers; `proptest` for route resolution.
- MUST: an integration suite runs the client matrix (psql 14/16/18, pgx, node-postgres, pgjdbc, psycopg 3, asyncpg) against a k3d cell running the pinned K3s version, on both hostnames, including failover between two node addresses.

## Doc comment template

```rust
//! Tenant resolution for incoming connections.
//!
//! Maps a TLS SNI name or an `options=endpoint=<ref>` startup parameter to a
//! tenant route. This module never performs I/O; routes are provided by
//! `routes::RouteTable`, which is fed by the Kubernetes watch.

/// Maximum SNI length accepted, in bytes (RFC 6066 limits a DNS name to 255).
const MAX_SNI_LEN: usize = 255;

/// Resolves the tenant for a connection.
///
/// SNI is preferred over the `options` fallback because the TLS handshake
/// binds it to the certificate check, while `options` is free-form client
/// input. When both are present they must name the same tenant.
///
/// # Errors
///
/// Returns [`ResolveError::UnknownTenant`] when neither source names a known
/// tenant, and [`ResolveError::Ambiguous`] when SNI and `options` disagree.
pub fn resolve(sni: Option<&str>, options: &StartupOptions, table: &RouteTable) -> Result<Route, ResolveError> {
    // ...
}
```
