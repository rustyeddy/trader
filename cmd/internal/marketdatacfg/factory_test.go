package marketdatacfg

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	svc "github.com/rustyeddy/trader/internal/service/marketdata"
)

func TestFactory_PerProviderRoots(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/xdg")
	f, err := NewFactory(Config{
		StoreRoot:   "/canonical",
		RawRoot:     "/my/raw/oanda",
		ArchiveRoot: "/my/archive/oanda",
		Provider:    "oanda",
	}, discard())
	require.NoError(t, err)
	assert.Equal(t, "oanda", f.DefaultProvider())

	t.Run("base provider keeps explicit roots", func(t *testing.T) {
		b, err := f.bundle("")
		require.NoError(t, err)
		assert.Equal(t, "oanda", b.Provider)
		assert.Equal(t, "/my/archive/oanda", b.ArchiveRoot)
	})
	t.Run("other providers get their own defaults", func(t *testing.T) {
		b, err := f.bundle("stooq")
		require.NoError(t, err)
		assert.Equal(t, "stooq", b.Provider)
		assert.Equal(t, filepath.Join("/xdg", "trader", "archive", "stooq"), b.ArchiveRoot,
			"an explicit oanda archive root never applies to stooq")
	})
	t.Run("fresh resolver per call", func(t *testing.T) {
		a, err := f.bundle("stooq")
		require.NoError(t, err)
		b, err := f.bundle("stooq")
		require.NoError(t, err)
		assert.NotSame(t, a.Resolver, b.Resolver)
	})
	t.Run("ForProvider returns a Service that resolves instruments", func(t *testing.T) {
		s, err := f.ForProvider("stooq")
		require.NoError(t, err)
		assert.Equal(t, "stooq", s.Provider())
		resp, err := s.ResolveInstrument(context.Background(), svc.InstrumentRequest{Symbol: "SPY"})
		require.NoError(t, err)
		assert.Equal(t, svc.KindETF, resp.Identity.Kind)

		other, err := f.ForProvider("stooq")
		require.NoError(t, err)
		assert.NotSame(t, s, other)
	})
}

func TestFactory_DefaultRootsWhenNothingConfigured(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/xdg")
	f, err := NewFactory(Config{Provider: "oanda"}, discard())
	require.NoError(t, err)
	b, err := f.bundle("alpaca")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/xdg", "trader", "archive", "alpaca"), b.ArchiveRoot)
}

func TestFactory_UnknownProvider(t *testing.T) {
	f, err := NewFactory(Config{StoreRoot: t.TempDir(), RawRoot: t.TempDir(), Provider: "oanda"}, discard())
	require.NoError(t, err)
	_, err = f.ForProvider("bloomberg")
	require.ErrorIs(t, err, ErrUnknownProvider)
	assert.Contains(t, err.Error(), "alpaca, oanda, stooq")

	_, err = NewFactory(Config{Provider: "bloomberg"}, discard())
	assert.ErrorIs(t, err, ErrUnknownProvider, "an unknown base provider fails at startup")
}

func TestFactory_InvalidBaseConfigFailsFast(t *testing.T) {
	_, err := NewFactory(Config{StoreRoot: t.TempDir(), RawRoot: t.TempDir(), Provider: "oanda", AlpacaKeyID: "only-half"}, discard())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "only-half")
}

func TestKnownProviders(t *testing.T) {
	assert.Equal(t, []string{"alpaca", "oanda", "stooq"}, KnownProviders())
}
