package strategies

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultPlan(t *testing.T) {
	assert.Equal(t, "hold", DefaultPlan.Reason)
	assert.Empty(t, DefaultPlan.Opens)
	assert.Empty(t, DefaultPlan.Closes)
	assert.Empty(t, DefaultPlan.Cancel)
}
