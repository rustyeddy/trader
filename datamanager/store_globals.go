package marketdata

import "sync"

const defaultStoreDir = "/srv/trading/data/candles"

type storeSwapFrame struct {
	store  *Store
	prev   *storeSwapFrame
	active bool
}

var (
	storeMu   sync.RWMutex
	baseStore = NewStoreAt(defaultStoreDir)
	store     = baseStore
	storeSwap *storeSwapFrame
)

// SetDataDir overrides the global store's base directory.
// Call from main before any data operations.
func SetDataDir(dir string) {
	if dir == "" {
		return
	}

	storeMu.Lock()
	baseStore = NewStoreAt(dir)
	if storeSwap == nil {
		store = baseStore
	}
	storeMu.Unlock()
}

// GetStore returns the global Store. Used by sibling packages
// (e.g. data/dukascopy) that need direct store access.
func GetStore() *Store {
	storeMu.RLock()
	defer storeMu.RUnlock()
	return store
}

// SwapStore replaces the global Store with the given one and returns
// a function that restores the previous Store. Useful in tests for
// sibling packages that need to point the global at a temp directory.
func SwapStore(s *Store) (restore func()) {
	storeMu.Lock()
	frame := &storeSwapFrame{store: s, prev: storeSwap, active: true}
	storeSwap = frame
	store = s
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
				store = storeSwap.store
			} else {
				store = baseStore
			}
		}
		storeMu.Unlock()
	}
}

// NewStoreAt returns a fresh Store rooted at basedir. Useful for tests.
func NewStoreAt(basedir string) *Store {
	return &Store{basedir: basedir}
}
