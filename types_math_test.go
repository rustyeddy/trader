package trader

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMulDiv64 performs TestMulDiv64.
func TestMulDiv64(t *testing.T) {
	t.Parallel()

	v, err := mulDiv64(10, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(8), v)

	_, err = mulDiv64(-1, 3, 4)
	assert.Error(t, err)
}

// TestMulDivFloor64 performs TestMulDivFloor64.
func TestMulDivFloor64(t *testing.T) {
	t.Parallel()

	v, err := mulDivFloor64(10, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(7), v)

	_, err = mulDivFloor64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)
}

// TestMulDivCeil64 performs TestMulDivCeil64.
func TestMulDivCeil64(t *testing.T) {
	t.Parallel()

	v, err := mulDivCeil64(10, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(8), v)

	_, err = mulDivCeil64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)
}

// TestAbs64AndAbsGeneric performs TestAbs64AndAbsGeneric.
func TestAbs64AndAbsGeneric(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(5), abs64(-5))
	assert.Equal(t, int64(0), abs64(0))
	assert.Equal(t, 7, abs(-7))
	assert.Equal(t, float64(2.5), abs(-2.5))
}

// TestMulChecked64AndRoundHelpers_Phase2 performs TestMulChecked64AndRoundHelpers_Phase2.
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

// TestAbsInt64CheckedAndSignedMulDivRound_Phase2 performs TestAbsInt64CheckedAndSignedMulDivRound_Phase2.
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

// TestMulDivVariants_OverflowBranches_Phase2 performs TestMulDivVariants_OverflowBranches_Phase2.
func TestMulDivVariants_OverflowBranches_Phase2(t *testing.T) {
	t.Parallel()

	_, err := mulDiv64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)

	_, err = mulDivFloor64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)

	_, err = mulDivCeil64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)
}
