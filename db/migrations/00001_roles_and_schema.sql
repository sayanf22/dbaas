-- Roles and the dbcloud schema (35-sql.md "Roles and privileges", plan/01 §10.2).
--
-- One role per service, all NOLOGIN here: LOGIN and passwords are set out of band (runbook in Step 1.4,
-- hack/admin-db.sh locally), because migrations never contain secrets. In production the roles are
-- created once by `postgres` before the first migration Job, so the IF NOT EXISTS branches are no-ops.
-- Every later migration starts with `set local role dbcloud_migrator`, so objects are owned by the
-- migrator whether goose connects as `postgres` (local) or as `dbcloud_migrator` (production Job).

-- +goose Up
set local lock_timeout = '5s';

-- +goose StatementBegin
do $$
declare
  r text;
begin
  foreach r in array array['dbcloud_migrator', 'dbcloud_app', 'dbcloud_worker'] loop
    if not exists (select from pg_catalog.pg_roles where rolname = r) then
      execute format('create role %I nologin', r);
    end if;
  end loop;
  -- The connecting admin role must be able to SET ROLE to the owner; skipped when goose runs as the owner.
  if current_user <> 'dbcloud_migrator' then
    execute format('grant dbcloud_migrator to %I', current_user);
  end if;
end
$$;
-- +goose StatementEnd

create schema if not exists dbcloud authorization dbcloud_migrator;
comment on schema dbcloud is 'dbcloud control-plane metadata (orgs, tenants, operations, billing, audit, OAuth, River). Owner: dbcloud_migrator. Never holds customer rows.';

-- Nobody creates objects in public; nothing in dbcloud is visible by default.
revoke create on schema public from public;
revoke all on schema dbcloud from public;
grant usage on schema dbcloud to dbcloud_app, dbcloud_worker;

-- Supabase's Data API roles get nothing in our schema (they don't exist on plain Postgres in CI).
-- +goose StatementBegin
do $$
declare
  r text;
begin
  foreach r in array array['anon', 'authenticated', 'service_role'] loop
    if exists (select from pg_catalog.pg_roles where rolname = r) then
      execute format('revoke all on schema dbcloud from %I', r);
    end if;
  end loop;
end
$$;
-- +goose StatementEnd

-- Pinned search_path and per-role timeouts (35-sql.md). lock_timeout < statement_timeout, otherwise it has
-- no effect; idle_session_timeout stays off because it breaks poolers.
--   app:      request handlers behind a 60 s edge deadline; queries are single-row or paginated.
--   worker:   River jobs; long work is split into short jobs that snooze (plan/01 §10.2).
--   migrator: schema changes; each migration also sets its own lock_timeout (≤ 5 s).
alter role dbcloud_app set search_path = dbcloud, pg_catalog;
alter role dbcloud_app set statement_timeout = '5s';
alter role dbcloud_app set lock_timeout = '2s';
alter role dbcloud_app set idle_in_transaction_session_timeout = '15s';
alter role dbcloud_worker set search_path = dbcloud, pg_catalog;
alter role dbcloud_worker set statement_timeout = '60s';
alter role dbcloud_worker set lock_timeout = '10s';
alter role dbcloud_worker set idle_in_transaction_session_timeout = '60s';
alter role dbcloud_migrator set search_path = dbcloud, pg_catalog;
alter role dbcloud_migrator set statement_timeout = '15min';
alter role dbcloud_migrator set lock_timeout = '5s';

-- +goose Down
set local lock_timeout = '5s';
drop schema if exists dbcloud cascade;
-- Roles are kept: they may hold passwords and Supavisor settings managed outside migrations.
-- Their settings are reset so a re-run of Up starts from a clean state.
alter role dbcloud_app reset all;
alter role dbcloud_worker reset all;
alter role dbcloud_migrator reset all;
