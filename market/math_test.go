package market

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMulDivFloor64 verifies expected behavior for this component.
func TestMulDivFloor64(t *testing.T) {
	t.Parallel()

	v, err := MulDivFloor64(10, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(7), v)

	_, err = MulDivFloor64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)
}

// TestMulDivCeil64 verifies expected behavior for this component.
func TestMulDivCeil64(t *testing.T) {
	t.Parallel()

	v, err := MulDivCeil64(10, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(8), v)

	_, err = MulDivCeil64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)
}

// TestMulDivCeil64_QuotientAtMaxInt64 covers the ceil increment at the int64
// boundary: (2^32-1)*(2^32+1) = 2^64-1, so dividing by 2 floors to MaxInt64
// with remainder 1. The ceil would be MaxInt64+1, which must error rather
// than wrap to MinInt64.
func TestMulDivCeil64_QuotientAtMaxInt64(t *testing.T) {
	t.Parallel()

	// Exact division landing on MaxInt64 succeeds (remainder 0, no increment).
	v, err := MulDivCeil64(math.MaxInt64, 2, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(math.MaxInt64), v)

	_, err = MulDivCeil64((1<<32)-1, (1<<32)+1, 2)
	assert.Error(t, err)
}

// TestMulDivCeil64_InvalidArgs verifies expected behavior for this component.
func TestMulDivCeil64_InvalidArgs(t *testing.T) {
	t.Parallel()

	_, err := MulDivCeil64(-1, 3, 4)
	assert.Error(t, err)
}

// TestMulChecked64AndRoundHelpers_Phase2 verifies expected behavior for this component.
func TestMulChecked64AndRoundHelpers_Phase2(t *testing.T) {
	t.Parallel()

	v, err := MulChecked64(3, 7)
	require.NoError(t, err)
	assert.Equal(t, int64(21), v)

	_, err = MulChecked64(-1, 2)
	assert.Error(t, err)

	_, err = MulChecked64(math.MaxInt64, 2)
	assert.Error(t, err)

	r, err := RoundHalfAwayFromZero(15, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(2), r)

	r, err = RoundHalfAwayFromZero(14, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), r)

	_, err = RoundHalfAwayFromZero(-1, 10)
	assert.Error(t, err)

	_, err = RoundHalfAwayFromZero(1, 0)
	assert.Error(t, err)
}

// TestAbsInt64Checked_Phase2 verifies expected behavior for this component.
func TestAbsInt64Checked_Phase2(t *testing.T) {
	t.Parallel()

	v, err := AbsInt64Checked(-9)
	require.NoError(t, err)
	assert.Equal(t, int64(9), v)

	_, err = AbsInt64Checked(math.MinInt64)
	assert.Error(t, err)
}

// TestMulDivVariants_OverflowBranches_Phase2 verifies expected behavior for this component.
func TestMulDivVariants_OverflowBranches_Phase2(t *testing.T) {
	t.Parallel()

	_, err := MulDivFloor64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)

	_, err = MulDivCeil64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)
}

// TestSignedMulDivRound covers rounding, sign handling, invalid args, and overflow.
func TestSignedMulDivRound(t *testing.T) {
	t.Parallel()

	v, err := SignedMulDivRound(3, 10, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(8), v)

	v, err = SignedMulDivRound(-3, 10, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(-8), v)

	v, err = SignedMulDivRound(1000, 500, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(5000), v)

	v, err = SignedMulDivRound(-1000, 500, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(-5000), v)

	_, err = SignedMulDivRound(1, -1, 2)
	assert.Error(t, err)

	_, err = SignedMulDivRound(1, 1, 0)
	assert.Error(t, err)

	_, err = SignedMulDivRound(1000, 500, -1)
	assert.Error(t, err)

	_, err = SignedMulDivRound(math.MinInt64, 1, 1)
	assert.Error(t, err)

	_, err = SignedMulDivRound(math.MaxInt64, 2, 1)
	assert.Error(t, err)

	_, err = SignedMulDivRound(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)
}

// TestMulChecked64_Normal - basic multiplication without overflow
func TestMulChecked64_Normal(t *testing.T) {
	t.Parallel()
	result, err := MulChecked64(1000, 2000)
	require.NoError(t, err)
	assert.Equal(t, int64(2_000_000), result)
}

// TestMulChecked64_Overflow - detect multiplication overflow
func TestMulChecked64_Overflow(t *testing.T) {
	t.Parallel()
	_, err := MulChecked64(math.MaxInt64, 2)
	assert.Error(t, err)
}

// TestMulChecked64_NegativeA - error on negative a
func TestMulChecked64_NegativeA(t *testing.T) {
	t.Parallel()
	_, err := MulChecked64(-1000, 2000)
	assert.Error(t, err)
}

// TestMulChecked64_NegativeB - error on negative b
func TestMulChecked64_NegativeB(t *testing.T) {
	t.Parallel()
	_, err := MulChecked64(1000, -2000)
	assert.Error(t, err)
}

// TestRoundHalfAwayFromZero_Exact - perfect division
func TestRoundHalfAwayFromZero_Exact(t *testing.T) {
	t.Parallel()
	result, err := RoundHalfAwayFromZero(100, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(10), result)
}

// TestRoundHalfAwayFromZero_RoundDown - remainder < (den+1)/2
func TestRoundHalfAwayFromZero_RoundDown(t *testing.T) {
	t.Parallel()
	// 10 / 3 = 3 remainder 1, (3+1)/2 = 2, so 1 < 2 -> no round up
	result, err := RoundHalfAwayFromZero(10, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(3), result)
}

// TestRoundHalfAwayFromZero_RoundUp - remainder >= (den+1)/2
func TestRoundHalfAwayFromZero_RoundUp(t *testing.T) {
	t.Parallel()
	// 11 / 3 = 3 remainder 2, (3+1)/2 = 2, so 2 >= 2 -> round up to 4
	result, err := RoundHalfAwayFromZero(11, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(4), result)
}

// TestRoundHalfAwayFromZero_InvalidNum - error on negative numerator
func TestRoundHalfAwayFromZero_InvalidNum(t *testing.T) {
	t.Parallel()
	_, err := RoundHalfAwayFromZero(-100, 10)
	assert.Error(t, err)
}

// TestRoundHalfAwayFromZero_InvalidDen - error on non-positive denominator
func TestRoundHalfAwayFromZero_InvalidDen(t *testing.T) {
	t.Parallel()
	_, err := RoundHalfAwayFromZero(100, 0)
	assert.Error(t, err)
	_, err = RoundHalfAwayFromZero(100, -1)
	assert.Error(t, err)
}

// TestAbsInt64Checked_Positive - positive values unchanged
func TestAbsInt64Checked_Positive(t *testing.T) {
	t.Parallel()
	result, err := AbsInt64Checked(1000)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), result)
}

// TestAbsInt64Checked_Negative - negation of negative values
func TestAbsInt64Checked_Negative(t *testing.T) {
	t.Parallel()
	result, err := AbsInt64Checked(-1000)
	require.NoError(t, err)
	assert.Equal(t, int64(1000), result)
}

// TestAbsInt64Checked_MinInt64Overflow - detect overflow on math.MinInt64
func TestAbsInt64Checked_MinInt64Overflow(t *testing.T) {
	t.Parallel()
	_, err := AbsInt64Checked(math.MinInt64)
	assert.Error(t, err)
}
