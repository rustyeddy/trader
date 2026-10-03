package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baseEnv points every data root at a temp dir and logs to a file, so a
// test never touches real data or writes to the test's stdout. The roots
// are set explicitly in the supplied environment, and XDG_DATA_HOME is
// also set on the process: DefaultDataDir, used for any root left unset
// (and for non-default providers), reads the process environment.
func baseEnv(t *testing.T) (env []string, logPath string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "xdg"))
	logPath = filepath.Join(dir, "trader-mcp.log")
	return []string{
		"TRADER_STORE_ROOT=" + filepath.Join(dir, "store"),
		"TRADER_RAW_ROOT=" + filepath.Join(dir, "raw"),
		"TRADER_ARCHIVE_ROOT=" + filepath.Join(dir, "archive"),
		"TRADER_OUTPUT=" + logPath,
	}, logPath
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func TestBuild_ServesTraderVersionOverMCP(t *testing.T) {
	env, _ := baseEnv(t)
	srv, closer, err := build(nil, env, &bytes.Buffer{})
	require.NoError(t, err)
	defer func() { _ = closer.Close() }()

	ctx := context.Background()
	clientT, serverT := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	require.NoError(t, err)
	defer func() { _ = ss.Close() }()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v1"}, nil).Connect(ctx, clientT, nil)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "trader_version", Arguments: map[string]any{}})
	require.NoError(t, err)
	assert.False(t, res.IsError)
}

func TestBuild_ConfigResolution(t *testing.T) {
	t.Run("environment selects the default provider", func(t *testing.T) {
		env, logPath := baseEnv(t)
		_, closer, err := build(nil, append(env, "TRADER_PROVIDER=stooq"), &bytes.Buffer{})
		require.NoError(t, err)
		_ = closer.Close()
		assert.Contains(t, readLog(t, logPath), "default_provider=stooq")
	})
	t.Run("a flag beats the environment", func(t *testing.T) {
		env, logPath := baseEnv(t)
		_, closer, err := build([]string{"--provider", "alpaca"}, append(env, "TRADER_PROVIDER=stooq"), &bytes.Buffer{})
		require.NoError(t, err)
		_ = closer.Close()
		assert.Contains(t, readLog(t, logPath), "default_provider=alpaca")
	})
}

func TestBuild_WriteAccess(t *testing.T) {
	cases := []struct {
		name string
		args []string
		env  []string
		want string
	}{
		{"off by default", nil, nil, "writes_enabled=false"},
		{"flag", []string{"--allow-writes"}, nil, "writes_enabled=true"},
		{"environment", nil, []string{"TRADER_MCP_ALLOW_WRITES=true"}, "writes_enabled=true"},
		{"flag overrides environment", []string{"--allow-writes=false"}, []string{"TRADER_MCP_ALLOW_WRITES=true"}, "writes_enabled=false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, logPath := baseEnv(t)
			_, closer, err := build(tc.args, append(env, tc.env...), &bytes.Buffer{})
			require.NoError(t, err)
			_ = closer.Close()
			assert.Contains(t, readLog(t, logPath), tc.want)
		})
	}
}

