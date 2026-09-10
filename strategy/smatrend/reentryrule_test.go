package smatrend

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/num"
)

func TestReEntryRuleRegistry_KnowsAllThreeNames(t *testing.T) {
	for _, name := range []string{"fresh-cross", "reclaim-exit-price", "breakout"} {
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
