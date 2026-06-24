package strategy

import (
	"context"

	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/market"
)

// StrategyContext is the minimal, read-only view of an in-progress run that a
// Strategy needs during Update: which instrument is trading and the currently
// open lots. It replaces passing the concrete *Backtest so the strategy layer
// does not depend on the backtest engine. *Backtest implements it.
type StrategyContext interface {
	// Instrument returns the instrument being traded (e.g. "EUR_USD").
	Instrument() string
	// OpenLots returns a read-only view of the currently open lots. It never
	// returns nil; an empty view is returned when there are no open lots.
	OpenLots() LotView
}

// LotView is a read-only view over a set of open lots. *LotBook satisfies it.
type LotView interface {
	Len() int
	Range(func(*execution.Lot) error) error
}

// Strategy is the single backtest strategy interface used across the repo.
type Strategy interface {
	Name() string
	Reset()
	Ready() bool
	Update(context.Context, *market.CandleTime, StrategyContext) *StrategyPlan

	// StopDescription returns a human-readable description of how this strategy
	// places stops, e.g. "ATR(14)×1.5", "25 pips", or "" if none.
	StopDescription() string
}
