package db_test

import (
	"io/fs"
	"testing"

	"github.com/sayanf22/dbaas/db"
)

// mustSub returns the migrations directory of the embedded FS, as goose expects the files at its root.
func mustSub(t *testing.T) fs.FS {
	t.Helper()
	sub, err := fs.Sub(db.Migrations, "migrations")
	if err != nil {
		t.Fatalf("fs.Sub(migrations): %v", err)
	}
	return sub
}
