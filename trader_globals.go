package trader

var (
	store = &Store{
		basedir: "/data/candles",
	}
)

// SetDataDir overrides the global store's base directory.
// Call from main before any data operations.
func SetDataDir(dir string) {
	if dir != "" {
		store.basedir = dir
	}
}

// GetStore returns the global Store. Used by sibling packages
// (e.g. data/dukascopy) that need direct store access.
func GetStore() *Store {
	return store
}

// SwapStore replaces the global Store with the given one and returns
// a function that restores the previous Store. Useful in tests for
// sibling packages that need to point the global at a temp directory.
func SwapStore(s *Store) (restore func()) {
	old := store
	store = s
	return func() { store = old }
}

// NewStoreAt returns a fresh Store rooted at basedir. Useful for tests.
func NewStoreAt(basedir string) *Store {
	return &Store{basedir: basedir}
}
