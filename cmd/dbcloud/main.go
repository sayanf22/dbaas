// Command dbcloud is the customer CLI (plan/01 §13, build plan Step 0.11).
// Step 0.1 skeleton: only `dbcloud version`; login and database commands arrive in Step 0.11.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/sayanf22/dbaas/internal/platform"
)

// main exits with the code returned by run.
func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one CLI command and returns the exit code. Output goes to the
// given writers so tests can capture it; a CLI, unlike a service, prints to stdout.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "version" {
		_, _ = fmt.Fprintln(stdout, platform.Version())
		return 0
	}
	_, _ = fmt.Fprintln(stderr, "usage: dbcloud version")
	return 2
}
