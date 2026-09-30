package platform

import (
	"net/http"
	"sync/atomic"
)

// Health serves the Kubernetes probe endpoints for one process.
//
// Liveness only says the process can answer HTTP; it never checks
// dependencies, so an outage of Supabase or Cloudflare can't make Kubernetes
// restart healthy pods (plan/07, 10-engineering-standards.md §4). Readiness
// means "can serve now" and is switched off first during shutdown. The zero
// value is not ready. Safe for concurrent use.
type Health struct {
	ready atomic.Bool
}

// SetReady switches the readiness endpoint between 200 and 503.
func (h *Health) SetReady(ready bool) { h.ready.Store(ready) }

// Ready reports the current readiness state.
func (h *Health) Ready() bool { return h.ready.Load() }

// Register adds GET /livez and GET /readyz to mux.
func (h *Health) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, _ *http.Request) {
		writeProbe(w, http.StatusOK, "ok")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if h.Ready() {
			writeProbe(w, http.StatusOK, "ready")
			return
		}
		writeProbe(w, http.StatusServiceUnavailable, "not ready")
	})
}

// writeProbe writes a short plain-text probe answer. A write error means the
// probe client has gone; the status code is already sent, so there is nothing
// left to do and nobody to report it to.
func writeProbe(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if _, err := w.Write([]byte(body + "\n")); err != nil {
		return
	}
}
