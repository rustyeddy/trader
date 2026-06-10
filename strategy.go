package trader

import "context"

// Strategy is the single backtest strategy interface used across the repo.
type Strategy interface {
	Name() string
	Reset()
	Ready() bool
	Update(context.Context, *CandleTime, *Backtest) *StrategyPlan

	// StopDescription returns a human-readable description of how this strategy
	// places stops, e.g. "ATR(14)×1.5", "25 pips", or "" if none.
	StopDescription() string
}
