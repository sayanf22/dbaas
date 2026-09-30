//! pg-gateway: the Postgres TLS/SNI router on every tenant node (plan/01 §8.2).
//!
//! Step 0.1 skeleton: it only starts, logs JSON, and shuts down cleanly on
//! SIGTERM or Ctrl-C through the same cancellation path the real listener will
//! use. The Postgres protocol handling (`SSLRequest`, SNI routing, cancel keys,
//! relay) arrives in build-plan Step 0.5; nothing here accepts connections yet.

#![forbid(unsafe_code)]

mod shutdown;

use tokio_util::sync::CancellationToken;
use tracing_subscriber::EnvFilter;

/// Entry point: installs JSON logging, waits for a shutdown signal, exits 0.
///
/// # Errors
///
/// Returns an error when the signal handlers can't be installed.
#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .json()
        .with_env_filter(
            EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("info")),
        )
        .with_current_span(false)
        .init();

    let cancel = CancellationToken::new();
    tracing::info!(version = env!("CARGO_PKG_VERSION"), "pg-gateway starting");
    shutdown::wait_for_signal(cancel.clone()).await?;
    tracing::info!("pg-gateway stopped");
    Ok(())
}
