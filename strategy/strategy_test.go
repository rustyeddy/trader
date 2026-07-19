package strategy

import (
	"testing"

	"github.com/rustyeddy/trader/account"
	"github.com/stretchr/testify/assert"
)

func TestStrategyPlanEmpty(t *testing.T) {
	assert.True(t, (*StrategyPlan)(nil).Empty())
	assert.True(t, (&StrategyPlan{}).Empty())
	assert.True(t, (&StrategyPlan{Reason: "hold"}).Empty())
	assert.False(t, (&StrategyPlan{Opens: []*account.OpenRequest{{}}}).Empty())
}
