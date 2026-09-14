package smatrend

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

func TestReEntryRuleRegistry_KnowsAllTenNames(t *testing.T) {
	for _, name := range []string{
		"fresh-cross", "reclaim-exit-price", "breakout", "above-sma", "breakout-2", "breakout-3",
		"above-sma-slope-20", "above-sma-slope-50", "retrace-25", "retrace-50",
	} {
		_, ok := reEntryRuleRegistry[name]
		assert.True(t, ok, name)
	}
}

func TestFreshCrossReEntryRule_MirrorsCrossedAboveSMA(t *testing.T) {
	rule, err := newFreshCrossReEntryRule(Config{})
	require.NoError(t, err)

	rule.OnExit(num.MustParsePrice("100"), mustBar(t, "100", "101", "99", "100"))
	assert.False(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "100", "101", "99", "100"), CrossedAboveSMA: false}))
	assert.True(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "100", "101", "99", "100"), CrossedAboveSMA: true}))
}

func TestReclaimExitPriceReEntryRule_EntersOnceCloseReclaimsExitPrice(t *testing.T) {
	rule, err := newReclaimExitPriceReEntryRule(Config{})
	require.NoError(t, err)
	rule.OnExit(num.MustParsePrice("100"), mustBar(t, "102", "103", "99", "100"))

	assert.False(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "98", "99", "95", "99")}), "close (99) below the 100 exit price must not enter")
	assert.False(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "99", "101", "98", "100")}), "close exactly at the exit price must not enter (strictly above required)")
	assert.True(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "100", "103", "99", "101")}), "close (101) strictly above the 100 exit price must enter")
}

func TestBreakoutReEntryRule_EntersOnCloseAboveSinceExitHighFromPriorBarsOnly(t *testing.T) {
	rule, err := newBreakoutReEntryRule(Config{})
	require.NoError(t, err)
	rule.OnExit(num.MustParsePrice("100"), mustBar(t, "102", "105", "99", "100")) // sinceExitHigh seeded at 105

	// A close above the exit bar's own high (105) is required — not merely
	// above the exit price itself. This bar's own High (105) must not
	// itself exceed the seeded since-exit high, or it would ratchet
	// sinceExitHigh upward before the next assertion gets to observe it.
	assert.False(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "104", "105", "103", "104")}), "close (104) below the since-exit high (105) must not enter")

	// This bar's own new High (110) must not let its own Close trivially
	// "break out" against a high it itself just set: the check uses only
	// the high established by prior bars (105), so a close of 106 here
	// does break out, and sinceExitHigh only updates to 110 afterward.
	assert.True(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "105", "110", "104", "106")}), "close (106) above the prior since-exit high (105) must enter")

	// Now that sinceExitHigh has updated to 110, a close of 108 must not
	// enter (confirming the update actually took effect and this rule
	// tracks a real ratcheting high, not a one-shot exit-bar value).
	assert.False(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "106", "109", "105", "108")}))
}

// nBarBreakoutCheckThenObserve mirrors Strategy.onFlat's own real call
// order for a ReEntryRule with continuous state (PR #362 review):
// ShouldEnter decides first, using whatever window ObserveFlatBar
// already built from *prior* bars, then ObserveFlatBar updates the
// window with this bar's own High only afterward. Tests below call
// this instead of ShouldEnter directly so they exercise the exact
// sequence the real strategy uses, not the pre-#362 behavior where
// ShouldEnter itself both decided and mutated.
func nBarBreakoutCheckThenObserve(rule ReEntryRule, bar marketdata.Bar) bool {
	enter := rule.ShouldEnter(ReEntryContext{Bar: bar})
	rule.ObserveFlatBar(bar)
	return enter
}

// nBarBreakoutOnExit mirrors Strategy.onFlat's own real invocation for
// a fresh Long->Flat transition (PR #362 review): onExit and onFlat
// both run on the identical bar, and onFlat's own ObserveFlatBar call
// fires immediately after OnExit for that same bar — so exitBar's own
// High enters the window via ObserveFlatBar, not via OnExit itself
// (OnExit only resets the window; see its own doc comment).
func nBarBreakoutOnExit(rule ReEntryRule, exitPrice string, exitBar marketdata.Bar) {
	rule.OnExit(num.MustParsePrice(exitPrice), exitBar)
	rule.ObserveFlatBar(exitBar)
}

