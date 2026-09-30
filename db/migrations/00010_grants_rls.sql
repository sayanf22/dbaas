-- Privileges, row-level security and the retention sweep (35-sql.md, plan/01 §10.2).
--
-- Grants follow what each role's queries do:
--   dbcloud_app    = control-api, mcp-server, mcp-auth (request paths; inserts River jobs with InsertTx)
--   dbcloud_worker = worker, River UI (jobs, sweeps of expired rows)
-- Nobody gets TRUNCATE, REFERENCES or TRIGGER. audit_events is insert + select only for everyone; only
-- dbcloud.retention_sweep() (SECURITY DEFINER, worker only) deletes aged audit, event and usage rows.
--
-- RLS is enabled on every table. Access is scoped per org in every query (35-sql.md), not by the database
-- session, because services connect as one role through the pooler; the policies below therefore grant
-- access per role, and any other role (anon, authenticated, PUBLIC) sees nothing.

-- +goose Up
set local lock_timeout = '5s';
set local role dbcloud_migrator;

-- +goose StatementBegin
do $$
declare
  t text;
  -- Tables whose rows services may delete, and why:
  --   memberships: removing a member; oauth_codes: redeemed codes; idempotency_keys: releasing an in-progress
  --   claim after a failure so the client can retry; approvals: never (used_at is set instead).
  app_delete text[] := array['memberships', 'oauth_codes', 'idempotency_keys'];
  --   expired rows swept by fleet jobs (idempotency 24 h, snapshots 7 d, OAuth expiry, River's own cleanup).
  worker_delete text[] := array['idempotency_keys', 'capacity_snapshots', 'oauth_codes', 'oauth_revoked_tokens',
                                'oauth_refresh_tokens', 'river_job', 'river_leader', 'river_client',
                                'river_client_queue', 'river_queue'];
begin
  for t in select c.relname from pg_catalog.pg_class c
           join pg_catalog.pg_namespace n on n.oid = c.relnamespace
           where n.nspname = 'dbcloud' and c.relkind in ('r', 'p') loop
    execute format('alter table dbcloud.%I enable row level security', t);
    if t = 'audit_events' then
      execute format('grant select, insert on dbcloud.%I to dbcloud_app, dbcloud_worker', t);
      execute format('create policy %I on dbcloud.%I for select to dbcloud_app, dbcloud_worker using (true)', t || '_read', t);
      execute format('create policy %I on dbcloud.%I for insert to dbcloud_app, dbcloud_worker with check (true)', t || '_append', t);
      continue;
    end if;
    execute format('grant select, insert, update on dbcloud.%I to dbcloud_app, dbcloud_worker', t);
    if t = any (app_delete) then
      execute format('grant delete on dbcloud.%I to dbcloud_app', t);
    end if;
    if t = any (worker_delete) then
      execute format('grant delete on dbcloud.%I to dbcloud_worker', t);
    end if;
    execute format('create policy %I on dbcloud.%I for all to dbcloud_app, dbcloud_worker using (true) with check (true)',
                   t || '_services', t);
  end loop;
end
$$;
-- +goose StatementEnd

-- Identity and River sequences (river_migration, river_job ids).
grant usage, select on all sequences in schema dbcloud to dbcloud_app, dbcloud_worker;
-- River functions and the job-state enum are used by both roles' River clients.
grant execute on all functions in schema dbcloud to dbcloud_app, dbcloud_worker;
-- River's only user-defined type (Postgres has no GRANT ... ON ALL TYPES).
grant usage on type dbcloud.river_job_state to dbcloud_app, dbcloud_worker;

-- +goose StatementBegin
create function dbcloud.retention_sweep(cutoff timestamptz, max_rows integer)
returns table (table_name text, deleted bigint)
language plpgsql
security definer
set search_path = dbcloud, pg_temp
as $$
declare
  n bigint;
begin
  -- 90 days is the retention floor (plan/01 §10.2); callers export older rows to R2 first.
  if cutoff > now() - interval '90 days' then
    raise exception 'retention_sweep: cutoff % is newer than 90 days', cutoff using errcode = '22023';
  end if;
  if max_rows not between 1 and 50000 then
    raise exception 'retention_sweep: max_rows % outside 1..50000', max_rows using errcode = '22023';
  end if;
  -- Bounded batches keep each call short (worker statement_timeout 60 s); the job repeats until 0.
  delete from dbcloud.audit_events where id in (select id from dbcloud.audit_events where at < cutoff limit max_rows);
  get diagnostics n = row_count; table_name := 'audit_events'; deleted := n; return next;
  delete from dbcloud.operation_events where ctid in (select ctid from dbcloud.operation_events where at < cutoff limit max_rows);
  get diagnostics n = row_count; table_name := 'operation_events'; deleted := n; return next;
  delete from dbcloud.usage_hourly where ctid in (select ctid from dbcloud.usage_hourly where hour < cutoff limit max_rows);
  get diagnostics n = row_count; table_name := 'usage_hourly'; deleted := n; return next;
end
$$;
-- +goose StatementEnd
comment on function dbcloud.retention_sweep(timestamptz, integer) is 'The only code path that deletes audit_events, operation_events and usage_hourly rows: older than 90 days, after export to R2, in batches. Worker only.';
revoke execute on function dbcloud.retention_sweep(timestamptz, integer) from public, dbcloud_app;
grant execute on function dbcloud.retention_sweep(timestamptz, integer) to dbcloud_worker;

-- Back to the connecting role: goose records the version in this transaction.
reset role;

-- +goose Down
set local lock_timeout = '5s';
set local role dbcloud_migrator;
drop function dbcloud.retention_sweep(timestamptz, integer);
-- +goose StatementBegin
do $$
declare
  t text;
  p record;
begin
  for p in select policyname, tablename from pg_catalog.pg_policies where schemaname = 'dbcloud' loop
    execute format('drop policy %I on dbcloud.%I', p.policyname, p.tablename);
  end loop;
  for t in select c.relname from pg_catalog.pg_class c
           join pg_catalog.pg_namespace n on n.oid = c.relnamespace
           where n.nspname = 'dbcloud' and c.relkind in ('r', 'p') loop
    execute format('alter table dbcloud.%I disable row level security', t);
  end loop;
end
$$;
-- +goose StatementEnd
revoke all on all tables in schema dbcloud from dbcloud_app, dbcloud_worker;
revoke all on all sequences in schema dbcloud from dbcloud_app, dbcloud_worker;
revoke all on all functions in schema dbcloud from dbcloud_app, dbcloud_worker;
revoke usage on type dbcloud.river_job_state from dbcloud_app, dbcloud_worker;
reset role;
