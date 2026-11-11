package marketdatacfg

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

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
	assert.Nil(t, f.slots["alpaca"].bundle, "nothing stored, so the next call retries")
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

// slowBuilds wraps f's build: the named provider's build blocks until
// release is closed, after signaling started; every build is counted.
type slowBuilds struct {
	mu       sync.Mutex
	counts   map[string]int
	started  chan struct{}
	release  chan struct{}
	provider string
	next     func(string) (Bundle, error)
}

func blockBuild(f *Factory, provider string) *slowBuilds {
	s := &slowBuilds{counts: map[string]int{}, started: make(chan struct{}, 64), release: make(chan struct{}), provider: provider, next: f.build}
	f.build = func(p string) (Bundle, error) {
		s.mu.Lock()
		s.counts[p]++
		s.mu.Unlock()
		if p == s.provider {
			s.started <- struct{}{}
			<-s.release
		}
		return s.next(p)
	}
	return s
}

func (s *slowBuilds) count(p string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[p]
}

// TestFactory_ProvidersBuildIndependently: while one provider's first
// build is in progress, other providers' lookups — cached or first-use —
// are not held up, and concurrent first uses of the slow provider wait for
// its one build instead of starting their own (PR #458 review).
func TestFactory_ProvidersBuildIndependently(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	f, err := NewFactory(Config{StoreRoot: t.TempDir(), Provider: "oanda"}, discard()) // oanda built
	require.NoError(t, err)
	slow := blockBuild(f, "alpaca")

	const waiters = 8
	alpaca := make(chan Bundle, waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			b, err := f.Bundle("alpaca")
			assert.NoError(t, err)
			alpaca <- b
		}()
	}
	<-slow.started // alpaca's build is now blocked, holding only its own slot

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := f.Bundle("") // cached oanda
		assert.NoError(t, err)
		_, err = f.Bundle("stooq") // first-use stooq
		assert.NoError(t, err)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("other providers were blocked by alpaca's in-progress build")
	}
	assert.Empty(t, alpaca, "alpaca callers are still waiting on its one build")

	close(slow.release)
	first := <-alpaca
	for i := 1; i < waiters; i++ {
		assert.Same(t, first.Manager, (<-alpaca).Manager, "every waiter shares the one build")
	}
	assert.Equal(t, 1, slow.count("alpaca"), "concurrent first uses coalesce")
	assert.Equal(t, 1, slow.count("stooq"))
	assert.Equal(t, 0, slow.count("oanda"), "a cached provider is not rebuilt")
}

// TestFactory_FailedBuildRetriesThenCaches: after a failed build the next
// call builds again; once a build succeeds it is reused.
func TestFactory_FailedBuildRetriesThenCaches(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	f, err := NewFactory(Config{StoreRoot: t.TempDir(), Provider: "oanda"}, discard())
	require.NoError(t, err)
	builds, next := 0, f.build
	f.build = func(p string) (Bundle, error) {
		builds++
		if builds == 1 {
			return Bundle{}, errors.New("transient")
		}
		return next(p)
	}

	_, err = f.Bundle("stooq")
	require.Error(t, err)
	a, err := f.Bundle("stooq")
	require.NoError(t, err, "retried after the failure")
	b, err := f.Bundle("stooq")
	require.NoError(t, err)
	assert.Same(t, a.Manager, b.Manager)
	assert.Equal(t, 2, builds, "one failure, one success, then reuse")
}
