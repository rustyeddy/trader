package trader

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSignedMulDivRound_Positive tests basic case: (1000 * 500) / 100 = 5000
func TestSignedMulDivRound_Positive(t *testing.T) {
	t.Parallel()
	result, err := signedMulDivRound(1000, 500, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(5000), result)
}

// TestSignedMulDivRound_Negative tests negative dividend: (-1000 * 500) / 100 = -5000
func TestSignedMulDivRound_Negative(t *testing.T) {
	t.Parallel()
	result, err := signedMulDivRound(-1000, 500, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(-5000), result)
}

// TestSignedMulDivRound_InvalidB validates error on negative b
func TestSignedMulDivRound_InvalidB(t *testing.T) {
	t.Parallel()
	_, err := signedMulDivRound(1000, -500, 100)
	assert.Error(t, err)
}

// TestSignedMulDivRound_InvalidDen validates error on non-positive denominator
func TestSignedMulDivRound_InvalidDen(t *testing.T) {
	t.Parallel()
	_, err := signedMulDivRound(1000, 500, 0)
	assert.Error(t, err)
	_, err = signedMulDivRound(1000, 500, -1)
	assert.Error(t, err)
}

// TestSignedMulDivRound_Overflow tests overflow detection
func TestSignedMulDivRound_Overflow(t *testing.T) {
	t.Parallel()
	_, err := signedMulDivRound(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)
}

// TestRR_ZeroRisk - when entry price equals stop loss, risk is zero
func TestRR_ZeroRisk(t *testing.T) {
	t.Parallel()
	entry := 1.0850
	stop := 1.0850
	takeProfit := 1.1500
	rr := RR(entry, stop, takeProfit)
	assert.Equal(t, 0.0, rr)
}

// TestRR_LongSetup - typical long trade: entry 1.0000, stop 0.9950, TP 1.0150
func TestRR_LongSetup(t *testing.T) {
	t.Parallel()
	entry := 1.0000
	stop := 0.9950       // 50 pips risk
	takeProfit := 1.0150 // 150 pips reward = 3:1 RR
	rr := RR(entry, stop, takeProfit)
	assert.InDelta(t, 3.0, rr, 0.0001)
}

// TestRR_ShortSetup - short trade: entry 1.0000, stop 1.0050, TP 0.9850
func TestRR_ShortSetup(t *testing.T) {
	t.Parallel()
	entry := 1.0000
	stop := 1.0050       // 50 pips risk (above entry)
	takeProfit := 0.9850 // 150 pips reward (below entry) = 3:1 RR
	rr := RR(entry, stop, takeProfit)
	assert.InDelta(t, 3.0, rr, 0.0001)
}

// TestMulChecked64_Normal - basic multiplication without overflow
func TestMulChecked64_Normal(t *testing.T) {
	t.Parallel()
	result, err := mulChecked64(1000, 2000)
	require.NoError(t, err)
	assert.Equal(t, int64(2_000_000), result)
}

// TestMulChecked64_Overflow - detect multiplication overflow
func TestMulChecked64_Overflow(t *testing.T) {
	t.Parallel()
	_, err := mulChecked64(math.MaxInt64, 2)
	assert.Error(t, err)
}

// TestMulChecked64_NegativeA - error on negative a
func TestMulChecked64_NegativeA(t *testing.T) {
	t.Parallel()
	_, err := mulChecked64(-1000, 2000)
	assert.Error(t, err)
}

// TestMulChecked64_NegativeB - error on negative b
func TestMulChecked64_NegativeB(t *testing.T) {
	t.Parallel()
	_, err := mulChecked64(1000, -2000)
	assert.Error(t, err)
}

// TestRoundHalfAwayFromZero_Exact - perfect division
func TestRoundHalfAwayFromZero_Exact(t *testing.T) {
	t.Parallel()
	result, err := roundHalfAwayFromZero(100, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(10), result)
}

// TestRoundHalfAwayFromZero_RoundDown - remainder < (den+1)/2
func TestRoundHalfAwayFromZero_RoundDown(t *testing.T) {
	t.Parallel()
	// 10 / 3 = 3 remainder 1, (3+1)/2 = 2, so 1 < 2 -> no round up
	result, err := roundHalfAwayFromZero(10, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(3), result)
}

// TestRoundHalfAwayFromZero_RoundUp - remainder >= (den+1)/2
func TestRoundHalfAwayFromZero_RoundUp(t *testing.T) {
	t.Parallel()
	// 11 / 3 = 3 remainder 2, (3+1)/2 = 2, so 2 >= 2 -> round up to 4
	result, err := roundHalfAwayFromZero(11, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(4), result)
}

// TestRoundHalfAwayFromZero_InvalidNum - error on negative numerator
func TestRoundHalfAwayFromZero_InvalidNum(t *testing.T) {
	t.Parallel()
	_, err := roundHalfAwayFromZero(-100, 10)
	assert.Error(t, err)
}

// TestRoundHalfAwayFromZero_InvalidDen - error on non-positive denominator
func TestRoundHalfAwayFromZero_InvalidDen(t *testing.T) {
	t.Parallel()
	_, err := roundHalfAwayFromZero(100, 0)
	assert.Error(t, err)
	_, err = roundHalfAwayFromZero(100, -1)
	assert.Error(t, err)
}

// TestAbsInt64Checked_Positive - positive values unchanged
func TestAbsInt64Checked_Positive(t *testing.T) {
	t.Parallel()
	result, err := absInt64Checked(1000)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), result)
}

// TestAbsInt64Checked_Negative - negation of negative values
func TestAbsInt64Checked_Negative(t *testing.T) {
	t.Parallel()
	result, err := absInt64Checked(-1000)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), result)
}

// TestAbsInt64Checked_MinInt64Overflow - detect overflow on math.MinInt64
func TestAbsInt64Checked_MinInt64Overflow(t *testing.T) {
	t.Parallel()
	_, err := absInt64Checked(math.MinInt64)
	assert.Error(t, err)
}
