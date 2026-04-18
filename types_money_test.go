package trader

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMoneyFromFloatAndFloat64(t *testing.T) {
	t.Parallel()

	cases := []float64{0, 1.0, 1.5, -2.25, 12345.678901}
	for _, v := range cases {
		m := MoneyFromFloat(v)
		assert.InDelta(t, v, m.Float64(), 1e-6)
	}
}

func TestMoneyStringRawScaledValue(t *testing.T) {
	t.Parallel()

	m := MoneyFromFloat(1.5)
	assert.Equal(t, "1500000.000000", m.String())
}

func TestPriceFromFloatScaling(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Price(123450), PriceFromFloat(1.2345))
	assert.Equal(t, Price(-150000), PriceFromFloat(-1.5))
}

func TestFormatNumber(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "1.23450", FormatNumber(PriceFromFloat(1.2345), int32(PriceScale)))
	assert.Equal(t, "1.5", FormatNumber(15, 10))
}

func TestParsePrice(t *testing.T) {
	t.Parallel()

	p, err := ParsePrice(" 12345 ")
	require.NoError(t, err)
	assert.Equal(t, Price(12345), p)

	_, err = ParsePrice("not-a-number")
	assert.Error(t, err)
}

func TestRateFromFloatFloat64AndString(t *testing.T) {
	t.Parallel()

	r := RateFromFloat(0.123456)
	assert.InDelta(t, 0.123456, r.Float64(), 1e-6)
	assert.Equal(t, "0.123456", r.String())
}

func TestScaleConsts(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Scale6(100000), PriceScale)
	assert.Equal(t, Scale7(1000000), MoneyScale)
}

func TestPriceOverflowBoundaryBehavior(t *testing.T) {
	t.Parallel()

	maxSafe := float64(math.MaxInt32) / float64(PriceScale)
	assert.Equal(t, Price(math.MaxInt32), PriceFromFloat(maxSafe))
}
