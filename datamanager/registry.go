package datamanager

import (
	"fmt"
	"sync"
)

var (
	mu        sync.RWMutex
	providers = map[string]Provider{}
)

// Register adds a provider to the global registry. Typically called from
// a provider package's init() function.
func Register(p Provider) {
	if p == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	providers[p.Name()] = p
}

// Get returns the provider with the given name, or an error if no
// provider with that name has been registered.
func Get(name string) (Provider, error) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := providers[name]
	if !ok {
		return nil, fmt.Errorf("data: no provider registered for %q", name)
	}
	return p, nil
}
