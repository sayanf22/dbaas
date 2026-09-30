-- Operations, operation events, idempotency keys and the audit log (plan/04 §9, 10-engineering §7).

-- +goose Up
set local lock_timeout = '5s';
set local role dbcloud_migrator;

create table dbcloud.operations (
  id                  uuid primary key,
  org_id              uuid not null references dbcloud.organizations (id),
  tenant_id           uuid references dbcloud.tenants (id),
  cell_id             text references dbcloud.cells (id),
  kind                text not null check (kind ~ '^[a-z][a-z_]{2,40}$'),
  state               text not null default 'pending'
                      check (state in ('pending', 'running', 'waitlisted', 'succeeded', 'failed', 'cancelling', 'cancelled')),
  idempotency_key     text check (length(idempotency_key) between 1 and 255),
  requested_by        uuid,
  provider_request_id text,
  error               jsonb check (error is null or jsonb_typeof(error) = 'object'),
  created_at          timestamptz not null default now(),
  finished_at         timestamptz,
  unique (org_id, idempotency_key)
);
create index operations_org_created_idx on dbcloud.operations (org_id, created_at desc, id);
create index operations_tenant_created_idx on dbcloud.operations (tenant_id, created_at desc) where tenant_id is not null;
comment on table dbcloud.operations is 'Every asynchronous change (202 + operation_id); the River job that acts on it commits in the same transaction. Owner: control-api (create), worker (progress).';
comment on column dbcloud.operations.error is 'RFC 9457 problem object when state = failed; never contains secrets.';

create table dbcloud.operation_events (
  operation_id uuid not null references dbcloud.operations (id),
  seq          integer not null check (seq >= 1),
  state        text not null,
  message      text not null check (length(message) <= 2000),
  at           timestamptz not null default now(),
  primary key (operation_id, seq)
);
create index operation_events_at_idx on dbcloud.operation_events (at);
comment on table dbcloud.operation_events is 'Progress log of an operation, shown to the customer. Exported to R2 and swept after 90 days (plan/01 §10.2). Owner: worker.';

create table dbcloud.idempotency_keys (
  principal      text not null check (principal ~ '^[a-z_]{2,20}:[A-Za-z0-9-]{1,64}$'),
  key            text not null check (length(key) between 8 and 255),
  request_hash   bytea not null check (length(request_hash) = 32),
  status_code    smallint check (status_code between 200 and 599),
  response       jsonb check (response is null or jsonb_typeof(response) = 'object'),
  created_at     timestamptz not null default now(),
  completed_at   timestamptz,
  primary key (principal, key),
  check ((status_code is null) = (completed_at is null))
);
create index idempotency_keys_created_idx on dbcloud.idempotency_keys (created_at);
comment on table dbcloud.idempotency_keys is 'Idempotency-Key records: the first response to a mutating request is replayed for 24 h (plan/01 §10.2). status_code null = in progress. Keys are scoped to the authenticated caller, not the org, because POST /v1/orgs has no org yet and a user can act in several orgs. Swept after 24 h by the worker. Owner: control-api.';
comment on column dbcloud.idempotency_keys.principal is 'Authenticated caller that sent the key, as kind:id (user:<uuid>, api_key:<uuid>).';
comment on column dbcloud.idempotency_keys.request_hash is 'SHA-256 of method, path, query and body; a reused key with a different request is rejected (422).';
comment on column dbcloud.idempotency_keys.response is 'Replayed response: status headers and base64 body. Responses marked Cache-Control: no-store (one-time secrets) are never stored; only {"no_store": true}.';

create table dbcloud.audit_events (
  id         uuid primary key,
  at         timestamptz not null default now(),
  actor      text not null check (length(actor) between 1 and 200),
  actor_type text not null check (actor_type in ('user', 'api_key', 'mcp_client', 'service', 'system')),
  org_id     uuid,
  tenant_id  uuid,
  cell_id    text,
  action     text not null check (action ~ '^[a-z][a-z0-9_.]{2,80}$'),
  target     text not null check (length(target) <= 500),
  request_id text,
  details    jsonb not null default '{}' check (jsonb_typeof(details) = 'object')
);
create index audit_events_org_at_idx on dbcloud.audit_events (org_id, at desc);
create index audit_events_at_idx on dbcloud.audit_events (at);
comment on table dbcloud.audit_events is 'Append-only audit log: services may insert and read, never update or delete; only dbcloud.retention_sweep() removes rows older than 90 days after export (plan/01 §10.2). Owner: every service (insert).';
comment on column dbcloud.audit_events.org_id is 'No foreign key on purpose: audit rows outlive the objects they describe.';

-- Back to the connecting role: goose records the version in this transaction.
reset role;

-- +goose Down
set local lock_timeout = '5s';
set local role dbcloud_migrator;
drop table dbcloud.audit_events;
drop table dbcloud.idempotency_keys;
drop table dbcloud.operation_events;
drop table dbcloud.operations;
reset role;
