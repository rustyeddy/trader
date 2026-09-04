package indicator

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewZScore_RejectsNonPositivePeriod(t *testing.T) {
	_, err := NewZScore(0)
	assert.ErrorIs(t, err, ErrInvalidPeriod)

	_, err = NewZScore(-2)
	assert.ErrorIs(t, err, ErrInvalidPeriod)
}

func TestZScore_NotReadyBeforeFullWindow(t *testing.T) {
	z, err := NewZScore(3)
	require.NoError(t, err)

	require.NoError(t, z.Update(1))
	assert.False(t, z.Ready())
	_, ok := z.Value()
	assert.False(t, ok)

	require.NoError(t, z.Update(2))
	require.NoError(t, z.Update(3))
	assert.True(t, z.Ready())
}

func TestZScore_HandChecked(t *testing.T) {
	z, err := NewZScore(4)
	require.NoError(t, err)

	// Window [2,4,4,4]: mean 3.5, population stddev sqrt(0.75).
	// Last sample supplied (4) scores (4-3.5)/sqrt(0.75).
	for _, x := range []float64{2, 4, 4, 4} {
		require.NoError(t, z.Update(x))
	}
	require.True(t, z.Ready())
	score, ok := z.Value()
	require.True(t, ok)
	want := (4.0 - 3.5) / math.Sqrt(0.75)
	assert.InDelta(t, want, score, 1e-9)
}

func TestZScore_NegativeDeviation(t *testing.T) {
	z, err := NewZScore(3)
	require.NoError(t, err)

	for _, x := range []float64{10, 10, 10} {
		require.NoError(t, z.Update(x))
	}
	// Slide in a low outlier: window becomes [10,10,1].
	require.NoError(t, z.Update(1))
	score, ok := z.Value()
	require.True(t, ok)
	assert.Less(t, score, 0.0)
}

func TestZScore_ZeroVarianceExcluded(t *testing.T) {
	z, err := NewZScore(3)
	require.NoError(t, err)

	for _, x := range []float64{1.2345, 1.2345, 1.2345} {
		require.NoError(t, z.Update(x))
	}
	require.True(t, z.Ready())

	score, ok := z.Value()
	assert.False(t, ok)
	assert.Equal(t, 0.0, score)
}

func TestZScore_RejectsNonFiniteSample(t *testing.T) {
	z, err := NewZScore(2)
	require.NoError(t, err)

	err = z.Update(math.NaN())
	assert.ErrorIs(t, err, ErrNonFiniteSample)
	err = z.Update(math.Inf(-1))
	assert.ErrorIs(t, err, ErrNonFiniteSample)
}

func TestZScore_Period(t *testing.T) {
	z, err := NewZScore(20)
	require.NoError(t, err)
	assert.Equal(t, 20, z.Period())
}

func TestZScore_DeterministicReplay(t *testing.T) {
	samples := []float64{1.10, 1.12, 1.09, 1.15, 1.13, 1.08, 1.20, 1.05, 1.30}

	run := func() []float64 {
		z, err := NewZScore(4)
		require.NoError(t, err)
		var out []float64
		for _, x := range samples {
			require.NoError(t, z.Update(x))
			if score, ok := z.Value(); ok {
				out = append(out, score)
			}
		}
		return out
	}

	assert.Equal(t, run(), run())
}
