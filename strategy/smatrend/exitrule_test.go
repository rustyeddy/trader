package smatrend

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

func mustBar(t *testing.T, open, high, low, close string) marketdata.Bar {
	t.Helper()
	return marketdata.Bar{
		Time:  testStart,
		Open:  num.MustParsePrice(open),
		High:  num.MustParsePrice(high),
		Low:   num.MustParsePrice(low),
		Close: num.MustParsePrice(close),
	}
}

func TestExitRuleRegistry_KnowsAllThreeNames(t *testing.T) {
	for _, name := range []string{"trailing-stop", "sma-cross", "probation-trend"} {
		_, ok := exitRuleRegistry[name]
		assert.True(t, ok, name)
	}
}

func TestTrailingStopExitRule_RatchetsMonotonicallyUpward(t *testing.T) {
	cfg := Config{TrailingStopPercent: num.MustParseRate("0.10")}
	rule, err := newTrailingStopExitRule(cfg)
	require.NoError(t, err)

	rule.OnEntry(mustBar(t, "100", "110", "99", "105"), num.MustParsePrice("100"), nil)

	decision, err := rule.OnLongBar(mustBar(t, "103", "110", "102", "105"), 90)
	require.NoError(t, err)
	require.NotNil(t, decision.NewStop)
	assert.Equal(t, "99", decision.NewStop.String(), "90%% of the 110 high-water mark")
	assert.False(t, decision.ExitNow)

	// A lower High must not move the stop at all.
	decision, err = rule.OnLongBar(mustBar(t, "106", "108", "100", "101"), 90)
	require.NoError(t, err)
	assert.Nil(t, decision.NewStop, "a lower bar high must never move the stop")

	// A higher High ratchets it up.
	decision, err = rule.OnLongBar(mustBar(t, "106", "120", "105", "118"), 90)
	require.NoError(t, err)
	require.NotNil(t, decision.NewStop)
	assert.Equal(t, "108", decision.NewStop.String())
}

func TestSMACrossExitRule_ExitsOnCloseAtOrBelowSMA(t *testing.T) {
	rule, err := newSMACrossExitRule(Config{})
	require.NoError(t, err)
	rule.OnEntry(mustBar(t, "100", "105", "99", "102"), num.MustParsePrice("100"), nil)

	decision, err := rule.OnLongBar(mustBar(t, "103", "106", "102", "105"), 100)
	require.NoError(t, err)
	assert.False(t, decision.ExitNow, "close (105) above SMA (100) must not exit")
	assert.Nil(t, decision.NewStop, "smaCrossExitRule never manages a resting stop")

	decision, err = rule.OnLongBar(mustBar(t, "104", "105", "98", "99"), 100)
	require.NoError(t, err)
	assert.True(t, decision.ExitNow, "close (99) at or below SMA (100) must exit")
}

func newProbationTrendRuleForTest(t *testing.T) *probationTrendExitRule {
	t.Helper()
	rule, err := newProbationTrendExitRule(Config{
		InitialStopBelowSMA: num.MustParseRate("0.01"),
		TrailActivationGain: num.MustParseRate("0.05"),
		TrailingStopPercent: num.MustParseRate("0.10"),
	})
	require.NoError(t, err)
	return rule.(*probationTrendExitRule)
}

