// Package idempotency implements the Idempotency-Key contract of api/openapi.yaml for mutating requests
// (plan/01 §10.2, 10-engineering-standards.md §4, 15-security-and-reliability.md §7).
//
// The first 2xx response to a (caller, key) pair is stored and replayed for 24 h with the header
// Idempotent-Replayed: true. A key reused with a different request is rejected with 422; a key whose request
// is still running gets 409 + Retry-After; a success that carried a one-time secret (Cache-Control: no-store)
// is never stored, so its replay gets 409 instead of the secret. Failed requests (non-2xx, panics, oversized
// responses, cancelled clients) release the key: handlers change nothing unless they succeed, so the client
// may retry with the same key.
//
// It doesn't authenticate: the caller's identity comes from a PrincipalFunc supplied by the identity
// middleware (Step 0.6). It doesn't replace handler-level uniqueness either: operations are also unique by
// (org_id, idempotency_key) in the admin DB, which covers the rare case where storing the response fails.
package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/sayanf22/dbaas/internal/platform"
)

// Header names of the contract (api/openapi.yaml, parameter IdempotencyKey).
const (
	// HeaderKey carries the client-chosen key.
	HeaderKey = "Idempotency-Key"
	// HeaderReplayed marks a response served from the stored record instead of the handler.
	HeaderReplayed = "Idempotent-Replayed"
)

const (
	// ttl: stored responses are replayed for 24 h (plan/01 §10.2); after that the key is free again and the
	// worker sweeps the row.
	ttl = 24 * time.Hour
	// staleAfter: an in-progress claim older than this is treated as abandoned. It must exceed the longest a
	// request can run: the server WriteTimeout is 60 s (internal/platform).
	staleAfter = 2 * time.Minute
	// minKeyLen, maxKeyLen: key length bounds in bytes, the same as the OpenAPI parameter.
	minKeyLen, maxKeyLen = 8, 255
	// maxRequestBody: 1 MiB, the API's body limit (15-security §5); it is also enforced by the platform server.
	maxRequestBody = 1 << 20
	// maxResponseBody: 4 MiB. The largest documented response (a 100-item page) is far smaller, so a response
	// beyond this is a bug; it is not stored and the request fails with 500.
	maxResponseBody = 4 << 20
	// bookkeepingTimeout bounds each store call made after the handler ran, independent of the client's
	// connection, so a disconnect can't leave a key claimed.
	bookkeepingTimeout = 5 * time.Second
	// inProgressRetryAfter, unavailableRetryAfter: Retry-After values in seconds.
	inProgressRetryAfter  = "2"
	unavailableRetryAfter = "5"
)

// ErrNotFound is returned by Store.Get when no record exists for the principal and key.
var ErrNotFound = errors.New("idempotency: record not found")

// errResponseTooLarge is returned to a handler that writes more than maxResponseBody.
var errResponseTooLarge = errors.New("idempotency: response exceeds the storable size")

// principalPattern matches the principal format the admin DB accepts (kind:id, e.g. user:<uuid>).
var principalPattern = regexp.MustCompile(`^[a-z_]{2,20}:[A-Za-z0-9-]{1,64}$`)

// Record is one stored key: the request it belongs to and, once finished, its response.
type Record struct {
	RequestHash []byte    // SHA-256 of method, path, query and body
	StatusCode  int       // 0 while the request is in progress
	Response    []byte    // JSON storedResponse; nil while in progress
	CreatedAt   time.Time // when the current claim was made; the compare-and-swap token for Reclaim
}

