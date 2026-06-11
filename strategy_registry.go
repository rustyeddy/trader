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
func RegisterStrategy(ctor StrategyConstructor, names ...string) error {
	if ctor == nil {
		return fmt.Errorf("RegisterStrategy: nil constructor")
	}
	if len(names) == 0 {
		return fmt.Errorf("RegisterStrategy: no strategy names provided")
	}
	strategyMu.Lock()
	defer strategyMu.Unlock()
	for _, name := range names {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if normalized == "" {
			return fmt.Errorf("RegisterStrategy: blank strategy name")
		}
		if _, exists := strategyRegistry[normalized]; exists {
			return fmt.Errorf("RegisterStrategy: duplicate strategy name %q", normalized)
		}
		strategyRegistry[normalized] = ctor
	}
	return nil
}

// MustRegisterStrategy registers a strategy and panics on error.
// Intended for use in package init() registration paths so invalid
// registrations fail fast at startup.
func MustRegisterStrategy(ctor StrategyConstructor, names ...string) {
	if err := RegisterStrategy(ctor, names...); err != nil {
		panic(err)
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
		return nil, fmt.Errorf("strategy.kind is required")
	}
	ctor := LookupStrategy(name)
	if ctor == nil {
		return nil, fmt.Errorf("unsupported strategy.kind %q (registered: %v)", name, RegisteredStrategies())
	}
	return ctor(scfg.Params)
}