// TestNBarBreakoutReEntryRule_RegistryConstructsDistinctLookbacks
// proves "breakout-2" and "breakout-3" are genuinely independent
// constructions (issue #361), not the same shared instance/lookback
// under two names: registering both, feeding each the identical exit
// and identical bar sequence, and observing them diverge exactly
// where a 2-bar vs. 3-bar window should diverge.
func TestNBarBreakoutReEntryRule_RegistryConstructsDistinctLookbacks(t *testing.T) {
	rule2, err := reEntryRuleRegistry["breakout-2"](Config{})
	require.NoError(t, err)
	rule3, err := reEntryRuleRegistry["breakout-3"](Config{})
	require.NoError(t, err)

	exitBar := mustBar(t, "102", "100", "99", "100") // window seeded at High=100
	nBarBreakoutOnExit(rule2, "100", exitBar)
	nBarBreakoutOnExit(rule3, "100", exitBar)

	// Two bars with a lower High (90, 80) — breakout-2's window has
	// evicted the seeded 100 by now (2-bar window: [90, 80]) while
	// breakout-3's has not yet (3-bar window: [100, 90, 80]).
	assert.False(t, nBarBreakoutCheckThenObserve(rule2, mustBar(t, "89", "90", "88", "89")))
	assert.False(t, nBarBreakoutCheckThenObserve(rule3, mustBar(t, "89", "90", "88", "89")))
	assert.False(t, nBarBreakoutCheckThenObserve(rule2, mustBar(t, "79", "80", "78", "79")))
	assert.False(t, nBarBreakoutCheckThenObserve(rule3, mustBar(t, "79", "80", "78", "79")))

	// A close of 95: breakout-2 now checks only against its evicted
	// window's own highest (90) and enters; breakout-3 still has 100
	// in its window and does not.
	assert.True(t, nBarBreakoutCheckThenObserve(rule2, mustBar(t, "90", "96", "89", "95")), "breakout-2's window no longer contains the seeded 100 high")
	assert.False(t, nBarBreakoutCheckThenObserve(rule3, mustBar(t, "90", "96", "89", "95")), "breakout-3's window still contains the seeded 100 high")
}

// TestNBarBreakoutReEntryRule_WindowSlidesAndEvictsTheOldestBar proves
// the core behavior that distinguishes this rule from the existing,
// unbounded breakoutReEntryRule (issue #361): once lookback bars have
// accumulated since the exit, the oldest bar's High is evicted from
// the window, so a since-exit high that would still govern
// breakoutReEntryRule no longer governs this rule at all.
func TestNBarBreakoutReEntryRule_WindowSlidesAndEvictsTheOldestBar(t *testing.T) {
	rule, err := newNBarBreakoutReEntryRule(2)(Config{})
	require.NoError(t, err)
	nBarBreakoutOnExit(rule, "100", mustBar(t, "102", "100", "99", "100")) // window: [100]

	// window grows to [100, 90] — a close of 95 must not enter (95 <=
	// the still-present seeded high of 100).
	assert.False(t, nBarBreakoutCheckThenObserve(rule, mustBar(t, "91", "90", "88", "89")))

	// window evicts 100, becoming [90, 80] — the seeded exit-bar high
	// is now gone from this rule's own 2-bar memory.
	assert.False(t, nBarBreakoutCheckThenObserve(rule, mustBar(t, "81", "80", "78", "79")))

	// A close of 95 would not break out against the original exit
	// high (100) but does break out against the now-current 2-bar
	// window's own highest (90) — proving eviction, not merely
	// growth, actually took effect.
	assert.True(t, nBarBreakoutCheckThenObserve(rule, mustBar(t, "90", "94", "89", "95")))
}

// TestNBarBreakoutReEntryRule_NeverBreaksOutAgainstItsOwnNewHigh
// mirrors TestBreakoutReEntryRule_EntersOnCloseAboveSinceExitHighFromPriorBarsOnly's
// own check-before-update assertion: a bar's own new High must never
// let its own Close trivially "break out" against a high it itself
// just set.
func TestNBarBreakoutReEntryRule_NeverBreaksOutAgainstItsOwnNewHigh(t *testing.T) {
	rule, err := newNBarBreakoutReEntryRule(2)(Config{})
	require.NoError(t, err)
	nBarBreakoutOnExit(rule, "100", mustBar(t, "102", "100", "99", "100")) // window: [100]

	// This bar's own new High (110) is not yet in the window when its
	// own Close (105) is evaluated against the window's existing
	// highest (100) — 105 > 100, so this enters. The window only
	// updates to include 110 *after* this decision (via
	// ObserveFlatBar, called after ShouldEnter here too).
	assert.True(t, nBarBreakoutCheckThenObserve(rule, mustBar(t, "100", "110", "99", "105"))) // window becomes [100, 110]

	// Now the window is [100, 110]; a close of 109 must not enter
	// (109 <= 110, the high this rule itself just observed).
	assert.False(t, nBarBreakoutCheckThenObserve(rule, mustBar(t, "105", "108", "104", "109")))
}