// Store persists records. Implementations must make Claim, Reclaim and Complete atomic per (principal, key).
// The admin-DB implementation is store.Idempotency; tests use an in-memory one.
type Store interface {
	// Claim inserts an in-progress record; false means a record already exists.
	Claim(ctx context.Context, principal, key string, requestHash []byte) (bool, error)
	// Get returns the record or ErrNotFound.
	Get(ctx context.Context, principal, key string) (Record, error)
	// Reclaim replaces the record as a new in-progress claim if its CreatedAt still equals observed.
	Reclaim(ctx context.Context, principal, key string, requestHash []byte, observed time.Time) (bool, error)
	// Complete stores the final response of the in-progress claim with requestHash; it runs at most once.
	Complete(ctx context.Context, principal, key string, requestHash []byte, statusCode int, response []byte) error
	// Release deletes the in-progress claim with requestHash, if any.
	Release(ctx context.Context, principal, key string, requestHash []byte) error
}

// PrincipalFunc returns the authenticated caller of r as kind:id (user:<uuid>, api_key:<uuid>), or false when
// the request is unauthenticated.
type PrincipalFunc func(r *http.Request) (principal string, ok bool)

// storedResponse is the JSON kept in Record.Response. Only headers needed to replay the response are kept.
type storedResponse struct {
	NoStore bool              `json:"no_store,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    []byte            `json:"body,omitempty"`
}

// replayedHeaders are the response headers stored and replayed; everything else is dropped.
var replayedHeaders = [...]string{"Content-Type", "Location"}

// Middleware enforces the contract for the handlers it wraps. Safe for concurrent use.
type Middleware struct {
	store     Store
	principal PrincipalFunc
	now       func() time.Time
	log       *slog.Logger
}

// New returns a Middleware. now is time.Now in production and a fake clock in tests.
func New(store Store, principal PrincipalFunc, now func() time.Time, log *slog.Logger) *Middleware {
	return &Middleware{store: store, principal: principal, now: now, log: log}
}

// Wrap applies the contract to POST, PUT, PATCH and DELETE requests; other methods pass straight through.
// Mount it only on routes that take an Idempotency-Key (not on webhooks, which dedupe by event id).
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
			m.serve(w, r, next)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// serve validates the request, acquires the key, runs next and records or releases the key.
func (m *Middleware) serve(w http.ResponseWriter, r *http.Request, next http.Handler) {
	key := r.Header.Get(HeaderKey)
	if !validKey(key) {
		problem(w, http.StatusBadRequest, "idempotency-key-invalid", "Invalid Idempotency-Key",
			"Send an Idempotency-Key header of 8 to 255 visible ASCII characters with every mutating request.")
		return
	}
	principal, ok := m.principal(r)
	if !ok {
		problem(w, http.StatusUnauthorized, "unauthorized", "Authentication required", "")
		return
	}
	if !principalPattern.MatchString(principal) {
		m.log.Error("idempotency: identity middleware produced a malformed principal")
		problem(w, http.StatusInternalServerError, "internal", "Internal error", "")
		return
	}
	body, err := readBody(r)
	if err != nil {
		if errors.Is(err, errRequestTooLarge) {
			problem(w, http.StatusRequestEntityTooLarge, "body-too-large", "Request body too large", "The limit is 1 MiB.")
			return
		}
		problem(w, http.StatusBadRequest, "body-unreadable", "Request body could not be read", "")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	hash := requestHash(r, body)
	ctx := r.Context()

	if !m.acquire(ctx, w, principal, key, hash) {
		return
	}
	// This request owns the key from here on.
	keep := false
	defer m.releaseUnlessKept(ctx, &keep, principal, key, hash)
	rec := &recorder{header: make(http.Header)}
	next.ServeHTTP(rec, r)
	if rec.overflow {
		m.log.Error("idempotency: response too large to store", slog.Int("limit_bytes", maxResponseBody))
		problem(w, http.StatusInternalServerError, "internal", "Internal error", "")
		return
	}
	if status := rec.statusCode(); status >= 200 && status < 300 {
		// The success happened; even if storing it fails, the key must not be re-run at once.
		keep = true
		m.complete(ctx, principal, key, hash, status, rec)
	}
	rec.writeTo(w)
}

// releaseUnlessKept frees the claim unless *keep was set. It is deferred by serve, so it also runs while a
// handler panic unwinds (the platform server recovers the panic): any failure frees the key.
func (m *Middleware) releaseUnlessKept(ctx context.Context, keep *bool, principal, key string, hash []byte) {
	if !*keep {
		m.release(ctx, principal, key, hash)
	}
}

// acquire makes this request the owner of the key, or answers it from the existing record. It returns true
// when the caller should run the handler.
func (m *Middleware) acquire(ctx context.Context, w http.ResponseWriter, principal, key string, hash []byte) bool {
	claimed, err := m.store.Claim(ctx, principal, key, hash)
	if err != nil {
		m.unavailable(w, err)
		return false
	}
	if claimed {
		return true
	}
	rec, err := m.store.Get(ctx, principal, key)
	if errors.Is(err, ErrNotFound) {
		// Released between our Claim and Get by a request that just failed; the client retries shortly.
		inProgress(w)
		return false
	}
	if err != nil {
		m.unavailable(w, err)
		return false
	}
	age := m.now().Sub(rec.CreatedAt)
	sameRequest := bytes.Equal(rec.RequestHash, hash)
	switch {
	case age >= ttl || (rec.StatusCode == 0 && sameRequest && age >= staleAfter):
		// Expired record, or an abandoned claim of this same request: take it over. Only one racer wins.
		won, err := m.store.Reclaim(ctx, principal, key, hash, rec.CreatedAt)
		if err != nil {
			m.unavailable(w, err)
			return false
		}
		if !won {
			inProgress(w)
		}
		return won
	case !sameRequest:
		problem(w, http.StatusUnprocessableEntity, "idempotency-key-reused", "Idempotency-Key reused",
			"This key was already used with a different request in the last 24 hours. Use a new key.")
	case rec.StatusCode == 0:
		inProgress(w)
	default:
		m.replay(w, rec)
	}
	return false
}

// replay answers from a completed record.
func (m *Middleware) replay(w http.ResponseWriter, rec Record) {
	var stored storedResponse
	if err := json.Unmarshal(rec.Response, &stored); err != nil {
		m.log.Error("idempotency: stored response is unreadable", slog.String("error", err.Error()))
		problem(w, http.StatusInternalServerError, "internal", "Internal error", "")
		return
	}
	if stored.NoStore {
		problem(w, http.StatusConflict, "secret-not-replayable", "Response not replayable",
			"The original request succeeded, but its response held a one-time secret and was not stored. "+
				"Look the resource up instead of retrying.")
		return
	}
	for name, value := range stored.Headers {
		w.Header().Set(name, value)
	}
	w.Header().Set(HeaderReplayed, "true")
	w.WriteHeader(rec.StatusCode)
	if _, err := w.Write(stored.Body); err != nil {
		return // the client has gone
	}
}

// complete stores the owner's successful response. A failure is logged, not surfaced: the client still gets
// the real response, and the key stays claimed until it goes stale.
func (m *Middleware) complete(ctx context.Context, principal, key string, hash []byte, status int, rec *recorder) {
	stored := storedResponse{NoStore: rec.noStore()}
	if !stored.NoStore {
		stored.Headers = rec.replayHeaders()
		stored.Body = rec.body.Bytes()
	}
	payload, err := json.Marshal(stored)
	if err == nil {
		bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
		defer cancel()
		err = m.store.Complete(bctx, principal, key, hash, status, payload)
	}
	if err != nil {
		m.log.Error("idempotency: store response; key stays claimed until stale",
			slog.String("error", err.Error()), slog.Duration("stale_after", staleAfter))
	}
}

// release frees the owner's claim after a failure. A failure here is harmless: the claim goes stale.
func (m *Middleware) release(ctx context.Context, principal, key string, hash []byte) {
	bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bookkeepingTimeout)
	defer cancel()
	if err := m.store.Release(bctx, principal, key, hash); err != nil {
		m.log.Warn("idempotency: release claim; it frees itself once stale",
			slog.String("error", err.Error()), slog.Duration("stale_after", staleAfter))
	}
}

// unavailable answers 503 when the store can't be reached (degrade explicitly, 15-security §7).
func (m *Middleware) unavailable(w http.ResponseWriter, err error) {
	m.log.Error("idempotency: store unavailable", slog.String("error", err.Error()))
	w.Header().Set("Retry-After", unavailableRetryAfter)
	problem(w, http.StatusServiceUnavailable, "unavailable", "Service temporarily unavailable",
		"The request was not processed. Retry with the same Idempotency-Key.")
}

// inProgress answers 409 while another request with the same key is running.
func inProgress(w http.ResponseWriter) {
	w.Header().Set("Retry-After", inProgressRetryAfter)
	problem(w, http.StatusConflict, "request-in-progress", "Request in progress",
		"A request with this Idempotency-Key is still running. Retry after a short wait.")
}

// problem writes an RFC 9457 error.
func problem(w http.ResponseWriter, status int, slug, title, detail string) {
	platform.WriteProblem(w, platform.NewProblem(status, slug, title, detail))
}

// validKey reports whether key has the allowed length and only visible ASCII (0x21-0x7e).
func validKey(key string) bool {
	if len(key) < minKeyLen || len(key) > maxKeyLen {
		return false
	}
	for i := range len(key) {
		if key[i] < 0x21 || key[i] > 0x7e {
			return false
		}
	}
	return true
}

// errRequestTooLarge is returned by readBody for bodies over maxRequestBody.
var errRequestTooLarge = errors.New("idempotency: request body too large")

// readBody reads the whole body, up to maxRequestBody.
func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return nil, errRequestTooLarge
	case err != nil:
		return nil, err
	case len(body) > maxRequestBody:
		return nil, errRequestTooLarge
	}
	return body, nil
}

// requestHash is SHA-256 over method, escaped path, raw query and body, NUL-separated (none of the first
// three can contain NUL), so the same key can't be replayed for a different request.
func requestHash(r *http.Request, body []byte) []byte {
	h := sha256.New()
	for _, part := range []string{r.Method, r.URL.EscapedPath(), r.URL.RawQuery} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	h.Write(body)
	return h.Sum(nil)
}

// recorder buffers the handler's response so it can be stored before it is sent.
type recorder struct {
	header   http.Header
	status   int
	body     bytes.Buffer
	overflow bool
}

// Header returns the buffered response headers.
func (r *recorder) Header() http.Header { return r.header }

// WriteHeader keeps the first status code, like net/http.
func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
}

// Write buffers b, failing once the response would exceed maxResponseBody.
func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	if r.body.Len()+len(b) > maxResponseBody {
		r.overflow = true
		return 0, errResponseTooLarge
	}
	return r.body.Write(b)
}

// statusCode is the response status, 200 if the handler set none.
func (r *recorder) statusCode() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

// noStore reports whether the handler marked the response as not storable (one-time secrets).
func (r *recorder) noStore() bool {
	return strings.Contains(strings.ToLower(r.header.Get("Cache-Control")), "no-store")
}

// replayHeaders returns the replayedHeaders the handler set.
func (r *recorder) replayHeaders() map[string]string {
	out := make(map[string]string, len(replayedHeaders))
	for _, name := range replayedHeaders {
		if v := r.header.Get(name); v != "" {
			out[name] = v
		}
	}
	return out
}

// writeTo sends the buffered response to w.
func (r *recorder) writeTo(w http.ResponseWriter) {
	for name, values := range r.header {
		w.Header()[name] = values
	}
	w.WriteHeader(r.statusCode())
	if _, err := w.Write(r.body.Bytes()); err != nil {
		return // the client has gone
	}
}
