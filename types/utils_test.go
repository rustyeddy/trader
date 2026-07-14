package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBitHelpers verifies BitSet and BitIsSet operate on multiword bitsets.
func TestBitHelpers(t *testing.T) {
	t.Parallel()

	bits := make([]uint64, 2)
	assert.False(t, BitIsSet(bits, 3))
	BitSet(bits, 3)
	assert.True(t, BitIsSet(bits, 3))
	assert.False(t, BitIsSet(bits, 64))
	BitSet(bits, 64)
	assert.True(t, BitIsSet(bits, 64))
}
