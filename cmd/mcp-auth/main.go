// Command mcp-auth runs the OAuth 2.1 authorization server for MCP clients (plan/01 §13.2). Step 0.1 skeleton: health endpoints only; OAuth arrives in Step 0.10.
package main

import (
	"os"

	"github.com/sayanf22/dbaas/internal/platform"
)

// main only wires the service name; all behaviour lives in internal packages.
func main() {
	os.Exit(platform.RunService("mcp-auth", nil))
}
