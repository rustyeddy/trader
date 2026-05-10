package trader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestNoopStrategyReset(t *testing.T) {
	t.Parallel()

	s := noopStrategy{}
	assert.NotPanics(t, func() {
		s.Reset()
	})
}

func TestNoopStrategyReady(t *testing.T) {
	t.Parallel()

	s := noopStrategy{}
	assert.True(t, s.Ready())
}

func TestNoopStrategyUpdateWithValues(t *testing.T) {
	t.Parallel()

	s := noopStrategy{}
	c := &CandleTime{
		Candle:    Candle{Close: PriceFromFloat(1.1)},
		Timestamp: Timestamp(100),
	}

	plan := s.Update(context.Background(), c, nil)
	require.NotNil(t, plan)
	assert.Equal(t, &DefaultStrategyPlan, plan)
	assert.Equal(t, "hold", plan.Reason)
	assert.Empty(t, plan.Opens)
	assert.Empty(t, plan.Closes)
}
