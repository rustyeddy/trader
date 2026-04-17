package types

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMulDiv64(t *testing.T) {
	t.Parallel()

	v, err := MulDiv64(10, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(8), v)

	_, err = MulDiv64(-1, 3, 4)
	assert.Error(t, err)
}

func TestMulDivFloor64(t *testing.T) {
	t.Parallel()

	v, err := MulDivFloor64(10, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(7), v)

	_, err = MulDivFloor64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)
}

func TestMulDivCeil64(t *testing.T) {
	t.Parallel()

	v, err := MulDivCeil64(10, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(8), v)

	_, err = MulDivCeil64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)
}

func TestAbs64AndAbsGeneric(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(5), Abs64(-5))
	assert.Equal(t, int64(0), Abs64(0))
	assert.Equal(t, 7, Abs(-7))
	assert.Equal(t, float64(2.5), Abs(-2.5))
}
