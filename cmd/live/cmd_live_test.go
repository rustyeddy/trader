package live

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── New command structure ─────────────────────────────────────────────────────

func TestNew_UseName(t *testing.T) {
	cmd := New(nil)
	assert.Equal(t, "live", cmd.Use)
}

func TestNew_HasJournalSubcommand(t *testing.T) {
	cmd := New(nil)
	names := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}
	assert.True(t, names["journal"], "expected 'journal' subcommand")
}

// ── journal command flags ─────────────────────────────────────────────────────

func TestJournalCmd_HasExpectedFlags(t *testing.T) {
	rc := &config.RootConfig{}
	cmd := newJournalCmd(rc)
	flags := cmd.Flags()
	for _, name := range []string{
		"account-id", "token", "env", "journal",
		"trades-file", "equity-file", "backfill-from",
	} {
		assert.NotNil(t, flags.Lookup(name), "expected flag --%s", name)
	}
}

func TestJournalCmd_DefaultEnvIsPractice(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "")
	t.Setenv("OANDA_ACCOUNT_ID", "")
	rc := &config.RootConfig{}
	cmd := newJournalCmd(rc)
	envFlag := cmd.Flags().Lookup("env")
	require.NotNil(t, envFlag)
	assert.Equal(t, "practice", envFlag.DefValue)
}

func TestJournalCmd_DefaultJournalIsJSON(t *testing.T) {
	rc := &config.RootConfig{}
	cmd := newJournalCmd(rc)
	f := cmd.Flags().Lookup("journal")
	require.NotNil(t, f)
	assert.Equal(t, "json", f.DefValue)
}

func TestJournalCmd_DefaultTradesFile(t *testing.T) {
	rc := &config.RootConfig{}
	cmd := newJournalCmd(rc)
	f := cmd.Flags().Lookup("trades-file")
	require.NotNil(t, f)
	assert.Equal(t, "live-trades.jsonl", f.DefValue)
}

func TestJournalCmd_DefaultEquityFile(t *testing.T) {
	rc := &config.RootConfig{}
	cmd := newJournalCmd(rc)
	f := cmd.Flags().Lookup("equity-file")
	require.NotNil(t, f)
	assert.Equal(t, "live-equity.jsonl", f.DefValue)
}

func TestJournalCmd_DefaultBackfillFromIsZero(t *testing.T) {
	rc := &config.RootConfig{}
	cmd := newJournalCmd(rc)
	f := cmd.Flags().Lookup("backfill-from")
	require.NotNil(t, f)
	assert.Equal(t, "0", f.DefValue)
}

// ── RunE error paths (no network needed) ─────────────────────────────────────

// runJournalCmd calls cmd.RunE with a background context so notifyContext
// does not panic on a nil parent.
func runJournalCmd(rc *config.RootConfig) error {
	cmd := newJournalCmd(rc)
	cmd.SetContext(context.Background())
	return cmd.RunE(cmd, nil)
}

func TestJournalCmd_NoToken_ReturnsError(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "")
	// Redirect HOME so service.New cannot find ~/.config/oanda/pat.txt.
	t.Setenv("HOME", t.TempDir())
	err := runJournalCmd(&config.RootConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token")
}

func TestJournalCmd_RCTokenUsedWhenFlagNotChanged(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "")
	// rc has a token so service.New passes the token check and fails later
	// (attempting a real OANDA network call), but NOT with "no OANDA token".
	err := runJournalCmd(&config.RootConfig{OANDAToken: "rc-token"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "no OANDA token")
}

func TestJournalCmd_EnvTokenUsedWhenFlagAndRCAreEmpty(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "env-token")
	err := runJournalCmd(&config.RootConfig{})
	require.Error(t, err)
	// Got past the token check; fails on the network call.
	assert.NotContains(t, err.Error(), "no OANDA token")
}

// ── notifyContext ─────────────────────────────────────────────────────────────

func TestNotifyContext_ReturnsCancellableContext(t *testing.T) {
	ctx, cancel := notifyContext(context.Background())
	defer cancel()

	// Context should not be done yet.
	select {
	case <-ctx.Done():
		t.Fatal("context should not be cancelled immediately")
	default:
	}

	// Cancelling via the returned function should close the Done channel.
	cancel()
	<-ctx.Done()
}
