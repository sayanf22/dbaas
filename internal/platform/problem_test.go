package platform

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteProblem(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteProblem(rec, NewProblem(http.StatusConflict, "request-in-progress", "Request in progress", "retry later"))

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d; want %d", rec.Code, http.StatusConflict)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q; want application/problem+json", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q; want no-store", got)
	}
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	want := Problem{Type: "/problems/request-in-progress", Title: "Request in progress", Status: 409, Detail: "retry later"}
	if p != want {
		t.Errorf("problem = %+v; want %+v", p, want)
	}
}
