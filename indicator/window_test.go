package indicator

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWindow_LongSlideOfNearIdenticalSamplesNeverProducesNaN reproduces
// the roundoff scenario Rusty's PR #290 review raised: a long sliding
// replay of samples that are all identical (true variance exactly 0)
// can, through remove's repeated floating-point subtraction, leave m2
// very slightly negative. Before the roundoffEpsilon clamp in
// window.variance, math.Sqrt of that negative value produced NaN —
// exactly the silent NaN/Inf issue #279 requires research code never
// emit.
func TestWindow_LongSlideOfNearIdenticalSamplesNeverProducesNaN(t *testing.T) {
	r, err := NewRollingStdDev(5)
	require.NoError(t, err)

	const sample = 1.23456789
	for i := range 100000 {
		require.NoError(t, r.Update(sample))
		if !r.Ready() {
			continue
		}
		v := r.Value()
		require.Falsef(t, math.IsNaN(v), "stddev went NaN at update %d", i)
		require.Falsef(t, math.IsInf(v, 0), "stddev went Inf at update %d", i)
		assert.InDelta(t, 0.0, v, 1e-6)
	}
}

// TestWindow_VarianceClampsTinyNegativeRoundoffToZero exercises
// window.variance's roundoff clamp directly, independent of whether a
// real sliding replay happens to trigger it.
func TestWindow_VarianceClampsTinyNegativeRoundoffToZero(t *testing.T) {
	w := newWindow(3)
	w.n = 3
	w.mean = 1.0
	w.m2 = -1e-12 // well within roundoffEpsilon

	assert.Equal(t, 0.0, w.variance())
	assert.Equal(t, 0.0, w.stddev())
}

// TestWindow_VarianceDoesNotMaskARealAccumulatorBug confirms the clamp
// is scoped to roundoff-sized negatives only: a materially negative m2
// (far larger in magnitude than any plausible roundoff accumulation)
// is left unclamped, so a real bug remains visible as NaN rather than
// being silently hidden.
func TestWindow_VarianceDoesNotMaskARealAccumulatorBug(t *testing.T) {
	w := newWindow(3)
	w.n = 3
	w.mean = 1.0
	w.m2 = -0.5 // far beyond roundoffEpsilon

	assert.True(t, w.variance() < 0)
	assert.True(t, math.IsNaN(w.stddev()))
}
