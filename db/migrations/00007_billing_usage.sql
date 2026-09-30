-- Subscriptions, Razorpay webhook events, invoices and hourly usage (plan/04 §9, plan/01 §17).
-- Money is integer paise; GST is stored separately from the net amount.

-- +goose Up
set local lock_timeout = '5s';
set local role dbcloud_migrator;

create table dbcloud.subscriptions (
  id                       uuid primary key,
  org_id                   uuid not null references dbcloud.organizations (id),
  tenant_id                uuid not null references dbcloud.tenants (id),
  plan_id                  text not null references dbcloud.plans (id),
  razorpay_subscription_id text unique check (razorpay_subscription_id ~ '^sub_[A-Za-z0-9]{8,32}$'),
  state                    text not null default 'created'
                           check (state in ('created', 'authenticated', 'active', 'pending', 'halted', 'cancelled', 'completed', 'expired')),
  current_period_start     timestamptz,
  current_period_end       timestamptz,
  grace_until              timestamptz,
  created_at               timestamptz not null default now(),
  check (current_period_end is null or current_period_start is null or current_period_end > current_period_start)
);
create index subscriptions_org_idx on dbcloud.subscriptions (org_id, created_at desc, id);
create index subscriptions_grace_idx on dbcloud.subscriptions (grace_until) where grace_until is not null;
comment on table dbcloud.subscriptions is 'Razorpay subscription per tenant; state mirrors Razorpay''s subscription states. Owner: worker (billing jobs).';
comment on column dbcloud.subscriptions.grace_until is 'After a failed renewal the tenant keeps running until this time, then it is suspended (dunning, Step 1.11).';

create table dbcloud.payment_events (
  razorpay_event_id text primary key check (length(razorpay_event_id) between 1 and 100),
  received_at       timestamptz not null default now(),
  kind              text not null check (length(kind) between 1 and 100),
  payload_hash      bytea not null check (length(payload_hash) = 32),
  processed_at      timestamptz
);
comment on table dbcloud.payment_events is 'Verified Razorpay webhooks, keyed by event id for deduplication (15-security §3). Only the SHA-256 of the payload is kept. Retained as a financial record (no sweep). Owner: control-api (insert), worker (process).';

create table dbcloud.invoices (
  id           uuid primary key,
  org_id       uuid not null references dbcloud.organizations (id),
  period       daterange not null,
  amount_paise bigint not null check (amount_paise >= 0),
  gst_paise    bigint not null check (gst_paise >= 0),
  currency     text not null default 'INR' check (currency = 'INR'),
  razorpay_ref text,
  state        text not null default 'draft' check (state in ('draft', 'issued', 'paid', 'void')),
  created_at   timestamptz not null default now()
);
create index invoices_org_idx on dbcloud.invoices (org_id, created_at desc, id);
comment on table dbcloud.invoices is 'GST invoices per organization and billing period; amounts in paise. Retained as a financial record (no sweep). Owner: worker.';

create table dbcloud.usage_hourly (
  tenant_id        uuid not null references dbcloud.tenants (id),
  cell_id          text not null references dbcloud.cells (id),
  hour             timestamptz not null check (date_trunc('hour', hour) = hour),
  cpu_m_hours      bigint not null default 0 check (cpu_m_hours >= 0),
  mem_mi_hours     bigint not null default 0 check (mem_mi_hours >= 0),
  storage_gb_hours numeric(12, 3) not null default 0 check (storage_gb_hours >= 0),
  backup_gb_hours  numeric(12, 3) not null default 0 check (backup_gb_hours >= 0),
  primary key (tenant_id, hour)
);
create index usage_hourly_hour_idx on dbcloud.usage_hourly (hour);
comment on table dbcloud.usage_hourly is 'Measured usage per tenant and hour, for customer charts and future usage-based add-ons. Exported to R2 and swept after 90 days (plan/01 §10.2). Owner: worker.';

-- Back to the connecting role: goose records the version in this transaction.
reset role;

-- +goose Down
set local lock_timeout = '5s';
set local role dbcloud_migrator;
drop table dbcloud.usage_hourly;
drop table dbcloud.invoices;
drop table dbcloud.payment_events;
drop table dbcloud.subscriptions;
reset role;
