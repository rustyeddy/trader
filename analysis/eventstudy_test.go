package analysis

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testEURUSD returns a stable instrument.ID for use as test
// provenance — analysis never inspects it beyond IsZero, so any real
// instrument works.
func testEURUSD(t *testing.T) instrument.ID {
	t.Helper()
	eurusd, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	return eurusd.ID()
}

// barsFromCloses builds a minimal []marketdata.Bar from a slice of
// close prices (as decimal strings), one hour apart starting at a
// fixed reference time. Only Close and Time are populated — the other
// OHLC fields are irrelevant to RunEventStudy, which never reads them.
func barsFromCloses(t *testing.T, closes []string) []marketdata.Bar {
	t.Helper()
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := make([]marketdata.Bar, len(closes))
	for i, c := range closes {
		bars[i] = marketdata.Bar{
			Time:  start.Add(time.Duration(i) * time.Hour),
			Close: num.MustParsePrice(c),
		}
	}
	return bars
}

func TestEventStudyConfig_Validate(t *testing.T) {
	eurusd := testEURUSD(t)
	h, err := NewH1Horizon(1)
	require.NoError(t, err)

	_, err = RunEventStudy(nil, EventStudyConfig{
		Interval: marketdata.H1, ZScorePeriod: 3, Horizons: []Horizon{h},
	})
	assert.ErrorIs(t, err, ErrMissingInstrument)

	_, err = RunEventStudy(nil, EventStudyConfig{
		Instrument: eurusd, ZScorePeriod: 3, Horizons: []Horizon{h},
	})
	assert.ErrorIs(t, err, ErrMissingInterval)

	_, err = RunEventStudy(nil, EventStudyConfig{
		Instrument: eurusd, Interval: marketdata.H1, ZScorePeriod: 0, Horizons: []Horizon{h},
	})
	assert.ErrorIs(t, err, ErrInvalidZScorePeriod)

	_, err = RunEventStudy(nil, EventStudyConfig{
		Instrument: eurusd, Interval: marketdata.H1, ZScorePeriod: 3, Horizons: nil,
	})
	assert.ErrorIs(t, err, ErrNoHorizons)

	_, err = RunEventStudy(nil, EventStudyConfig{
		Instrument: eurusd, Interval: marketdata.H1, ZScorePeriod: 3,
		Horizons: []Horizon{{Label: "bad", Bars: 0}},
	})
	assert.ErrorIs(t, err, ErrInvalidHorizon)
}

// TestEventStudyConfig_Validate_HorizonIntervalMismatch confirms
// issue #280's provenance requirement is mechanically enforced: an
// hour-labeled Horizon (as NewH1Horizon produces) must actually total
// that many hours at the configured Interval's fixed bar duration.
func TestEventStudyConfig_Validate_HorizonIntervalMismatch(t *testing.T) {
	eurusd := testEURUSD(t)
	h4, err := NewH1Horizon(4)
	require.NoError(t, err)

	// h4.Bars == 4, but H4 bars are 4 hours each, so 4 bars is really
	// 16 hours, not the 4 the label claims.
	_, err = RunEventStudy(nil, EventStudyConfig{
		Instrument: eurusd, Interval: marketdata.H4, ZScorePeriod: 3,
		Horizons: []Horizon{h4},
	})
	assert.ErrorIs(t, err, ErrHorizonIntervalMismatch)

	// The same horizon against its intended H1 cadence is fine.
	_, err = RunEventStudy(nil, EventStudyConfig{
		Instrument: eurusd, Interval: marketdata.H1, ZScorePeriod: 3,
		Horizons: []Horizon{h4},
	})
	assert.NoError(t, err)

	// A calendar-based interval (D1) is deliberately not checked
	// against an hour label — D1 bars have variable duration.
	_, err = RunEventStudy(nil, EventStudyConfig{
		Instrument: eurusd, Interval: marketdata.D1, ZScorePeriod: 3,
		Horizons: []Horizon{h4},
	})
	assert.NoError(t, err)

	// A non-hour-labeled horizon is never checked against Interval.
	custom := Horizon{Label: "4bars", Bars: 4}
	_, err = RunEventStudy(nil, EventStudyConfig{
		Instrument: eurusd, Interval: marketdata.H4, ZScorePeriod: 3,
		Horizons: []Horizon{custom},
	})
	assert.NoError(t, err)
}

