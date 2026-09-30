package platform

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

// version is set at link time with -ldflags "-X .../internal/platform.version=<git describe>".
// It is the one package-level variable in this package; nothing writes it at run time.
var version = "dev"

// Version returns the build version stamped into the binary ("dev" for local builds).
func Version() string { return version }

// RunService is the whole body of a service's main: it reads the server
// config from the environment, builds the service's handler, serves it next
// to /livez and /readyz, and drains on SIGTERM or SIGINT.
//
// build receives the logger and returns the service's routes; nil means the
// service exposes only the health endpoints for now. RunService returns the
// process exit code (0 after a clean drain, 1 on any error, already logged).
func RunService(service string, build func(log *slog.Logger) (http.Handler, error)) int {
	log := NewLogger(os.Stdout, service)
	if err := run(service, build, log); err != nil {
		log.Error("exit", slog.String("error", err.Error()))
		return 1
	}
	return 0
}

// run holds RunService's steps so every error returns through one place.
func run(service string, build func(log *slog.Logger) (http.Handler, error), log *slog.Logger) error {
	cfg, err := LoadServerConfig(os.Getenv)
	if err != nil {
		return err
	}
	// Kubernetes sends SIGTERM on pod deletion; SIGINT covers Ctrl-C locally.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	mux := http.NewServeMux()
	health := &Health{}
	health.Register(mux)
	if build != nil {
		h, err := build(log)
		if err != nil {
			return fmt.Errorf("platform: build %s handler: %w", service, err)
		}
		mux.Handle("/", h)
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("platform: listen on %s: %w", cfg.Addr, err)
	}
	health.SetReady(true)
	return Serve(ctx, ln, mux, cfg, health, log)
}
