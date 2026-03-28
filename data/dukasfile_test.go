package data

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBitIsSet(t *testing.T) {
	t.Parallel()

	bits := make([]uint64, 2)

	require.False(t, bitIsSet(bits, 0))
	require.False(t, bitIsSet(bits, 63))
	require.False(t, bitIsSet(bits, 64))

	bitSet(bits, 0)
	require.True(t, bitIsSet(bits, 0))
	require.False(t, bitIsSet(bits, 1))

	bitSet(bits, 63)
	require.True(t, bitIsSet(bits, 63))

	bitSet(bits, 64)
	require.True(t, bitIsSet(bits, 64))

	// Idempotent
	bitSet(bits, 0)
	require.True(t, bitIsSet(bits, 0))
}
