// Package db holds the admin-DB schema (goose migrations in migrations/) and the sqlc query sources
// (queries/). It embeds the migrations so the in-cell migration Job (plan/01 §10.2) and the tests run
// exactly the files in git.
//
// It deliberately contains no runtime queries or connection handling; those live in internal/store.
package db

import "embed"

// Migrations is the goose migration set, in version order (0000N_name.sql).
//
//go:embed migrations/*.sql
var Migrations embed.FS

// GooseTable is the goose version table name used by every environment (hack/admin-db.sh, the
// migration Job, tests), so all of them agree on which migrations have run.
const GooseTable = "dbcloud_goose_version"
