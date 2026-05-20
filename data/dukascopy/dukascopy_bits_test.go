package dukascopy

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBitHelpers_Ungated(t *testing.T) {
	t.Parallel()

	bits := make([]uint64, 2)

	require.False(t, BitIsSet(bits, 0))
	require.False(t, BitIsSet(bits, 63))
	require.False(t, BitIsSet(bits, 64))

	BitSet(bits, 0)
	require.True(t, BitIsSet(bits, 0))
	require.False(t, BitIsSet(bits, 1))

	BitSet(bits, 63)
	require.True(t, BitIsSet(bits, 63))

	BitSet(bits, 64)
	require.True(t, BitIsSet(bits, 64))

	BitSet(bits, 0)
	require.True(t, BitIsSet(bits, 0))
}
