package noop

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/market"
)

func TestStrategy_Update(t *testing.T) {
	sig := Strategy{}.Update(context.Background(), nil, nil)
	assert.Equal(t, market.Flat, sig.Side)
}

func TestStrategy_Name(t *testing.T) {
	assert.Equal(t, "NoOp", Strategy{}.Name())
}

func TestStrategy_Reset(t *testing.T) {
	t.Parallel()
	assert.NotPanics(t, func() {
		Strategy{}.Reset()
	})
}

func TestStrategy_Ready(t *testing.T) {
	t.Parallel()
	assert.True(t, Strategy{}.Ready())
}

func TestStrategy_UpdateWithValues(t *testing.T) {
	t.Parallel()

	c := &market.CandleTime{
		Candle:    market.Candle{Close: market.PriceFromFloat(1.1)},
		Timestamp: market.Timestamp(100),
	}

	sig := Strategy{}.Update(context.Background(), c, nil)
	require.Equal(t, market.Flat, sig.Side)
	assert.Equal(t, "noop", sig.Reason)
}
