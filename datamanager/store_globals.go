package datamanager

import "sync"

const defaultStoreDir = "/srv/trading/data/candles"

type storeSwapFrame struct {
	store  *store
	prev   *storeSwapFrame
	active bool
}

var (
	storeMu     sync.RWMutex
	baseStore   = newStoreAt(defaultStoreDir)
	globalStore = baseStore
	storeSwap   *storeSwapFrame
)

// SetDataDir overrides the global store's base directory.
// Call from main before any data operations.
func SetDataDir(dir string) {
	if dir == "" {
		return
	}

	storeMu.Lock()
	baseStore = newStoreAt(dir)
	if storeSwap == nil {
		globalStore = baseStore
	}
	storeMu.Unlock()
}

// RawRoot returns the root directory for raw source data under the current
// global data directory, e.g. /srv/trading/data/raw.
func RawRoot() string {
	storeMu.RLock()
	defer storeMu.RUnlock()
	return globalStore.rawRoot()
}

// getStore returns the global store. Internal use only — nothing outside
// datamanager may hold a *store.
func getStore() *store {
	storeMu.RLock()
	defer storeMu.RUnlock()
	return globalStore
}

// swapStore replaces the global store with the given one and returns
// a function that restores the previous store. Used by tests (via
// SeedCandles) to point the global at a temp directory.
func swapStore(s *store) (restore func()) {
	storeMu.Lock()
	frame := &storeSwapFrame{store: s, prev: storeSwap, active: true}
	storeSwap = frame
	globalStore = s
	storeMu.Unlock()

	return func() {
		storeMu.Lock()
		if !frame.active {
			storeMu.Unlock()
			return
		}
		frame.active = false

		if storeSwap == frame {
			for storeSwap != nil && !storeSwap.active {
				storeSwap = storeSwap.prev
			}
			if storeSwap != nil {
				globalStore = storeSwap.store
			} else {
				globalStore = baseStore
			}
		}
		storeMu.Unlock()
	}
}

// newStoreAt returns a fresh store rooted at basedir. Useful for tests.
func newStoreAt(basedir string) *store {
	return &store{basedir: basedir}
}
