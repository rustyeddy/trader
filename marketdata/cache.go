package marketdata

import (
	"container/list"
	"sync"
)

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
// barCache is safe for concurrent use: Manager.Bars is an
// application-service entry point, and nothing about it promises a
// caller single-goroutine use, so every method locks mu. A review
// finding on the original, unsynchronized implementation is what added
// this — see TestBarCacheConcurrentAccess for the -race regression.
//
// A zero-value *barCache (nil) is safe to use as an always-miss cache;
// every method treats a nil receiver as an empty, capacity-zero cache
// rather than panicking. This is deliberate defense for a hypothetical
// Manager constructed without going through New, not an expected runtime
// path.
type barCache struct {
	mu       sync.Mutex
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

// get returns the cached (Manifest, BarSet) for key, if present. The
// returned Manifest is a defensive clone (see cloneManifest): it shares
// no mutable state with the cached entry, so a caller mutating it (for
// example through its Parent pointer) can never poison what a later get
// for the same key returns.
func (c *barCache) get(key partitionKey) (Manifest, BarSet, bool) {
	if c == nil {
		return Manifest{}, BarSet{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.entries[key]
	if !ok {
		return Manifest{}, BarSet{}, false
	}
	e := el.Value.(*cacheEntry)
	return cloneManifest(e.manifest), e.bars, true
}

// put caches (m, bs) under key, refreshing an existing entry in place
// rather than reordering it, and evicting the oldest entry (FIFO, by
// insertion order) once capacity is exceeded. A refreshed entry keeps its
// original insertion position: put is not a "touch," so a hot partition
// does not get to skip the eviction queue simply by being read
// repeatedly — issue #78 asks for simple bounded behavior, not an
// adaptive policy.
//
// m is stored as a defensive clone (see cloneManifest), so a caller that
// goes on to mutate the Manifest value it passed in — through its Parent
// pointer — cannot reach back into the cache.
func (c *barCache) put(key partitionKey, m Manifest, bs BarSet) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.capacity <= 0 {
		return
	}
	m = cloneManifest(m)
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
	c.mu.Lock()
	defer c.mu.Unlock()

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
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// cloneManifest returns a copy of m that shares no mutable state with m.
// Manifest is otherwise a plain value type, but its one pointer field,
// Parent *ParentRef, is shared by a shallow assignment — a caller
// mutating *m.Parent (for example Revision) would otherwise reach
// through to any other Manifest value copied from the same m, including
// one sitting in barCache or already handed out by an earlier get/
// Manifests call. cloneManifest is the single place that guards every
// such boundary: barCache.get, barCache.put, and BarReader.Manifests
// all route through it rather than duplicating the *ParentRef check.
func cloneManifest(m Manifest) Manifest {
	if m.Parent == nil {
		return m
	}
	parent := *m.Parent
	m.Parent = &parent
	return m
}
