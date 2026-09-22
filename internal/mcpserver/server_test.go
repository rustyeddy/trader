package mcpserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rustyeddy/trader/version"
	"github.com/stretchr/testify/require"
)

func TestVersionToolThroughMCP(t *testing.T) {
	ctx := context.Background()
	server := New()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer func() { _ = serverSession.Close() }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	tools, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	require.NoError(t, err)
	require.Len(t, tools.Tools, 1)
	require.Equal(t, "trader_version", tools.Tools[0].Name)
	require.NotNil(t, tools.Tools[0].InputSchema)

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "trader_version", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, result.IsError)

	var got VersionOutput
	raw, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &got))
	info := version.Current()
	require.Equal(t, info.Version, got.Version)
	require.Equal(t, info.Revision, got.Revision)
	require.Equal(t, info.Dirty, got.Dirty)
	if info.CommitTime.IsZero() {
		require.Empty(t, got.CommitTime)
	} else {
		require.Equal(t, info.CommitTime.UTC().Format("2006-01-02T15:04:05Z07:00"), got.CommitTime)
	}
}