// TestNBarBreakoutReEntryRule_OnExitResetsStateBetweenEpisodes proves
// a second OnExit call (a later episode) discards the previous
// episode's own window entirely rather than leaking stale highs
// across episodes.
func TestNBarBreakoutReEntryRule_OnExitResetsStateBetweenEpisodes(t *testing.T) {
	rule, err := newNBarBreakoutReEntryRule(2)(Config{})
	require.NoError(t, err)

	// First episode seeded high at 200; a close of 155 does not break
	// out (155 <= 200).
	nBarBreakoutOnExit(rule, "200", mustBar(t, "202", "200", "199", "200"))
	assert.False(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "150", "160", "149", "155")}))

	// Second episode, seeded far lower (50): if the first episode's
	// window (which still contains 200) leaked into this one, a close
	// of 51 would incorrectly fail to break out (51 <= 200). Entering
	// here confirms OnExit genuinely discarded the prior episode's
	// window rather than merely appending to it.
	nBarBreakoutOnExit(rule, "50", mustBar(t, "52", "50", "49", "50"))
	assert.True(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "49", "51", "48", "51")}))
}

// TestAboveSMAReEntryRule_AlwaysEntersRelyingEntirelyOnTheCentralGate
// proves aboveSMAReEntryRule has no condition of its own at all
// (issue #349/#350 review): unlike every other ReEntryRule, it must
// return true unconditionally, since the central above-SMA regime
// gate in Strategy.onFlat is the only thing standing between "flat"
// and "re-entered" for this rule.
func TestAboveSMAReEntryRule_AlwaysEntersRelyingEntirelyOnTheCentralGate(t *testing.T) {
	rule, err := newAboveSMAReEntryRule(Config{})
	require.NoError(t, err)
	rule.OnExit(num.MustParsePrice("100"), mustBar(t, "102", "103", "99", "100"))

	assert.True(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "50", "51", "49", "50"), CrossedAboveSMA: false}))
	assert.True(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "200", "201", "199", "200"), CrossedAboveSMA: true}))
}

// smaSlopeCheckThenObserve mirrors Strategy.OnBar's own real call
// order for an SMAObserver-implementing ReEntryRule: ObserveSMA is
// called unconditionally for the current bar's own smaValue *before*
// ShouldEnter ever runs (Strategy.OnBar calls it before the Flat/Long
// dispatch) — so unlike nBarBreakoutCheckThenObserve's own
// check-then-observe order, this rule's window already includes the
// current bar's own value by the time ShouldEnter is consulted.
func smaSlopeCheckThenObserve(rule ReEntryRule, ctx ReEntryContext, smaValue float64) bool {
	rule.(SMAObserver).ObserveSMA(smaValue)
	return rule.ShouldEnter(ctx)
}

// TestSMASlopeReEntryRule_RegistryConstructsDistinctLookbacks proves
// "above-sma-slope-20" and "above-sma-slope-50" are genuinely
// independent constructions (issue #365), mirroring
// TestNBarBreakoutReEntryRule_RegistryConstructsDistinctLookbacks'
// own proof for breakout-2/breakout-3.
func TestSMASlopeReEntryRule_RegistryConstructsDistinctLookbacks(t *testing.T) {
	rule20, err := reEntryRuleRegistry["above-sma-slope-20"](Config{})
	require.NoError(t, err)
	rule50, err := reEntryRuleRegistry["above-sma-slope-50"](Config{})
	require.NoError(t, err)

	// Feed 20 rising values (100, 101, ..., 119) then one more at 118
	// (a dip): above-sma-slope-20's own 21-value window now spans
	// [101..118 plus the new 118] — 20 bars ago from the new sample is
	// index len-21, i.e. the value 100 fed first has already been
	// evicted, so its own comparison is against 101 (still a rise, so
	// still true). above-sma-slope-50 has nowhere near 51 samples yet
	// and must report false regardless.
	var enter20, enter50 bool
	for i := 0; i < 20; i++ {
		v := 100.0 + float64(i)
		enter20 = smaSlopeCheckThenObserve(rule20, ReEntryContext{}, v)
		enter50 = smaSlopeCheckThenObserve(rule50, ReEntryContext{}, v)
	}
	assert.False(t, enter50, "above-sma-slope-50 needs 51 samples; only 20 fed")
	_ = enter20 // not yet meaningful with fewer than 21 samples; the next call is.

	enter20 = smaSlopeCheckThenObserve(rule20, ReEntryContext{}, 118.0)
	assert.True(t, enter20, "the 21st sample completes above-sma-slope-20's own window: latest (118) > value from 20 bars ago (101)")
}

