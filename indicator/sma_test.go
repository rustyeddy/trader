package indicator

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSMA_RejectsNonPositivePeriod(t *testing.T) {
	_, err := NewSMA(0)
	assert.ErrorIs(t, err, ErrInvalidPeriod)

	_, err = NewSMA(-1)
	assert.ErrorIs(t, err, ErrInvalidPeriod)
}

func TestSMA_NotReadyBeforeFullWindow(t *testing.T) {
	s, err := NewSMA(3)
	require.NoError(t, err)

	require.NoError(t, s.Update(1))
	assert.False(t, s.Ready())
	require.NoError(t, s.Update(2))
	assert.False(t, s.Ready())
	require.NoError(t, s.Update(3))
	assert.True(t, s.Ready())
}

func TestSMA_HandChecked(t *testing.T) {
	s, err := NewSMA(3)
	require.NoError(t, err)

	for _, x := range []float64{1, 2, 3} {
		require.NoError(t, s.Update(x))
	}
	require.True(t, s.Ready())
	assert.InDelta(t, 2.0, s.Value(), 1e-12)

	// Slide the window: [2,3,4] -> mean 3.
	require.NoError(t, s.Update(4))
	assert.InDelta(t, 3.0, s.Value(), 1e-12)

	// Slide again: [3,4,10] -> mean 17/3.
	require.NoError(t, s.Update(10))
	assert.InDelta(t, 17.0/3.0, s.Value(), 1e-9)
}

func TestSMA_RejectsNonFiniteSample(t *testing.T) {
	s, err := NewSMA(2)
	require.NoError(t, err)

	require.NoError(t, s.Update(1))
	err = s.Update(math.NaN())
	assert.ErrorIs(t, err, ErrNonFiniteSample)
	err = s.Update(math.Inf(1))
	assert.ErrorIs(t, err, ErrNonFiniteSample)
	err = s.Update(math.Inf(-1))
	assert.ErrorIs(t, err, ErrNonFiniteSample)

	// Rejected samples must not have mutated state.
	assert.False(t, s.Ready())
	require.NoError(t, s.Update(3))
	assert.True(t, s.Ready())
	assert.InDelta(t, 2.0, s.Value(), 1e-12)
}

func TestSMA_Period(t *testing.T) {
	s, err := NewSMA(7)
	require.NoError(t, err)
	assert.Equal(t, 7, s.Period())
}

func TestSMA_DeterministicReplay(t *testing.T) {
	samples := []float64{1.10, 1.12, 1.09, 1.15, 1.13, 1.08, 1.20}

	run := func() []float64 {
		s, err := NewSMA(4)
		require.NoError(t, err)
		var out []float64
		for _, x := range samples {
			require.NoError(t, s.Update(x))
			if s.Ready() {
				out = append(out, s.Value())
			}
		}
		return out
	}

	assert.Equal(t, run(), run())
}
