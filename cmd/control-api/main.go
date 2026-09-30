// Command control-api runs the public /v1 and internal /internal/v1 HTTP API (plan/01 §10.1). Step 0.1 skeleton: health endpoints only; routes arrive in Step 0.6.
package main

import (
	"os"

	"github.com/sayanf22/dbaas/internal/platform"
)

// main only wires the service name; all behaviour lives in internal packages.
func main() {
	os.Exit(platform.RunService("control-api", nil))
}
