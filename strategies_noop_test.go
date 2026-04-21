package trader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNoopStrategyUpdate(t *testing.T) {
	strat := noopStrategy{}
	ctx := context.Background()

	// NoopStrategy should do nothing and return no error
	dec := strat.Update(ctx, nil, nil)
	assert.NotNil(t, dec)
}

func TestNoopStrategyName(t *testing.T) {
	strat := noopStrategy{}
	assert.Equal(t, "NoOp", strat.Name())
}

func TestNoopStrategyReason(t *testing.T) {
	strat := noopStrategy{}
	assert.Equal(t, "No-op", strat.Reason())
}
