package indicator

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRollingStdDev_RejectsNonPositivePeriod(t *testing.T) {
	_, err := NewRollingStdDev(0)
	assert.ErrorIs(t, err, ErrInvalidPeriod)

	_, err = NewRollingStdDev(-5)
	assert.ErrorIs(t, err, ErrInvalidPeriod)
}

func TestRollingStdDev_NotReadyBeforeFullWindow(t *testing.T) {
	r, err := NewRollingStdDev(3)
	require.NoError(t, err)

	require.NoError(t, r.Update(1))
	assert.False(t, r.Ready())
	require.NoError(t, r.Update(2))
	assert.False(t, r.Ready())
	require.NoError(t, r.Update(3))
	assert.True(t, r.Ready())
}

func TestRollingStdDev_HandChecked(t *testing.T) {
	r, err := NewRollingStdDev(4)
	require.NoError(t, err)

	// Population variance of [2,4,4,4] is 0.75 -> stddev 0.8660254...
	for _, x := range []float64{2, 4, 4, 4} {
		require.NoError(t, r.Update(x))
	}
	require.True(t, r.Ready())
	assert.InDelta(t, math.Sqrt(0.75), r.Value(), 1e-9)
}

func TestRollingStdDev_ZeroVarianceWindow(t *testing.T) {
	r, err := NewRollingStdDev(3)
	require.NoError(t, err)

	for _, x := range []float64{1.1, 1.1, 1.1} {
		require.NoError(t, r.Update(x))
	}
	require.True(t, r.Ready())
	assert.InDelta(t, 0.0, r.Value(), 1e-12)
}

func TestRollingStdDev_RejectsNonFiniteSample(t *testing.T) {
	r, err := NewRollingStdDev(2)
	require.NoError(t, err)

	err = r.Update(math.NaN())
	assert.ErrorIs(t, err, ErrNonFiniteSample)
	err = r.Update(math.Inf(1))
	assert.ErrorIs(t, err, ErrNonFiniteSample)
}

func TestRollingStdDev_ValueIsZeroBeforeReady(t *testing.T) {
	r, err := NewRollingStdDev(3)
	require.NoError(t, err)

	assert.Equal(t, 0.0, r.Value())
	require.NoError(t, r.Update(1))
	require.NoError(t, r.Update(100))
	assert.False(t, r.Ready())
	// A partial window of [1,100] has substantial spread — confirm
	// Value stays gated on Ready rather than exposing it.
	assert.Equal(t, 0.0, r.Value())
}

func TestRollingStdDev_Period(t *testing.T) {
	r, err := NewRollingStdDev(9)
	require.NoError(t, err)
	assert.Equal(t, 9, r.Period())
}

func TestRollingStdDev_SlideMatchesNaiveRecompute(t *testing.T) {
	samples := []float64{1.10, 1.12, 1.09, 1.15, 1.13, 1.08, 1.20, 1.05, 1.30}
	period := 4

	r, err := NewRollingStdDev(period)
	require.NoError(t, err)

	for i, x := range samples {
		require.NoError(t, r.Update(x))
		if i+1 < period {
			continue
		}
		window := samples[i+1-period : i+1]
		var sum float64
		for _, v := range window {
			sum += v
		}
		mean := sum / float64(period)
		var sq float64
		for _, v := range window {
			sq += (v - mean) * (v - mean)
		}
		want := math.Sqrt(sq / float64(period))
		assert.InDelta(t, want, r.Value(), 1e-9)
	}
}
