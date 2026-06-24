package live

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/rustyeddy/trader/strategy"
)

// LiveStrategyConstructor builds a LiveStrategy from a params map.
type LiveStrategyConstructor func(params map[string]any) (LiveStrategy, error)

var (
	liveStrategyMu       sync.RWMutex
	liveStrategyRegistry = map[string]LiveStrategyConstructor{}
)

// RegisterLiveStrategy registers a LiveStrategy constructor under one or more names.
// Typically called from a package's init() function.
func RegisterLiveStrategy(ctor LiveStrategyConstructor, names ...string) error {
	if ctor == nil {
		return fmt.Errorf("RegisterLiveStrategy: nil constructor")
	}
	if len(names) == 0 {
		return fmt.Errorf("RegisterLiveStrategy: no names provided")
	}
	liveStrategyMu.Lock()
	defer liveStrategyMu.Unlock()
	for _, name := range names {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" {
			return fmt.Errorf("RegisterLiveStrategy: blank name")
		}
		if _, exists := liveStrategyRegistry[normalized]; exists {
			return fmt.Errorf("RegisterLiveStrategy: duplicate name %q", normalized)
		}
		liveStrategyRegistry[normalized] = ctor
	}
	return nil
}

// MustRegisterLiveStrategy registers a LiveStrategy and panics on error.
func MustRegisterLiveStrategy(ctor LiveStrategyConstructor, names ...string) {
	if err := RegisterLiveStrategy(ctor, names...); err != nil {
		panic(err)
	}
}

// LookupLiveStrategy returns the constructor registered under name, or nil.
func LookupLiveStrategy(name string) LiveStrategyConstructor {
	liveStrategyMu.RLock()
	defer liveStrategyMu.RUnlock()
	return liveStrategyRegistry[strings.ToLower(strings.TrimSpace(name))]
}

// RegisteredLiveStrategies returns the sorted list of registered live strategy names.
func RegisteredLiveStrategies() []string {
	liveStrategyMu.RLock()
	defer liveStrategyMu.RUnlock()
	out := make([]string, 0, len(liveStrategyRegistry))
	for k := range liveStrategyRegistry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// GetLiveStrategy looks up and constructs a LiveStrategy by kind.
func GetLiveStrategy(scfg strategy.StrategyConfig) (LiveStrategy, error) {
	name := strings.ToLower(strings.TrimSpace(scfg.Kind))
	if name == "" {
		return nil, fmt.Errorf("strategy.kind is required")
	}
	ctor := LookupLiveStrategy(name)
	if ctor == nil {
		return nil, fmt.Errorf("unsupported live strategy.kind %q (registered: %v)", name, RegisteredLiveStrategies())
	}
	return ctor(scfg.Params)
}
