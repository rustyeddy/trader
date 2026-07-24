//go:build blackbox

package mcp_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/rustyeddy/trader/api/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// harness wires a mcp.Server to in-memory pipes and exposes send/recv helpers.
type harness struct {
	t      *testing.T
	srv    *mcp.Server
	stdin  *io.PipeWriter // test writes here → server reads
	stdout *bufio.Scanner // test reads here ← server writes
	cancel context.CancelFunc
	done   chan error
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	srv := mcp.New(slog.Default(), "")

	clientR, clientW := io.Pipe() // server reads from clientR
	serverR, serverW := io.Pipe() // server writes to serverW, test reads from serverR

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(ctx, clientR, serverW)
		serverW.Close()
	}()

	t.Cleanup(func() {
		cancel()
		clientW.Close()
		<-done
	})

	return &harness{
		t:      t,
		srv:    srv,
		stdin:  clientW,
		stdout: bufio.NewScanner(serverR),
		cancel: cancel,
		done:   done,
	}
}

// send writes one JSON-RPC 2.0 request line to the server.
func (h *harness) send(id any, method string, params any) {
	h.t.Helper()
	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}
	b, err := json.Marshal(msg)
	require.NoError(h.t, err)
	_, err = fmt.Fprintf(h.stdin, "%s\n", b)
	require.NoError(h.t, err)
}

// sendNotification sends a notification (no id field).
func (h *harness) sendNotification(method string, params any) {
	h.t.Helper()
	msg := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		msg["params"] = params
	}
	b, _ := json.Marshal(msg)
	fmt.Fprintf(h.stdin, "%s\n", b)
}

// recv reads the next response line and returns it as a decoded map.
func (h *harness) recv() map[string]any {
	h.t.Helper()
	require.True(h.t, h.stdout.Scan(), "expected a response line; scanner err: %v", h.stdout.Err())
	var m map[string]any
	require.NoError(h.t, json.Unmarshal(h.stdout.Bytes(), &m))
	return m
}

// ── JSON-RPC protocol correctness ────────────────────────────────────────────

func TestMCPBlackbox_InvalidJSON_ReturnsParseError(t *testing.T) {
	h := newHarness(t)
	fmt.Fprintf(h.stdin, "this is not json\n")
	resp := h.recv()

	require.NotNil(t, resp["error"])
	errObj := resp["error"].(map[string]any)
	assert.Equal(t, float64(-32700), errObj["code"])
}

func TestMCPBlackbox_WrongJSONRPCVersion_ReturnsInvalidRequest(t *testing.T) {
	h := newHarness(t)
	fmt.Fprintf(h.stdin, `{"jsonrpc":"1.0","id":1,"method":"tools/list"}`+"\n")
	resp := h.recv()

	require.NotNil(t, resp["error"])
	errObj := resp["error"].(map[string]any)
	assert.Equal(t, float64(-32600), errObj["code"])
}

func TestMCPBlackbox_UnknownMethod_ReturnsMethodNotFound(t *testing.T) {
	h := newHarness(t)
	h.send(1, "no/such/method", nil)
	resp := h.recv()

	require.NotNil(t, resp["error"])
	errObj := resp["error"].(map[string]any)
	assert.Equal(t, float64(-32601), errObj["code"])
}

func TestMCPBlackbox_Notification_ProducesNoResponse(t *testing.T) {
	h := newHarness(t)

	// Notifications have no id → server must not send a response.
	// Send a notification then a regular request; only one response should arrive.
	h.sendNotification("notifications/initialized", nil)
	h.send(42, "tools/list", nil)

	resp := h.recv()
	assert.Equal(t, float64(42), resp["id"])
	assert.NotNil(t, resp["result"])
}

// ── initialize ───────────────────────────────────────────────────────────────

func TestMCPBlackbox_Initialize(t *testing.T) {
	h := newHarness(t)
	h.send(1, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]any{"name": "test", "version": "0.1"},
	})
	resp := h.recv()

	require.Nil(t, resp["error"])
	result := resp["result"].(map[string]any)
	assert.Equal(t, "2024-11-05", result["protocolVersion"])

	caps := result["capabilities"].(map[string]any)
	assert.NotNil(t, caps["tools"])
	assert.NotNil(t, caps["resources"])

	info := result["serverInfo"].(map[string]any)
	assert.Equal(t, "trader", info["name"])
}

