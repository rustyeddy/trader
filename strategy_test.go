package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultPlan(t *testing.T) {
	assert.Equal(t, "hold", DefaultStrategyPlan.Reason)
	assert.Empty(t, DefaultStrategyPlan.Opens)
	assert.Empty(t, DefaultStrategyPlan.Closes)
	assert.Empty(t, DefaultStrategyPlan.Cancel)
}
