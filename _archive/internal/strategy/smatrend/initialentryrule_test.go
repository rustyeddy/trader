package smatrend

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitialEntryRuleRegistry_KnowsBothNames(t *testing.T) {
	for _, name := range []string{"fresh-cross", "above-sma"} {
		_, ok := initialEntryRuleRegistry[name]
		assert.True(t, ok, name)
	}
}

func TestFreshCrossInitialEntryRule_RequiresGenuineCross(t *testing.T) {
	rule, err := newFreshCrossInitialEntryRule(Config{})
	require.NoError(t, err)

	assert.False(t, rule.ShouldEnter(InitialEntryContext{CrossedAboveSMA: false, AboveSMA: true}), "already above the SMA, but no cross, must not enter")
	assert.True(t, rule.ShouldEnter(InitialEntryContext{CrossedAboveSMA: true, AboveSMA: true}))
}

func TestAboveSMAInitialEntryRule_EntersWithoutRequiringACross(t *testing.T) {
	rule, err := newAboveSMAInitialEntryRule(Config{})
	require.NoError(t, err)

	assert.False(t, rule.ShouldEnter(InitialEntryContext{CrossedAboveSMA: false, AboveSMA: false}))
	assert.True(t, rule.ShouldEnter(InitialEntryContext{CrossedAboveSMA: false, AboveSMA: true}), "already above the SMA on startup must enter even without a fresh cross")
	assert.True(t, rule.ShouldEnter(InitialEntryContext{CrossedAboveSMA: true, AboveSMA: true}))
}
