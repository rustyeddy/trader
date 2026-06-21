package data

import (
	"testing"

	trader "github.com/rustyeddy/trader"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// ── defaultOandaAuth ──────────────────────────────────────────────────────────

func TestDefaultOandaAuth_ReadsEnvVars(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "env-tok")
	t.Setenv("OANDA_ACCOUNT_ID", "env-acct")

	auth := defaultOandaAuth()
	assert.Equal(t, "env-tok", auth.token)
	assert.Equal(t, "env-acct", auth.accountID)
	assert.Equal(t, "practice", auth.env)
}

func TestDefaultOandaAuth_DefaultsEnvToPractice(t *testing.T) {
	t.Setenv("OANDA_TOKEN", "")
	t.Setenv("OANDA_ACCOUNT_ID", "")

	auth := defaultOandaAuth()
	assert.Equal(t, "practice", auth.env)
}

// ── applyGlobalOANDA ──────────────────────────────────────────────────────────

// cmdWithOANDAFlags returns a cobra.Command with the three OANDA flags registered.
func cmdWithOANDAFlags() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().String("token", "", "")
	cmd.Flags().String("env", "", "")
	cmd.Flags().String("account-id", "", "")
	return cmd
}

func TestApplyGlobalOANDA_NilRCIsNoOp(t *testing.T) {
	cmd := cmdWithOANDAFlags()
	auth := oandaAuth{token: "existing"}
	applyGlobalOANDA(cmd, &auth, nil)
	assert.Equal(t, "existing", auth.token) // unchanged
}

func TestApplyGlobalOANDA_AppliesRCTokenWhenFlagNotChanged(t *testing.T) {
	cmd := cmdWithOANDAFlags()
	auth := oandaAuth{}
	applyGlobalOANDA(cmd, &auth, &trader.RootConfig{OANDAToken: "rc-tok"})
	assert.Equal(t, "rc-tok", auth.token)
}

func TestApplyGlobalOANDA_SkipsTokenWhenFlagChanged(t *testing.T) {
	cmd := cmdWithOANDAFlags()
	_ = cmd.Flags().Set("token", "flag-tok") // marks as Changed
	auth := oandaAuth{token: "flag-tok"}
	applyGlobalOANDA(cmd, &auth, &trader.RootConfig{OANDAToken: "rc-tok"})
	assert.Equal(t, "flag-tok", auth.token) // rc value not applied
}

func TestApplyGlobalOANDA_AppliesRCAccountIDWhenFlagNotChanged(t *testing.T) {
	cmd := cmdWithOANDAFlags()
	auth := oandaAuth{}
	applyGlobalOANDA(cmd, &auth, &trader.RootConfig{OANDAAccountID: "rc-acct"})
	assert.Equal(t, "rc-acct", auth.accountID)
}

func TestApplyGlobalOANDA_SkipsAccountIDWhenFlagChanged(t *testing.T) {
	cmd := cmdWithOANDAFlags()
	_ = cmd.Flags().Set("account-id", "flag-acct")
	auth := oandaAuth{accountID: "flag-acct"}
	applyGlobalOANDA(cmd, &auth, &trader.RootConfig{OANDAAccountID: "rc-acct"})
	assert.Equal(t, "flag-acct", auth.accountID)
}

func TestApplyGlobalOANDA_AppliesRCEnvWhenFlagNotChanged(t *testing.T) {
	cmd := cmdWithOANDAFlags()
	auth := oandaAuth{env: "practice"}
	applyGlobalOANDA(cmd, &auth, &trader.RootConfig{OANDAEnv: "live"})
	assert.Equal(t, "live", auth.env)
}

func TestApplyGlobalOANDA_SkipsEnvWhenFlagChanged(t *testing.T) {
	cmd := cmdWithOANDAFlags()
	_ = cmd.Flags().Set("env", "live")
	auth := oandaAuth{env: "live"}
	applyGlobalOANDA(cmd, &auth, &trader.RootConfig{OANDAEnv: "practice"})
	assert.Equal(t, "live", auth.env)
}

func TestApplyGlobalOANDA_EmptyRCFieldsAreIgnored(t *testing.T) {
	cmd := cmdWithOANDAFlags()
	auth := oandaAuth{token: "keep-me", env: "live"}
	applyGlobalOANDA(cmd, &auth, &trader.RootConfig{})
	assert.Equal(t, "keep-me", auth.token)
	assert.Equal(t, "live", auth.env)
}
