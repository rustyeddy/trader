package smatrend

import (
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// ReEntryContext is what ShouldEnter needs to decide whether to
// re-enter on one bar. CrossedAboveSMA is the strategy's own
// continuously-maintained cross-state result for this bar (see
// crossState's own doc comment for why it must run unconditionally,
// every ready bar, regardless of position side) — passed in rather
// than duplicated per rule, so a "fresh cross" ReEntryRule stays a
// trivial, stateless wrapper around it instead of running a second,
// redundant cross-detection machine.
type ReEntryContext struct {
	Bar             marketdata.Bar
	CrossedAboveSMA bool
}

// ReEntryRule decides, once per bar while flat, whether to re-enter —
// but only ever *after* at least one prior exit (issue #347). The
// very first entry (before any exit has ever occurred) always uses
// the cross-above-SMA trigger directly, never a configured
// ReEntryRule: there is nothing yet to "reclaim" or "break out of."
// See Strategy.onFlat for exactly where that split happens.
type ReEntryRule interface {
	// OnExit is called exactly once, on the bar a position is
	// observed to have exited (whether via a triggered stop or a
	// direct ExitRule.ExitNow), so the rule can initialize any state
	// relative to that specific exit (for example seeding a post-exit
	// high, or simply remembering the exit price). exitPrice is the
	// exit rule's own last-known intended stop level — not
	// necessarily the real broker fill price to the cent in a
	// gap-through case (ADR-026). That is a deliberate choice: a rule
	// like reclaimExitPriceReEntryRule cares about the level the
	// strategy itself was protecting at, not incidental slippage from
	// the fill.
	OnExit(exitPrice num.Price, exitBar marketdata.Bar)
	// ShouldEnter is called once per bar while flat, after at least
	// one prior OnExit call.
	ShouldEnter(ctx ReEntryContext) bool
}

// reEntryRuleRegistry maps a Config.ReEntryRuleName to its
// constructor. A new ReEntryRule is a new small type plus one entry
// here — never a change to Strategy's own control flow (issue #347).
var reEntryRuleRegistry = map[string]func(Config) (ReEntryRule, error){
	"fresh-cross":        newFreshCrossReEntryRule,
	"reclaim-exit-price": newReclaimExitPriceReEntryRule,
	"breakout":           newBreakoutReEntryRule,
}

// freshCrossReEntryRule is EQS-01's own original, only re-entry
// mechanism (issue #335), now extracted behind ReEntryRule: require a
// fresh cross back above the SMA, exactly the same trigger the very
// first entry itself uses. Stateless: OnExit does nothing, since the
// strategy's own crossState already continuously tracks the
// information this rule needs, regardless of position side.
type freshCrossReEntryRule struct{}

func newFreshCrossReEntryRule(Config) (ReEntryRule, error) { return freshCrossReEntryRule{}, nil }

func (freshCrossReEntryRule) OnExit(num.Price, marketdata.Bar) {}

func (freshCrossReEntryRule) ShouldEnter(ctx ReEntryContext) bool {
	return ctx.CrossedAboveSMA
}

// reclaimExitPriceReEntryRule re-enters the first bar the close moves
// back strictly above the price the strategy exited at (issue #347) —
// intended to avoid missing a normal pullback-and-resume without
// waiting for a fresh SMA cross that may lag well behind the recovery.
type reclaimExitPriceReEntryRule struct {
	exitPrice num.Price
}

func newReclaimExitPriceReEntryRule(Config) (ReEntryRule, error) {
	return &reclaimExitPriceReEntryRule{}, nil
}

func (r *reclaimExitPriceReEntryRule) OnExit(exitPrice num.Price, _ marketdata.Bar) {
	r.exitPrice = exitPrice
}

func (r *reclaimExitPriceReEntryRule) ShouldEnter(ctx ReEntryContext) bool {
	return ctx.Bar.Close.Cmp(r.exitPrice) > 0
}

// breakoutReEntryRule re-enters the first bar the close moves above
// the highest High observed since the last exit — a Donchian-style
// breakout (issue #347), independent of the old exit level entirely,
// intended for a genuine trend resumption rather than a mere pullback
// recovery. The breakout threshold is always the high established by
// *prior* bars since the exit, never including the current bar's own
// new High in its own check (sinceExitHigh updates only after
// ShouldEnter has already decided), so a bar can never trivially
// "break out" against a high it itself just set.
type breakoutReEntryRule struct {
	sinceExitHigh num.Price
}

func newBreakoutReEntryRule(Config) (ReEntryRule, error) {
	return &breakoutReEntryRule{}, nil
}

func (r *breakoutReEntryRule) OnExit(_ num.Price, exitBar marketdata.Bar) {
	r.sinceExitHigh = exitBar.High
}

func (r *breakoutReEntryRule) ShouldEnter(ctx ReEntryContext) bool {
	enter := ctx.Bar.Close.Cmp(r.sinceExitHigh) > 0
	if ctx.Bar.High.Cmp(r.sinceExitHigh) > 0 {
		r.sinceExitHigh = ctx.Bar.High
	}
	return enter
}
