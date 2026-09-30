// Command tenant-operator runs the TenantDatabase operator that renders CloudNativePG objects per tenant (plan/01 §6). Step 0.1 skeleton: health endpoints only; controllers arrive in Step 0.4.
package main

import (
	"os"

	"github.com/sayanf22/dbaas/internal/platform"
)

// main only wires the service name; all behaviour lives in internal packages.
func main() {
	os.Exit(platform.RunService("tenant-operator", nil))
}
