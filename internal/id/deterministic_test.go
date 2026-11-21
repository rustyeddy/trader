package id

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeterministic_ReproducesTheSequence(t *testing.T) {
	run := func() []string {
		now := t0
		g := NewGenerator(func() time.Time { return now }, NewDeterministic(42, 7))
		out := make([]string, 10)
		for i := range out {
			now = t0.Add(time.Duration(i) * time.Millisecond)
			got, err := g.RunID()
			require.NoError(t, err)
			out[i] = got.String()
		}
		return out
	}
	assert.Equal(t, run(), run(), "the same seed and clock give the same IDs")
}

func TestDeterministic_DifferentSeedsDiffer(t *testing.T) {
	first := func(seed uint64) string {
		got, err := NewGenerator(fixed(t0), NewDeterministic(seed, seed)).RunID()
		require.NoError(t, err)
		return got.String()
	}
	assert.NotEqual(t, first(1), first(2))
}
