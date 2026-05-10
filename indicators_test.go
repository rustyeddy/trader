package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIndicatorEMAName(t *testing.T) {
	t.Parallel()

	ema := NewEMA(14, PriceScale)
	assert.Equal(t, "EMA(14)", ema.Name())
}

func TestIndicatorADXName(t *testing.T) {
	t.Parallel()

	adx := NewADX(14, PriceScale)
	assert.Equal(t, "ADX(14)", adx.Name())
}
