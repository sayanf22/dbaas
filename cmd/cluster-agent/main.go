// Command cluster-agent runs the per-cell pull agent that applies desired state from control-api (plan/01 §10.5). Step 0.1 skeleton: health endpoints only; the agent loop arrives in Step 0.8.
package main

import (
	"os"

	"github.com/sayanf22/dbaas/internal/platform"
)

// main only wires the service name; all behaviour lives in internal packages.
func main() {
	os.Exit(platform.RunService("cluster-agent", nil))
}
