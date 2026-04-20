package trader

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMulDiv64(t *testing.T) {
	t.Parallel()

	v, err := mulDiv64(10, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(8), v)

	_, err = mulDiv64(-1, 3, 4)
	assert.Error(t, err)
}

func TestMulDivFloor64(t *testing.T) {
	t.Parallel()

	v, err := mulDivFloor64(10, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(7), v)

	_, err = mulDivFloor64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)
}

func TestMulDivCeil64(t *testing.T) {
	t.Parallel()

	v, err := mulDivCeil64(10, 3, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(8), v)

	_, err = mulDivCeil64(math.MaxInt64, math.MaxInt64, 1)
	assert.Error(t, err)
}

func TestAbs64AndAbsGeneric(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(5), abs64(-5))
	assert.Equal(t, int64(0), abs64(0))
	assert.Equal(t, 7, abs(-7))
	assert.Equal(t, float64(2.5), abs(-2.5))
}
