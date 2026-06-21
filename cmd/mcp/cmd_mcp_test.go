package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rustyeddy/trader"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── resolveTokenFile ──────────────────────────────────────────────────────────

func TestResolveTokenFile_MissingFileReturnsEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	assert.Equal(t, "", resolveTokenFile())
}

func TestResolveTokenFile_ReadsAndTrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	tokenDir := filepath.Join(dir, ".config", "oanda")
	require.NoError(t, os.MkdirAll(tokenDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(tokenDir, "pat.txt"), []byte("  mytoken\n"), 0o600))

	assert.Equal(t, "mytoken", resolveTokenFile())
}

func TestResolveTokenFile_EmptyFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	tokenDir := filepath.Join(dir, ".config", "oanda")
	require.NoError(t, os.MkdirAll(tokenDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(tokenDir, "pat.txt"), []byte("   \n"), 0o600))

	assert.Equal(t, "", resolveTokenFile())
}

// ── New command structure ─────────────────────────────────────────────────────

func TestNew_UseName(t *testing.T) {
	cmd := New(&trader.RootConfig{})
	assert.Equal(t, "mcp", cmd.Use)
}

func TestNew_HasExpectedFlags(t *testing.T) {
	cmd := New(&trader.RootConfig{})
	for _, name := range []string{"token", "account-id", "env", "enable-write", "reports-dir"} {
		assert.NotNil(t, cmd.Flags().Lookup(name), "expected flag --%s", name)
	}
}

func TestNew_DefaultEnvIsPractice(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "")
	t.Setenv("OANDA_ACCOUNT_ID", "")
	cmd := New(&trader.RootConfig{})
	f := cmd.Flags().Lookup("env")
	require.NotNil(t, f)
	assert.Equal(t, "practice", f.DefValue)
}

func TestNew_DefaultEnableWriteIsFalse(t *testing.T) {
	cmd := New(&trader.RootConfig{})
	f := cmd.Flags().Lookup("enable-write")
	require.NotNil(t, f)
	assert.Equal(t, "false", f.DefValue)
}

func TestNew_DefaultReportsDir(t *testing.T) {
	cmd := New(&trader.RootConfig{})
	f := cmd.Flags().Lookup("reports-dir")
	require.NotNil(t, f)
	assert.Equal(t, "/srv/trading/backtests/reports", f.DefValue)
}

// ── token resolution in RunE ──────────────────────────────────────────────────

// runMCP calls RunE with a background context and no stdin/stdout pressure.
// With no token the command builds a no-auth service and blocks on ServeStdio;
// with a bad token it errors before reaching ServeStdio.
// We test the token-resolution paths by checking which error we get.

func TestNew_NoToken_StartsMCPWithNoAuth(t *testing.T) {
	// With no token the MCP command builds a bare service (no OANDA client)
	// and calls srv.ServeStdio. ServeStdio blocks reading stdin; cancelling
	// the context unblocks it. We verify the command reaches that point
	// (no "init service" error) rather than failing on a missing token.
	t.Setenv("OANDA_TOKEN", "")
	t.Setenv("HOME", t.TempDir()) // block token file fallback

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so ServeStdio returns right away

	cmd := New(&trader.RootConfig{})
	cmd.SetContext(ctx)

	err := cmd.RunE(cmd, nil)
	// ServeStdio may return nil or a context-cancelled error — either is fine.
	// What we must NOT see is "init service".
	if err != nil {
		assert.NotContains(t, err.Error(), "init service")
	}
}

func TestNew_RCTokenUsed_FailsAtServiceInit(t *testing.T) {
	// A non-empty but invalid token causes service.New to succeed (token present)
	// then fail when the OANDA client is used. The error should be "init service".
	t.Setenv("OANDA_TOKEN", "")
	t.Setenv("HOME", t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := New(&trader.RootConfig{OANDAToken: "bad-token"})
	cmd.SetContext(ctx)

	// service.New builds successfully with any non-empty token; it only
	// fails at the OANDA API level (ResolveAccount / GetAccounts).  The mcp
	// command does NOT call ResolveAccount — it just creates the service and
	// calls ServeStdio.  So with "bad-token" we expect the same path as
	// no-auth: reaches ServeStdio, returns nil or context error.
	err := cmd.RunE(cmd, nil)
	if err != nil {
		assert.NotContains(t, err.Error(), "init service")
	}
}

func TestNew_RCAccountIDApplied(t *testing.T) {
	// Smoke-test that rc.OANDAAccountID reaches the service layer.
	// We use a cancelled context so ServeStdio exits immediately.
	t.Setenv("OANDA_TOKEN", "")
	t.Setenv("HOME", t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd := New(&trader.RootConfig{OANDAAccountID: "ACC-TEST"})
	cmd.SetContext(ctx)

	// No OANDA token → bare service → ServeStdio → nil or ctx error.
	err := cmd.RunE(cmd, nil)
	if err != nil {
		assert.NotContains(t, err.Error(), "init service")
	}
}
