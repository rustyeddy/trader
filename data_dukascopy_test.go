package trader

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBitIsSet(t *testing.T) {
	t.Parallel()

	bits := make([]uint64, 2)

	require.False(t, dukasBitIsSet(bits, 0))
	require.False(t, dukasBitIsSet(bits, 63))
	require.False(t, dukasBitIsSet(bits, 64))

	dukasBitSet(bits, 0)
	require.True(t, dukasBitIsSet(bits, 0))
	require.False(t, dukasBitIsSet(bits, 1))

	dukasBitSet(bits, 63)
	require.True(t, dukasBitIsSet(bits, 63))

	dukasBitSet(bits, 64)
	require.True(t, dukasBitIsSet(bits, 64))

	// Idempotent
	dukasBitSet(bits, 0)
	require.True(t, dukasBitIsSet(bits, 0))
}
