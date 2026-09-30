package db_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/sayanf22/dbaas/db"
)

// testTimeout bounds each schema test; the whole migration set applies in well under a second locally.
const testTimeout = 2 * time.Minute

// adminURL returns ADMIN_DB_TEST_URL (a role that may create databases and roles: `postgres` on the local
// Supabase stack or the CI service). Tests are skipped without it so `go test ./...` works offline.
func adminURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("ADMIN_DB_TEST_URL")
	if u == "" {
		t.Skip("ADMIN_DB_TEST_URL not set; run `task db:test` or set it to the local admin DB")
	}
	return u
}

// freshDatabase creates an empty, uniquely named database and drops it when the test ends, so tests never
// touch the developer's migrated database. Roles are cluster-wide and created idempotently by 00001.
func freshDatabase(ctx context.Context, t *testing.T) *pgx.Conn {
	t.Helper()
	admin, err := pgx.Connect(ctx, adminURL(t))
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatalf("random name: %v", err)
	}
	name := "dbcloud_schema_test_" + hex.EncodeToString(suffix)
	// The name is generated above from [0-9a-f], so quoting it as an identifier is safe.
	if _, err := admin.Exec(ctx, "create database "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := admin.Exec(cctx, "drop database if exists "+pgx.Identifier{name}.Sanitize()+" with (force)"); err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
		if err := admin.Close(cctx); err != nil {
			t.Errorf("close admin: %v", err)
		}
	})

	cfg, err := pgx.ParseConfig(adminURL(t))
	if err != nil {
		t.Fatalf("parse admin url: %v", err)
	}
	cfg.Database = name
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := conn.Close(cctx); err != nil {
			t.Errorf("close test connection: %v", err)
		}
	})
	return conn
}

// provider returns a goose provider for the embedded migrations on conn's database.
func provider(t *testing.T, conn *pgx.Conn) *goose.Provider {
	t.Helper()
	sqlDB := stdlib.OpenDB(*conn.Config())
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close sql.DB: %v", err)
		}
	})
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, mustSub(t),
		goose.WithTableName(db.GooseTable))
	if err != nil {
		t.Fatalf("goose provider: %v", err)
	}
	return p
}

// TestMigrationsRoundTrip applies every migration, rolls all of them back, and applies them again
// (35-sql.md: every migration has a tested Down).
func TestMigrationsRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()
	p := provider(t, freshDatabase(ctx, t))

	for i, step := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"up", func(ctx context.Context) error { _, err := p.Up(ctx); return err }},
		{"down to 0", func(ctx context.Context) error { _, err := p.DownTo(ctx, 0); return err }},
		{"up again", func(ctx context.Context) error { _, err := p.Up(ctx); return err }},
	} {
		if err := step.run(ctx); err != nil {
			t.Fatalf("step %d (%s): %v", i, step.name, err)
		}
	}
	v, err := p.GetDBVersion(ctx)
	if err != nil {
		t.Fatalf("GetDBVersion: %v", err)
	}
	sources := p.ListSources()
	if want := sources[len(sources)-1].Version; v != want {
		t.Errorf("GetDBVersion() = %d; want %d (latest migration)", v, want)
	}
}

// TestSchemaInvariants checks the rules in 35-sql.md on the fully migrated schema. Each query returns the
// offending objects; an empty result passes.
func TestSchemaInvariants(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()
	conn := freshDatabase(ctx, t)
	if _, err := provider(t, conn).Up(ctx); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	tests := []struct {
		rule  string
		query string
	}{
		{"RLS is enabled on every table", `
			select c.relname from pg_class c join pg_namespace n on n.oid = c.relnamespace
			where n.nspname = 'dbcloud' and c.relkind in ('r', 'p') and not c.relrowsecurity`},
		{"every table is owned by dbcloud_migrator", `
			select c.relname from pg_class c join pg_namespace n on n.oid = c.relnamespace
			where n.nspname = 'dbcloud' and c.relkind in ('r', 'p', 'S', 'v') and pg_get_userbyid(c.relowner) <> 'dbcloud_migrator'`},
		{"every table has a comment", `
			select c.relname from pg_class c join pg_namespace n on n.oid = c.relnamespace
			where n.nspname = 'dbcloud' and c.relkind in ('r', 'p') and c.relname not like 'river\_%'
			  and obj_description(c.oid, 'pg_class') is null`},
		{"PUBLIC has no privilege on dbcloud tables", `
			select c.relname from pg_class c join pg_namespace n on n.oid = c.relnamespace,
			     lateral aclexplode(coalesce(c.relacl, acldefault('r', c.relowner))) a
			where n.nspname = 'dbcloud' and a.grantee = 0`},
		{"PUBLIC cannot create in schema public or use schema dbcloud", `
			select n.nspname || ':' || a.privilege_type from pg_namespace n,
			     lateral aclexplode(coalesce(n.nspacl, acldefault('n', n.nspowner))) a
			where a.grantee = 0 and ((n.nspname = 'public' and a.privilege_type = 'CREATE') or n.nspname = 'dbcloud')`},
		{"Supabase Data API roles have no privilege in dbcloud", `
			select r.rolname || ':' || c.relname from pg_roles r, pg_class c join pg_namespace n on n.oid = c.relnamespace
			where r.rolname in ('anon', 'authenticated', 'service_role') and n.nspname = 'dbcloud' and c.relkind in ('r', 'p')
			  and has_table_privilege(r.oid, c.oid, 'select,insert,update,delete')`},
		{"nobody can update or delete audit_events", `
			select r.rolname from pg_roles r
			where r.rolname in ('dbcloud_app', 'dbcloud_worker')
			  and has_table_privilege(r.oid, 'dbcloud.audit_events', 'update,delete,truncate')`},
		{"only dbcloud_worker may run retention_sweep", `
			select r.rolname from pg_roles r
			where r.rolname in ('dbcloud_app', 'dbcloud_worker') and has_function_privilege(r.oid,
			  'dbcloud.retention_sweep(timestamptz, integer)', 'execute') <> (r.rolname = 'dbcloud_worker')`},
		{"service roles pin search_path and have lock_timeout < statement_timeout", `
			select r.rolname from pg_roles r
			where r.rolname in ('dbcloud_app', 'dbcloud_worker', 'dbcloud_migrator') and not exists (
			  select 1 from pg_db_role_setting s where s.setrole = r.oid and s.setdatabase = 0
			    and 'search_path=dbcloud, pg_catalog' = any (s.setconfig))`},
		{"service roles have no VALID UNTIL (it breaks Supavisor auth)", `
			select rolname from pg_roles where rolname like 'dbcloud\_%' and rolvaliduntil is not null`},
	}
	for _, tt := range tests {
		t.Run(tt.rule, func(t *testing.T) {
			rows, err := conn.Query(ctx, tt.query)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			bad, err := pgx.CollectRows(rows, pgx.RowTo[string])
			if err != nil {
				t.Fatalf("collect: %v", err)
			}
			if len(bad) > 0 {
				t.Errorf("violations = %v; want none", bad)
			}
		})
	}
}

