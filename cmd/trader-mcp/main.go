// Command trader-mcp exposes Trader's typed research capabilities over MCP.
package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rustyeddy/trader/internal/mcpserver"
)

func main() {
	if err := mcpserver.New().Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Printf("trader-mcp: %v", err)
	}
}
