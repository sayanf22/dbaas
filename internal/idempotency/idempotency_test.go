package idempotency

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// memStore is an in-memory Store with the same atomicity as the admin-DB one (one mutex).
type memStore struct {
	mu      sync.Mutex
	now     func() time.Time
	records map[string]Record
	failAll bool
}

func newMemStore(now func() time.Time) *memStore {
	return &memStore{now: now, records: map[string]Record{}}
}

func (s *memStore) id(p, key string) string { return p + "\x00" + key }

func (s *memStore) fail() error {
	if s.failAll {
		return errors.New("store down")
	}
	return nil
}

func (s *memStore) Claim(_ context.Context, p, key string, h []byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.fail(); err != nil {
		return false, err
	}
	if _, ok := s.records[s.id(p, key)]; ok {
		return false, nil
	}
	s.records[s.id(p, key)] = Record{RequestHash: h, CreatedAt: s.now()}
	return true, nil
}

func (s *memStore) Get(_ context.Context, p, key string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.fail(); err != nil {
		return Record{}, err
	}
	r, ok := s.records[s.id(p, key)]
	if !ok {
		return Record{}, ErrNotFound
	}
	return r, nil
}

func (s *memStore) Reclaim(_ context.Context, p, key string, h []byte, observed time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.fail(); err != nil {
		return false, err
	}
	r, ok := s.records[s.id(p, key)]
	if !ok || !r.CreatedAt.Equal(observed) {
		return false, nil
	}
	s.records[s.id(p, key)] = Record{RequestHash: h, CreatedAt: s.now()}
	return true, nil
}

func (s *memStore) Complete(_ context.Context, p, key string, h []byte, status int, resp []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.fail(); err != nil {
		return err
	}
	r, ok := s.records[s.id(p, key)]
	if ok && bytes.Equal(r.RequestHash, h) && r.StatusCode == 0 {
		r.StatusCode, r.Response = status, resp
		s.records[s.id(p, key)] = r
	}
	return nil
}

func (s *memStore) Release(_ context.Context, p, key string, h []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.fail(); err != nil {
		return err
	}
	if r, ok := s.records[s.id(p, key)]; ok && bytes.Equal(r.RequestHash, h) && r.StatusCode == 0 {
		delete(s.records, s.id(p, key))
	}
	return nil
}

// clock is a settable fake clock; tests never sleep.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// fixture wires the middleware around a counting handler.
type fixture struct {
	store *memStore
	clock *clock
	calls atomic.Int64
	h     http.Handler
}

func newFixture(handler func(w http.ResponseWriter, r *http.Request, call int64)) *fixture {
	f := &fixture{clock: &clock{t: time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)}}
	f.store = newMemStore(f.clock.now)
	principal := func(r *http.Request) (string, bool) {
		p := r.Header.Get("X-Test-Principal")
		return p, p != ""
	}
	m := New(f.store, principal, f.clock.now, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.h = m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(w, r, f.calls.Add(1))
	}))
	return f
}

// do sends one request and returns the recorded response.
func (f *fixture) do(ctx context.Context, method, key, principal, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(ctx, method, "/v1/databases", strings.NewReader(body))
	if key != "" {
		req.Header.Set(HeaderKey, key)
	}
	if principal != "" {
		req.Header.Set("X-Test-Principal", principal)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

// accepted answers 202 with a body that differs per call, so a replay is distinguishable from a re-run.
func accepted(w http.ResponseWriter, _ *http.Request, call int64) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", "/v1/operations/op-1")
	w.WriteHeader(http.StatusAccepted)
	if err := json.NewEncoder(w).Encode(map[string]int64{"call": call}); err != nil {
		panic(err) // the recorder never fails for this size
	}
}

func problemSlug(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var p struct{ Type string }
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("body %q is not a problem: %v", rec.Body.String(), err)
	}
	return strings.TrimPrefix(p.Type, "/problems/")
}

const (
	alice = "user:0192a3b4-0000-7000-8000-000000000001"
	bob   = "user:0192a3b4-0000-7000-8000-000000000002"
	key1  = "key-00000001"
)

