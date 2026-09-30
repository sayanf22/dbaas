// Package store is the admin-DB access layer (Supabase Postgres, schema dbcloud): the connection pool, the
// transaction helper and the typed stores used by the services. Queries come only from sqlc
// (internal/store/queries, generated from db/queries); nothing here builds SQL from strings.
//
// It deliberately contains no business rules and no HTTP; callers pass contexts with deadlines.
package store

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/sayanf22/dbaas/internal/store/queries"
)

// PoolMode says which Supavisor pooler a pool connects through (plan/01 §10.2).
type PoolMode int

const (
	// TransactionPooler (port 6543): request handlers. No prepared-statement cache, batches, LISTEN or
	// session state (20-go.md "Database").
	TransactionPooler PoolMode = iota + 1
	// SessionPooler (port 5432): the River coordinator, River UI, migrations and dumps.
	SessionPooler
)

const (
	// maxConnLifetime recycles connections so Supavisor rebalances and credential rotations take effect.
	maxConnLifetime = 30 * time.Minute
	// maxConnIdleTime closes idle connections; Supabase Free counts every connection against 60.
	maxConnIdleTime = 5 * time.Minute
	// connectTimeout bounds TCP + TLS + auth; each request has its own deadline on top.
	connectTimeout = 5 * time.Second
)

// PoolConfig configures one pool.
type PoolConfig struct {
	// URL is the Postgres URL; the password comes from the environment, never from a flag.
	URL string
	// Mode is the pooler this URL points at.
	Mode PoolMode
	// MaxConns is this process's share of the connection budget in plan/01 §10.2 (for example 4 for control-api).
	MaxConns int32
	// AllowInsecureLocal permits sslmode other than verify-full for loopback hosts only (local Supabase stack).
	AllowInsecureLocal bool
}

// ErrInsecureConnection is returned when a non-local URL doesn't use sslmode=verify-full (15-security §6).
var ErrInsecureConnection = errors.New("store: admin DB connections must use sslmode=verify-full")

// NewPool opens a pgx pool configured for cfg.Mode. It fails fast on invalid configuration.
func NewPool(ctx context.Context, cfg PoolConfig) (*pgxpool.Pool, error) {
	if cfg.MaxConns < 1 || cfg.MaxConns > 20 {
		return nil, fmt.Errorf("store: MaxConns %d outside 1..20 (see plan/01 §10.2 budget)", cfg.MaxConns)
	}
	if err := checkTLS(cfg); err != nil {
		return nil, err
	}
	pc, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("store: parse database URL: %w", err)
	}
	pc.MaxConns = cfg.MaxConns
	pc.MaxConnLifetime = maxConnLifetime
	pc.MaxConnIdleTime = maxConnIdleTime
	pc.ConnConfig.ConnectTimeout = connectTimeout
	switch cfg.Mode {
	case TransactionPooler:
		// Supavisor transaction mode can't keep prepared statements across transactions.
		pc.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeExec
	case SessionPooler:
	default:
		return nil, fmt.Errorf("store: unknown pool mode %d", cfg.Mode)
	}
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("store: open pool: %w", err)
	}
	return pool, nil
}

// checkTLS enforces verify-full everywhere except loopback hosts when AllowInsecureLocal is set.
func checkTLS(cfg PoolConfig) error {
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return fmt.Errorf("store: parse database URL: %w", err)
	}
	if u.Query().Get("sslmode") == "verify-full" {
		return nil
	}
	host := u.Hostname()
	if cfg.AllowInsecureLocal && (host == "127.0.0.1" || host == "localhost" || host == "::1") {
		return nil
	}
	return ErrInsecureConnection
}

// DB is the part of pgxpool.Pool and pgx.Tx the stores need; it lets a store run inside a transaction.
type DB = queries.DBTX

// TxBeginner starts transactions; *pgxpool.Pool implements it.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// InTx runs fn in a transaction and commits if fn returns nil. On any error, or a panic in fn, it rolls
// back (rollback after commit is a no-op). Callers put the state change and its River job in the same fn
// (10-engineering §7, InsertTx).
func InTx(ctx context.Context, db TxBeginner, fn func(tx pgx.Tx) error) (err error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: begin: %w", err)
	}
	defer func() {
		// A new context: the caller's may already be cancelled, and a rollback must still reach the server.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), connectTimeout)
		defer cancel()
		if rbErr := tx.Rollback(rctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) && err == nil {
			err = fmt.Errorf("store: rollback: %w", rbErr)
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: commit: %w", err)
	}
	return nil
}
