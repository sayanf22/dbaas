package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestIsSensitiveKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"password", true},
		{"DB_Password", true},
		{"refresh_token", true},
		{"Authorization", true},
		{"database_dsn", true},
		{"tenant_id", false},
		{"method", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isSensitiveKey(tt.key); got != tt.want {
			t.Errorf("isSensitiveKey(%q) = %v; want %v", tt.key, got, tt.want)
		}
	}
}

func TestNewLoggerRedacts(t *testing.T) {
	var buf bytes.Buffer
	log := NewLogger(&buf, "test")
	log.Info("login", slog.String("password", "hunter2"), slog.Group("req", slog.String("token", "abc")), slog.String("tenant_id", "t-1"))

	out := buf.String()
	for _, secret := range []string{"hunter2", "abc"} {
		if strings.Contains(out, secret) {
			t.Errorf("log output %s contains secret %q", out, secret)
		}
	}
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log output is not JSON: %v", err)
	}
	if rec["service"] != "test" || rec["tenant_id"] != "t-1" {
		t.Errorf("record = %v; want service=test and tenant_id=t-1", rec)
	}
}

func TestLoadServerConfig(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
		check   func(ServerConfig) bool
	}{
		{"defaults", nil, false, func(c ServerConfig) bool { return c.Addr == ":8080" && c.DrainTimeout == defaultDrainTimeout }},
		{"custom", map[string]string{"LISTEN_ADDR": "127.0.0.1:9000", "SHUTDOWN_DELAY": "0s"}, false,
			func(c ServerConfig) bool { return c.Addr == "127.0.0.1:9000" && c.ShutdownDelay == 0 }},
		{"bad addr", map[string]string{"LISTEN_ADDR": "8080"}, true, nil},
		{"bad duration", map[string]string{"DRAIN_TIMEOUT": "soon"}, true, nil},
		{"negative duration", map[string]string{"DRAIN_TIMEOUT": "-1s"}, true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadServerConfig(func(k string) string { return tt.env[k] })
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadServerConfig(%v) error = %v; want error %v", tt.env, err, tt.wantErr)
			}
			if tt.check != nil && !tt.check(cfg) {
				t.Errorf("LoadServerConfig(%v) = %+v; unexpected values", tt.env, cfg)
			}
		})
	}
}

func TestHealthEndpoints(t *testing.T) {
	mux := http.NewServeMux()
	h := &Health{}
	h.Register(mux)

	probe := func(path string) int {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		return rec.Code
	}
	if got := probe("/readyz"); got != http.StatusServiceUnavailable {
		t.Errorf("zero-value /readyz = %d; want %d", got, http.StatusServiceUnavailable)
	}
	h.SetReady(true)
	if got := probe("/readyz"); got != http.StatusOK {
		t.Errorf("ready /readyz = %d; want %d", got, http.StatusOK)
	}
	if got := probe("/livez"); got != http.StatusOK {
		t.Errorf("/livez = %d; want %d", got, http.StatusOK)
	}
}

func TestRecoverPanics(t *testing.T) {
	var buf bytes.Buffer
	h := recoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }), NewLogger(&buf, "test"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("panicking handler status = %d; want 500", rec.Code)
	}
	if !strings.Contains(buf.String(), "handler panic") {
		t.Errorf("panic was not logged: %s", buf.String())
	}
}

func TestLimitBody(t *testing.T) {
	h := limitBody(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
		}
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", bytes.NewReader(make([]byte, maxBodyBytes+1))))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body status = %d; want 413", rec.Code)
	}
}

// TestServeDrainsInFlight checks the shutdown order: readiness goes off and an
// in-flight request still completes before Serve returns nil.
func TestServeDrainsInFlight(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		if _, err := w.Write([]byte("done")); err != nil {
			t.Errorf("write response: %v", err)
		}
	})
	cfg := DefaultServerConfig()
	cfg.ShutdownDelay, cfg.DrainTimeout = 0, 5*time.Second
	health := &Health{}
	health.SetReady(true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, ln, h, cfg, health, NewLogger(io.Discard, "test")) }()

	body := make(chan string, 1)
	go func() {
		// The request context is independent of ctx: cancelling the server must not cancel the client.
		req, err := http.NewRequestWithContext(context.WithoutCancel(t.Context()), http.MethodGet, "http://"+ln.Addr().String()+"/", nil)
		if err != nil {
			body <- "error: " + err.Error()
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			body <- "error: " + err.Error()
			return
		}
		b, readErr := io.ReadAll(resp.Body)
		if closeErr := resp.Body.Close(); readErr == nil {
			readErr = closeErr
		}
		if readErr != nil {
			body <- "error: " + readErr.Error()
			return
		}
		body <- string(b)
	}()

	<-started
	cancel()
	// Readiness must drop before the in-flight request is released.
	deadline := time.Now().Add(2 * time.Second)
	for health.Ready() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if health.Ready() {
		t.Fatal("readiness still on after shutdown started")
	}
	close(release)

	if got := <-body; got != "done" {
		t.Errorf("in-flight response = %q; want %q", got, "done")
	}
	if err := <-served; err != nil {
		t.Errorf("Serve() = %v; want nil after clean drain", err)
	}
}
