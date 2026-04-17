package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isValidCrockfordChar(ch rune) bool {
	return (ch >= '0' && ch <= '9') ||
		(ch >= 'A' && ch <= 'Z' && ch != 'I' && ch != 'L' && ch != 'O' && ch != 'U')
}

func TestNewULIDFormat(t *testing.T) {
	t.Parallel()

	id := NewULID()
	require.Len(t, id, 26)

	for _, ch := range id {
		assert.True(t, isValidCrockfordChar(ch), "unexpected character %q in ULID %s", ch, id)
	}
}

func TestNewULIDUniqueness(t *testing.T) {
	t.Parallel()

	const n = 1000
	seen := make(map[string]struct{}, n)

	for i := 0; i < n; i++ {
		id := NewULID()
		require.NotEmpty(t, id)
		_, exists := seen[id]
		require.False(t, exists, "duplicate ULID generated: %s", id)
		seen[id] = struct{}{}
	}
}

func TestNewULIDLexicographicNonDecreasing(t *testing.T) {
	t.Parallel()

	const n = 1000
	prev := NewULID()
	require.NotEmpty(t, prev)

	for i := 1; i < n; i++ {
		cur := NewULID()
		require.NotEmpty(t, cur)
		assert.LessOrEqual(t, prev, cur, "ULIDs must be non-decreasing lexicographically")
		prev = cur
	}
}
