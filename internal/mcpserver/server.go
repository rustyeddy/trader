// Package mcpserver provides Trader's narrow MCP research adapter.
package mcpserver

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rustyeddy/trader/version"
)

// VersionInput is empty because trader_version has no arguments.
type VersionInput struct{}

// VersionOutput identifies the Trader build serving MCP requests.
type VersionOutput struct {
	Version    string `json:"version"`
	Revision   string `json:"revision,omitempty"`
	Dirty      bool   `json:"dirty"`
	CommitTime string `json:"commit_time,omitempty"`
}

// New constructs the initial Trader MCP server.
func New() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "trader-mcp", Version: version.Current().Version}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "trader_version",
		Description: "Return the Trader build identity serving this MCP session.",
	}, traderVersion)
	return server
}

func traderVersion(context.Context, *mcp.CallToolRequest, VersionInput) (*mcp.CallToolResult, VersionOutput, error) {
	info := version.Current()
	output := VersionOutput{Version: info.Version, Revision: info.Revision, Dirty: info.Dirty}
	if !info.CommitTime.IsZero() {
		output.CommitTime = info.CommitTime.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return nil, output, nil
}
