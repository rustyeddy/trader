package trader

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMulDivFloor64 verifies expected behavior for this component.
func TestMulDivFloor64(t *testing.T) {
	t.Parallel()

	v, err := mulDivFloor64(10, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(7), v)

	_, err = mulDivFloor64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)
}

// TestMulDivCeil64 verifies expected behavior for this component.
func TestMulDivCeil64(t *testing.T) {
	t.Parallel()

	v, err := mulDivCeil64(10, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(8), v)

	_, err = mulDivCeil64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)
}

// TestMulDivCeil64_InvalidArgs verifies expected behavior for this component.
func TestMulDivCeil64_InvalidArgs(t *testing.T) {
	t.Parallel()

	_, err := mulDivCeil64(-1, 3, 4)
	assert.Error(t, err)
}

// TestMulChecked64AndRoundHelpers_Phase2 verifies expected behavior for this component.
func TestMulChecked64AndRoundHelpers_Phase2(t *testing.T) {
	t.Parallel()

	v, err := mulChecked64(3, 7)
	require.NoError(t, err)
	assert.Equal(t, int64(21), v)

	_, err = mulChecked64(-1, 2)
	assert.Error(t, err)

	_, err = mulChecked64(math.MaxInt64, 2)
	assert.Error(t, err)

	r, err := roundHalfAwayFromZero(15, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(2), r)

	r, err = roundHalfAwayFromZero(14, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), r)

	_, err = roundHalfAwayFromZero(-1, 10)
	assert.Error(t, err)

	_, err = roundHalfAwayFromZero(1, 0)
	assert.Error(t, err)
}

// TestAbsInt64CheckedAndSignedMulDivRound_Phase2 verifies expected behavior for this component.
func TestAbsInt64CheckedAndSignedMulDivRound_Phase2(t *testing.T) {
	t.Parallel()

	v, err := absInt64Checked(-9)
	require.NoError(t, err)
	assert.Equal(t, int64(9), v)

	_, err = absInt64Checked(math.MinInt64)
	assert.Error(t, err)

	v, err = signedMulDivRound(3, 10, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(8), v)

	v, err = signedMulDivRound(-3, 10, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(-8), v)

	_, err = signedMulDivRound(1, -1, 2)
	assert.Error(t, err)

	_, err = signedMulDivRound(1, 1, 0)
	assert.Error(t, err)

	_, err = signedMulDivRound(math.MinInt64, 1, 1)
	assert.Error(t, err)

	_, err = signedMulDivRound(math.MaxInt64, 2, 1)
	assert.Error(t, err)
}

// TestMulDivVariants_OverflowBranches_Phase2 verifies expected behavior for this component.
func TestMulDivVariants_OverflowBranches_Phase2(t *testing.T) {
	t.Parallel()

	_, err := mulDivFloor64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)

	_, err = mulDivCeil64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)
}

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
