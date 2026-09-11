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
// very first entry ever (before any exit has ever occurred) is
// instead governed by InitialEntryRule, never a configured
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
	// ObserveFlatBar is called once per bar while flat, after at
	// least one prior OnExit call, for *every* such bar regardless of
	// the central above-SMA regime gate in Strategy.onFlat (PR #362
	// review) — unlike ShouldEnter, which is only ever consulted once
	// price is already back above the SMA. A rule that needs
	// continuous, chronologically correct bar-by-bar state (for
	// example nBarBreakoutReEntryRule's own rolling N-bar window)
	// must maintain it here, not inside ShouldEnter, or it silently
	// skips every bar observed during a below-SMA stretch: the bug PR
	// #362's review caught in an earlier version of
	// nBarBreakoutReEntryRule, where the rolling window was fed only
	// from ShouldEnter and therefore missed the immediately preceding
	// *below-SMA* completed bars entirely. Called strictly before
	// ShouldEnter's own decision is journaled/acted on, but *after*
	// Strategy.onFlat has already computed this bar's enter/no-enter
	// decision (see that method's own doc comment) — so ObserveFlatBar
	// must never influence ShouldEnter's own already-made decision for
	// this same bar; it only prepares state for the *next* bar. Most
	// rules need no such state and implement this as a no-op.
	ObserveFlatBar(bar marketdata.Bar)
	// ShouldEnter is called once per bar while flat and above the SMA
	// (never below it — see Strategy.onFlat's own doc comment for the
	// central regime gate), after at least one prior OnExit call.
	ShouldEnter(ctx ReEntryContext) bool
}