// TestProbationTrendExitRule_ProbationStopRatchetsFromSMAOnly proves
// the PROBATION-phase protective stop tracks 99% of the *current*
// SMA (never the high-water mark), ratcheting upward only — a lower
// SMA on a later bar must never pull the stop back down.
func TestProbationTrendExitRule_ProbationStopRatchetsFromSMAOnly(t *testing.T) {
	rule := newProbationTrendRuleForTest(t)
	rule.OnEntry(mustBar(t, "100", "101", "99", "100"), num.MustParsePrice("100"), nil)
	assert.Equal(t, PhaseProbation, rule.Phase())

	// sma=99, close (101) above it and well under the 105 activation
	// threshold: 99 * 0.99 = 98.01.
	decision, err := rule.OnLongBar(mustBar(t, "100", "102", "99", "101"), 99)
	require.NoError(t, err)
	assert.False(t, decision.ExitNow)
	require.NotNil(t, decision.NewStop)
	assert.Equal(t, "98.01", decision.NewStop.String())

	// sma rises to 100: 100 * 0.99 = 99, above 98.01, ratchets up.
	decision, err = rule.OnLongBar(mustBar(t, "101", "103", "100", "102"), 100)
	require.NoError(t, err)
	require.NotNil(t, decision.NewStop)
	assert.Equal(t, "99", decision.NewStop.String())

	// sma dips back to 99: 99 * 0.99 = 98.01, below the current 99 —
	// must not move the stop down.
	decision, err = rule.OnLongBar(mustBar(t, "102", "104", "101", "103"), 99)
	require.NoError(t, err)
	assert.Nil(t, decision.NewStop, "a lower SMA must never pull the probation stop back down")
	assert.Equal(t, PhaseProbation, rule.Phase(), "still short of the 105 activation threshold")
}

// TestProbationTrendExitRule_SMACrossExitsDuringProbation proves the
// independent SMA-cross override fires during PROBATION exactly like
// smaCrossExitRule, regardless of where the protective stop itself
// currently rests.
func TestProbationTrendExitRule_SMACrossExitsDuringProbation(t *testing.T) {
	rule := newProbationTrendRuleForTest(t)
	rule.OnEntry(mustBar(t, "100", "101", "99", "100"), num.MustParsePrice("100"), nil)

	decision, err := rule.OnLongBar(mustBar(t, "99", "100", "95", "97"), 100)
	require.NoError(t, err)
	assert.True(t, decision.ExitNow, "close (97) at or below SMA (100) must exit outright during probation")
	assert.Nil(t, decision.NewStop)
}

// TestProbationTrendExitRule_ActivatesOnCloseGainThresholdWithoutRetroactivelyTighteningTheBarItself
// proves both required timing rules from issue #349's review at once:
// the bar that first satisfies the activation condition still returns
// a decision computed under the *old* (probation) rule, never the
// trending rule's own high-water-mark math, and the phase transition
// itself only takes effect starting the *next* OnLongBar call.
func TestProbationTrendExitRule_ActivatesOnCloseGainThresholdWithoutRetroactivelyTighteningTheBarItself(t *testing.T) {
	rule := newProbationTrendRuleForTest(t)
	rule.OnEntry(mustBar(t, "100", "100", "99", "100"), num.MustParsePrice("100"), nil)

	// A large spike High while still in probation, never exceeded
	// again — this is the "since entry, not since activation" case
	// issue #349's review specifically asked to be proven.
	_, err := rule.OnLongBar(mustBar(t, "100", "130", "99", "101"), 95)
	require.NoError(t, err)
	assert.Equal(t, PhaseProbation, rule.Phase())

	// close (105) reaches the 100*(1+0.05)=105 activation threshold
	// exactly, and is above this bar's own sma (104), so no SMA-cross
	// exit either. The decision this bar must still be probation-
	// shaped (104*0.99=102.96), not the wildly different trending
	// value (high-water-mark 130*0.90=117) activation would imply if
	// it applied retroactively to this same bar.
	decision, err := rule.OnLongBar(mustBar(t, "103", "106", "102", "105"), 104)
	require.NoError(t, err)
	assert.False(t, decision.ExitNow)
	require.NotNil(t, decision.NewStop)
	assert.Equal(t, "102.96", decision.NewStop.String(), "the activation bar itself must still use probation math")
	assert.Equal(t, PhaseTrending, rule.Phase(), "the transition itself takes effect immediately after this call")

	// The very next bar: now genuinely governed by the trending rule.
	// Deep below the SMA (immune) and a lower High (105, still under
	// the 130 set two bars ago during probation) — the returned stop
	// must reflect that still-standing 130 high-water mark, proving it
	// survived the probation->trending boundary intact:
	// 130 * 0.90 = 117.
	decision, err = rule.OnLongBar(mustBar(t, "60", "105", "45", "50"), 200)
	require.NoError(t, err)
	assert.False(t, decision.ExitNow, "trending phase is immune to a close under the SMA")
	require.NotNil(t, decision.NewStop)
	assert.Equal(t, "117", decision.NewStop.String(), "must use the high-water mark set back during probation, not just since activation")
}

