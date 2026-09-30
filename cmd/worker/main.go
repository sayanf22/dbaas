// Command worker runs the River job worker: provisioning, placement, capacity, DNS, billing and retention jobs (plan/01 §10.3). Step 0.1 skeleton: health endpoints only; jobs arrive in Step 0.7.
package main

import (
	"os"

	"github.com/sayanf22/dbaas/internal/platform"
)

// main only wires the service name; all behaviour lives in internal packages.
func main() {
	os.Exit(platform.RunService("worker", nil))
}