// reEntryRuleRegistry maps a Config.ReEntryRuleName to its
// constructor. A new ReEntryRule is a new small type plus one entry
// here — never a change to Strategy's own control flow (issue #347).
var reEntryRuleRegistry = map[string]func(Config) (ReEntryRule, error){
	"fresh-cross":        newFreshCrossReEntryRule,
	"reclaim-exit-price": newReclaimExitPriceReEntryRule,
	"breakout":           newBreakoutReEntryRule,
	"above-sma":          newAboveSMAReEntryRule,
	// breakout-2 and breakout-3 are issue #361's own N-bar breakout
	// re-entry candidates — see nBarBreakoutReEntryRule's own doc
	// comment for why they are two fixed names rather than one rule
	// with a configurable lookback (issue #361's own guardrail: no
	// broader N sweep in this issue).
	"breakout-2": newNBarBreakoutReEntryRule(2),
	"breakout-3": newNBarBreakoutReEntryRule(3),
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
func (freshCrossReEntryRule) ObserveFlatBar(marketdata.Bar)    {}

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

func (*reclaimExitPriceReEntryRule) ObserveFlatBar(marketdata.Bar) {}

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
//
// Known limitation, not fixed here (PR #362 review; tracked as a
// follow-up rather than silently changed in an unrelated issue's own
// PR): sinceExitHigh is updated only inside ShouldEnter, which — like
// every ReEntryRule's own ShouldEnter — is never consulted while
// Strategy.onFlat's central SMA gate is false. This rule therefore has
// the same latent gap nBarBreakoutReEntryRule had before this same PR
// added ObserveFlatBar: a below-SMA stretch's own bars never update
// sinceExitHigh, so the "since exit" high this rule tracks can
// understate the real since-exit high whenever a meaningful new high
// occurs while price is below the SMA. ObserveFlatBar is implemented
// here as a no-op, preserving this rule's exact existing (buggy)
// behavior rather than changing it as a side effect of #361's own
// interface addition.
type breakoutReEntryRule struct {
	sinceExitHigh num.Price
}

func newBreakoutReEntryRule(Config) (ReEntryRule, error) {
	return &breakoutReEntryRule{}, nil
}

func (r *breakoutReEntryRule) OnExit(_ num.Price, exitBar marketdata.Bar) {
	r.sinceExitHigh = exitBar.High
}

func (*breakoutReEntryRule) ObserveFlatBar(marketdata.Bar) {}

func (r *breakoutReEntryRule) ShouldEnter(ctx ReEntryContext) bool {
	enter := ctx.Bar.Close.Cmp(r.sinceExitHigh) > 0
	if ctx.Bar.High.Cmp(r.sinceExitHigh) > 0 {
		r.sinceExitHigh = ctx.Bar.High
	}
	return enter
}

// nBarBreakoutReEntryRule re-enters the first bar the close moves
// above the highest High of the immediately preceding lookback
// *completed* bars — a fixed, sliding N-bar window, independent of
// the exit level and of how long the strategy has been flat (issue
// #361), unlike breakoutReEntryRule's own unbounded "since exit"
// high-water mark. Two fixed instances are registered, "breakout-2"
// and "breakout-3" — issue #361's own guardrail against a broader N
// sweep in this issue means lookback is never exposed through Config
// at all, only through which of these two constructors a caller
// selects.
//
// OnExit resets the window to empty rather than seeding it directly
// (a prior revision seeded from exitBar here, double-counting it —
// see OnExit's own doc comment): the window instead grows one bar per
// ObserveFlatBar call, starting with the exit bar itself, up to
// lookback bars, then slides (oldest evicted, newest appended).
// ObserveFlatBar runs for *every* flat bar, not only the ones
// ShouldEnter is consulted on, precisely so this window stays
// chronologically correct across a below-SMA stretch (PR #362 review
// fixed an earlier version of this rule that updated the window only
// from inside ShouldEnter, silently skipping every bar observed while
// Strategy.onFlat's central SMA gate was false). The window can never
// include a bar from before the exit:
// ObserveFlatBar/ShouldEnter are only ever called while flat (see
// their own doc comments), so no bar observed while the position was
// open is available to this rule at all — unlike an offline analysis
// with full hindsight over the whole bar series (for example issue
// #354's own diagnostic, which could and did look further back before
// the exit for a very early flat bar), this live rule genuinely
// cannot, and does not try to. The breakout threshold ShouldEnter
// checks is always the high established by *prior* bars in the
// window, never including the current bar's own new High, since
// ObserveFlatBar for this bar runs only *after* Strategy.onFlat has
// already computed this bar's own enter/no-enter decision — mirroring
// breakoutReEntryRule's identical check-before-update ordering, so a
// bar can never trivially "break out" against a high it itself just
// set.
type nBarBreakoutReEntryRule struct {
	lookback int
	highs    []num.Price // most recent up to lookback bars' High, oldest first
}

func newNBarBreakoutReEntryRule(lookback int) func(Config) (ReEntryRule, error) {
	return func(Config) (ReEntryRule, error) {
		return &nBarBreakoutReEntryRule{lookback: lookback}, nil
	}
}

// OnExit resets the window to empty rather than seeding it from
// exitBar directly: Strategy.onFlat calls ObserveFlatBar on this same
// exitBar immediately afterward, within the same OnBar invocation
// (every real Long->Flat transition triggers onExit then onFlat on
// the identical bar) — seeding here too would double-count the exit
// bar's own High.
func (r *nBarBreakoutReEntryRule) OnExit(num.Price, marketdata.Bar) {
	r.highs = nil
}

// ObserveFlatBar is the sole mutator of r.highs — ShouldEnter is now
// purely a read-only comparison against whatever window state
// ObserveFlatBar has already established (PR #362 review).
func (r *nBarBreakoutReEntryRule) ObserveFlatBar(bar marketdata.Bar) {
	r.highs = append(r.highs, bar.High)
	if len(r.highs) > r.lookback {
		r.highs = r.highs[len(r.highs)-r.lookback:]
	}
}

func (r *nBarBreakoutReEntryRule) ShouldEnter(ctx ReEntryContext) bool {
	if len(r.highs) == 0 {
		return false
	}
	highest := r.highs[0]
	for _, h := range r.highs[1:] {
		if h.Cmp(highest) > 0 {
			highest = h
		}
	}
	return ctx.Bar.Close.Cmp(highest) > 0
}

// aboveSMAReEntryRule re-enters on the first eligible flat bar with no
// further condition of its own at all (issue #349/#350 review): the
// SMA Long Hold playbook's own "AboveSMA" re-entry mode. It relies
// entirely on the central above-SMA regime gate Strategy.onFlat
// already enforces before any ReEntryRule is even consulted — by the
// time ShouldEnter runs, price is already known to be back above the
// SMA, so this rule's own meaning is simply "re-enter immediately once
// the bullish regime resumes, without waiting for a fresh cross or a
// reclaim/breakout threshold." Stateless: OnExit does nothing, since
// this rule tracks no state of its own relative to the exit at all.
type aboveSMAReEntryRule struct{}

func newAboveSMAReEntryRule(Config) (ReEntryRule, error) { return aboveSMAReEntryRule{}, nil }

func (aboveSMAReEntryRule) OnExit(num.Price, marketdata.Bar) {}

func (aboveSMAReEntryRule) ObserveFlatBar(marketdata.Bar) {}

func (aboveSMAReEntryRule) ShouldEnter(ReEntryContext) bool { return true }
