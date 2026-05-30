package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestUnitsInt64AndString_Phase1 performs TestUnitsInt64AndString_Phase1.
func TestUnitsInt64AndString_Phase1(t *testing.T) {
	t.Parallel()

	u := Units(12345)
	assert.Equal(t, int64(12345), u.Int64())
	assert.Equal(t, "12345", u.String())
}

// TestPipsFromFloat_RoundingCases_Phase1 performs TestPipsFromFloat_RoundingCases_Phase1.
func TestPipsFromFloat_RoundingCases_Phase1(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Pips(0), pipsFromFloat(0.0))
	assert.Equal(t, Pips(13), pipsFromFloat(1.25))
	assert.Equal(t, Pips(14), pipsFromFloat(1.35))
	assert.Equal(t, Pips(-13), pipsFromFloat(-1.25))
}

// TestPipsFloat64_Phase1 performs TestPipsFloat64_Phase1.
func TestPipsFloat64_Phase1(t *testing.T) {
	t.Parallel()

	assert.InDelta(t, 1.3, Pips(13).Float64(), 1e-9)
	assert.InDelta(t, -1.2, Pips(-12).Float64(), 1e-9)
}