// TestSMASlopeReEntryRule_RequiresFullLookbackBeforeEntering proves
// ShouldEnter reports false until the window has accumulated
// lookback+1 samples, rather than comparing against a partial or
// zero-valued history.
func TestSMASlopeReEntryRule_RequiresFullLookbackBeforeEntering(t *testing.T) {
	rule, err := newSMASlopeReEntryRule(3)(Config{})
	require.NoError(t, err)

	// Only 3 samples fed; the window needs 4 (lookback+1) before it
	// can compare "today" against "3 bars ago" at all.
	assert.False(t, smaSlopeCheckThenObserve(rule, ReEntryContext{}, 100.0))
	assert.False(t, smaSlopeCheckThenObserve(rule, ReEntryContext{}, 101.0))
	assert.False(t, smaSlopeCheckThenObserve(rule, ReEntryContext{}, 102.0))
}

// TestSMASlopeReEntryRule_RisingAndFallingSlope proves the rule
// reports true only while the SMA value from lookback bars ago is
// genuinely exceeded by today's, in both directions.
func TestSMASlopeReEntryRule_RisingAndFallingSlope(t *testing.T) {
	rule, err := newSMASlopeReEntryRule(2)(Config{})
	require.NoError(t, err)

	smaSlopeCheckThenObserve(rule, ReEntryContext{}, 100.0)
	smaSlopeCheckThenObserve(rule, ReEntryContext{}, 100.0)
	// Window now [100, 100]; this 3rd sample completes a 3-value
	// window [100, 100, 105] — compare newest (105) against oldest
	// (100): rising, must enter.
	assert.True(t, smaSlopeCheckThenObserve(rule, ReEntryContext{}, 105.0))

	// Window slides to [100, 105, 95] (the 100 fed above evicted) —
	// compare newest (95) against oldest of this window (100):
	// falling, must not enter.
	assert.False(t, smaSlopeCheckThenObserve(rule, ReEntryContext{}, 95.0))

	// Window slides to [105, 95, 96] — compare newest (96) against
	// oldest (105): still falling.
	assert.False(t, smaSlopeCheckThenObserve(rule, ReEntryContext{}, 96.0))
}

// percentRetraceOnExit mirrors Strategy.onFlat's own real invocation
// for a fresh Long->Flat transition: OnExit and ObserveFlatBar both
// run on the identical exit bar (see OnExit's own doc comment for
// why OnExit does not seed postExitLow itself).
func percentRetraceOnExit(rule ReEntryRule, exitPrice string, exitBar marketdata.Bar) {
	rule.OnExit(num.MustParsePrice(exitPrice), exitBar)
	rule.ObserveFlatBar(exitBar)
}

// percentRetraceCheckThenObserve mirrors Strategy.onFlat's own real
// call order (check before update).
func percentRetraceCheckThenObserve(rule ReEntryRule, bar marketdata.Bar) bool {
	enter := rule.ShouldEnter(ReEntryContext{Bar: bar})
	rule.ObserveFlatBar(bar)
	return enter
}

// TestPercentRetraceReEntryRule_RegistryConstructsDistinctThresholds
// proves "retrace-25" and "retrace-50" are genuinely independent
// constructions with different fractions.
func TestPercentRetraceReEntryRule_RegistryConstructsDistinctThresholds(t *testing.T) {
	rule25, err := reEntryRuleRegistry["retrace-25"](Config{})
	require.NoError(t, err)
	rule50, err := reEntryRuleRegistry["retrace-50"](Config{})
	require.NoError(t, err)

	// exit at 100, post-exit low at 80: selloff = 20.
	// retrace-25 recovery level = 80 + 0.25*20 = 85.
	// retrace-50 recovery level = 80 + 0.50*20 = 90.
	exitBar := mustBar(t, "100", "100", "80", "90")
	percentRetraceOnExit(rule25, "100", exitBar)
	percentRetraceOnExit(rule50, "100", exitBar)

	// Close of 87: above retrace-25's own 85 level, below retrace-50's
	// own 90 level.
	assert.True(t, percentRetraceCheckThenObserve(rule25, mustBar(t, "86", "88", "85", "87")))
	assert.False(t, percentRetraceCheckThenObserve(rule50, mustBar(t, "86", "88", "85", "87")))
}