func TestRunEventStudy_EmptyBarsProducesEmptyResult(t *testing.T) {
	eurusd := testEURUSD(t)
	h, err := NewH1Horizon(1)
	require.NoError(t, err)

	result, err := RunEventStudy(nil, EventStudyConfig{
		Instrument: eurusd, Interval: marketdata.H1, ZScorePeriod: 3, Horizons: []Horizon{h},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.BarCount)
	assert.Empty(t, result.Observations)
	assert.Empty(t, result.ForwardReturns)
	assert.Empty(t, result.Stats)
	assert.True(t, result.Start.IsZero())
	assert.True(t, result.End.IsZero())
}

// TestRunEventStudy_HandCheckedFixture is the deterministic fixture
// issue #280's acceptance criteria requires. Every Z-score, bucket
// assignment, forward return, and aggregate statistic below is
// verified by hand in the PR description / commit message, not just
// asserted against the implementation's own output.
//
// Closes: [10,10,10,10, 20, 10,10, 10,10, 5], ZScorePeriod=3.
// The rolling window is zero-variance (all 10s) everywhere except
// around the 20 and 5 outliers, so only indices 4, 5, 6, and 9 produce
// a defined Z-score:
//
//	i=4: window [10,10,20] -> Z = +sqrt(2)  (ModeratePositive)
//	i=5: window [10,20,10] -> Z = -1/sqrt(2) (Neutral)
//	i=6: window [20,10,10] -> Z = -1/sqrt(2) (Neutral)
//	i=9: window [10,10,5]  -> Z = -sqrt(2)  (ModerateNegative)
func TestRunEventStudy_HandCheckedFixture(t *testing.T) {
	eurusd := testEURUSD(t)
	bars := barsFromCloses(t, []string{
		"10", "10", "10", "10", "20", "10", "10", "10", "10", "5",
	})

	h1 := Horizon{Label: "1bar", Bars: 1}
	h2 := Horizon{Label: "2bar", Bars: 2}

	result, err := RunEventStudy(bars, EventStudyConfig{
		Instrument:   eurusd,
		Interval:     marketdata.H1,
		ZScorePeriod: 3,
		Horizons:     []Horizon{h1, h2},
	})
	require.NoError(t, err)

	require.Len(t, result.Observations, 4)
	wantIndex := []int{4, 5, 6, 9}
	wantBucket := []Bucket{BucketModeratePositive, BucketNeutral, BucketNeutral, BucketModerateNegative}
	wantZ := []float64{1.4142135623730951, -0.7071067811865476, -0.7071067811865476, -1.4142135623730951}
	for i, obs := range result.Observations {
		assert.Equal(t, wantIndex[i], obs.Index)
		assert.InDelta(t, wantZ[i], obs.Z, 1e-9)
		assert.Equal(t, wantBucket[i], obs.Bucket)
	}

	// i=9 is the last bar (index 9 of 10), so neither horizon (needing
	// bars[10] or bars[11]) has a future bar to label it with — no
	// value is fabricated for it.
	for _, fr := range result.ForwardReturns {
		assert.NotEqual(t, 9, fr.Observation.Index)
	}
	assert.Len(t, result.ForwardReturns, 6) // (i=4,5,6) x (h1,h2)

	// Hand-checked per-horizon returns:
	//   i=4 (close 20 -> 10 at both +1 and +2): return -0.5
	//   i=5 (close 10 -> 10 at both +1 and +2): return 0
	//   i=6 (close 10 -> 10 at both +1 and +2): return 0
	for _, fr := range result.ForwardReturns {
		switch fr.Observation.Index {
		case 4:
			assert.InDelta(t, -0.5, fr.Return, 1e-12)
		case 5, 6:
			assert.InDelta(t, 0.0, fr.Return, 1e-12)
		}
	}

	require.Len(t, result.Stats, 4) // {ModeratePositive,Neutral} x {1bar,2bar}
	for _, s := range result.Stats {
		switch s.Bucket {
		case BucketModeratePositive:
			assert.Equal(t, 1, s.Count)
			assert.InDelta(t, -0.5, s.MeanReturn, 1e-12)
			assert.InDelta(t, -0.5, s.MedianReturn, 1e-12)
			assert.InDelta(t, 0.0, s.StdDevReturn, 1e-12)
			// return (-0.5) and Z (+sqrt2) carry opposite signs: moved
			// toward the mean.
			assert.InDelta(t, 1.0, s.FractionTowardMean, 1e-12)
		case BucketNeutral:
			assert.Equal(t, 2, s.Count)
			assert.InDelta(t, 0.0, s.MeanReturn, 1e-12)
			assert.InDelta(t, 0.0, s.MedianReturn, 1e-12)
			assert.InDelta(t, 0.0, s.StdDevReturn, 1e-12)
			// return is exactly 0 for both observations: no directional
			// move, so neither counts as "toward the mean".
			assert.InDelta(t, 0.0, s.FractionTowardMean, 1e-12)
		default:
			t.Fatalf("unexpected bucket in stats: %v", s.Bucket)
		}
	}

	assert.Equal(t, bars[0].Time, result.Start)
	assert.Equal(t, bars[len(bars)-1].Time, result.End)
	assert.Equal(t, len(bars), result.BarCount)
}

// TestRunEventStudy_ObservationsAreLookaheadFree is issue #280's own
// "no lookahead" requirement, proven structurally rather than merely
// asserted: truncating the input bar slice must never change any
// Observation computed at or before the truncation point, since
// RunEventStudy only ever feeds bars[0:i+1] into the rolling Z-score
// accumulator to build the observation at index i.
func TestRunEventStudy_ObservationsAreLookaheadFree(t *testing.T) {
	eurusd := testEURUSD(t)
	closes := []string{
		"1.1000", "1.1010", "1.0990", "1.1050", "1.0900", "1.1200",
		"1.0800", "1.1300", "1.0700", "1.1400", "1.0600", "1.1500",
	}
	bars := barsFromCloses(t, closes)
	h, err := NewH1Horizon(1)
	require.NoError(t, err)
	cfg := EventStudyConfig{
		Instrument: eurusd, Interval: marketdata.H1, ZScorePeriod: 3, Horizons: []Horizon{h},
	}

	full, err := RunEventStudy(bars, cfg)
	require.NoError(t, err)
	require.NotEmpty(t, full.Observations)

	truncateAt := len(bars) - 2
	truncated, err := RunEventStudy(bars[:truncateAt], cfg)
	require.NoError(t, err)
	require.NotEmpty(t, truncated.Observations)

	// Every observation the truncated run produced must appear
	// identically (same Index, Time, Z, Bucket) in the full run's own
	// observation list.
	fullByIndex := make(map[int]Observation, len(full.Observations))
	for _, obs := range full.Observations {
		fullByIndex[obs.Index] = obs
	}
	for _, obs := range truncated.Observations {
		want, ok := fullByIndex[obs.Index]
		require.True(t, ok, "index %d missing from full run", obs.Index)
		assert.Equal(t, want, obs)
	}
}

// TestRunEventStudy_FiniteClosesNeverError confirms the ordinary path
// (every bar's Close converting to a finite float64) never errors.
// num.Price cannot itself represent NaN/Inf (fixed-point, no such bit
// pattern exists), so RunEventStudy's non-finite guard — inherited
// from indicator.ZScore.Update's own contract — is unreachable from a
// valid num.Price input; that guard's own behavior is covered directly
// by indicator's tests, not re-tested here.
func TestRunEventStudy_FiniteClosesNeverError(t *testing.T) {
	eurusd := testEURUSD(t)
	bars := barsFromCloses(t, []string{"1", "1", "1", "1"})
	h, err := NewH1Horizon(1)
	require.NoError(t, err)
	_, err = RunEventStudy(bars, EventStudyConfig{
		Instrument: eurusd, Interval: marketdata.H1, ZScorePeriod: 3, Horizons: []Horizon{h},
	})
	assert.NoError(t, err)
}

// TestRunEventStudy_ZeroCloseSkipsForwardReturnNotObservation confirms
// a zero observation close (a real, reachable input — marketdata.Bar
// does not itself reject a zero Close) never divides by zero into a
// +Inf/-Inf/NaN forward return. The observation's own Z-score and
// bucket, which do not depend on this bar's close as a denominator,
// are unaffected: index 2 (the zero-close bar) still appears in
// Observations with a defined Z, but contributes no ForwardReturn.
//
// Closes: [10,10,0,10], ZScorePeriod=3.
//
//	i=2: window [10,10,0] -> Z = -sqrt(2) (ModerateNegative)
//	i=3: window [10,0,10] -> Z = +1/sqrt(2) (Neutral)
//
// Both observations' only configured horizon (1 bar ahead) points past
// the end of this 4-bar input, so ForwardReturns is empty regardless —
// this fixture isolates the zero-close divide-by-zero guard from the
// ordinary "future bar doesn't exist" case by using an input short
// enough that no forward return would exist even without a zero close.
func TestRunEventStudy_ZeroCloseSkipsForwardReturnNotObservation(t *testing.T) {
	eurusd := testEURUSD(t)
	bars := barsFromCloses(t, []string{"10", "10", "0", "10"})
	h, err := NewH1Horizon(1)
	require.NoError(t, err)

	result, err := RunEventStudy(bars, EventStudyConfig{
		Instrument: eurusd, Interval: marketdata.H1, ZScorePeriod: 3, Horizons: []Horizon{h},
	})
	require.NoError(t, err)

	require.Len(t, result.Observations, 2)
	assert.Equal(t, 2, result.Observations[0].Index)
	assert.Equal(t, BucketModerateNegative, result.Observations[0].Bucket)
	assert.Equal(t, 3, result.Observations[1].Index)
	assert.Empty(t, result.ForwardReturns)
}

// TestAggregate_KeysByFullHorizonNotLabelAlone is a direct unit test
// of the unexported aggregate helper (this file is in-package) using
// synthetic ForwardReturn values, confirming two distinct horizons
// sharing a Label are kept in separate BucketHorizonStats cells rather
// than being silently merged — the bug Copilot's PR #291 review
// caught (grouping by Label alone).
func TestAggregate_KeysByFullHorizonNotLabelAlone(t *testing.T) {
	same1 := Horizon{Label: "same", Bars: 1}
	same2 := Horizon{Label: "same", Bars: 2}

	frs := []ForwardReturn{
		{
			Observation: Observation{Index: 0, Z: 1.5, Bucket: BucketModeratePositive},
			Horizon:     same1,
			Return:      -0.1,
		},
		{
			Observation: Observation{Index: 1, Z: 1.5, Bucket: BucketModeratePositive},
			Horizon:     same2,
			Return:      -0.2,
		},
	}

	stats := aggregate(frs, []Horizon{same1, same2})
	require.Len(t, stats, 2)
	for _, s := range stats {
		assert.Equal(t, BucketModeratePositive, s.Bucket)
		assert.Equal(t, "same", s.Horizon.Label)
		assert.Equal(t, 1, s.Count)
		switch s.Horizon.Bars {
		case 1:
			assert.InDelta(t, -0.1, s.MeanReturn, 1e-12)
		case 2:
			assert.InDelta(t, -0.2, s.MeanReturn, 1e-12)
		default:
			t.Fatalf("unexpected horizon bars: %d", s.Horizon.Bars)
		}
	}
}

func TestRunEventStudy_DeterministicReplay(t *testing.T) {
	eurusd := testEURUSD(t)
	closes := []string{
		"1.1000", "1.1010", "1.0990", "1.1050", "1.0900", "1.1200",
		"1.0800", "1.1300", "1.0700", "1.1400",
	}
	bars := barsFromCloses(t, closes)
	h, err := NewH1Horizon(2)
	require.NoError(t, err)
	cfg := EventStudyConfig{
		Instrument: eurusd, Interval: marketdata.H1, ZScorePeriod: 4, Horizons: []Horizon{h},
	}

	first, err := RunEventStudy(bars, cfg)
	require.NoError(t, err)
	second, err := RunEventStudy(bars, cfg)
	require.NoError(t, err)

	assert.Equal(t, first, second)
}