func TestBuild_InvalidConfigFailsClearly(t *testing.T) {
	cases := []struct {
		name string
		args []string
		env  []string
		want string
	}{
		{"log output stdout by flag", []string{"--log-output", "stdout"}, nil, "corrupt the MCP stdio stream"},
		{"log output stdout by environment", nil, []string{"TRADER_OUTPUT=stdout"}, "corrupt the MCP stdio stream"},
		{"bad log level", []string{"--log-level", "LOUD"}, nil, "level"},
		{"unknown provider", []string{"--provider", "bloomberg"}, nil, "unknown provider"},
		{"half an Alpaca credential", nil, []string{"TRADER_ALPACA_KEY_ID=kid-only"}, "TRADER_ALPACA_SECRET_KEY"},
		{"unknown flag", []string{"--nope"}, nil, "nope"},
		{"bad allow-writes value", nil, []string{"TRADER_MCP_ALLOW_WRITES=maybe"}, "mcp_allow_writes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, _ := baseEnv(t)
			_, _, err := build(tc.args, append(env, tc.env...), &bytes.Buffer{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.NotContains(t, err.Error(), "kid-only", "credentials never appear in errors")
		})
	}
}

func TestBuild_CredentialsNeverLogged(t *testing.T) {
	env, logPath := baseEnv(t)
	env = append(env,
		"TRADER_LEVEL=DEBUG",
		"TRADER_OANDA_TOKEN=oanda-secret-token",
		"TRADER_OANDA_BASE_URL=https://example.invalid",
		"TRADER_ALPACA_KEY_ID=alpaca-secret-id",
		"TRADER_ALPACA_SECRET_KEY=alpaca-secret-key",
	)
	_, closer, err := build(nil, env, &bytes.Buffer{})
	require.NoError(t, err)
	_ = closer.Close()
	logs := readLog(t, logPath)
	assert.Contains(t, logs, "trader-mcp starting")
	for _, secret := range []string{"oanda-secret-token", "alpaca-secret-id", "alpaca-secret-key"} {
		assert.NotContains(t, logs, secret)
	}
}

func TestRun_StopsWhenContextEnds(t *testing.T) {
	env, _ := baseEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	_, serverT := mcp.NewInMemoryTransports()
	done := make(chan error, 1)
	go func() { done <- run(ctx, nil, env, serverT, &bytes.Buffer{}) }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after the context ended")
	}
}

func TestRun_HelpIsSuccess(t *testing.T) {
	env, _ := baseEnv(t)
	_, serverT := mcp.NewInMemoryTransports()
	var stderr bytes.Buffer
	err := run(context.Background(), []string{"--help"}, env, serverT, &stderr)
	require.NoError(t, err, "help is not a startup failure")
	assert.Contains(t, stderr.String(), "-allow-writes", "usage was printed")
}

func TestBuild_RejectsPositionalArguments(t *testing.T) {
	env, _ := baseEnv(t)
	_, _, err := build([]string{"--allow-writes", "typo"}, env, &bytes.Buffer{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unexpected arguments ["typo"]`)
}

func TestRun_BuildErrorReturned(t *testing.T) {
	env, _ := baseEnv(t)
	_, serverT := mcp.NewInMemoryTransports()
	err := run(context.Background(), []string{"--log-output", "stdout"}, env, serverT, &bytes.Buffer{})
	assert.ErrorIs(t, err, errLogToStdout)
}

// TestBuild_ServesMarketDataTools exercises the read-only market-data
// tools (#436) through the real composition root: the configured raw
// root holds the committed EURUSD fixture, and both tools resolve and
// report on it through the factory-built Service.
func TestBuild_ServesMarketDataTools(t *testing.T) {
	env, _ := baseEnv(t)
	rawRoot := filepath.Join(t.TempDir(), "raw")
	require.NoError(t, os.CopyFS(rawRoot, os.DirFS(filepath.Join("..", "..", "internal", "service", "marketdata", "testdata", "raw", "oanda"))))
	for i, kv := range env {
		if strings.HasPrefix(kv, "TRADER_RAW_ROOT=") {
			env[i] = "TRADER_RAW_ROOT=" + rawRoot
		}
	}
	srv, closer, err := build(nil, env, &bytes.Buffer{})
	require.NoError(t, err)
	defer func() { _ = closer.Close() }()

	ctx := context.Background()
	clientT, serverT := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, serverT, nil)
	require.NoError(t, err)
	defer func() { _ = ss.Close() }()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v1"}, nil).Connect(ctx, clientT, nil)
	require.NoError(t, err)
	defer func() { _ = cs.Close() }()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "trader_instruments", Arguments: map[string]any{"symbols": []string{"EURUSD"}}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	assert.Contains(t, res.StructuredContent, "instruments")

	res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "trader_marketdata_coverage", Arguments: map[string]any{"symbols": []string{"EURUSD"}, "interval": "H1"}})
	require.NoError(t, err)
	require.False(t, res.IsError)
	results := res.StructuredContent.(map[string]any)["results"].([]any)
	require.Len(t, results, 1)
	raw := results[0].(map[string]any)["raw"].(map[string]any)
	assert.Equal(t, "2024-01-07T22:00:00Z", raw["first"], "the configured raw root is the one inspected")
}
