package strategy

import (
	"testing"

	"github.com/rustyeddy/trader/execution"
	"github.com/stretchr/testify/assert"
)

func TestDefaultPlan(t *testing.T) {
	assert.Equal(t, "hold", DefaultStrategyPlan.Reason)
	assert.Empty(t, DefaultStrategyPlan.Opens)
	assert.Empty(t, DefaultStrategyPlan.Closes)
	assert.Empty(t, DefaultStrategyPlan.Cancel)
}

func TestDefaultPlanReturnsFreshCopy(t *testing.T) {
	first := DefaultPlan()
	second := DefaultPlan()

	if assert.NotSame(t, first, second) {
		first.Reason = "changed"
		assert.Equal(t, "hold", second.Reason)
		assert.Equal(t, "hold", DefaultStrategyPlan.Reason)
	}
}

func TestHoldPlan(t *testing.T) {
	assert.Equal(t, "hold", HoldPlan("").Reason)
	assert.Equal(t, "waiting", HoldPlan("waiting").Reason)
}

func TestStrategyPlanEmpty(t *testing.T) {
	assert.True(t, (*StrategyPlan)(nil).Empty())
	assert.True(t, (&StrategyPlan{}).Empty())
	assert.True(t, (&StrategyPlan{Reason: "hold"}).Empty())
	assert.False(t, (&StrategyPlan{Opens: []*execution.OpenRequest{{}}}).Empty())
}