// ── tools/list ───────────────────────────────────────────────────────────────

func TestMCPBlackbox_ToolsList_AccountAndInfraOnly(t *testing.T) {
	h := newHarness(t)
	h.send(2, "tools/list", nil)
	resp := h.recv()

	require.Nil(t, resp["error"])
	result := resp["result"].(map[string]any)
	tools := result["tools"].([]any)

	names := toolNames(tools)
	assert.Contains(t, names, "list_accounts")
	assert.Contains(t, names, "account_summary")
	assert.Contains(t, names, "account_orders")
	assert.Contains(t, names, "get_version")
	assert.Contains(t, names, "get_health")
}

// ── tools/call ───────────────────────────────────────────────────────────────

func TestMCPBlackbox_ToolsCall_UnknownTool(t *testing.T) {
	h := newHarness(t)
	h.send(10, "tools/call", map[string]any{"name": "no_such_tool", "arguments": map[string]any{}})
	resp := h.recv()

	require.NotNil(t, resp["error"])
	errObj := resp["error"].(map[string]any)
	assert.Equal(t, float64(-32601), errObj["code"])
}

// ── resources/list ────────────────────────────────────────────────────────────

func TestMCPBlackbox_ResourcesList(t *testing.T) {
	h := newHarness(t)
	h.send(20, "resources/list", nil)
	resp := h.recv()

	require.Nil(t, resp["error"])
	result := resp["result"].(map[string]any)
	resources := result["resources"].([]any)
	require.NotEmpty(t, resources)

	uris := make([]string, 0, len(resources))
	for _, r := range resources {
		rm := r.(map[string]any)
		uris = append(uris, rm["uri"].(string))
	}
	assert.Contains(t, uris, "backtest://results")
	assert.Contains(t, uris, "config://configs")
}

// ── resources/read ────────────────────────────────────────────────────────────

func TestMCPBlackbox_ResourcesRead_UnknownURI(t *testing.T) {
	h := newHarness(t)
	h.send(21, "resources/read", map[string]any{"uri": "unknown://foo"})
	resp := h.recv()

	require.NotNil(t, resp["error"])
	errObj := resp["error"].(map[string]any)
	assert.Equal(t, float64(-32601), errObj["code"])
}

func TestMCPBlackbox_ResourcesRead_MissingURI(t *testing.T) {
	h := newHarness(t)
	h.send(22, "resources/read", map[string]any{})
	resp := h.recv()

	require.NotNil(t, resp["error"])
	errObj := resp["error"].(map[string]any)
	assert.Equal(t, float64(-32602), errObj["code"])
}

func TestMCPBlackbox_ResourcesRead_BacktestResults(t *testing.T) {
	h := newHarness(t)
	h.send(23, "resources/read", map[string]any{"uri": "backtest://results"})
	resp := h.recv()

	// May or may not have files but must not error at the RPC level.
	require.Nil(t, resp["error"])
	result := resp["result"].(map[string]any)
	assert.NotNil(t, result["contents"])
}

func TestMCPBlackbox_ResourcesRead_ConfigList(t *testing.T) {
	h := newHarness(t)
	h.send(24, "resources/read", map[string]any{"uri": "config://configs"})
	resp := h.recv()

	require.Nil(t, resp["error"])
	result := resp["result"].(map[string]any)
	contents := result["contents"].([]any)
	require.NotEmpty(t, contents)
	text := contents[0].(map[string]any)["text"].(string)
	// testdata/configs/ exists at repo root; at minimum some .yml files are present
	assert.NotEmpty(t, text)
}

// ── prompts/list (empty stub) ─────────────────────────────────────────────────

func TestMCPBlackbox_PromptsList_Empty(t *testing.T) {
	h := newHarness(t)
	h.send(30, "prompts/list", nil)
	resp := h.recv()

	require.Nil(t, resp["error"])
	result := resp["result"].(map[string]any)
	prompts := result["prompts"].([]any)
	assert.Empty(t, prompts)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func toolNames(tools []any) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		tm := t.(map[string]any)
		names = append(names, tm["name"].(string))
	}
	return names
}
