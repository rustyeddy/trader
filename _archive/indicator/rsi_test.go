package indicator

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRSI_RejectsInvalidPeriod(t *testing.T) {
	for _, period := range []int{0, -1, -20} {
		_, err := NewRSI(period)
		require.ErrorIs(t, err, ErrInvalidPeriod, "period %d", period)
	}
}

func TestNewRSI_AcceptsPositivePeriod(t *testing.T) {
	rsi, err := NewRSI(2)
	require.NoError(t, err)
	assert.Equal(t, 2, rsi.Period())
}

// TestRSI_WarmupRequiresPeriodPlusOneSamples proves the exact
// "period + 1 input prices needed" contract: for period 2, Ready must
// stay false through the 1st and 2nd Update (only one price change has
// been observed after the 2nd), and become true on the exact call that
// supplies the 3rd sample.
func TestRSI_WarmupRequiresPeriodPlusOneSamples(t *testing.T) {
	rsi, err := NewRSI(2)
	require.NoError(t, err)

	assert.False(t, rsi.Ready())
	assert.Zero(t, rsi.Value())

	require.NoError(t, rsi.Update(100)) // 1st sample: no delta yet
	assert.False(t, rsi.Ready())
	assert.Zero(t, rsi.Value())

	require.NoError(t, rsi.Update(102)) // 2nd sample: 1st delta only
	assert.False(t, rsi.Ready())
	assert.Zero(t, rsi.Value())

	require.NoError(t, rsi.Update(101)) // 3rd sample: 2nd delta -> ready
	assert.True(t, rsi.Ready())
	assert.NotZero(t, rsi.Value())
}

// TestRSI_UpdateRejectsNonFiniteSample proves Update rejects NaN/+-Inf
// outright, both while still warming up and once already Ready, and
// leaves state completely unchanged either way — the same contract
// EMA.Update/SMA.Update already establish.
func TestRSI_UpdateRejectsNonFiniteSample(t *testing.T) {
	rsi, err := NewRSI(2)
	require.NoError(t, err)

	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		err := rsi.Update(bad)
		require.ErrorIs(t, err, ErrNonFiniteSample)
		assert.False(t, rsi.Ready())
	}

	require.NoError(t, rsi.Update(100))
	require.NoError(t, rsi.Update(102))
	require.NoError(t, rsi.Update(101))
	require.True(t, rsi.Ready())
	before := rsi.Value()

	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		err := rsi.Update(bad)
		require.ErrorIs(t, err, ErrNonFiniteSample)
		assert.Equal(t, before, rsi.Value(), "state must be unchanged after a rejected sample")
	}
}

// TestRSI_RisingSequenceReachesOneHundred proves the "no losses and
// positive gains -> RSI = 100" boundary for a purely rising input,
// definitionally checkable without any external reference: every
// delta is a gain, so average loss is always exactly zero.
func TestRSI_RisingSequenceReachesOneHundred(t *testing.T) {
	rsi, err := NewRSI(2)
	require.NoError(t, err)
	for _, p := range []float64{1, 2, 3, 4, 5, 6} {
		require.NoError(t, rsi.Update(p))
	}
	require.True(t, rsi.Ready())
	assert.Equal(t, 100.0, rsi.Value())
}

// TestRSI_FallingSequenceReachesZero proves the "no gains and positive
// losses -> RSI = 0" boundary, the mirror image of the rising case.
func TestRSI_FallingSequenceReachesZero(t *testing.T) {
	rsi, err := NewRSI(2)
	require.NoError(t, err)
	for _, p := range []float64{6, 5, 4, 3, 2, 1} {
		require.NoError(t, rsi.Update(p))
	}
	require.True(t, rsi.Ready())
	assert.Equal(t, 0.0, rsi.Value())
}

// TestRSI_FlatSequenceIsFifty proves the "no gains and no losses ->
// RSI = 50" boundary: every delta is exactly zero.
func TestRSI_FlatSequenceIsFifty(t *testing.T) {
	rsi, err := NewRSI(2)
	require.NoError(t, err)
	for _, p := range []float64{5, 5, 5, 5, 5} {
		require.NoError(t, rsi.Update(p))
	}
	require.True(t, rsi.Ready())
	assert.Equal(t, 50.0, rsi.Value())
}

// TestRSI_OutputBoundedInZeroToOneHundred exercises a long, varied
// pseudo-random-looking (but fixed, deterministic) sequence and checks
// every post-warmup Value stays within [0, 100], across several
// periods.
func TestRSI_OutputBoundedInZeroToOneHundred(t *testing.T) {
	prices := []float64{
		100, 102, 101, 105, 103, 103, 108, 95, 120, 60,
		61, 200, 199.99, 199.98, 0.01, 50, 50, 50.0001, 49.9999, 1000,
	}
	for _, period := range []int{1, 2, 3, 5, 14} {
		rsi, err := NewRSI(period)
		require.NoError(t, err)
		for _, p := range prices {
			require.NoError(t, rsi.Update(p))
			if rsi.Ready() {
				v := rsi.Value()
				assert.GreaterOrEqual(t, v, 0.0, "period %d", period)
				assert.LessOrEqual(t, v, 100.0, "period %d", period)
			}
		}
	}
}

