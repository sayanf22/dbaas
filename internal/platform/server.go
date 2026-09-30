package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"time"
)

// Server timeouts and limits (20-go.md "HTTP servers"; gosec G112/G114).
const (
	// defaultReadHeaderTimeout bounds slow-loris header reads.
	defaultReadHeaderTimeout = 5 * time.Second
	// defaultReadTimeout bounds reading a whole request; bodies are ≤ 1 MiB.
	defaultReadTimeout = 30 * time.Second
	// defaultWriteTimeout stays below Cloudflare's 100 s proxy timeout so our
	// error, not Cloudflare's, reaches the client.
	defaultWriteTimeout = 60 * time.Second
	// defaultIdleTimeout keeps keep-alive connections from cloudflared cheap to reuse.
	defaultIdleTimeout = 120 * time.Second
	// defaultMaxHeaderBytes is 64 KiB: enough for JWTs and cookies, far below Go's 1 MiB default.
	defaultMaxHeaderBytes = 64 << 10
	// maxBodyBytes is 1 MiB, the default body limit in 15-security-and-reliability.md §5.
	maxBodyBytes = 1 << 20
	// defaultShutdownDelay lets Kubernetes remove the pod from Service endpoints
	// after readiness fails, before connections stop being accepted.
	defaultShutdownDelay = 5 * time.Second
	// defaultDrainTimeout must stay below the pod's terminationGracePeriodSeconds (30 s)
	// minus defaultShutdownDelay.
	defaultDrainTimeout = 20 * time.Second
)

// ServerConfig holds the listener address and HTTP server limits.
type ServerConfig struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
	ShutdownDelay     time.Duration
	DrainTimeout      time.Duration
}

// DefaultServerConfig returns the limits above, listening on :8080.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Addr:              ":8080",
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
		MaxHeaderBytes:    defaultMaxHeaderBytes,
		ShutdownDelay:     defaultShutdownDelay,
		DrainTimeout:      defaultDrainTimeout,
	}
}

// LoadServerConfig reads LISTEN_ADDR, SHUTDOWN_DELAY and DRAIN_TIMEOUT through
// getenv (os.Getenv in production, a map in tests) on top of the defaults.
//
// It fails fast with a message naming the variable and the expected format.
func LoadServerConfig(getenv func(string) string) (ServerConfig, error) {
	cfg := DefaultServerConfig()
	if v := getenv("LISTEN_ADDR"); v != "" {
		if _, port, err := net.SplitHostPort(v); err != nil || port == "" {
			return ServerConfig{}, fmt.Errorf("platform: LISTEN_ADDR %q: want host:port, e.g. :8080", v)
		}
		cfg.Addr = v
	}
	for _, d := range []struct {
		name string
		dst  *time.Duration
	}{
		{"SHUTDOWN_DELAY", &cfg.ShutdownDelay},
		{"DRAIN_TIMEOUT", &cfg.DrainTimeout},
	} {
		v := getenv(d.name)
		if v == "" {
			continue
		}
		parsed, err := time.ParseDuration(v)
		if err != nil || parsed < 0 {
			return ServerConfig{}, fmt.Errorf("platform: %s %q: want a non-negative Go duration, e.g. 5s", d.name, v)
		}
		*d.dst = parsed
	}
	return cfg, nil
}

// Serve runs an HTTP server on ln until ctx is cancelled, then drains.
//
// Shutdown order (10-engineering-standards.md §4): readiness off → wait
// ShutdownDelay so load balancers stop sending traffic → stop accepting →
// wait up to DrainTimeout for in-flight requests → return. It returns nil
// after a clean drain and an error if serving fails or the drain times out.
// Every handler runs behind panic recovery and a 1 MiB body limit.
func Serve(ctx context.Context, ln net.Listener, h http.Handler, cfg ServerConfig, health *Health, log *slog.Logger) error {
	srv := &http.Server{
		Handler:           recoverPanics(limitBody(h), log),
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}

	// serveErr receives exactly one value from the Serve goroutine below; the
	// buffer of 1 lets that goroutine exit even if we return early on drain.
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	log.Info("listening", slog.String("addr", ln.Addr().String()))

	select {
	case err := <-serveErr:
		return fmt.Errorf("platform: serve: %w", err)
	case <-ctx.Done():
	}

	health.SetReady(false)
	log.Info("shutting down", slog.Duration("delay", cfg.ShutdownDelay), slog.Duration("drain", cfg.DrainTimeout))
	time.Sleep(cfg.ShutdownDelay)

	drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.DrainTimeout)
	defer cancel()
	if err := srv.Shutdown(drainCtx); err != nil {
		return fmt.Errorf("platform: drain: %w", err)
	}
	// Serve returns ErrServerClosed once Shutdown starts; anything else is real.
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("platform: serve: %w", err)
	}
	log.Info("stopped")
	return nil
}

// limitBody caps every request body at maxBodyBytes; reads beyond it fail and
// handlers answer 413.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// recoverPanics turns a handler panic into a logged 500 so one bad request
// can't crash the process. http.ErrAbortHandler is re-raised because net/http
// uses it deliberately to abort a response.
func recoverPanics(next http.Handler, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rec := recover()
			if rec == nil {
				return
			}
			if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				panic(rec)
			}
			log.Error("handler panic",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("panic", fmt.Sprint(rec)),
				slog.String("stack", string(debug.Stack())))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}()
		next.ServeHTTP(w, r)
	})
}