func TestReplayReturnsTheOriginalResponse(t *testing.T) {
	f := newFixture(accepted)
	first := f.do(t.Context(), http.MethodPost, key1, alice, `{"name":"a"}`)
	second := f.do(t.Context(), http.MethodPost, key1, alice, `{"name":"a"}`)

	if first.Code != http.StatusAccepted || second.Code != http.StatusAccepted {
		t.Fatalf("status = %d, %d; want 202, 202", first.Code, second.Code)
	}
	if second.Body.String() != first.Body.String() {
		t.Errorf("replayed body = %q; want original %q", second.Body.String(), first.Body.String())
	}
	if got := f.calls.Load(); got != 1 {
		t.Errorf("handler calls = %d; want 1", got)
	}
	if second.Header().Get(HeaderReplayed) != "true" || first.Header().Get(HeaderReplayed) != "" {
		t.Errorf("Idempotent-Replayed = %q (first %q); want true only on the replay",
			second.Header().Get(HeaderReplayed), first.Header().Get(HeaderReplayed))
	}
	if got := second.Header().Get("Location"); got != "/v1/operations/op-1" {
		t.Errorf("replayed Location = %q; want /v1/operations/op-1", got)
	}
}

func TestContractErrors(t *testing.T) {
	tests := []struct {
		name       string
		prepare    func(ctx context.Context, f *fixture)
		key        string
		principal  string
		body       string
		wantStatus int
		wantSlug   string
	}{
		{"missing key", nil, "", alice, "{}", 400, "idempotency-key-invalid"},
		{"key too short", nil, "short", alice, "{}", 400, "idempotency-key-invalid"},
		{"key with space", nil, "has a space1", alice, "{}", 400, "idempotency-key-invalid"},
		{"key too long", nil, strings.Repeat("k", 256), alice, "{}", 400, "idempotency-key-invalid"},
		{"unauthenticated", nil, key1, "", "{}", 401, "unauthorized"},
		{"malformed principal", nil, key1, "not a principal", "{}", 500, "internal"},
		{"body over 1 MiB", nil, key1, alice, strings.Repeat("x", maxRequestBody+1), 413, "body-too-large"},
		{"same key, different body", func(ctx context.Context, f *fixture) {
			f.do(ctx, http.MethodPost, key1, alice, `{"name":"a"}`)
		}, key1, alice, `{"name":"b"}`, 422, "idempotency-key-reused"},
		{"store down", func(_ context.Context, f *fixture) { f.store.failAll = true }, key1, alice, "{}", 503, "unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(accepted)
			if tt.prepare != nil {
				tt.prepare(t.Context(), f)
			}
			calls := f.calls.Load()
			rec := f.do(t.Context(), http.MethodPost, tt.key, tt.principal, tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d; want %d (body %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if got := problemSlug(t, rec); got != tt.wantSlug {
				t.Errorf("problem = %q; want %q", got, tt.wantSlug)
			}
			if f.calls.Load() != calls {
				t.Error("handler ran on a rejected request")
			}
			if tt.wantStatus == 503 && rec.Header().Get("Retry-After") == "" {
				t.Error("503 without Retry-After")
			}
		})
	}
}

func TestKeysAreScopedPerCaller(t *testing.T) {
	f := newFixture(accepted)
	a := f.do(t.Context(), http.MethodPost, key1, alice, `{"name":"a"}`)
	b := f.do(t.Context(), http.MethodPost, key1, bob, `{"name":"a"}`)
	if a.Code != 202 || b.Code != 202 || f.calls.Load() != 2 {
		t.Errorf("status %d/%d, calls %d; want 202/202 and 2 calls", a.Code, b.Code, f.calls.Load())
	}
	if b.Header().Get(HeaderReplayed) != "" {
		t.Error("bob received alice's replayed response")
	}
}

func TestFailuresReleaseTheKey(t *testing.T) {
	for _, status := range []int{400, 409, 429, 500, 503} {
		f := newFixture(func(w http.ResponseWriter, r *http.Request, call int64) {
			if call == 1 {
				w.WriteHeader(status)
				return
			}
			accepted(w, r, call)
		})
		first := f.do(t.Context(), http.MethodPost, key1, alice, "{}")
		retry := f.do(t.Context(), http.MethodPost, key1, alice, "{}")
		if first.Code != status || retry.Code != 202 || f.calls.Load() != 2 {
			t.Errorf("after %d: retry status %d, calls %d; want 202 and 2 calls", status, retry.Code, f.calls.Load())
		}
	}
}

func TestPanicReleasesTheKey(t *testing.T) {
	f := newFixture(func(w http.ResponseWriter, r *http.Request, call int64) {
		if call == 1 {
			panic("boom")
		}
		accepted(w, r, call)
	})
	func() {
		defer func() {
			if recover() == nil {
				t.Error("panic was swallowed; the platform server's recovery must see it")
			}
		}()
		f.do(t.Context(), http.MethodPost, key1, alice, "{}")
	}()
	if retry := f.do(t.Context(), http.MethodPost, key1, alice, "{}"); retry.Code != 202 {
		t.Errorf("retry after panic = %d; want 202", retry.Code)
	}
}

func TestInProgressThenStaleClaim(t *testing.T) {
	f := newFixture(accepted)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/databases", nil)
	// A claim left by a request that is still running, or that crashed.
	if _, err := f.store.Claim(t.Context(), alice, key1, requestHash(req, []byte("{}"))); err != nil {
		t.Fatal(err)
	}
	busy := f.do(t.Context(), http.MethodPost, key1, alice, "{}")
	if busy.Code != 409 || problemSlug(t, busy) != "request-in-progress" || busy.Header().Get("Retry-After") == "" {
		t.Fatalf("running claim: %d %s; want 409 request-in-progress with Retry-After", busy.Code, busy.Body.String())
	}
	f.clock.advance(staleAfter)
	if again := f.do(t.Context(), http.MethodPost, key1, alice, "{}"); again.Code != 202 || f.calls.Load() != 1 {
		t.Errorf("stale claim: status %d, calls %d; want the request to run exactly once", again.Code, f.calls.Load())
	}
}

func TestExpiredKeyRunsAgain(t *testing.T) {
	f := newFixture(accepted)
	f.do(t.Context(), http.MethodPost, key1, alice, `{"name":"a"}`)
	f.clock.advance(ttl)
	// After 24 h the key is free: even a different request may use it.
	rec := f.do(t.Context(), http.MethodPost, key1, alice, `{"name":"b"}`)
	if rec.Code != 202 || rec.Header().Get(HeaderReplayed) != "" || f.calls.Load() != 2 {
		t.Errorf("after ttl: status %d, replayed %q, calls %d; want a fresh run", rec.Code, rec.Header().Get(HeaderReplayed), f.calls.Load())
	}
}

func TestOneTimeSecretsAreNeverStored(t *testing.T) {
	f := newFixture(func(w http.ResponseWriter, _ *http.Request, _ int64) {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusCreated)
		if _, err := w.Write([]byte(`{"secret":"dbc_live_notarealsecret"}`)); err != nil {
			panic(err)
		}
	})
	first := f.do(t.Context(), http.MethodPost, key1, alice, "{}")
	if first.Code != 201 || !strings.Contains(first.Body.String(), "notarealsecret") {
		t.Fatalf("first response = %d %s; want the secret once", first.Code, first.Body.String())
	}
	for _, r := range f.store.records {
		if bytes.Contains(r.Response, []byte("notarealsecret")) {
			t.Fatal("the one-time secret was stored")
		}
	}
	replay := f.do(t.Context(), http.MethodPost, key1, alice, "{}")
	if replay.Code != 409 || problemSlug(t, replay) != "secret-not-replayable" || f.calls.Load() != 1 {
		t.Errorf("replay = %d %s, calls %d; want 409 secret-not-replayable without re-running", replay.Code, replay.Body.String(), f.calls.Load())
	}
}

