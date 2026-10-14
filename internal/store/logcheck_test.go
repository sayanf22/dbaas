package store_test

import (
	"context"
	"log/slog"
	"sync"
	"testing"
)

// failOnErrorLog is a slog handler that fails the test on any record at Error level. Integration tests use it
// so a problem the code only logs (such as a failed Complete) can't hide behind a passing status code.
type failOnErrorLog struct {
	t     *testing.T
	mu    *sync.Mutex
	attrs []slog.Attr
}

// testLogger returns a logger bound to t that fails t on Error records.
func testLogger(t *testing.T) *slog.Logger {
	return slog.New(&failOnErrorLog{t: t, mu: &sync.Mutex{}})
}

// Enabled accepts every level; Handle filters, so the check can't be bypassed by a logger's level.
func (h *failOnErrorLog) Enabled(context.Context, slog.Level) bool { return true }

// Handle fails the test with the message and attributes of any Error-level record and ignores the rest.
// The mutex serializes t.Errorf calls from concurrent request goroutines.
func (h *failOnErrorLog) Handle(_ context.Context, r slog.Record) error {
	if r.Level < slog.LevelError {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	msg := r.Message
	r.Attrs(func(a slog.Attr) bool { msg += " " + a.String(); return true })
	for _, a := range h.attrs {
		msg += " " + a.String()
	}
	h.t.Errorf("unexpected error log: %s", msg)
	return nil
}

// WithAttrs returns a handler that also reports attrs; it copies the slice so siblings don't share it.
func (h *failOnErrorLog) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &failOnErrorLog{t: h.t, mu: h.mu, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...)}
}

// WithGroup ignores groups: the failure message only needs the attribute text, not its nesting.
func (h *failOnErrorLog) WithGroup(string) slog.Handler { return h }
