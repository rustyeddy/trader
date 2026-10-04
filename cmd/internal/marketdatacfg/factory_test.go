package marketdatacfg

import (
	"context"
	"path/filepath"
	"sync"
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
		b, err := f.Bundle("")
		require.NoError(t, err)
		assert.Equal(t, "oanda", b.Provider)
		assert.Equal(t, "/my/archive/oanda", b.ArchiveRoot)
	})
	t.Run("other providers get their own defaults", func(t *testing.T) {
		b, err := f.Bundle("stooq")
		require.NoError(t, err)
		assert.Equal(t, "stooq", b.Provider)
		assert.Equal(t, filepath.Join("/xdg", "trader", "archive", "stooq"), b.ArchiveRoot,
			"an explicit oanda archive root never applies to stooq")
	})
	t.Run("one bundle per provider, reused", func(t *testing.T) {
		a, err := f.Bundle("stooq")
		require.NoError(t, err)
		b, err := f.Bundle("stooq")
		require.NoError(t, err)
		assert.Same(t, a.Manager, b.Manager, "issue #442: reused, not rebuilt")
		assert.Same(t, a.Resolver, b.Resolver)
		o, err := f.Bundle("")
		require.NoError(t, err)
		assert.NotSame(t, a.Manager, o.Manager, "providers have their own")
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
		assert.Same(t, s, other, "the provider's Service is reused")
	})
}

func TestFactory_DefaultRootsWhenNothingConfigured(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "/xdg")
	f, err := NewFactory(Config{Provider: "oanda"}, discard())
	require.NoError(t, err)
	b, err := f.Bundle("alpaca")
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

// TestFactory_SharesWriteLockPerProvider: every Bundle the Factory builds
// for a provider shares that provider's write lock, so per-request
// Services over the same stores never write concurrently (PR #452
// review); different providers' locks are independent.
func TestFactory_SharesWriteLockPerProvider(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	f, err := NewFactory(Config{StoreRoot: t.TempDir(), Provider: "oanda"}, discard())
	require.NoError(t, err)

	a, err := f.Bundle("stooq")
	require.NoError(t, err)
	b, err := f.Bundle("stooq")
	require.NoError(t, err)
	require.NotNil(t, a.writeLock)
	assert.Same(t, a.writeLock, b.writeLock, "same provider, same lock")

	o, err := f.Bundle("")
	require.NoError(t, err)
	assert.NotSame(t, a.writeLock, o.writeLock, "providers lock independently")

	cli, err := New(Config{StoreRoot: t.TempDir(), RawRoot: t.TempDir(), Provider: "oanda"}, discard())
	require.NoError(t, err)
	assert.Nil(t, cli.writeLock, "a standalone Bundle gets its Manager's own lock")
}

// TestFactory_FailedBuildIsNotCached: a provider whose bundle fails to
// build reports the error on every attempt, not once.
func TestFactory_FailedBuildIsNotCached(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	f, err := NewFactory(Config{StoreRoot: t.TempDir(), Provider: "oanda"}, discard())
	require.NoError(t, err)
	f.base.AlpacaKeyID = "only-half" // a one-sided Alpaca credential: alpaca's build fails

	for i := 0; i < 2; i++ {
		_, err := f.Bundle("alpaca")
		require.Error(t, err, "attempt %d", i+1)
	}
	assert.NotContains(t, f.bundles, "alpaca")
}

// TestFactory_ConcurrentFirstUseBuildsOnce: concurrent first requests for
// a provider share one bundle.
func TestFactory_ConcurrentFirstUseBuildsOnce(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	f, err := NewFactory(Config{StoreRoot: t.TempDir(), Provider: "oanda"}, discard())
	require.NoError(t, err)

	const n = 16
	managers := make([]any, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, err := f.Bundle("stooq")
			assert.NoError(t, err)
			managers[i] = b.Manager
		}()
	}
	wg.Wait()
	for _, m := range managers[1:] {
		assert.Same(t, managers[0], m)
	}
}
