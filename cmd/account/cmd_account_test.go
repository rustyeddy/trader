package account

import (
	"testing"

	traderpkg "github.com/rustyeddy/trader"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// cmdWithFlag builds a minimal cobra.Command with the named flag bound to dest
// and optionally marks it as Changed (simulating an explicit CLI flag).
func flagCmd(name, val string, dest *string, changed bool) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().StringVar(dest, name, val, "")
	if changed {
		_ = cmd.Flags().Set(name, val)
	}
	return cmd
}

// ── resolveToken ─────────────────────────────────────────────────────────────

func TestResolveToken_FlagTakesPrecedence(t *testing.T) {
	cmd := flagCmd("token", "flag-tok", &token, true)
	result := resolveToken(cmd, &traderpkg.RootConfig{OANDAToken: "rc-tok"})
	assert.Equal(t, "flag-tok", result)
}

func TestResolveToken_RCTokenUsedWhenFlagNotSet(t *testing.T) {
	cmd := flagCmd("token", "", &token, false)
	result := resolveToken(cmd, &traderpkg.RootConfig{OANDAToken: "rc-tok"})
	assert.Equal(t, "rc-tok", result)
}

func TestResolveToken_EnvVarFallback(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "env-tok")
	cmd := flagCmd("token", "", &token, false)
	result := resolveToken(cmd, nil)
	assert.Equal(t, "env-tok", result)
}

func TestResolveToken_NilRCNoEnvReturnsEmpty(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "")
	cmd := flagCmd("token", "", &token, false)
	result := resolveToken(cmd, nil)
	assert.Equal(t, "", result)
}

// ── resolveEnv ───────────────────────────────────────────────────────────────

func TestResolveEnv_FlagTakesPrecedence(t *testing.T) {
	cmd := flagCmd("env", "live", &env, true)
	result := resolveEnv(cmd, &traderpkg.RootConfig{OANDAEnv: "practice"})
	assert.Equal(t, "live", result)
}

func TestResolveEnv_RCEnvUsedWhenFlagNotSet(t *testing.T) {
	cmd := flagCmd("env", "practice", &env, false)
	result := resolveEnv(cmd, &traderpkg.RootConfig{OANDAEnv: "live"})
	assert.Equal(t, "live", result)
}

func TestResolveEnv_DefaultPracticeWhenNilRC(t *testing.T) {
	cmd := flagCmd("env", "practice", &env, false)
	result := resolveEnv(cmd, nil)
	assert.Equal(t, "practice", result)
}

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

func TestNew_HasTokenAndEnvFlags(t *testing.T) {
	cmd := New(nil)
	assert.NotNil(t, cmd.PersistentFlags().Lookup("token"))
	assert.NotNil(t, cmd.PersistentFlags().Lookup("env"))
}

// ── missing token error ───────────────────────────────────────────────────────

func TestListCmd_MissingTokenReturnsError(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "")
	cmd := listCmd(nil)
	// Bind the token flag so resolveToken can check Changed.
	cmd.Flags().StringVar(&token, "token", "", "")
	token = ""

	err := cmd.RunE(cmd, nil)
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "no OANDA token")
	}
}

func TestSummaryCmd_MissingTokenReturnsError(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "")
	cmd := summaryCmd(nil)
	cmd.Flags().StringVar(&token, "token", "", "")
	token = ""

	err := cmd.RunE(cmd, nil)
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "no OANDA token")
	}
}
