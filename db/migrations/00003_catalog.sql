-- Plans, cells, nodes and capacity snapshots (plan/04 §9, plan/03 §4-§6).

-- +goose Up
set local lock_timeout = '5s';
set local role dbcloud_migrator;

create table dbcloud.plans (
  id                text primary key check (id ~ '^[a-z][a-z0-9-]{1,30}$'),
  name              text not null check (length(name) between 1 and 60),
  cpu_request_m     integer not null check (cpu_request_m > 0),
  cpu_limit_m       integer not null check (cpu_limit_m >= cpu_request_m),
  mem_mi            integer not null check (mem_mi > 0),
  storage_gb        integer not null check (storage_gb > 0),
  storage_class     text not null default 'tenant-local',
  instances         smallint not null default 1 check (instances between 1 and 3),
  max_connections   integer not null check (max_connections > 0),
  pooled_clients    integer not null check (pooled_clients > 0),
  archive_timeout_s integer not null check (archive_timeout_s between 30 and 3600),
  retention_days    smallint not null check (retention_days between 1 and 35),
  pg_parameters     jsonb not null default '{}' check (jsonb_typeof(pg_parameters) = 'object'),
  price_paise       bigint not null check (price_paise >= 0),
  active            boolean not null default true
);
comment on table dbcloud.plans is 'Plan catalog (plan/03 §4); the only source of tenant resources and Postgres parameters. Rows are managed by migrations and the admin app. Owner: control-api.';
comment on column dbcloud.plans.price_paise is 'Monthly price excluding GST, in paise (₹299 = 29900).';
comment on column dbcloud.plans.pg_parameters is 'Postgres parameters rendered by the tenant-operator; never taken from customer input.';

-- Launch plans (plan/03 §4-§5). Catalog data, not personal data; prices of plus/premium are proposed.
insert into dbcloud.plans (id, name, cpu_request_m, cpu_limit_m, mem_mi, storage_gb, max_connections, pooled_clients,
                           archive_timeout_s, retention_days, pg_parameters, price_paise)
values
  ('base',    'Base',    100,  500,  512,  5,  25, 100, 300, 7, '{"shared_buffers": "128MB"}', 29900),
  ('plus',    'Plus',    250, 1000, 1024, 10,  50, 200,  60, 7, '{"shared_buffers": "256MB"}', 69900),
  ('premium', 'Premium', 500, 1000, 2048, 20, 100, 400,  60, 7, '{"shared_buffers": "512MB"}', 139900);

create table dbcloud.cells (
  id            text primary key check (id ~ '^c[0-9]{1,3}$'),
  provider      text not null check (length(provider) between 1 and 40),
  region        text not null check (length(region) between 1 and 40),
  name          text not null check (length(name) between 1 and 60),
  state         text not null default 'provisioning' check (state in ('provisioning', 'active', 'draining', 'retired')),
  k8s_version   text,
  agent_cert_fp text check (agent_cert_fp ~ '^[0-9a-f]{64}$'),
  max_nodes     smallint not null default 1 check (max_nodes between 1 and 100),
  capacity      jsonb not null default '{}' check (jsonb_typeof(capacity) = 'object'),
  created_at    timestamptz not null default now()
);
comment on table dbcloud.cells is 'K3s cells (plan/01 §5). Owner: control-api (registration), worker (capacity).';
comment on column dbcloud.cells.agent_cert_fp is 'SHA-256 fingerprint of the cell agent''s mTLS client certificate.';
comment on column dbcloud.cells.max_nodes is 'Owner-set spending cap for the capacity controller (plan/03 §10).';

create table dbcloud.nodes (
  id           uuid primary key,
  cell_id      text not null references dbcloud.cells (id),
  name         text not null check (name ~ '^[a-z0-9][a-z0-9-]{0,62}$'),
  provider_ref text,
  role         text not null check (role in ('server', 'agent')),
  vcpu         smallint not null check (vcpu > 0),
  mem_gb       smallint not null check (mem_gb > 0),
  disk_gb      integer not null check (disk_gb > 0),
  public_ipv4  inet check (family(public_ipv4) = 4),
  state        text not null default 'joining' check (state in ('joining', 'ready', 'cordoned', 'evacuating', 'removed')),
  joined_at    timestamptz,
  unique (cell_id, name)
);
comment on table dbcloud.nodes is 'VMs of each cell; the worker keeps tenant-node A records in sync from this table. Owner: worker.';

create table dbcloud.capacity_snapshots (
  cell_id        text not null references dbcloud.cells (id),
  node_id        uuid not null references dbcloud.nodes (id),
  taken_at       timestamptz not null,
  allocatable    jsonb not null check (jsonb_typeof(allocatable) = 'object'),
  requested      jsonb not null check (jsonb_typeof(requested) = 'object'),
  lvm_free_bytes bigint not null check (lvm_free_bytes >= 0),
  free_pod_slots integer not null check (free_pod_slots >= 0),
  primary key (node_id, taken_at)
);
create index capacity_snapshots_cell_taken_idx on dbcloud.capacity_snapshots (cell_id, taken_at desc);
comment on table dbcloud.capacity_snapshots is 'Per-node capacity reported by the cluster-agent; input to placement. Kept 7 days (plan/01 §10.2). Owner: control-api (writes), worker (reads, sweeps).';

-- Back to the connecting role: goose records the version in this transaction.
reset role;

-- +goose Down
set local lock_timeout = '5s';
set local role dbcloud_migrator;
drop table dbcloud.capacity_snapshots;
drop table dbcloud.nodes;
drop table dbcloud.cells;
drop table dbcloud.plans;
reset role;
