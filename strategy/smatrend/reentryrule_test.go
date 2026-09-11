package smatrend

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/num"
)

func TestReEntryRuleRegistry_KnowsAllSixNames(t *testing.T) {
	for _, name := range []string{"fresh-cross", "reclaim-exit-price", "breakout", "above-sma", "breakout-2", "breakout-3"} {
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
	rule2.OnExit(num.MustParsePrice("100"), exitBar)
	rule3.OnExit(num.MustParsePrice("100"), exitBar)

	// Two bars with a lower High (90, 80) — breakout-2's window has
	// evicted the seeded 100 by now (2-bar window: [90, 80]) while
	// breakout-3's has not yet (3-bar window: [100, 90, 80]).
	assert.False(t, rule2.ShouldEnter(ReEntryContext{Bar: mustBar(t, "89", "90", "88", "89")}))
	assert.False(t, rule3.ShouldEnter(ReEntryContext{Bar: mustBar(t, "89", "90", "88", "89")}))
	assert.False(t, rule2.ShouldEnter(ReEntryContext{Bar: mustBar(t, "79", "80", "78", "79")}))
	assert.False(t, rule3.ShouldEnter(ReEntryContext{Bar: mustBar(t, "79", "80", "78", "79")}))

	// A close of 95: breakout-2 now checks only against its evicted
	// window's own highest (90) and enters; breakout-3 still has 100
	// in its window and does not.
	assert.True(t, rule2.ShouldEnter(ReEntryContext{Bar: mustBar(t, "90", "96", "89", "95")}), "breakout-2's window no longer contains the seeded 100 high")
	assert.False(t, rule3.ShouldEnter(ReEntryContext{Bar: mustBar(t, "90", "96", "89", "95")}), "breakout-3's window still contains the seeded 100 high")
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
	rule.OnExit(num.MustParsePrice("100"), mustBar(t, "102", "100", "99", "100")) // window: [100]

	// window grows to [100, 90] — a close of 95 must not enter (95 <=
	// the still-present seeded high of 100).
	assert.False(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "91", "90", "88", "89")}))

	// window evicts 100, becoming [90, 80] — the seeded exit-bar high
	// is now gone from this rule's own 2-bar memory.
	assert.False(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "81", "80", "78", "79")}))

	// A close of 95 would not break out against the original exit
	// high (100) but does break out against the now-current 2-bar
	// window's own highest (90) — proving eviction, not merely
	// growth, actually took effect.
	assert.True(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "90", "94", "89", "95")}))
}

// TestNBarBreakoutReEntryRule_NeverBreaksOutAgainstItsOwnNewHigh
// mirrors TestBreakoutReEntryRule_EntersOnCloseAboveSinceExitHighFromPriorBarsOnly's
// own check-before-update assertion: a bar's own new High must never
// let its own Close trivially "break out" against a high it itself
// just set.
func TestNBarBreakoutReEntryRule_NeverBreaksOutAgainstItsOwnNewHigh(t *testing.T) {
	rule, err := newNBarBreakoutReEntryRule(2)(Config{})
	require.NoError(t, err)
	rule.OnExit(num.MustParsePrice("100"), mustBar(t, "102", "100", "99", "100")) // window: [100]

	// This bar's own new High (110) is not yet in the window when its
	// own Close (105) is evaluated against the window's existing
	// highest (100) — 105 > 100, so this enters. The window only
	// updates to include 110 *after* this decision.
	assert.True(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "100", "110", "99", "105")})) // window becomes [100, 110]

	// Now the window is [100, 110]; a close of 109 must not enter
	// (109 <= 110, the high this rule itself just observed).
	assert.False(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "105", "108", "104", "109")}))
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
	rule.OnExit(num.MustParsePrice("200"), mustBar(t, "202", "200", "199", "200"))
	assert.False(t, rule.ShouldEnter(ReEntryContext{Bar: mustBar(t, "150", "160", "149", "155")}))

	// Second episode, seeded far lower (50): if the first episode's
	// window (which still contains 200) leaked into this one, a close
	// of 51 would incorrectly fail to break out (51 <= 200). Entering
	// here confirms OnExit genuinely discarded the prior episode's
	// window rather than merely appending to it.
	rule.OnExit(num.MustParsePrice("50"), mustBar(t, "52", "50", "49", "50"))
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
