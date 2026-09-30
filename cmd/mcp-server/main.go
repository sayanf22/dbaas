// Command mcp-server runs the remote MCP server for AI clients (plan/01 §13). Step 0.1 skeleton: health endpoints only; tools arrive in Step 0.10.
package main

import (
	"os"

	"github.com/sayanf22/dbaas/internal/platform"
)

// main only wires the service name; all behaviour lives in internal packages.
func main() {
	os.Exit(platform.RunService("mcp-server", nil))
}
