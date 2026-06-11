package trader

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMoneyFromFloatAndFloat64 verifies expected behavior for this component.
func TestMoneyFromFloatAndFloat64(t *testing.T) {
	t.Parallel()

	cases := []float64{0, 1.0, 1.5, -2.25, 12345.678901}
	for _, v := range cases {
		m := MoneyFromFloat(v)
		assert.InDelta(t, v, m.Float64(), 1e-6)
	}
}

// TestMoneyStringScaledValue verifies expected behavior for this component.
func TestMoneyStringScaledValue(t *testing.T) {
	t.Parallel()

	m := MoneyFromFloat(1.5)
	assert.Equal(t, "1.500000", m.String())
}

// TestPriceFromFloatScaling verifies expected behavior for this component.
func TestPriceFromFloatScaling(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Price(123450), PriceFromFloat(1.2345))
	assert.Equal(t, Price(-150000), PriceFromFloat(-1.5))
}

// TestFormatNumber verifies expected behavior for this component.
func TestFormatNumber(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1.23450", formatNumber(PriceFromFloat(1.2345), int32(PriceScale)))
	assert.Equal(t, "1.5", formatNumber(15, 10))
}

// TestPriceStringScaledValue verifies expected behavior for this component.
func TestPriceStringScaledValue(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1.23450", PriceFromFloat(1.2345).String())
}

// TestParsePrice verifies expected behavior for this component.
func TestParsePrice(t *testing.T) {
	t.Parallel()

	p, err := parsePrice(" 12345 ")
	require.NoError(t, err)
	assert.Equal(t, Price(12345), p)

	_, err = parsePrice("not-a-number")
	assert.Error(t, err)
}

// TestRateFromFloatFloat64AndString verifies expected behavior for this component.
func TestRateFromFloatFloat64AndString(t *testing.T) {
	t.Parallel()

	r := RateFromFloat(0.123456)
	assert.InDelta(t, 0.123456, r.Float64(), 1e-6)
	assert.Equal(t, "0.123456", r.String())
}

// TestScaleConsts verifies expected behavior for this component.
func TestScaleConsts(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Scale6(100000), PriceScale)
	assert.Equal(t, Scale7(1000000), MoneyScale)
}

// TestPriceOverflowBoundaryBehavior verifies expected behavior for this component.
func TestPriceOverflowBoundaryBehavior(t *testing.T) {
	t.Parallel()

	maxSafe := float64(math.MaxInt32) / float64(PriceScale)
	assert.Equal(t, Price(math.MaxInt32), PriceFromFloat(maxSafe))
}
