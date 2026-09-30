-- Idempotency-Key records (plan/01 §10.2, 10-engineering §4): the first response to a mutating request is
-- stored and replayed for 24 h. Keys are scoped to the authenticated caller (principal); every query filters
-- by principal and key, which is the table's primary key. No session state: safe through the transaction pooler.

-- name: ClaimIdempotencyKey :execrows
-- Inserts the key as "in progress" (status_code null). 0 rows means the key already exists; the caller then
-- reads it with GetIdempotencyKey.
insert into dbcloud.idempotency_keys (principal, key, request_hash)
values (sqlc.arg(principal), sqlc.arg(key), sqlc.arg(request_hash))
on conflict (principal, key) do nothing;

-- name: GetIdempotencyKey :one
select request_hash, status_code, response, created_at
from dbcloud.idempotency_keys
where principal = sqlc.arg(principal)
  and key = sqlc.arg(key);

-- name: ReclaimIdempotencyKey :execrows
-- Takes over a stale in-progress claim or an expired record as a new request. observed_created_at is the
-- compare-and-swap token from GetIdempotencyKey: only one of several racing requests wins.
update dbcloud.idempotency_keys
set request_hash = sqlc.arg(request_hash),
    status_code = null,
    response = null,
    created_at = now(),
    completed_at = null
where principal = sqlc.arg(principal)
  and key = sqlc.arg(key)
  and created_at = sqlc.arg(observed_created_at);

-- name: CompleteIdempotencyKey :execrows
-- Stores the final response exactly once; a second completion changes nothing.
update dbcloud.idempotency_keys
set status_code = sqlc.arg(status_code),
    -- Sent as text and cast here: in QueryExecModeExec (transaction pooler) pgx sends []byte as bytea,
    -- which Postgres won't assign to jsonb.
    response = sqlc.arg(response)::text::jsonb,
    completed_at = now()
where principal = sqlc.arg(principal)
  and key = sqlc.arg(key)
  and request_hash = sqlc.arg(request_hash)
  and status_code is null;

-- name: ReleaseIdempotencyKey :execrows
-- Drops an in-progress claim after a failure that must not be replayed (5xx, 429, 409, cancelled request),
-- so the client can retry with the same key.
delete from dbcloud.idempotency_keys
where principal = sqlc.arg(principal)
  and key = sqlc.arg(key)
  and request_hash = sqlc.arg(request_hash)
  and status_code is null;
