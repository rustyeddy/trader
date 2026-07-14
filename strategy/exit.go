package strategy

import (
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// ExitStrategy manages stop placement after a position is open.
// It is called every bar regardless of position state (to warm up indicators),
// and is consulted to set/update the stop price on open lots.
type ExitStrategy interface {
	// Name returns a human-readable description for reports.
	Name() string

	// Ready reports whether the exit strategy has enough history to place stops.
	Ready() bool

	// Tick updates internal indicators. Called every bar before strategy.Update().
	Tick(c market.Candle)

	// InitialStop returns the stop price at the moment a position is opened.
	InitialStop(side types.Side, entry types.Price, c market.Candle) types.Price

	// UpdateStop returns the new stop price for an open lot each bar.
	// extreme is the lot's ExtremePrice (highest high for longs, lowest low for shorts).
	// The implementation must never move the stop against the position.
	UpdateStop(side types.Side, currentStop types.Price, entry types.Price, extreme types.Price, c market.Candle) types.Price
}

// NoopExit is a pass-through exit strategy. It never moves stops; the entry
// strategy is responsible for setting an initial stop via the OpenRequest.
type NoopExit struct{}

func (NoopExit) Name() string                                                         { return "" }
func (NoopExit) Ready() bool                                                          { return true }
func (NoopExit) Tick(_ market.Candle)                                                 {}
func (NoopExit) InitialStop(_ types.Side, _ types.Price, _ market.Candle) types.Price { return 0 }
func (NoopExit) UpdateStop(_ types.Side, currentStop types.Price, _ types.Price, _ types.Price, _ market.Candle) types.Price {
	return currentStop
}
