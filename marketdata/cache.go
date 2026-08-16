package marketdata

import "container/list"

// defaultCacheCapacity is the number of canonical partitions barCache
// retains when Config.CacheCapacity is not set to a positive value.
const defaultCacheCapacity = 32

// cacheEntry is one cached (Manifest, BarSet) pair, keyed by the
// partition it was loaded from.
type cacheEntry struct {
	key      partitionKey
	manifest Manifest
	bars     BarSet
}

// barCache is Manager's own bounded, FIFO-evicted memory cache of
// published canonical partitions (issue #78, ADR-020). It exists purely
// to avoid re-reading and re-parsing a canonical store file on every
// query that touches the same partition; it is not a coherence layer over
// concurrent writers, and it implements no adaptive or LRU eviction —
// issue #78 explicitly scopes those out.
//
// barCache is never exported and never appears in Manager's public
// surface: no Config field can inject one, and no Manager method returns
// one. Only Manager itself reads or writes it, the same boundary that
// keeps the canonical store itself invisible outside this package.
//
// A zero-value *barCache (nil) is safe to use as an always-miss cache;
// every method treats a nil receiver as an empty, capacity-zero cache
// rather than panicking. This is deliberate defense for a hypothetical
// Manager constructed without going through New, not an expected runtime
// path.
type barCache struct {
	capacity int
	order    *list.List
	entries  map[partitionKey]*list.Element
}

// newBarCache returns a barCache holding at most capacity partitions.
// A non-positive capacity selects defaultCacheCapacity.
func newBarCache(capacity int) *barCache {
	if capacity <= 0 {
		capacity = defaultCacheCapacity
	}
	return &barCache{
		capacity: capacity,
		order:    list.New(),
		entries:  make(map[partitionKey]*list.Element),
	}
}

// get returns the cached (Manifest, BarSet) for key, if present.
func (c *barCache) get(key partitionKey) (Manifest, BarSet, bool) {
	if c == nil {
		return Manifest{}, BarSet{}, false
	}
	el, ok := c.entries[key]
	if !ok {
		return Manifest{}, BarSet{}, false
	}
	e := el.Value.(*cacheEntry)
	return e.manifest, e.bars, true
}

// put caches (m, bs) under key, refreshing an existing entry in place
// rather than reordering it, and evicting the oldest entry (FIFO, by
// insertion order) once capacity is exceeded. A refreshed entry keeps its
// original insertion position: put is not a "touch," so a hot partition
// does not get to skip the eviction queue simply by being read
// repeatedly — issue #78 asks for simple bounded behavior, not an
// adaptive policy.
func (c *barCache) put(key partitionKey, m Manifest, bs BarSet) {
	if c == nil || c.capacity <= 0 {
		return
	}
	if el, ok := c.entries[key]; ok {
		e := el.Value.(*cacheEntry)
		e.manifest = m
		e.bars = bs
		return
	}
	el := c.order.PushBack(&cacheEntry{key: key, manifest: m, bars: bs})
	c.entries[key] = el
	for c.order.Len() > c.capacity {
		oldest := c.order.Front()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		delete(c.entries, oldest.Value.(*cacheEntry).key)
	}
}

// invalidate evicts key's cached entry, if any. This is the cache's
// revision-awareness hook: it exists so that when Manager gains a
// canonical-build/publish operation, publishing a new revision under key
// can evict any older revision already cached, rather than leaving a
// stale partition servable from memory after the store's own current
// revision has moved on. No such publish path exists yet (Manager's
// operations remain read-only through this issue), so invalidate is
// exercised directly by tests today rather than by another Manager
// method; it is not dead code, it is the seam the future write path
// requires.
func (c *barCache) invalidate(key partitionKey) {
	if c == nil {
		return
	}
	if el, ok := c.entries[key]; ok {
		c.order.Remove(el)
		delete(c.entries, key)
	}
}

// len reports the number of partitions currently cached, for tests.
func (c *barCache) len() int {
	if c == nil {
		return 0
	}
	return c.order.Len()
}
