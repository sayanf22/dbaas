-- Tenants, their databases, placements, routes and tombstones (plan/04 §9, plan/01 §6, §10.5).

-- +goose Up
set local lock_timeout = '5s';
set local role dbcloud_migrator;

create table dbcloud.tenants (
  id         uuid primary key,
  ref        text not null unique check (ref ~ '^t-[a-z2-7]{8}$'),
  org_id     uuid not null references dbcloud.organizations (id),
  plan_id    text not null references dbcloud.plans (id),
  name       text not null check (length(name) between 1 and 63),
  lifecycle  text not null default 'waitlisted'
             check (lifecycle in ('waitlisted', 'active', 'suspended', 'deletion_pending', 'purged')),
  created_at timestamptz not null default now(),
  deleted_at timestamptz
);
create index tenants_org_id_idx on dbcloud.tenants (org_id, created_at desc, id);
comment on table dbcloud.tenants is 'One customer database service per row; ref is the opaque public name (t- + base32). Owner: control-api.';
comment on column dbcloud.tenants.lifecycle is 'waitlisted | active | suspended | deletion_pending | purged (staged delete, plan/01 §6).';

create table dbcloud.databases (
  id                  uuid primary key,
  tenant_id           uuid not null unique references dbcloud.tenants (id),
  cell_id             text references dbcloud.cells (id),
  pg_major            smallint not null check (pg_major between 16 and 30),
  backup_slot         smallint check (backup_slot between 0 and 167),
  desired_generation  bigint not null default 1 check (desired_generation >= 1),
  desired_spec        jsonb not null check (jsonb_typeof(desired_spec) = 'object'),
  observed_generation bigint not null default 0 check (observed_generation >= 0),
  observed_status     jsonb not null default '{}' check (jsonb_typeof(observed_status) = 'object'),
  updated_at          timestamptz not null default now()
);
create index databases_cell_id_idx on dbcloud.databases (cell_id, desired_generation);
comment on table dbcloud.databases is 'Desired TenantDatabase spec per tenant and the status the cell agent reports back. Owner: control-api (desired), cluster-agent via control-api (observed).';
comment on column dbcloud.databases.backup_slot is 'Hour of the week (0-167) of the weekly base backup, staggered per node.';

create table dbcloud.placements (
  tenant_id  uuid not null references dbcloud.tenants (id),
  cell_id    text not null references dbcloud.cells (id),
  node_id    uuid references dbcloud.nodes (id),
  decided_at timestamptz not null default now(),
  reason     jsonb not null default '{}' check (jsonb_typeof(reason) = 'object'),
  primary key (tenant_id, decided_at)
);
comment on table dbcloud.placements is 'History of placement decisions (latest row wins). Owner: worker.';

create table dbcloud.routes (
  tenant_id        uuid primary key references dbcloud.tenants (id),
  hostname_pooled  text not null unique,
  hostname_direct  text not null unique,
  cell_id          text not null references dbcloud.cells (id),
  route_generation bigint not null default 1 check (route_generation >= 1),
  state            text not null default 'pending' check (state in ('pending', 'active', 'moving', 'removed')),
  checked_at       timestamptz
);
comment on table dbcloud.routes is 'Public hostnames per tenant and the cell that serves them. Owner: worker.';

create table dbcloud.tombstones (
  id           uuid primary key,
  tenant_id    uuid not null references dbcloud.tenants (id),
  cell_id      text not null references dbcloud.cells (id),
  generation   bigint not null check (generation >= 1),
  requested_by uuid not null,
  approved_by  uuid,
  created_at   timestamptz not null default now(),
  applied_at   timestamptz,
  check (approved_by is null or approved_by <> requested_by)
);
create index tombstones_cell_pending_idx on dbcloud.tombstones (cell_id, created_at) where applied_at is null;
comment on table dbcloud.tombstones is 'The only thing that lets a cluster-agent delete a tenant (plan/01 §10.5); purge needs a second approver. Owner: control-api.';

-- Back to the connecting role: goose records the version in this transaction.
reset role;

-- +goose Down
set local lock_timeout = '5s';
set local role dbcloud_migrator;
drop table dbcloud.tombstones;
drop table dbcloud.routes;
drop table dbcloud.placements;
drop table dbcloud.databases;
drop table dbcloud.tenants;
reset role;
