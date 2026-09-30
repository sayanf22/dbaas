package store_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/sayanf22/dbaas/db"
	"github.com/sayanf22/dbaas/internal/idempotency"
	"github.com/sayanf22/dbaas/internal/store"
)

// testTimeout bounds each integration test.
const testTimeout = 2 * time.Minute

// appPool migrates a throwaway database and returns a pool that connects as dbcloud_app the way control-api
// does: through the transaction pooler when APP_POOLER_TEST_URL is set, otherwise directly. Skipped without
// ADMIN_DB_TEST_URL so `go test ./...` works offline.
func appPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()
	adminURL := os.Getenv("ADMIN_DB_TEST_URL")
	if adminURL == "" {
		t.Skip("ADMIN_DB_TEST_URL not set; run `task db:test`")
	}
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	t.Cleanup(func() {
		if err := admin.Close(context.Background()); err != nil {
			t.Errorf("close admin: %v", err)
		}
	})

	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := "dbcloud_store_test_" + hex.EncodeToString(suffix)
	if _, err := admin.Exec(ctx, "create database "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := admin.Exec(cctx, "drop database if exists "+pgx.Identifier{name}.Sanitize()+" with (force)"); err != nil {
			t.Errorf("drop database: %v", err)
		}
	})

	cfg, err := pgx.ParseConfig(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database = name
	sqlDB := stdlib.OpenDB(*cfg)
	defer func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close migration connection: %v", err)
		}
	}()
	migrations, err := fs.Sub(db.Migrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations, goose.WithTableName(db.GooseTable))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// The service role with a throwaway password, as the Step 1.4 runbook does for real.
	pw := hex.EncodeToString(suffix) + "Aa9-test-only"
	if _, err := admin.Exec(ctx, "alter role dbcloud_app login password "+quoteLiteral(pw)); err != nil {
		t.Fatalf("enable dbcloud_app login: %v", err)
	}
	// Direct connection as dbcloud_app, but with the transaction-pooler settings (QueryExecModeExec), so the
	// code path is the production one. The pooler itself is exercised by `task db:test` against Supavisor.
	base, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	base.User = url.UserPassword("dbcloud_app", pw)
	base.Path = "/" + name
	pool, err := store.NewPool(ctx, store.PoolConfig{
		URL: base.String(), Mode: store.TransactionPooler, MaxConns: 4, AllowInsecureLocal: true,
	})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// quoteLiteral quotes a test-generated password for ALTER ROLE, which can't take a bind parameter.
func quoteLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// TestRepeatedPostReturnsOriginalResponse is the Step 0.2 "Done when" check against the real admin DB:
// a repeated POST with the same Idempotency-Key returns the original response and the handler runs once.
func TestRepeatedPostReturnsOriginalResponse(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()
	pool := appPool(ctx, t)

	var calls atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		if _, err := io.WriteString(w, `{"id":"op-`+strconv.FormatInt(n, 10)+`","state":"pending"}`); err != nil {
			t.Errorf("write response: %v", err)
		}
	})
	principal := func(*http.Request) (string, bool) { return "user:0192a3b4-0000-7000-8000-00000000000a", true }
	mw := idempotency.New(store.NewIdempotency(pool), principal, time.Now, testLogger(t))
	srv := httptest.NewServer(mw.Wrap(handler))
	t.Cleanup(srv.Close)

	post := func(key, body string) (int, string, string) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/v1/databases", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(idempotency.HeaderKey, key)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if closeErr := resp.Body.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(b), resp.Header.Get(idempotency.HeaderReplayed)
	}

	s1, b1, r1 := post("key-integration-1", `{"name":"shop"}`)
	s2, b2, r2 := post("key-integration-1", `{"name":"shop"}`)
	if s1 != 202 || s2 != 202 || b1 != b2 || r1 != "" || r2 != "true" || calls.Load() != 1 {
		t.Errorf("first %d %s (replayed %q), second %d %s (replayed %q), calls %d; want identical 202s, second replayed, 1 call",
			s1, b1, r1, s2, b2, r2, calls.Load())
	}
	if s3, _, _ := post("key-integration-1", `{"name":"other"}`); s3 != 422 {
		t.Errorf("same key, different body = %d; want 422", s3)
	}

	// Concurrent duplicates against the real database: exactly one execution.
	var wg sync.WaitGroup
	codes := make(chan int, 10)
	for range 10 {
		wg.Go(func() { s, _, _ := post("key-integration-2", `{"name":"race"}`); codes <- s })
	}
	wg.Wait()
	close(codes)
	for c := range codes {
		if c != 202 && c != 409 {
			t.Errorf("concurrent duplicate status = %d; want 202 or 409", c)
		}
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("handler calls after the race = %d; want 2 (one per key)", got)
	}
}

// TestInTxRollsBackOnError checks the transaction helper against the real database.
func TestInTxRollsBackOnError(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()
	pool := appPool(ctx, t)
	ids := store.NewIdempotency(pool)

	boom := io.ErrUnexpectedEOF
	err := store.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := store.NewIdempotency(tx).Claim(ctx, "user:rollback-test", "key-rollback", make([]byte, 32)); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("InTx() = %v; want the callback's error", err)
	}
	if _, err := ids.Get(ctx, "user:rollback-test", "key-rollback"); !errors.Is(err, idempotency.ErrNotFound) {
		t.Errorf("Get after rollback = %v; want ErrNotFound", err)
	}
}
