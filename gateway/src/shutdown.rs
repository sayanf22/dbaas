//! Shutdown signalling.
//!
//! Turns SIGTERM (Kubernetes pod deletion) or Ctrl-C into a cancelled
//! [`CancellationToken`]. Every task watches that token; the listener stops
//! accepting and drains under a deadline (30-rust.md "graceful shutdown").

use tokio::signal::unix::{SignalKind, signal};
use tokio_util::sync::CancellationToken;

/// Waits for SIGTERM or SIGINT, then cancels `cancel`.
///
/// Returns early without error if `cancel` is cancelled by someone else. The
/// `select!` is `biased` so an already cancelled token wins over a pending
/// signal. It runs once, not in a loop, so dropping the losing branches
/// loses no state.
///
/// # Errors
///
/// Returns an error if the SIGTERM handler can't be registered, or if
/// listening for Ctrl-C fails.
pub async fn wait_for_signal(cancel: CancellationToken) -> std::io::Result<()> {
    let mut term = signal(SignalKind::terminate())?;
    tokio::select! {
        biased;
        () = cancel.cancelled() => return Ok(()),
        _ = term.recv() => tracing::info!(signal = "SIGTERM", "shutdown requested"),
        res = tokio::signal::ctrl_c() => {
            res?;
            tracing::info!(signal = "SIGINT", "shutdown requested");
        }
    }
    cancel.cancel();
    Ok(())
}

/// Unit tests for the signal wait; they never send real signals to the test process.
#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;

    /// An externally cancelled token ends the wait without a signal.
    #[tokio::test]
    async fn returns_when_token_cancelled() {
        let cancel = CancellationToken::new();
        cancel.cancel();
        let res =
            tokio::time::timeout(Duration::from_secs(1), wait_for_signal(cancel.clone())).await;
        assert!(
            matches!(res, Ok(Ok(()))),
            "wait_for_signal did not return after cancel: {res:?}"
        );
    }
}
