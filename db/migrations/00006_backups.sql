-- Backups, restore points and credential metadata (plan/04 §9, plan/01 §14).

-- +goose Up
set local lock_timeout = '5s';
set local role dbcloud_migrator;

create table dbcloud.backups (
  id          uuid primary key,
  tenant_id   uuid not null references dbcloud.tenants (id),
  cell_id     text not null references dbcloud.cells (id),
  kind        text not null check (kind in ('scheduled', 'on_demand', 'pre_change')),
  started_at  timestamptz not null,
  finished_at timestamptz,
  size_bytes  bigint check (size_bytes >= 0),
  begin_lsn   pg_lsn,
  end_lsn     pg_lsn,
  status      text not null check (status in ('running', 'completed', 'failed'))
);
create index backups_tenant_started_idx on dbcloud.backups (tenant_id, started_at desc);
comment on table dbcloud.backups is 'Base backups reported by the cell (CNPG Backup objects). Owner: worker.';

create table dbcloud.restore_points (
  tenant_id                  uuid primary key references dbcloud.tenants (id),
  cell_id                    text not null references dbcloud.cells (id),
  first_recoverability_point timestamptz,
  last_archived_wal_at       timestamptz
);
comment on table dbcloud.restore_points is 'PITR window per tenant, shown to customers and used by the WAL-archive-age alert. Owner: worker.';

create table dbcloud.credentials_meta (
  id         uuid primary key,
  tenant_id  uuid not null references dbcloud.tenants (id),
  role       text not null check (role ~ '^[a-z_][a-z0-9_]{0,62}$'),
  version    integer not null check (version >= 1),
  created_at timestamptz not null default now(),
  revoked_at timestamptz,
  unique (tenant_id, role, version)
);
comment on table dbcloud.credentials_meta is 'Versions of tenant database credentials for create-before-revoke rotation. Never stores the secret (15-security §4). Owner: worker.';

-- Back to the connecting role: goose records the version in this transaction.
reset role;

-- +goose Down
set local lock_timeout = '5s';
set local role dbcloud_migrator;
drop table dbcloud.credentials_meta;
drop table dbcloud.restore_points;
drop table dbcloud.backups;
reset role;
