package mcp

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── New command structure ─────────────────────────────────────────────────────

func TestNew_UseName(t *testing.T) {
	cmd := New(&config.RootConfig{})
	assert.Equal(t, "mcp", cmd.Use)
}

func TestNew_HasExpectedFlags(t *testing.T) {
	cmd := New(&config.RootConfig{})
	for _, name := range []string{"account-id", "reports-dir"} {
		assert.NotNil(t, cmd.Flags().Lookup(name), "expected flag --%s", name)
	}
}

func TestNew_DefaultReportsDir(t *testing.T) {
	cmd := New(&config.RootConfig{})
	f := cmd.Flags().Lookup("reports-dir")
	require.NotNil(t, f)
	assert.Equal(t, "/srv/trading/backtests/reports", f.DefValue)
}

// ── RunE ───────────────────────────────────────────────────────────────────

func TestNew_ServeStdio_ReturnsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so ServeStdio returns right away

	cmd := New(&config.RootConfig{})
	cmd.SetContext(ctx)

	assert.NoError(t, cmd.RunE(cmd, nil))
}

func TestNew_RCAccountIDApplied(t *testing.T) {
	// Smoke-test that rc.OANDA.AccountID reaches the server — it's used only
	// to mark the default account in list_accounts, so nothing here needs
	// network access.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmd := New(&config.RootConfig{OANDA: config.GlobalOANDAConfig{AccountID: "ACC-TEST"}})
	cmd.SetContext(ctx)

	assert.NoError(t, cmd.RunE(cmd, nil))
}
