package trader

// ExitStrategy manages stop placement after a position is open.
// It is called every bar regardless of position state (to warm up indicators),
// and is consulted to set/update the stop price on open lots.
type ExitStrategy interface {
	// Name returns a human-readable description for reports.
	Name() string

	// Ready reports whether the exit strategy has enough history to place stops.
	Ready() bool

	// Tick updates internal indicators. Called every bar before strategy.Update().
	Tick(c Candle)

	// InitialStop returns the stop price at the moment a position is opened.
	InitialStop(side Side, entry Price, c Candle) Price

	// UpdateStop returns the new stop price for an open lot each bar.
	// extreme is the lot's ExtremePrice (highest high for longs, lowest low for shorts).
	// The implementation must never move the stop against the position.
	UpdateStop(side Side, currentStop Price, entry Price, extreme Price, c Candle) Price
}

// NoopExit is a pass-through exit strategy. It never moves stops; the entry
// strategy is responsible for setting an initial stop via the OpenRequest.
type NoopExit struct{}

func (NoopExit) Name() string { return "" }
func (NoopExit) Ready() bool  { return true }
func (NoopExit) Tick(_ Candle) {}
func (NoopExit) InitialStop(_ Side, _ Price, _ Candle) Price { return 0 }
func (NoopExit) UpdateStop(_ Side, currentStop Price, _ Price, _ Price, _ Candle) Price {
	return currentStop
}
