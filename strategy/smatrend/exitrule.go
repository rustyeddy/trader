package smatrend

import (
	"fmt"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// ExitDecision is what an ExitRule returns for one bar while long.
type ExitDecision struct {
	// NewStop, if non-nil, requests placing/ratcheting the protective
	// stop to this price via order.IntentAdjustStop — the mechanism
	// every ExitRule that manages a resting stop uses.
	NewStop *num.Price
	// ExitNow, if true, requests an immediate order.IntentExit
	// instead — for a rule whose own trigger condition is not itself
	// expressible as a resting broker-side stop (for example "close
	// crosses back below the SMA").
	ExitNow bool
}

// ExitRule decides, once per bar while a long position is open and
// has survived the bar (see Strategy.OnBar's own doc comment for why
// any broker-triggered stop from a prior bar has already resolved by
// the time this runs), what protective action — if any — to take
// (issue #347). It owns whatever state it needs relative to the
// current position (for example a high-water mark); OnEntry
// establishes that state fresh for each new position, never carrying
// over stale state from a previous one.
type ExitRule interface {
	// OnEntry is called exactly once, on the first bar a fresh long
	// position is observed, so the rule can initialize any state
	// relative to this specific position (for example seeding a
	// high-water mark from the entry bar's own High).
	OnEntry(entryBar marketdata.Bar)
	// OnLongBar is called once per bar while long, after the position
	// has survived the bar. smaValue is the strategy's own current
	// SMA value, supplied for a rule whose trigger depends on it (for
	// example smaCrossExitRule) rather than requiring every rule to
	// maintain its own SMA independently.
	OnLongBar(bar marketdata.Bar, smaValue float64) (ExitDecision, error)
}

// exitRuleRegistry maps a Config.ExitRuleName to its constructor. A
// new ExitRule is a new small type plus one entry here — never a
// change to Strategy's own control flow (issue #347).
var exitRuleRegistry = map[string]func(Config) (ExitRule, error){
	"trailing-stop": newTrailingStopExitRule,
	"sma-cross":     newSMACrossExitRule,
}

// trailingStopExitRule is EQS-01's own original, only exit mechanism
// (issue #335), now extracted behind ExitRule: maintain a high-water
// mark from each bar's own High while long, and ratchet a protective
// stop to Config's own TrailingStopPercent below it — monotonically
// upward only, never emitting a downward adjustment.
type trailingStopExitRule struct {
	retainFraction num.Rate
	highWaterMark  *num.Price
	lastStop       *num.Price
}

func newTrailingStopExitRule(cfg Config) (ExitRule, error) {
	retain, err := cfg.StopFraction()
	if err != nil {
		return nil, err
	}
	return &trailingStopExitRule{retainFraction: retain}, nil
}

func (r *trailingStopExitRule) OnEntry(entryBar marketdata.Bar) {
	high := entryBar.High
	r.highWaterMark = &high
	r.lastStop = nil
}

func (r *trailingStopExitRule) OnLongBar(bar marketdata.Bar, smaValue float64) (ExitDecision, error) {
	high := bar.High
	if r.highWaterMark == nil || high.Cmp(*r.highWaterMark) > 0 {
		r.highWaterMark = &high
	}

	newStop, err := r.highWaterMark.MulRate(r.retainFraction)
	if err != nil {
		return ExitDecision{}, fmt.Errorf("smatrend: computing trailing stop from high-water mark: %w", err)
	}

	if r.lastStop != nil && newStop.Cmp(*r.lastStop) <= 0 {
		return ExitDecision{}, nil
	}
	r.lastStop = &newStop
	return ExitDecision{NewStop: &newStop}, nil
}

// smaCrossExitRule exits immediately (order.IntentExit, not a resting
// stop) the first bar the close falls back to or below the current
// SMA value — issue #347's second ExitRule implementation,
// demonstrating a rule whose own trigger is not itself expressible as
// a resting broker-side price at all. Stateless: it needs nothing
// beyond the bar and SMA value OnLongBar is already given.
type smaCrossExitRule struct{}

func newSMACrossExitRule(Config) (ExitRule, error) {
	return smaCrossExitRule{}, nil
}

func (smaCrossExitRule) OnEntry(marketdata.Bar) {}

func (smaCrossExitRule) OnLongBar(bar marketdata.Bar, smaValue float64) (ExitDecision, error) {
	// bar.Close.Float64() is ADR-045's explicit exact-to-analytical
	// conversion boundary: a direct numeric conversion, never a
	// String()/strconv.ParseFloat() round-trip.
	if bar.Close.Float64() <= smaValue {
		return ExitDecision{ExitNow: true}, nil
	}
	return ExitDecision{}, nil
}
