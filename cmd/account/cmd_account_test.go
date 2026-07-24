package account

import (
	"bytes"
	"testing"

	accountsvc "github.com/rustyeddy/trader/service/account"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── command construction ─────────────────────────────────────────────────────

func TestNew_HasExpectedSubcommands(t *testing.T) {
	cmd := New(nil)
	names := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}
	assert.True(t, names["list"], "expected 'list' subcommand")
	assert.True(t, names["summary"], "expected 'summary' subcommand")
}

// ── missing token error ───────────────────────────────────────────────────────
//
// These commands have no --token/--env flags of their own; auth/env come
// from OANDA_TOKEN / ~/.config/oanda/pat.txt / "practice" via
// accountsvc.List/Summary's internal broker construction.

func TestListCmd_MissingTokenReturnsError(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "")
	t.Setenv("HOME", t.TempDir()) // block the ~/.config/oanda/pat.txt fallback
	broker, accountID = "oanda", ""
	cmd := listCmd(nil)

	err := cmd.RunE(cmd, nil)
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "no token")
	}
}

func TestSummaryCmd_MissingTokenReturnsError(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "")
	t.Setenv("HOME", t.TempDir()) // block the ~/.config/oanda/pat.txt fallback
	broker, accountID = "oanda", ""
	cmd := summaryCmd(nil)

	err := cmd.RunE(cmd, nil)
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "no token")
	}
}

func TestOrdersCmd_MissingTokenReturnsError(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "")
	t.Setenv("HOME", t.TempDir())
	broker, accountID = "oanda", ""
	cmd := ordersCmd(nil)

	err := cmd.RunE(cmd, nil)
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "no token")
	}
}

// ── broker/account-id flags ───────────────────────────────────────────────────

func TestNew_HasBrokerAndAccountIDFlags(t *testing.T) {
	cmd := New(nil)
	assert.NotNil(t, cmd.PersistentFlags().Lookup("broker"))
	assert.NotNil(t, cmd.PersistentFlags().Lookup("account-id"))
}

func TestNew_HasDefaultSubcommand(t *testing.T) {
	cmd := New(nil)
	names := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}
	assert.True(t, names["default"], "expected 'default' subcommand")
}

// ── defaultCmd ────────────────────────────────────────────────────────────────

func TestDefaultCmd_NoFlagsPrintsNoDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := defaultCmd(nil)
	cmd.Flags().StringVar(&broker, "broker", "oanda", "")
	cmd.Flags().StringVar(&accountID, "account-id", "", "")
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)

	require.NoError(t, cmd.RunE(cmd, nil))
	assert.Contains(t, buf.String(), "No default set")
}

func TestDefaultCmd_SetBothPersistsSelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := defaultCmd(nil)
	cmd.Flags().StringVar(&broker, "broker", "oanda", "")
	cmd.Flags().StringVar(&accountID, "account-id", "", "")
	require.NoError(t, cmd.Flags().Set("broker", "oanda"))
	require.NoError(t, cmd.Flags().Set("account-id", "acc-1"))
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)

	require.NoError(t, cmd.RunE(cmd, nil))
	assert.Contains(t, buf.String(), "Default set to oanda/acc-1")

	sel, err := accountsvc.DefaultSelection()
	require.NoError(t, err)
	assert.Equal(t, accountsvc.Selection{Broker: "oanda", AccountID: "acc-1"}, sel)
}

func TestDefaultCmd_OnlyOneFlagErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := defaultCmd(nil)
	cmd.Flags().StringVar(&broker, "broker", "oanda", "")
	cmd.Flags().StringVar(&accountID, "account-id", "", "")
	require.NoError(t, cmd.Flags().Set("account-id", "acc-1"))

	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be set together")
}

func TestDefaultCmd_UnknownBrokerErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := defaultCmd(nil)
	cmd.Flags().StringVar(&broker, "broker", "oanda", "")
	cmd.Flags().StringVar(&accountID, "account-id", "", "")
	require.NoError(t, cmd.Flags().Set("broker", "bogus"))
	require.NoError(t, cmd.Flags().Set("account-id", "acc-1"))

	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown broker")
}
