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

// Names returns the names of all registered providers.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(providers))
	for k := range providers {
		out = append(out, k)
	}
	return out
}

var (
	candleMu        sync.RWMutex
	candleProviders = map[string]CandleProvider{}
)

// RegisterCandleProvider adds a candle-native provider to the global
// registry. Unlike Register, callers typically call this once at service
// startup with an already-credentialed provider instance rather than from
// init(), since candle-native sources (OANDA) need runtime configuration.
func RegisterCandleProvider(p CandleProvider) {
	if p == nil {
		return
	}
	candleMu.Lock()
	defer candleMu.Unlock()
	candleProviders[p.Name()] = p
}

// GetCandleProvider returns the candle-native provider with the given name,
// or an error if none has been registered.
func GetCandleProvider(name string) (CandleProvider, error) {
	candleMu.RLock()
	defer candleMu.RUnlock()
	p, ok := candleProviders[name]
	if !ok {
		return nil, fmt.Errorf("data: no candle provider registered for %q", name)
	}
	return p, nil
}
