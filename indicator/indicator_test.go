package indicator

import (
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
)

func TestIndicatorEMAName(t *testing.T) {
	t.Parallel()

	ema, err := NewEMA(14, types.PriceScale)
	assert.NoError(t, err)
	assert.Equal(t, "EMA(14)", ema.Name())
}

func TestIndicatorADXName(t *testing.T) {
	t.Parallel()

	adx, err := NewADX(14, types.PriceScale)
	assert.NoError(t, err)
	assert.Equal(t, "ADX(14)", adx.Name())
}
