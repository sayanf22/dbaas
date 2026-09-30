package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sayanf22/dbaas/internal/idempotency"
	"github.com/sayanf22/dbaas/internal/store/queries"
)

// Idempotency is the admin-DB implementation of idempotency.Store (table dbcloud.idempotency_keys, used by
// control-api as dbcloud_app through the transaction pooler). Safe for concurrent use.
type Idempotency struct {
	q *queries.Queries
}

// Idempotency implements the middleware's store.
var _ idempotency.Store = (*Idempotency)(nil)

// NewIdempotency returns a store on db (a pool, or a transaction in tests).
func NewIdempotency(db DB) *Idempotency { return &Idempotency{q: queries.New(db)} }

// Claim inserts an in-progress record; false means the key already exists.
func (s *Idempotency) Claim(ctx context.Context, principal, key string, requestHash []byte) (bool, error) {
	n, err := s.q.ClaimIdempotencyKey(ctx, queries.ClaimIdempotencyKeyParams{
		Principal: principal, Key: key, RequestHash: requestHash,
	})
	if err != nil {
		return false, fmt.Errorf("store: claim idempotency key: %w", err)
	}
	return n == 1, nil
}

// Get returns the record, or idempotency.ErrNotFound.
func (s *Idempotency) Get(ctx context.Context, principal, key string) (idempotency.Record, error) {
	row, err := s.q.GetIdempotencyKey(ctx, queries.GetIdempotencyKeyParams{Principal: principal, Key: key})
	if errors.Is(err, pgx.ErrNoRows) {
		return idempotency.Record{}, idempotency.ErrNotFound
	}
	if err != nil {
		return idempotency.Record{}, fmt.Errorf("store: get idempotency key: %w", err)
	}
	rec := idempotency.Record{RequestHash: row.RequestHash, Response: row.Response, CreatedAt: row.CreatedAt}
	if row.StatusCode != nil {
		rec.StatusCode = int(*row.StatusCode)
	}
	return rec, nil
}

// Reclaim takes over the record if its created_at still equals observed (compare-and-swap).
func (s *Idempotency) Reclaim(ctx context.Context, principal, key string, requestHash []byte, observed time.Time) (bool, error) {
	n, err := s.q.ReclaimIdempotencyKey(ctx, queries.ReclaimIdempotencyKeyParams{
		RequestHash: requestHash, Principal: principal, Key: key, ObservedCreatedAt: observed,
	})
	if err != nil {
		return false, fmt.Errorf("store: reclaim idempotency key: %w", err)
	}
	return n == 1, nil
}

// Complete stores the final response once; completing an already completed claim changes nothing.
func (s *Idempotency) Complete(ctx context.Context, principal, key string, requestHash []byte, statusCode int, response []byte) error {
	if statusCode < 200 || statusCode > 599 || statusCode > math.MaxInt16 {
		return fmt.Errorf("store: status code %d outside 200..599", statusCode)
	}
	code := int16(statusCode) // #nosec G115 -- bounded to 200..599 above
	n, err := s.q.CompleteIdempotencyKey(ctx, queries.CompleteIdempotencyKeyParams{
		StatusCode: &code, Response: string(response), Principal: principal, Key: key, RequestHash: requestHash,
	})
	if err != nil {
		return fmt.Errorf("store: complete idempotency key: %w", err)
	}
	if n != 1 {
		// Our claim is gone or was reclaimed as stale: the response is not stored, so say so.
		return ErrClaimLost
	}
	return nil
}

// ErrClaimLost is returned by Complete when the in-progress claim no longer belongs to this request.
var ErrClaimLost = errors.New("store: idempotency claim lost before completion")

// Release deletes the in-progress claim with requestHash, if any.
func (s *Idempotency) Release(ctx context.Context, principal, key string, requestHash []byte) error {
	if _, err := s.q.ReleaseIdempotencyKey(ctx, queries.ReleaseIdempotencyKeyParams{
		Principal: principal, Key: key, RequestHash: requestHash,
	}); err != nil {
		return fmt.Errorf("store: release idempotency key: %w", err)
	}
	return nil
}
