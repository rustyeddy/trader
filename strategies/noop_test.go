package strategies

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/broker"
	"github.com/stretchr/testify/assert"
)

func TestNoopStrategy_OnTick(t *testing.T) {
	strat := NoopStrategy{}
	ctx := context.Background()

	// NoopStrategy should do nothing and return no error
	err := strat.OnTick(ctx, nil, broker.Price{})
	assert.NoError(t, err)
}