// TestPercentRetraceReEntryRule_NoDeclineEntersUnconditionally proves
// the degenerate case: if postExitLow never actually fell below
// exitPrice, there is nothing to "recover" from, so ShouldEnter enters
// unconditionally once flat and above the SMA (the central gate,
// simulated here by simply calling ShouldEnter).
func TestPercentRetraceReEntryRule_NoDeclineEntersUnconditionally(t *testing.T) {
	rule, err := newPercentRetraceReEntryRule("0.25")(Config{})
	require.NoError(t, err)

	// exit at 100; the exit bar's own Low (101) never dips below the
	// exit price at all.
	percentRetraceOnExit(rule, "100", mustBar(t, "102", "103", "101", "102"))
	assert.True(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "101", "104", "100.5", "103")}))
}

// TestPercentRetraceReEntryRule_ShouldEnterBeforeFirstObserveFlatBarIsFalse
// proves ShouldEnter is false, not a panic or a spurious true, on the
// exit bar itself — consulted (if the exit bar's own Close is already
// back above the SMA) before ObserveFlatBar has run even once.
func TestPercentRetraceReEntryRule_ShouldEnterBeforeFirstObserveFlatBarIsFalse(t *testing.T) {
	rule, err := newPercentRetraceReEntryRule("0.25")(Config{})
	require.NoError(t, err)
	rule.OnExit(num.MustParsePrice("100"), mustBar(t, "100", "101", "80", "95"))

	assert.False(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "95", "96", "94", "95")}))
}

// TestPercentRetraceReEntryRule_ResetsFromANewLowerLow proves the
// recovery threshold genuinely resets from a new, lower low observed
// while flat (issue #365's own explicit requirement), rather than
// staying anchored to the original post-exit low.
func TestPercentRetraceReEntryRule_ResetsFromANewLowerLow(t *testing.T) {
	rule, err := newPercentRetraceReEntryRule("0.50")(Config{})
	require.NoError(t, err)

	// exit at 100, first observed low (via the exit bar itself) at 80:
	// selloff 20, recovery level = 80 + 0.5*20 = 90.
	percentRetraceOnExit(rule, "100", mustBar(t, "100", "100", "80", "90"))
	assert.False(t, percentRetraceCheckThenObserve(rule, mustBar(t, "85", "86", "84", "85")), "85 is below the 90 recovery level")

	// A new, lower low (60) resets the threshold: selloff becomes 40,
	// recovery level = 60 + 0.5*40 = 80. A close of 85 now qualifies.
	assert.False(t, percentRetraceCheckThenObserve(rule, mustBar(t, "84", "85", "60", "70")), "this bar's own new low (60) must not let its own close (70) satisfy its own just-reset threshold")
	assert.True(t, percentRetraceCheckThenObserve(rule, mustBar(t, "72", "86", "65", "85")), "85 now exceeds the reset 80 recovery level")
}

// TestPercentRetraceReEntryRule_OnExitResetsStateBetweenEpisodes
// proves a second OnExit call discards the previous episode's own
// exitPrice/postExitLow entirely rather than leaking stale state.
func TestPercentRetraceReEntryRule_OnExitResetsStateBetweenEpisodes(t *testing.T) {
	rule, err := newPercentRetraceReEntryRule("0.50")(Config{})
	require.NoError(t, err)

	percentRetraceOnExit(rule, "200", mustBar(t, "200", "200", "150", "180")) // selloff 50, recovery level 175
	assert.False(t, percentRetraceCheckThenObserve(rule, mustBar(t, "160", "170", "159", "170")))

	// Second episode, exit at 10, low at 8: selloff 2, recovery level
	// 9. If the first episode's state leaked, a close of 9 would
	// incorrectly compare against the stale 175 level.
	percentRetraceOnExit(rule, "10", mustBar(t, "10", "10", "8", "9"))
	assert.True(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "8.5", "9.2", "8.4", "9")}))
}
