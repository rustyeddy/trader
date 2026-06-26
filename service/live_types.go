package service

import (
	"context"
	"time"
)

// LiveStrategy is the internal runner protocol implemented by CandleStrategyAdapter
// and its decorators (statsTrackingStrategy, circuitBreakerStrategy).
// Tick is called on each price poll; the runner passes the current price
// and all tracked open positions so the strategy can decide what to do.
type LiveStrategy interface {
	Name() string
	Tick(ctx context.Context, price LivePrice, openTrades []LiveTrade) *LivePlan
}

// LivePrice is a bid/ask snapshot from the broker.
type LivePrice struct {
	Instrument string
	Bid        float64
	Ask        float64
	Time       time.Time
}

// Mid returns the mid-price.
func (p LivePrice) Mid() float64 { return (p.Bid + p.Ask) / 2 }

// LiveTrade describes an open position as seen by the live runner.
type LiveTrade struct {
	ID           string
	Instrument   string
	Units        int64 // positive = long, negative = short
	EntryPrice   float64
	UnrealizedPL float64
	OpenTime     time.Time // when OANDA opened the trade
	TicksOpen    int       // estimated ticks elapsed, seeded from OpenTime on restart
}

// Side returns "long" or "short".
func (t LiveTrade) Side() string {
	if t.Units >= 0 {
		return "long"
	}
	return "short"
}

// LivePlan is what the strategy asks the runner to do this tick.
// At most one new position is opened per tick; zero or more are closed.
type LivePlan struct {
	// Open describes a new position to open. Nil means hold.
	Open *LiveOpenRequest
	// CloseIDs lists trade IDs the strategy wants to close.
	CloseIDs []string
	// Reason is a human-readable note logged by the runner.
	Reason string
}

// LiveOpenRequest carries the parameters for a new live position.
type LiveOpenRequest struct {
	Side     string  // "long" or "short"
	StopPips float64 // stop-loss distance in pips
	TakePips float64 // take-profit distance in pips (0 = none)
	RiskPct  float64 // percent of account NAV to risk
	Reason   string  // strategy signal reason, e.g. "donchian-v6-breakout-down"
}
