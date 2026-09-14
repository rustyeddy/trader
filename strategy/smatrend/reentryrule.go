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

// SMAObserver is an optional ReEntryRule capability (issue #365): a
// rule whose own trigger condition depends on the SMA's trajectory
// itself (for example whether it is rising), not merely on price
// relative to it, needs the SMA value on *every* ready bar — including
// while Long, since the SMA keeps moving regardless of position side,
// the identical reason Strategy's own crossState is maintained
// unconditionally (see its own doc comment). ObserveFlatBar cannot
// supply this: it only ever fires while flat, but an SMA slope
// computed only from flat-bar samples would silently skip every bar
// spent Long, corrupting the very trajectory a slope rule needs to
// measure continuously. Strategy.OnBar calls ObserveSMA on every bar
// the SMA is ready, immediately once smaValue itself is computed —
// before the Flat/Long dispatch for that same bar — so a rule
// implementing this always has today's own current value available
// by the time ShouldEnter might run later in that identical call. A
// rule with no such dependency implements nothing extra at all
// (SMAObserver is optional, mirroring ExitRule's own
// InitialStopProvider/InitialStopSeeder capability pattern).
type SMAObserver interface {
	ObserveSMA(smaValue float64)
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
	// above-sma-slope-20/-50 and retrace-25/-50 are issue #365's own
	// two re-entry-confirmation hypotheses following #361's own
	// inconclusive N-bar breakout finding — see
	// smaSlopeReEntryRule/percentRetraceReEntryRule's own doc
	// comments. Fixed names rather than a configurable lookback/
	// threshold, matching breakout-2/breakout-3's own precedent
	// (issue #365's own guardrail: no broad sweep in this issue).
	"above-sma-slope-20": newSMASlopeReEntryRule(20),
	"above-sma-slope-50": newSMASlopeReEntryRule(50),
	"retrace-25":         newPercentRetraceReEntryRule("0.25"),
	"retrace-50":         newPercentRetraceReEntryRule("0.50"),
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
// new High in its own check, so a bar can never trivially "break out"
// against a high it itself just set.
//
// ObserveFlatBar is the sole mutator of sinceExitHigh — ShouldEnter is
// a pure read-only comparison against whatever value ObserveFlatBar
// has already established (issue #363, mirroring PR #362's identical
// fix for nBarBreakoutReEntryRule below). PR #362 review found and
// deliberately deferred this exact bug rather than fixing it as a
// side effect of issue #361's own, unrelated scope: sinceExitHigh was
// previously updated only inside ShouldEnter, which — like every
// ReEntryRule's own ShouldEnter — is never consulted while
// Strategy.onFlat's central SMA gate is false, so a below-SMA
// stretch's own bars never updated sinceExitHigh, and the "since
// exit" high this rule tracks could understate the real since-exit
// high whenever a meaningful new high occurred while price was below
// the SMA. ObserveFlatBar now runs for *every* flat bar, closing that
// gap the same way it already does for nBarBreakoutReEntryRule.
type breakoutReEntryRule struct {
	sinceExitHigh num.Price
}

func newBreakoutReEntryRule(Config) (ReEntryRule, error) {
	return &breakoutReEntryRule{}, nil
}

func (r *breakoutReEntryRule) OnExit(_ num.Price, exitBar marketdata.Bar) {
	r.sinceExitHigh = exitBar.High
}

// ObserveFlatBar is the sole mutator of r.sinceExitHigh — ShouldEnter
// is now purely a read-only comparison against whatever value
// ObserveFlatBar has already established (issue #363 fix).
func (r *breakoutReEntryRule) ObserveFlatBar(bar marketdata.Bar) {
	if bar.High.Cmp(r.sinceExitHigh) > 0 {
		r.sinceExitHigh = bar.High
	}
}

func (r *breakoutReEntryRule) ShouldEnter(ctx ReEntryContext) bool {
	return ctx.Bar.Close.Cmp(r.sinceExitHigh) > 0
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

// smaSlopeReEntryRule re-enters once the long SMA itself is rising —
// issue #365's hypothesis A, following #361's own inconclusive N-bar
// breakout finding: rather than confirming a short-term price
// breakout, require evidence that the long-term *regime* has turned
// upward before re-entering.
//
//	slope = SMA(today) - SMA(lookback bars ago)
//	require slope > 0
//
// (the normalized form, slope/SMA(lookback bars ago) > 0, is
// equivalent in sign — this rule compares the two raw values
// directly, since only the sign matters and dividing adds nothing but
// float64 rounding risk near zero.)
//
// "Close > SMA" is not checked here at all: Strategy.onFlat's own
// central above-SMA regime gate already guarantees it before
// ShouldEnter is ever consulted (the same reliance
// aboveSMAReEntryRule's own doc comment describes) — this rule's own
// contribution is purely the slope condition.
//
// Implements SMAObserver (not ObserveFlatBar) because the SMA's own
// trajectory is continuous regardless of position side: an N-bar
// window fed only from flat bars would silently skip every bar spent
// Long, understating how long the SMA has actually been moving in one
// direction. lookback is one of two fixed values registered under
// explicit names ("above-sma-slope-20"/"above-sma-slope-50") rather
// than a configurable Config field (issue #365's own guardrail against
// a broader lookback sweep).
type smaSlopeReEntryRule struct {
	lookback int
	history  []float64 // most recent up to lookback+1 SMA values, oldest first
}

func newSMASlopeReEntryRule(lookback int) func(Config) (ReEntryRule, error) {
	return func(Config) (ReEntryRule, error) {
		return &smaSlopeReEntryRule{lookback: lookback}, nil
	}
}

func (r *smaSlopeReEntryRule) OnExit(num.Price, marketdata.Bar) {}

func (r *smaSlopeReEntryRule) ObserveFlatBar(marketdata.Bar) {}

// ObserveSMA implements SMAObserver: maintains a fixed-size sliding
// window of the most recent lookback+1 SMA values, oldest first, so
// ShouldEnter can always compare today's own value (the newest
// entry) against the value from exactly lookback bars ago (the
// oldest entry) once the window is full.
func (r *smaSlopeReEntryRule) ObserveSMA(smaValue float64) {
	r.history = append(r.history, smaValue)
	if len(r.history) > r.lookback+1 {
		r.history = r.history[len(r.history)-(r.lookback+1):]
	}
}

func (r *smaSlopeReEntryRule) ShouldEnter(ReEntryContext) bool {
	if len(r.history) < r.lookback+1 {
		return false // not enough history yet to compute the slope
	}
	latest := r.history[len(r.history)-1]
	past := r.history[0]
	return latest > past
}

// percentRetraceReEntryRule re-enters once price has recovered a
// fixed fraction of its own decline from the exit price down to the
// lowest Low observed since that exit — issue #365's hypothesis B,
// the second of two re-entry-confirmation candidates following #361's
// own inconclusive N-bar breakout finding.
//
//	selloff           = exitPrice - postExitLow
//	recoveryLevel     = postExitLow + fraction * selloff
//	require Close >= recoveryLevel
//
// fraction is one of two fixed thresholds registered under explicit
// names ("retrace-25"/"retrace-50") rather than a configurable Config
// field (issue #365's own guardrail against testing every value from
// 10-50%).
//
// postExitLow must keep updating while flat, resetting the recovery
// threshold from any new, lower low (issue #365's own explicit
// requirement). Unlike nBarBreakoutReEntryRule's own check-before-
// update ordering, ShouldEnter here does *not* wait for ObserveFlatBar
// to persist a new low before considering it (PR #372 review): a bar
// that itself sets a new post-exit low is, by the hypothesis's own
// definition ("recovered a fraction of the decline from the exit price
// down to the lowest Low observed since exit"), already part of that
// history — excluding the current bar's own Low would silently ignore
// the very extreme the recovery fraction is measured from. ShouldEnter
// therefore computes an *effective* low — min(the low prior bars
// already established, this bar's own Low) — without mutating any
// state itself; ObserveFlatBar remains the sole place r.postExitLow is
// actually persisted, strictly after ShouldEnter's own decision for
// this bar, so the *next* bar's decision reads back exactly what this
// one computed. This is a real behavior difference from
// nBarBreakoutReEntryRule's own "never use a bar's own new extreme
// against itself" rule (deliberately correct there, since a breakout
// threshold is a fixed prior level a bar's own High must exceed — a
// retrace threshold is instead itself a *function* of the low, so
// using the current bar's own Low is the hypothesis definition, not a
// lookahead or self-referential shortcut).
//
// If no decline below the exit price has actually occurred yet
// (the effective low >= exitPrice — price only ever rose since the
// exit), there is nothing to "recover" from: ShouldEnter enters
// unconditionally in that case, the same as reclaim-exit-price would
// once back above the SMA. This also keeps every computation exact:
// recoveryLevel is built additively (low + fraction*selloff), never
// via a selloff/recovery *fraction* division that could divide by a
// near-zero selloff.
//
// Like every ReEntryRule, this still only runs once Strategy.onFlat's
// own central above-SMA regime gate already passed (issue #365's own
// explicit requirement to keep honoring that gate) — this rule adds
// no bypass of its own.
type percentRetraceReEntryRule struct {
	fraction    num.Rate
	exitPrice   num.Price
	postExitLow *num.Price
}

func newPercentRetraceReEntryRule(fraction string) func(Config) (ReEntryRule, error) {
	f := num.MustParseRate(fraction)
	return func(Config) (ReEntryRule, error) {
		return &percentRetraceReEntryRule{fraction: f}, nil
	}
}

// OnExit records exitPrice and resets postExitLow to nil rather than
// seeding it from exitBar directly: Strategy.onFlat calls
// ObserveFlatBar on this same exitBar immediately afterward, within
// the same OnBar invocation (every real Long->Flat transition
// triggers onExit then onFlat on the identical bar) — seeding here
// too would double-count the exit bar's own Low, the identical
// reasoning nBarBreakoutReEntryRule's own OnExit already documents.
// (ShouldEnter no longer depends on postExitLow being non-nil to
// produce a correct answer even on the exit bar itself — see its own
// doc comment — but OnExit still resets it here so ObserveFlatBar's
// own persistence starts fresh each episode.)
func (r *percentRetraceReEntryRule) OnExit(exitPrice num.Price, _ marketdata.Bar) {
	r.exitPrice = exitPrice
	r.postExitLow = nil
}

// ObserveFlatBar is the sole mutator of r.postExitLow, persisting
// whatever effective low ShouldEnter already computed (including this
// bar's own Low) for the *next* bar to read back.
func (r *percentRetraceReEntryRule) ObserveFlatBar(bar marketdata.Bar) {
	if r.postExitLow == nil || bar.Low.Cmp(*r.postExitLow) < 0 {
		low := bar.Low
		r.postExitLow = &low
	}
}

func (r *percentRetraceReEntryRule) ShouldEnter(ctx ReEntryContext) bool {
	low := ctx.Bar.Low
	if r.postExitLow != nil && r.postExitLow.Cmp(low) < 0 {
		low = *r.postExitLow
	}
	if low.Cmp(r.exitPrice) >= 0 {
		// No decline below the exit price has occurred since exiting:
		// nothing to recover from, so any bar back above the SMA
		// already qualifies.
		return true
	}

	selloff, err := r.exitPrice.Sub(low)
	if err != nil {
		return false
	}
	recovered, err := selloff.MulRate(r.fraction)
	if err != nil {
		return false
	}
	recoveryLevel, err := low.Add(recovered)
	if err != nil {
		return false
	}
	return ctx.Bar.Close.Cmp(recoveryLevel) >= 0
}
