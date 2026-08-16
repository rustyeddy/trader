package marketdata

import (
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
