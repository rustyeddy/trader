package marketdata

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func keyFor(t *testing.T, symbol string) partitionKey {
	t.Helper()
	return partitionKey{
		provider:   "oanda",
		symbol:     symbol,
		instrument: eurusd(),
		interval:   H1,
		year:       2020,
		month:      3,
	}
}

func TestBarCacheMissThenHit(t *testing.T) {
	c := newBarCache(4)
	key := keyFor(t, "EURUSD")

	_, _, ok := c.get(key)
	assert.False(t, ok)

	m := validManifest(t)
	bs := validBarSet(t)
	c.put(key, m, bs)

	gotM, gotBS, ok := c.get(key)
	require.True(t, ok)
	assert.Equal(t, m.Revision(), gotM.Revision())
	assert.Equal(t, bs.Bars, gotBS.Bars)
}

func TestBarCacheEvictsOldestOnceOverCapacity(t *testing.T) {
	c := newBarCache(2)
	m, bs := validManifest(t), validBarSet(t)

	k1 := keyFor(t, "AAA")
	k2 := keyFor(t, "BBB")
	k3 := keyFor(t, "CCC")

	c.put(k1, m, bs)
	c.put(k2, m, bs)
	c.put(k3, m, bs) // should evict k1, the oldest

	_, _, ok := c.get(k1)
	assert.False(t, ok, "oldest entry should have been evicted")
	_, _, ok = c.get(k2)
	assert.True(t, ok)
	_, _, ok = c.get(k3)
	assert.True(t, ok)
	assert.Equal(t, 2, c.len())
}

func TestBarCachePutRefreshesWithoutReorderingEviction(t *testing.T) {
	c := newBarCache(2)
	m, bs := validManifest(t), validBarSet(t)

	k1 := keyFor(t, "AAA")
	k2 := keyFor(t, "BBB")
	k3 := keyFor(t, "CCC")

	c.put(k1, m, bs)
	c.put(k2, m, bs)

	// Refresh k1's value; a "refresh," not a "touch," so it must not move
	// to the back of the eviction queue.
	refreshed := m
	refreshed.BuiltAt = m.BuiltAt.Add(1)
	c.put(k1, refreshed, bs)

	c.put(k3, m, bs) // should still evict k1, not k2

	_, _, ok := c.get(k1)
	assert.False(t, ok, "k1 should still be evicted despite the refresh")
	_, _, ok = c.get(k2)
	assert.True(t, ok)
}

func TestBarCacheInvalidate(t *testing.T) {
	c := newBarCache(4)
	key := keyFor(t, "EURUSD")
	c.put(key, validManifest(t), validBarSet(t))

	c.invalidate(key)

	_, _, ok := c.get(key)
	assert.False(t, ok)
	assert.Equal(t, 0, c.len())
}

func TestBarCacheInvalidateUnknownKeyIsNoop(t *testing.T) {
	c := newBarCache(4)
	assert.NotPanics(t, func() { c.invalidate(keyFor(t, "EURUSD")) })
}

func TestBarCacheDefaultCapacity(t *testing.T) {
	c := newBarCache(0)
	assert.Equal(t, defaultCacheCapacity, c.capacity)

	c = newBarCache(-1)
	assert.Equal(t, defaultCacheCapacity, c.capacity)
}

// A nil *barCache behaves as an always-miss, no-op cache so a Manager
// somehow constructed without New (bypassing the zero-value-unusable
// contract) cannot panic merely by touching its cache.
func TestNilBarCacheIsSafe(t *testing.T) {
	var c *barCache
	_, _, ok := c.get(keyFor(t, "EURUSD"))
	assert.False(t, ok)
	assert.NotPanics(t, func() { c.put(keyFor(t, "EURUSD"), validManifest(t), validBarSet(t)) })
	assert.NotPanics(t, func() { c.invalidate(keyFor(t, "EURUSD")) })
	assert.Equal(t, 0, c.len())
}

// TestBarCacheConcurrentAccess is the -race regression a design review
// asked for: barCache's map and list must survive concurrent get/put/
// invalidate/len from multiple goroutines without racing or panicking.
// Run with `go test -race` to be meaningful.
func TestBarCacheConcurrentAccess(t *testing.T) {
	c := newBarCache(8)
	m, bs := validManifest(t), validBarSet(t)
	keys := make([]partitionKey, 4)
	for i := range keys {
		keys[i] = keyFor(t, string(rune('A'+i))+string(rune('A'+i))+string(rune('A'+i)))
	}

	var wg sync.WaitGroup
	for g := range 16 {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			key := keys[g%len(keys)]
			for i := range 50 {
				c.put(key, m, bs)
				c.get(key)
				c.len()
				if i%10 == 0 {
					c.invalidate(key)
				}
			}
		}(g)
	}
	wg.Wait()
}

// TestBarCacheGetReturnsIndependentManifestClone proves the cache
// boundary a design review asked for directly: mutating a Manifest
// returned by get must never affect what a later get for the same key
// returns.
func TestBarCacheGetReturnsIndependentManifestClone(t *testing.T) {
	c := newBarCache(4)
	key := keyFor(t, "EURUSD")
	c.put(key, validDerivedManifest(t), validBarSet(t))

	got1, _, ok := c.get(key)
	require.True(t, ok)
	require.NotNil(t, got1.Parent)
	got1.Parent.Revision = "mutated-by-caller"

	got2, _, ok := c.get(key)
	require.True(t, ok)
	require.NotNil(t, got2.Parent)
	assert.Equal(t, validDerivedManifest(t).Parent.Revision, got2.Parent.Revision,
		"mutating one get's result must not affect a later get")
}

// TestBarCachePutClonesManifestFromCaller proves the other direction:
// mutating the Manifest a caller passed to put, after put returns, must
// not affect what the cache goes on to serve.
func TestBarCachePutClonesManifestFromCaller(t *testing.T) {
	c := newBarCache(4)
	key := keyFor(t, "EURUSD")
	m := validDerivedManifest(t)
	c.put(key, m, validBarSet(t))

	m.Parent.Revision = "mutated-after-put"

	got, _, ok := c.get(key)
	require.True(t, ok)
	assert.Equal(t, validDerivedManifest(t).Parent.Revision, got.Parent.Revision)
}
