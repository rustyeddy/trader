package indicator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEMA_RejectsInvalidPeriod(t *testing.T) {
	for _, period := range []int{0, -1, -20} {
		_, err := NewEMA(period)
		require.ErrorIs(t, err, ErrInvalidPeriod, "period %d", period)
	}
}

func TestNewEMA_AcceptsPositivePeriod(t *testing.T) {
	ema, err := NewEMA(20)
	require.NoError(t, err)
	assert.Equal(t, 20, ema.Period())
}

// TestEMA_NotReadyBeforeSeedComplete proves Update accumulates state
// during seeding without becoming Ready early, and that Value reports
// 0 (never a partially-averaged value) until then.
func TestEMA_NotReadyBeforeSeedComplete(t *testing.T) {
	ema, err := NewEMA(3)
	require.NoError(t, err)

	assert.False(t, ema.Ready())
	assert.Zero(t, ema.Value())

	ema.Update(104)
	assert.False(t, ema.Ready())
	assert.Zero(t, ema.Value())

	ema.Update(103)
	assert.False(t, ema.Ready())
	assert.Zero(t, ema.Value())
}

// TestEMA_SMASeeding proves the exact SMA-seeding convention
// (docs/research/ema-01-experiment-definition.org): the Period-th
// Update produces the arithmetic mean of every sample supplied so far,
// and Ready becomes true on that exact call, not before and not after.
func TestEMA_SMASeeding(t *testing.T) {
	ema, err := NewEMA(3)
	require.NoError(t, err)

	ema.Update(104)
	ema.Update(103)
	ema.Update(102) // the 3rd sample: seeding completes here

	require.True(t, ema.Ready())
	assert.InDelta(t, 103.0, ema.Value(), 1e-12)
}

// TestEMA_MatchesHandVerifiedFixture replays
// docs/research/ema-01-experiment-definition.org's own worked toy
// fixture (fast EMA(3), slow EMA(5)) and asserts every value the
// document's table records — the values in that document were
// themselves verified by an independent script, not by hand alone
// (the document says so explicitly), so this test is the two
// implementations agreeing, not one checking itself.
func TestEMA_MatchesHandVerifiedFixture(t *testing.T) {
	closes := []float64{104, 103, 102, 101, 100, 101, 104, 108, 112, 110, 105, 100, 95, 90}

	// index i holds the expected value after closes[i] is applied;
	// NaN marks "not ready yet," matched by asserting !Ready() instead
	// of comparing Value().
	const notReady = -1
	wantFast := []float64{notReady, notReady, 103.0, 102.0, 101.0, 101.0, 102.5, 105.25, 108.625, 109.3125, 107.15625, 103.578125, 99.2890625, 94.64453125}
	wantSlow := []float64{notReady, notReady, notReady, notReady, 102.0, 101.66666666666669, 102.44444444444446, 104.29629629629632, 106.86419753086422, 107.90946502057616, 106.93964334705079, 104.62642889803386, 101.41761926535591, 97.61174617690395}

	fast, err := NewEMA(3)
	require.NoError(t, err)
	slow, err := NewEMA(5)
	require.NoError(t, err)

	for i, c := range closes {
		fast.Update(c)
		slow.Update(c)

		if wantFast[i] == notReady {
			assert.Falsef(t, fast.Ready(), "bar %d: fast EMA should not be ready yet", i+1)
		} else {
			require.Truef(t, fast.Ready(), "bar %d: fast EMA should be ready", i+1)
			assert.InDeltaf(t, wantFast[i], fast.Value(), 1e-9, "bar %d: fast EMA value", i+1)
		}

		if wantSlow[i] == notReady {
			assert.Falsef(t, slow.Ready(), "bar %d: slow EMA should not be ready yet", i+1)
		} else {
			require.Truef(t, slow.Ready(), "bar %d: slow EMA should be ready", i+1)
			assert.InDeltaf(t, wantSlow[i], slow.Value(), 1e-9, "bar %d: slow EMA value", i+1)
		}
	}
}

// TestEMA_DeterministicAcrossRepeatedRuns proves issue #248's own
// acceptance criterion: replaying the identical input sequence through
// a freshly constructed EMA produces bit-identical output every time.
func TestEMA_DeterministicAcrossRepeatedRuns(t *testing.T) {
	closes := []float64{104, 103, 102, 101, 100, 101, 104, 108, 112, 110, 105, 100, 95, 90}

	run := func() []float64 {
		ema, err := NewEMA(5)
		require.NoError(t, err)
		var values []float64
		for _, c := range closes {
			ema.Update(c)
			values = append(values, ema.Value())
		}
		return values
	}

	first := run()
	for i := range 5 {
		assert.Equal(t, first, run(), "run %d diverged from the first run", i+2)
	}
}