// TestRSI_Period2MixedSequenceMatchesHandCalculation is the primary
// "independently calculated expected values" check the issue requires
// for RSI(2) specifically. Every expected value below was derived by
// hand, off the implementation, from the standard Wilder recursion:
//
//	prices:  100  102  101  105  103  103  108
//	deltas:      +2   -1   +4   -2    0   +5
//	gain/loss:  (2,0)(0,1)(4,0)(0,2)(0,0)(5,0)
//
//	seed (deltas 1,2): avgGain = (2+0)/2 = 1, avgLoss = (0+1)/2 = 0.5
//	  RS = 1/0.5 = 2         -> RSI = 100 - 100/3       = 200/3
//	after delta 3 (+4,0): avgGain = (1*1+4)/2 = 2.5, avgLoss = (1*0.5+0)/2 = 0.25
//	  RS = 2.5/0.25 = 10     -> RSI = 100 - 100/11      = 1000/11
//	after delta 4 (0,-2): avgGain = (1*2.5+0)/2 = 1.25, avgLoss = (1*0.25+2)/2 = 1.125
//	  RS = 1.25/1.125 = 10/9 -> RSI = 100 - 900/19      = 1000/19
//	after delta 5 (0,0):  avgGain = 1.25/2 = 0.625, avgLoss = 1.125/2 = 0.5625
//	  RS = 0.625/0.5625 = 10/9 (unchanged: a zero delta scales both
//	  averages by the same factor (period-1)/period, leaving their
//	  ratio, and therefore RSI, unchanged) -> RSI = 1000/19 (same as above)
//	after delta 6 (+5,0): avgGain = (1*0.625+5)/2 = 2.8125, avgLoss = (1*0.5625+0)/2 = 0.28125
//	  RS = 2.8125/0.28125 = 10 -> RSI = 100 - 100/11    = 1000/11 (same as delta 3's result)
//
// This is a hand-derived reference sequence, not a second copy of the
// implementation's own algorithm re-run in a different order — the
// arithmetic above was worked out independently and is checked here
// against RSI's actual output.
func TestRSI_Period2MixedSequenceMatchesHandCalculation(t *testing.T) {
	const tol = 1e-9
	rsi, err := NewRSI(2)
	require.NoError(t, err)

	prices := []float64{100, 102, 101, 105, 103, 103, 108}
	expected := []float64{200.0 / 3, 1000.0 / 11, 1000.0 / 19, 1000.0 / 19, 1000.0 / 11}

	var got []float64
	for _, p := range prices {
		require.NoError(t, rsi.Update(p))
		if rsi.Ready() {
			got = append(got, rsi.Value())
		}
	}

	require.Len(t, got, len(expected))
	for i, want := range expected {
		assert.InDelta(t, want, got[i], tol, "RSI value #%d", i+1)
	}
}

// TestRSI_Period3MixedSequenceMatchesHandCalculation proves RSI's
// Wilder recursion generalizes correctly to a period other than 2 —
// the protocol pins period 2 for EQR-01, but this package is a general
// primitive (issue #317's own requirement) and must not silently only
// work for that one value. Expected values are again hand-derived
// independently:
//
//	prices:  50   51   49   53   52   50
//	deltas:     +1   -2   +4   -1   -2
//	gain/loss: (1,0)(0,2)(4,0)(0,1)(0,2)
//
//	seed (deltas 1,2,3): avgGain = (1+0+4)/3 = 5/3, avgLoss = (0+2+0)/3 = 2/3
//	  RS = 2.5              -> RSI = 100 - 100/3.5      = 500/7
//	after delta 4 (0,-1): avgGain = (2*5/3+0)/3 = 10/9, avgLoss = (2*2/3+1)/3 = 7/9
//	  RS = 10/7              -> RSI = 100 - 700/17      = 1000/17
//	after delta 5 (0,-2): avgGain = (2*10/9+0)/3 = 20/27, avgLoss = (2*7/9+2)/3 = 32/27
//	  RS = 20/32 = 0.625     -> RSI = 100 - 100/1.625   = 500/13
func TestRSI_Period3MixedSequenceMatchesHandCalculation(t *testing.T) {
	const tol = 1e-9
	rsi, err := NewRSI(3)
	require.NoError(t, err)

	prices := []float64{50, 51, 49, 53, 52, 50}
	expected := []float64{500.0 / 7, 1000.0 / 17, 500.0 / 13}

	var got []float64
	for _, p := range prices {
		require.NoError(t, rsi.Update(p))
		if rsi.Ready() {
			got = append(got, rsi.Value())
		}
	}

	require.Len(t, got, len(expected))
	for i, want := range expected {
		assert.InDelta(t, want, got[i], tol, "RSI value #%d", i+1)
	}
}

// TestRSI_WilderSmoothingDiffersFromSimpleMovingAverage proves this
// implementation is genuinely Wilder-recursive, not a rolling simple
// average of the last `period` gains/losses re-averaged from scratch
// on every update (the issue's own explicit "not a rolling
// simple-average RSI" requirement). After the seed step, a simple
// moving average of the last 2 gains/losses would depend only on the
// two most recent deltas and would "forget" delta 1 entirely once
// delta 3 arrives; Wilder smoothing never fully forgets any prior
// delta. Using the period-2 hand calculation above: after delta 3, a
// simple 2-average would use deltas {2,3} (gains {0,4}, losses {1,0})
// giving avgGain=2, avgLoss=0.5, RS=4, RSI=100-20=80 — different from
// this implementation's hand-derived 1000/11 (~90.91), confirming the
// two algorithms are not the same and this one matches Wilder's, not
// the simple-average variant.
func TestRSI_WilderSmoothingDiffersFromSimpleMovingAverage(t *testing.T) {
	rsi, err := NewRSI(2)
	require.NoError(t, err)
	for _, p := range []float64{100, 102, 101, 105} {
		require.NoError(t, rsi.Update(p))
	}
	require.True(t, rsi.Ready())

	const simpleMovingAverageRSI = 80.0
	const wilderExpected = 1000.0 / 11
	assert.InDelta(t, wilderExpected, rsi.Value(), 1e-9)
	assert.False(t, math.Abs(simpleMovingAverageRSI-rsi.Value()) < 0.5,
		"RSI must not match a simple-moving-average recomputation (%v)", rsi.Value())
}
