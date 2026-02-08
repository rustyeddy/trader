package strategies

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/pricing"
	"github.com/stretchr/testify/assert"
)

func TestNoopStrategy_OnTick(t *testing.T) {
	strat := NoopStrategy{}
	ctx := context.Background()

	// NoopStrategy should do nothing and return no error
	err := strat.OnTick(ctx, nil, pricing.Tick{})
	assert.NoError(t, err)
}
