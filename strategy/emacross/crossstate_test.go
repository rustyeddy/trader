package emacross

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyRelation(t *testing.T) {
	assert.Equal(t, relationAbove, classifyRelation(2, 1))
	assert.Equal(t, relationBelow, classifyRelation(1, 2))
	assert.Equal(t, relationTie, classifyRelation(1, 1))
}

// TestCrossState_TieHandling replays
// docs/research/ema-01-experiment-definition.org's own abstract "Tie
// Handling" worked example verbatim: it proves Above/Tie/Above never
// appears to cross, and that a real reversal immediately following a
// tie is still detected against the pre-tie relation, not against the
// tie itself.
func TestCrossState_TieHandling(t *testing.T) {
	var s crossState

	// Step 1: Above, no prior relation -> no cross; last becomes Above.
	bullish, bearish := s.update(relationAbove)
	assert.False(t, bullish)
	assert.False(t, bearish)
	assert.Equal(t, relationAbove, s.last)
	assert.True(t, s.have)

	// Step 2: Tie -> no cross; last unchanged (still Above).
	bullish, bearish = s.update(relationTie)
	assert.False(t, bullish)
	assert.False(t, bearish)
	assert.Equal(t, relationAbove, s.last)

	// Step 3: Above again -> same side as before the tie, no cross.
	bullish, bearish = s.update(relationAbove)
	assert.False(t, bullish)
	assert.False(t, bearish)
	assert.Equal(t, relationAbove, s.last)

	// Step 4: Tie -> no cross; last still Above.
	bullish, bearish = s.update(relationTie)
	assert.False(t, bullish)
	assert.False(t, bearish)
	assert.Equal(t, relationAbove, s.last)

	// Step 5: Below -> bearish cross, detected against the pre-tie
	// relation (Above from step 3), not against step 4's Tie.
	bullish, bearish = s.update(relationBelow)
	assert.False(t, bullish)
	assert.True(t, bearish)
	assert.Equal(t, relationBelow, s.last)
}

func TestCrossState_FirstNonTieRelationNeverCrosses(t *testing.T) {
	var s crossState
	bullish, bearish := s.update(relationBelow)
	assert.False(t, bullish)
	assert.False(t, bearish)
}

func TestCrossState_RepeatedSameSideNeverCrosses(t *testing.T) {
	var s crossState
	s.update(relationAbove)
	bullish, bearish := s.update(relationAbove)
	assert.False(t, bullish)
	assert.False(t, bearish)
}

func TestCrossState_BullishThenBearish(t *testing.T) {
	var s crossState
	s.update(relationBelow)

	bullish, bearish := s.update(relationAbove)
	assert.True(t, bullish)
	assert.False(t, bearish)

	bullish, bearish = s.update(relationBelow)
	assert.False(t, bullish)
	assert.True(t, bearish)
}
