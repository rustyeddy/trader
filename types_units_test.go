package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestUnitsInt64AndString_Phase1 verifies expected behavior for this component.
func TestUnitsInt64AndString_Phase1(t *testing.T) {
	t.Parallel()

	u := Units(12345)
	assert.Equal(t, int64(12345), u.Int64())
	assert.Equal(t, "12345", u.String())
}

// TestPipsFromFloat_RoundingCases_Phase1 verifies expected behavior for this component.
func TestPipsFromFloat_RoundingCases_Phase1(t *testing.T) {
	t.Parallel()

	assert.Equal(t, Pips(0), PipsFromFloat(0.0))
	assert.Equal(t, Pips(13), PipsFromFloat(1.25))
	assert.Equal(t, Pips(14), PipsFromFloat(1.35))
	assert.Equal(t, Pips(-13), PipsFromFloat(-1.25))
}

// TestPipsFloat64_Phase1 verifies expected behavior for this component.
func TestPipsFloat64_Phase1(t *testing.T) {
	t.Parallel()

	assert.InDelta(t, 1.3, Pips(13).Float64(), 1e-9)
	assert.InDelta(t, -1.2, Pips(-12).Float64(), 1e-9)
}

func TestAvgSpreadPips(t *testing.T) {
	t.Parallel()

	inst := GetInstrument("EURUSD")
	assert.InDelta(t, 1.5, AvgSpreadPips(Price(30), 2, inst), 1e-9)
	assert.Zero(t, AvgSpreadPips(Price(30), 0, inst))
	assert.Zero(t, AvgSpreadPips(Price(30), 2, nil))
}
