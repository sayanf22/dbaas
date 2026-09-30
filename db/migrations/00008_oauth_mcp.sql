-- OAuth 2.1 storage for mcp-auth and MCP grants/approvals (plan/04 §9, plan/01 §13, ADR-009).
-- Codes and tokens are stored only as SHA-256 hashes (15-security §3).

-- +goose Up
set local lock_timeout = '5s';
set local role dbcloud_migrator;

create table dbcloud.oauth_clients (
  id         uuid primary key,
  client_id  text not null unique check (length(client_id) between 1 and 2048),
  kind       text not null check (kind in ('cimd', 'dcr', 'first_party')),
  metadata   jsonb not null check (jsonb_typeof(metadata) = 'object'),
  fetched_at timestamptz not null default now(),
  blocked    boolean not null default false
);
comment on table dbcloud.oauth_clients is 'MCP OAuth clients: Client ID Metadata Document cache (client_id is the document URL), DCR clients and first-party clients (CLI). Owner: mcp-auth.';

create table dbcloud.mcp_grants (
  id           uuid primary key,
  user_id      uuid not null references dbcloud.users (id),
  client_id    text not null references dbcloud.oauth_clients (client_id),
  org_id       uuid not null references dbcloud.organizations (id),
  tenant_ids   uuid[] not null default '{}',
  mode         text not null check (mode in ('read', 'read_write')),
  capabilities text[] not null default '{}',
  created_at   timestamptz not null default now(),
  expires_at   timestamptz not null,
  revoked_at   timestamptz
);
create index mcp_grants_org_idx on dbcloud.mcp_grants (org_id, created_at desc, id);
create index mcp_grants_user_idx on dbcloud.mcp_grants (user_id);
comment on table dbcloud.mcp_grants is 'What an MCP client may do for a user in one org: tenants, read or read_write, capabilities. Owner: control-api (consent), mcp-server (checks).';

create table dbcloud.oauth_codes (
  code_hash      bytea primary key check (length(code_hash) = 32),
  client_id      text not null references dbcloud.oauth_clients (client_id),
  user_id        uuid not null references dbcloud.users (id),
  grant_id       uuid not null references dbcloud.mcp_grants (id),
  pkce_challenge text not null check (pkce_challenge ~ '^[A-Za-z0-9_-]{43}$'),
  resource       text not null check (length(resource) between 1 and 2048),
  expires_at     timestamptz not null
);
create index oauth_codes_expires_idx on dbcloud.oauth_codes (expires_at);
comment on table dbcloud.oauth_codes is 'Authorization codes (single use, ≤ 60 s); deleted when redeemed or expired. pkce_challenge is S256 only. Owner: mcp-auth.';

create table dbcloud.oauth_refresh_tokens (
  token_hash bytea primary key check (length(token_hash) = 32),
  client_id  text not null references dbcloud.oauth_clients (client_id),
  user_id    uuid not null references dbcloud.users (id),
  grant_id   uuid not null references dbcloud.mcp_grants (id),
  family_id  uuid not null,
  expires_at timestamptz not null,
  revoked_at timestamptz
);
create index oauth_refresh_tokens_family_idx on dbcloud.oauth_refresh_tokens (family_id);
create index oauth_refresh_tokens_expires_idx on dbcloud.oauth_refresh_tokens (expires_at);
comment on table dbcloud.oauth_refresh_tokens is 'Rotating refresh tokens; reuse of a rotated token revokes its whole family. Owner: mcp-auth.';

create table dbcloud.oauth_revoked_tokens (
  jti        text primary key check (length(jti) between 16 and 64),
  expires_at timestamptz not null
);
create index oauth_revoked_tokens_expires_idx on dbcloud.oauth_revoked_tokens (expires_at);
comment on table dbcloud.oauth_revoked_tokens is 'Access-token revocation list, pruned once the token would have expired anyway. Owner: mcp-auth.';

create table dbcloud.approvals (
  id          uuid primary key,
  grant_id    uuid not null references dbcloud.mcp_grants (id),
  op_hash     bytea not null check (length(op_hash) = 32),
  tenant_id   uuid not null references dbcloud.tenants (id),
  database_id uuid not null references dbcloud.databases (id),
  actor       uuid not null references dbcloud.users (id),
  scope       text not null check (length(scope) between 1 and 200),
  created_at  timestamptz not null default now(),
  expires_at  timestamptz not null,
  used_at     timestamptz
);
create index approvals_grant_idx on dbcloud.approvals (grant_id, created_at desc);
comment on table dbcloud.approvals is 'Human approval for one exact MCP write (op_hash binds it to the statement); single use. Owner: control-api (approve), mcp-server (consume).';

-- Back to the connecting role: goose records the version in this transaction.
reset role;

-- +goose Down
set local lock_timeout = '5s';
set local role dbcloud_migrator;
drop table dbcloud.approvals;
drop table dbcloud.oauth_revoked_tokens;
drop table dbcloud.oauth_refresh_tokens;
drop table dbcloud.oauth_codes;
drop table dbcloud.mcp_grants;
drop table dbcloud.oauth_clients;
reset role;
