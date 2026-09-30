// Package platform holds the process plumbing every dbcloud Go service shares:
// structured logging with secret redaction, the HTTP server with the timeouts
// and graceful drain required by plan/07 and 20-go.md, health endpoints, and
// the RunService entry point used by each cmd/<service>/main.go.
//
// It deliberately contains no business logic, no database access and no
// Kubernetes client; those live in their own internal packages.
package platform
