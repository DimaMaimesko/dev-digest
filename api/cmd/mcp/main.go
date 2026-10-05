// Command mcp is devdigest-mcp: an MCP server over standard input and output
// that lets a coding agent use DevDigest through its HTTP API.
//
// It calls the API at DEVDIGEST_API_URL, else at 127.0.0.1:$API_PORT, else
// at http://127.0.0.1:3001; the API must be running. Standard output is the
// protocol, so logs go to standard error.
//
// Usage, from the repository's .mcp.json:
//
//	go run ./api/cmd/mcp
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/DimaMaimesko/dev-digest/api/internal/mcpserver"
)

const version = "0.1.0"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := mcpserver.New(mcpserver.Config{APIURL: apiURL(os.Getenv)}, version)
	if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, "mcp:", err)
		stop()
		os.Exit(1)
	}
}

// apiURL is where the API listens: DEVDIGEST_API_URL, else API_PORT on the
// loopback address the API binds to.
func apiURL(getenv func(string) string) string {
	if u := getenv("DEVDIGEST_API_URL"); u != "" {
		return u
	}
	if port := getenv("API_PORT"); port != "" {
		return "http://127.0.0.1:" + port
	}
	return mcpserver.DefaultAPIURL
}
