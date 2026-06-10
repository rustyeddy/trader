package trader

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// StrategyConstructor builds a Strategy from a config's Params map.
// Each implementation owns its own param parsing.
type StrategyConstructor func(params map[string]any) (Strategy, error)

var (
	strategyMu       sync.RWMutex
	strategyRegistry = map[string]StrategyConstructor{}
)

// RegisterStrategy adds a strategy constructor under one or more names.
// Typically called from an implementation package's init() function.
// Multiple aliases are supported (e.g. "donchian", "donchian-breakout").
func RegisterStrategy(ctor StrategyConstructor, names ...string) {
	if ctor == nil || len(names) == 0 {
		return
	}
	strategyMu.Lock()
	defer strategyMu.Unlock()
	for _, name := range names {
		strategyRegistry[strings.ToLower(strings.TrimSpace(name))] = ctor
	}
}

// LookupStrategy returns the constructor registered under name, or nil.
func LookupStrategy(name string) StrategyConstructor {
	strategyMu.RLock()
	defer strategyMu.RUnlock()
	return strategyRegistry[strings.ToLower(strings.TrimSpace(name))]
}

// RegisteredStrategies returns the sorted list of registered strategy names.
// Useful for help text and validation.
func RegisteredStrategies() []string {
	strategyMu.RLock()
	defer strategyMu.RUnlock()
	out := make([]string, 0, len(strategyRegistry))
	for k := range strategyRegistry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// GetStrategy is the public dispatcher used by config-driven backtest setup.
// It looks the strategy up in the registry; implementations register
// themselves via init() in their own packages.
func GetStrategy(scfg StrategyConfig) (Strategy, error) {
	name := strings.ToLower(strings.TrimSpace(scfg.Kind))
	if name == "" {
		name = "fake" // historical default
	}
	ctor := LookupStrategy(name)
	if ctor == nil {
		return nil, fmt.Errorf("unsupported strategy.kind %q (registered: %v)", name, RegisteredStrategies())
	}
	return ctor(scfg.Params)
}
