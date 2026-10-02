package marketdatacfg

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	marketruntime "github.com/rustyeddy/trader/internal/marketdata"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)) }

func TestLoad_EnvironmentAndOverrides(t *testing.T) {
	cfg, err := Load([]string{
		"TRADER_STORE_ROOT=/env/store",
		"TRADER_PROVIDER=alpaca",
		"TRADER_OANDA_TOKEN=tok",
		"TRADER_ALPACA_KEY_ID=kid",
		"TRADER_ALPACA_SECRET_KEY=sec",
	}, map[string]string{"store-root": "/flag/store"})
	require.NoError(t, err)
	assert.Equal(t, "/flag/store", cfg.StoreRoot, "an override beats the environment")
	assert.Equal(t, "alpaca", cfg.Provider)
	assert.Equal(t, "tok", cfg.OANDAToken)
	assert.Equal(t, "kid", cfg.AlpacaKeyID)
	assert.Equal(t, "https://data.alpaca.markets", cfg.AlpacaBaseURL, "default applies")
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load(nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "oanda", cfg.Provider)
	assert.Empty(t, cfg.StoreRoot, "roots are resolved later by ApplyDefaultRoots")
}

func TestIsFXProvider(t *testing.T) {
	assert.True(t, IsFXProvider("oanda"))
	assert.False(t, IsFXProvider("alpaca"))
	assert.False(t, IsFXProvider("stooq"))
}

func TestNew_BuildsServiceForResolvedRoots(t *testing.T) {
	dir := t.TempDir()
	b, err := New(Config{StoreRoot: filepath.Join(dir, "store"), RawRoot: filepath.Join(dir, "raw"), ArchiveRoot: "/arch", Provider: "stooq"}, discard())
	require.NoError(t, err)
	assert.NotNil(t, b.Service)
	assert.NotNil(t, b.Resolver)
	assert.Equal(t, "stooq", b.Provider)
	assert.Equal(t, "/arch", b.ArchiveRoot)
	assert.NoDirExists(t, filepath.Join(dir, "store"), "construction creates nothing")
}

func TestNew_OneSidedAlpacaCredentialRejectedWithoutLeakingIt(t *testing.T) {
	dir := t.TempDir()
	for _, cfg := range []Config{
		{AlpacaKeyID: "super-secret-key-id"},
		{AlpacaSecretKey: "super-secret-value"},
	} {
		cfg.StoreRoot, cfg.RawRoot, cfg.Provider = dir, dir, "alpaca"
		_, err := New(cfg, discard())
		require.ErrorIs(t, err, marketruntime.ErrInvalidConfig)
		assert.Contains(t, err.Error(), "TRADER_ALPACA_KEY_ID")
		assert.NotContains(t, err.Error(), "super-secret")
	}
}