// TestRowLevelSecurityDefaultDeny proves RLS, not only grants, protects the tables: a role with a SELECT
// grant but no policy sees no rows, while dbcloud_app (which has a policy) sees the seeded plans.
func TestRowLevelSecurityDefaultDeny(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()
	conn := freshDatabase(ctx, t)
	if _, err := provider(t, conn).Up(ctx); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	stmts := []string{
		// Test-only role, removed at the end; the admin needs membership to SET ROLE (PG 16+ rules).
		`do $$ begin if not exists (select from pg_roles where rolname = 'dbcloud_test_nopolicy') then create role dbcloud_test_nopolicy nologin; end if; end $$`,
		`grant usage on schema dbcloud to dbcloud_test_nopolicy`,
		`grant select on dbcloud.plans to dbcloud_test_nopolicy`,
		`grant dbcloud_test_nopolicy, dbcloud_app to current_user`,
	}
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s); err != nil {
			t.Fatalf("setup %q: %v", s, err)
		}
	}
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, s := range []string{
			`reset role`,
			`revoke dbcloud_app from current_user`,
			`revoke all on dbcloud.plans from dbcloud_test_nopolicy`,
			`revoke usage on schema dbcloud from dbcloud_test_nopolicy`,
			`drop role if exists dbcloud_test_nopolicy`,
		} {
			if _, err := conn.Exec(cctx, s); err != nil {
				t.Errorf("cleanup %q: %v", s, err)
			}
		}
	})

	tests := []struct {
		role string
		want int
	}{
		{"dbcloud_test_nopolicy", 0}, // grant without policy: RLS denies every row
		{"dbcloud_app", 3},           // base, plus, premium
	}
	for _, tt := range tests {
		var got int
		err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "set local role "+pgx.Identifier{tt.role}.Sanitize()); err != nil {
				return err
			}
			return tx.QueryRow(ctx, "select count(*) from dbcloud.plans").Scan(&got)
		})
		if err != nil {
			t.Fatalf("count plans as %s: %v", tt.role, err)
		}
		if got != tt.want {
			t.Errorf("count(plans) as %s = %d; want %d", tt.role, got, tt.want)
		}
	}
}

// TestRetentionSweepRefusesRecentCutoff checks the 90-day floor is enforced by the database itself.
func TestRetentionSweepRefusesRecentCutoff(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()
	conn := freshDatabase(ctx, t)
	if _, err := provider(t, conn).Up(ctx); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	tests := []struct {
		name     string
		cutoff   time.Time
		maxRows  int
		wantCode string // SQLSTATE, "" = success
	}{
		{"cutoff newer than 90 days", time.Now().Add(-30 * 24 * time.Hour), 100, "22023"},
		{"batch size out of range", time.Now().Add(-100 * 24 * time.Hour), 0, "22023"},
		{"valid call", time.Now().Add(-100 * 24 * time.Hour), 100, ""},
	}
	for _, tt := range tests {
		_, err := conn.Exec(ctx, "select * from dbcloud.retention_sweep($1, $2)", tt.cutoff, tt.maxRows)
		var pgErr *pgconn.PgError
		switch {
		case tt.wantCode == "" && err != nil:
			t.Errorf("%s: retention_sweep() error = %v; want nil", tt.name, err)
		case tt.wantCode != "" && (!errors.As(err, &pgErr) || pgErr.Code != tt.wantCode):
			t.Errorf("%s: retention_sweep() error = %v; want SQLSTATE %s", tt.name, err, tt.wantCode)
		}
	}
}