func TestSafeMethodsPassThrough(t *testing.T) {
	f := newFixture(accepted)
	for range 2 {
		if rec := f.do(t.Context(), http.MethodGet, "", "", ""); rec.Code != 202 {
			t.Fatalf("GET status = %d; want the handler's 202", rec.Code)
		}
	}
	if f.calls.Load() != 2 || len(f.store.records) != 0 {
		t.Errorf("GET: calls %d, records %d; want 2 calls and nothing stored", f.calls.Load(), len(f.store.records))
	}
}

// TestConcurrentDuplicatesRunOnce fires the same request 20 times at once: the handler runs exactly once,
// every other request gets either the replay or 409 in progress, never a second execution.
func TestConcurrentDuplicatesRunOnce(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	f := newFixture(func(w http.ResponseWriter, r *http.Request, call int64) {
		close(started) // a second call would panic here: that is the assertion that it runs once
		<-release
		accepted(w, r, call)
	})
	const n = 20
	codes := make(chan int, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() { codes <- f.do(t.Context(), http.MethodPost, key1, alice, "{}").Code })
	}
	// The winner holds the key until released; requests that arrive meanwhile see it in progress.
	<-started
	close(release)
	wg.Wait()
	close(codes)
	counts := map[int]int{}
	for c := range codes {
		counts[c]++
	}
	if f.calls.Load() != 1 {
		t.Errorf("handler calls = %d; want 1", f.calls.Load())
	}
	if counts[202]+counts[409] != n || counts[202] < 1 {
		t.Errorf("status counts = %v; want only 202 and 409, at least one 202", counts)
	}
}