// TestProbationTrendExitRule_HandoffNeverLoosensProtection proves the
// PROBATION->TRENDING handoff policy issue #349's review settled on
// ("never-loosen"): TRENDING's own raw high-water-mark formula can
// imply a lower stop than PROBATION already protected (a wide
// trailing percentage against a high-water mark that hasn't run up
// much since activation), and the handoff must never actually loosen
// protection to that lower value — it must simply emit nothing until
// TRENDING's own ratchet genuinely earns a level above what PROBATION
// already had in place.
func TestProbationTrendExitRule_HandoffNeverLoosensProtection(t *testing.T) {
	rule := newProbationTrendRuleForTest(t)
	rule.OnEntry(mustBar(t, "100", "100", "99", "100"), num.MustParsePrice("100"), nil)

	// Activates this bar: sma=104, close=105 >= 100*1.05=105
	// threshold. Probation stop = 104*0.99 = 102.96.
	decision, err := rule.OnLongBar(mustBar(t, "103", "105", "102", "105"), 104)
	require.NoError(t, err)
	require.NotNil(t, decision.NewStop)
	assert.Equal(t, "102.96", decision.NewStop.String())
	assert.Equal(t, PhaseTrending, rule.Phase())

	// Now genuinely trending. A modest new high-water mark (110) would
	// imply 110*0.90=99 under TRENDING's own raw formula — below the
	// 102.96 already protected. Nothing must be emitted.
	decision, err = rule.OnLongBar(mustBar(t, "80", "110", "50", "60"), 200)
	require.NoError(t, err)
	assert.Nil(t, decision.NewStop, "must never loosen from 102.96 down to 99")

	// A high-water mark large enough (130) that 130*0.90=117 finally
	// exceeds 102.96: normal ratcheting resumes once it's genuinely
	// earned.
	decision, err = rule.OnLongBar(mustBar(t, "80", "130", "50", "60"), 200)
	require.NoError(t, err)
	require.NotNil(t, decision.NewStop)
	assert.Equal(t, "117", decision.NewStop.String())
}

// TestProbationTrendExitRule_TrendingStopRatchetsMonotonicallyUpward
// proves the TRENDING-phase stop behaves exactly like
// trailingStopExitRule's own ratchet once activated.
func TestProbationTrendExitRule_TrendingStopRatchetsMonotonicallyUpward(t *testing.T) {
	rule := newProbationTrendRuleForTest(t)
	rule.OnEntry(mustBar(t, "100", "100", "99", "100"), num.MustParsePrice("100"), nil)
	// Force activation immediately.
	_, err := rule.OnLongBar(mustBar(t, "100", "100", "99", "105"), 100)
	require.NoError(t, err)
	require.Equal(t, PhaseTrending, rule.Phase())

	decision, err := rule.OnLongBar(mustBar(t, "106", "120", "105", "118"), 200)
	require.NoError(t, err)
	require.NotNil(t, decision.NewStop)
	assert.Equal(t, "108", decision.NewStop.String(), "90%% of the 120 high-water mark")

	// A lower High must not move the stop at all.
	decision, err = rule.OnLongBar(mustBar(t, "117", "119", "110", "112"), 200)
	require.NoError(t, err)
	assert.Nil(t, decision.NewStop)
}

func TestProbationTrendExitRule_OnLongBarErrorsIfCalledWithoutOnEntry(t *testing.T) {
	rule := newProbationTrendRuleForTest(t)
	_, err := rule.OnLongBar(mustBar(t, "100", "101", "99", "100"), 99)
	require.Error(t, err, "phase is PhaseFlat until OnEntry establishes PhaseProbation")
}
