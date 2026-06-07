package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIndicatorEMAName(t *testing.T) {
	t.Parallel()

	ema, err := NewEMA(14, PriceScale)
	assert.NoError(t, err)
	assert.Equal(t, "EMA(14)", ema.Name())
}

func TestIndicatorADXName(t *testing.T) {
	t.Parallel()

	adx, err := NewADX(14, PriceScale)
	assert.NoError(t, err)
	assert.Equal(t, "ADX(14)", adx.Name())
}
