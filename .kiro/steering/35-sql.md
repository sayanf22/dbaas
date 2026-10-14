---
inclusion: fileMatch
fileMatchPattern: 'db/**'
---

# SQL rules (admin DB on Supabase: goose migrations + sqlc queries)

## Queries (`db/queries/`)
- MUST: every value is a parameter (`$1`, `sqlc.arg(...)`); no string-built SQL. Table names, column names and sort orders come from fixed query variants, never from input.
- MUST: every query filters by the caller's `org_id` (or `tenant_id`) unless it is a documented fleet job, and every list query is keyset-paginated with a `LIMIT`.
- MUST: every new query has a supporting index; the PR includes `EXPLAIN (ANALYZE, BUFFERS)` of the realistic case.
- MUST: queries used through the transaction pooler don't depend on session state (no `SET` without `LOCAL`, no temp tables, no session advisory locks, no `LISTEN`).
- MUST: long-running reads use `SET LOCAL statement_timeout` inside their transaction.

## Roles and privileges
- MUST: one role per service (`dbcloud_app`, `dbcloud_worker`, `dbcloud_migrator`), each with only the grants its queries need; no service uses `postgres` except the dump Job. Roles have no `VALID UNTIL`, because an expired role breaks Supavisor authentication.
- MUST: objects live in schema `dbcloud`, owned by `dbcloud_migrator`; `anon` and `authenticated` get no grants in it; `REVOKE CREATE ON SCHEMA public FROM PUBLIC` is asserted by a migration test.
- MUST: roles have `search_path` pinned (`ALTER ROLE ... SET search_path = dbcloud, pg_catalog`), and migration code schema-qualifies every object.
- MUST: RLS is enabled on every table (default deny for roles without a policy). Our service roles don't own tables, so RLS isn't bypassed by ownership; the policies that grant access are explicit and tested.
- MUST: `SECURITY DEFINER` functions are rare, reviewed, and always `SET search_path = dbcloud, pg_temp`; `EXECUTE` is revoked from `PUBLIC` and granted to one role, in the same migration that creates the function. The retention sweep (the only code allowed to delete audit and usage rows) is one of these.
- MUST: per-role timeouts: `statement_timeout`, `lock_timeout` (smaller than `statement_timeout`, or it has no effect), `idle_in_transaction_session_timeout`; `idle_session_timeout` stays off because it breaks poolers.

## Migrations (`db/migrations/`, goose)
- MUST: expand/contract, compatible with the previous release (N-1). Never rename or drop a column in the release that stops using it.
- MUST: each migration sets `lock_timeout` (≤ 5 s) and is safe to retry after a timeout.
- MUST: `CREATE INDEX CONCURRENTLY` runs in its own migration marked `-- +goose NO TRANSACTION`; the migration first drops an `INVALID` index left by a failed attempt.
- MUST: adding a `NOT NULL` column uses a default or a backfill job; constraints on large tables are added `NOT VALID` and validated in a later migration.
- MUST: every migration has a tested `Down` or an explicit `-- irreversible: reason` note.
- MUST: migrations never contain secrets, seed personal data or grant to `PUBLIC`.
- MUST: keep the database under the Supabase plan's size limit: tables that grow with time (events, usage, audit) have a retention rule in `plan/01-architecture.md` §10.2 before they ship.

## Style
- SHOULD: lower-case keywords, one clause per line, explicit column lists (never `SELECT *` in application queries), `timestamptz` for times, `uuid` (UUIDv7) keys, `text` with `CHECK` constraints instead of `varchar(n)`, money in integer paise.
- MUST: every table and non-obvious column has a `COMMENT ON` describing its meaning and owner component.
