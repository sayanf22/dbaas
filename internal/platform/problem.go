package platform

import (
	"encoding/json"
	"net/http"
)

// problemTypePrefix makes problem types relative URI references (RFC 9457 §3.1.1), so they don't hard-code
// the API's domain; each slug is documented at /problems/<slug>.
const problemTypePrefix = "/problems/"

// Problem is an RFC 9457 problem details object, the only error body the API returns
// (api/openapi.yaml#/components/schemas/Problem). Detail is shown to clients: never put internal errors,
// SQL, identifiers of other tenants or secrets in it.
type Problem struct {
	// Type is the relative URI /problems/<slug> that identifies the error class; clients branch on it.
	Type string `json:"type"`
	// Title is a short, stable, human-readable summary of the type.
	Title string `json:"title"`
	// Status repeats the HTTP status code, as RFC 9457 §3.1.2 recommends.
	Status int `json:"status"`
	// Detail explains this occurrence and how to fix it; omitted when empty.
	Detail string `json:"detail,omitempty"`
}

// NewProblem builds a Problem whose type is /problems/<slug>.
func NewProblem(status int, slug, title, detail string) Problem {
	return Problem{Type: problemTypePrefix + slug, Title: title, Status: status, Detail: detail}
}

// WriteProblem writes p as application/problem+json with its status code. Error responses are never
// cached. Headers such as Retry-After must be set by the caller before calling it.
func WriteProblem(w http.ResponseWriter, p Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(p.Status)
	if err := json.NewEncoder(w).Encode(p); err != nil {
		return // the client has gone; the status line is already sent
	}
}
