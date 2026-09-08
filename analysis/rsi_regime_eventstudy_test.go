package analysis

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testSPY returns a stable instrument.ID for use as test provenance —
// RunRSIRegimeEventStudy never inspects it beyond IsZero, so any real
// instrument works.
func testSPY(t *testing.T) instrument.ID {
	t.Helper()
	spy, err := instrument.NewETF("ARCA", "SPY")
	require.NoError(t, err)
	return spy.ID()
}

// rsiBarsFromCloses builds a minimal []marketdata.Bar from a slice of
// close prices (as decimal strings), one day apart starting at a fixed
// reference time. Only Close and Time are populated — the other OHLC
// fields are irrelevant to RunRSIRegimeEventStudy, which never reads
// them.
func rsiBarsFromCloses(t *testing.T, closes []string) []marketdata.Bar {
	t.Helper()
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := make([]marketdata.Bar, len(closes))
	for i, c := range closes {
		bars[i] = marketdata.Bar{
			Time:  start.AddDate(0, 0, i),
			Close: num.MustParsePrice(c),
		}
	}
	return bars
}

func baseRSIRegimeConfig(t *testing.T, horizons ...Horizon) RSIRegimeEventStudyConfig {
	t.Helper()
	return RSIRegimeEventStudyConfig{
		Instrument:      testSPY(t),
		Interval:        marketdata.D1,
		RSIPeriod:       2,
		EMAPeriod:       2,
		Horizons:        horizons,
		MinObservations: 1,
		Bootstrap:       BootstrapConfig{Resamples: 200, Seed1: 42, Seed2: 7},
	}
}

func day(days int) Horizon {
	h, err := NewDayHorizon(days)
	if err != nil {
		panic(err)
	}
	return h
}

func TestRSIRegimeEventStudyConfig_Validate(t *testing.T) {
	spy := testSPY(t)
	h1 := day(1)
	validBootstrap := BootstrapConfig{Resamples: 100, Seed1: 1, Seed2: 1}

	_, err := RunRSIRegimeEventStudy(nil, RSIRegimeEventStudyConfig{
		Interval: marketdata.D1, RSIPeriod: 2, EMAPeriod: 200,
		Horizons: []Horizon{h1}, MinObservations: 30, Bootstrap: validBootstrap,
	})
	assert.ErrorIs(t, err, ErrMissingInstrument)

	_, err = RunRSIRegimeEventStudy(nil, RSIRegimeEventStudyConfig{
		Instrument: spy, RSIPeriod: 2, EMAPeriod: 200,
		Horizons: []Horizon{h1}, MinObservations: 30, Bootstrap: validBootstrap,
	})
	assert.ErrorIs(t, err, ErrMissingInterval)

	_, err = RunRSIRegimeEventStudy(nil, RSIRegimeEventStudyConfig{
		Instrument: spy, Interval: marketdata.D1, RSIPeriod: 0, EMAPeriod: 200,
		Horizons: []Horizon{h1}, MinObservations: 30, Bootstrap: validBootstrap,
	})
	assert.ErrorIs(t, err, ErrInvalidRSIPeriod)

	_, err = RunRSIRegimeEventStudy(nil, RSIRegimeEventStudyConfig{
		Instrument: spy, Interval: marketdata.D1, RSIPeriod: 2, EMAPeriod: 0,
		Horizons: []Horizon{h1}, MinObservations: 30, Bootstrap: validBootstrap,
	})
	assert.ErrorIs(t, err, ErrInvalidEMAPeriod)

	_, err = RunRSIRegimeEventStudy(nil, RSIRegimeEventStudyConfig{
		Instrument: spy, Interval: marketdata.D1, RSIPeriod: 2, EMAPeriod: 200,
		Horizons: nil, MinObservations: 30, Bootstrap: validBootstrap,
	})
	assert.ErrorIs(t, err, ErrNoHorizons)

	_, err = RunRSIRegimeEventStudy(nil, RSIRegimeEventStudyConfig{
		Instrument: spy, Interval: marketdata.D1, RSIPeriod: 2, EMAPeriod: 200,
		Horizons: []Horizon{{Label: "bad", Bars: 0}}, MinObservations: 30, Bootstrap: validBootstrap,
	})
	assert.ErrorIs(t, err, ErrInvalidHorizon)

	_, err = RunRSIRegimeEventStudy(nil, RSIRegimeEventStudyConfig{
		Instrument: spy, Interval: marketdata.D1, RSIPeriod: 2, EMAPeriod: 200,
		Horizons: []Horizon{h1}, MinObservations: 0, Bootstrap: validBootstrap,
	})
	assert.ErrorIs(t, err, ErrInvalidMinObservations)

	_, err = RunRSIRegimeEventStudy(nil, RSIRegimeEventStudyConfig{
		Instrument: spy, Interval: marketdata.D1, RSIPeriod: 2, EMAPeriod: 200,
		Horizons: []Horizon{h1}, MinObservations: 30, Bootstrap: BootstrapConfig{Resamples: 0},
	})
	assert.ErrorIs(t, err, ErrInvalidBootstrapResamples)
}

func TestRunRSIRegimeEventStudy_EmptyBars(t *testing.T) {
	cfg := baseRSIRegimeConfig(t, day(1))
	result, err := RunRSIRegimeEventStudy(nil, cfg)
	require.NoError(t, err)
	assert.Zero(t, result.BarCount)
	assert.Empty(t, result.Observations)
	assert.Empty(t, result.ForwardReturns)
	assert.True(t, result.Start.IsZero())
	assert.True(t, result.End.IsZero())
}

// TestRunRSIRegimeEventStudy_ObservationsMatchHandCalculation is the
// primary "independently calculated expected values" check issue #319
// requires. It reuses the exact price sequence and Wilder RSI(2) hand
// calculation already independently derived and verified in
// indicator/rsi_test.go's own
// TestRSI_Period2MixedSequenceMatchesHandCalculation, and additionally
// hand-derives the matching EMA(2) values (conventional SMA-seeded EMA
// recurrence, indicator.EMA's own documented algorithm) and the
// resulting Close-vs-EMA regime at each observation:
//
//	prices: 100  102  101  105  103  103  108
//	RSI(2) ready from index 2 (period+1=3 samples): 200/3, 1000/11,
//	  1000/19, 1000/19, 1000/11 (indices 2..6)
//	EMA(2) ready from index 1 (period=2 samples), SMA-seeded at 101:
//	  idx1=101, idx2=101, idx3=311/3, idx4=929/9, idx5=2783/27,
//	  idx6=106.358024691...
//
// Both indicators are Ready together starting at index 2 (RSI is the
// binding constraint), giving 5 Observations (indices 2-6):
//
//	idx  close  RSI        EMA          regime (close>ema?)
//	2    101    200/3      101          non_positive (equal, not >)
//	3    105    1000/11    311/3        positive
//	4    103    1000/19    929/9        non_positive
//	5    103    1000/19    2783/27      non_positive
//	6    108    1000/11    106.358...   positive
//
// Every RSI value here happens to land in RSIBucketB6 ([50, 100]) for
// this particular price path; bucket-boundary coverage across all six
// buckets is exercised directly by TestClassifyRSIBucket_Boundaries,
// not re-derived here.
func TestRunRSIRegimeEventStudy_ObservationsMatchHandCalculation(t *testing.T) {
	const tol = 1e-9
	bars := rsiBarsFromCloses(t, []string{"100", "102", "101", "105", "103", "103", "108"})
	cfg := baseRSIRegimeConfig(t, day(1), day(2))

	result, err := RunRSIRegimeEventStudy(bars, cfg)
	require.NoError(t, err)

	require.Len(t, result.Observations, 5)

	wantRSI := []float64{200.0 / 3, 1000.0 / 11, 1000.0 / 19, 1000.0 / 19, 1000.0 / 11}
	wantEMA := []float64{101, 311.0 / 3, 929.0 / 9, 2783.0 / 27, 108*(2.0/3) + (2783.0/27)*(1.0/3)}
	wantRegime := []Regime{RegimeNonPositive, RegimePositive, RegimeNonPositive, RegimeNonPositive, RegimePositive}
	wantIndex := []int{2, 3, 4, 5, 6}

	for i, obs := range result.Observations {
		assert.Equal(t, wantIndex[i], obs.Index, "observation %d index", i)
		assert.InDelta(t, wantRSI[i], obs.RSI, tol, "observation %d RSI", i)
		assert.Equal(t, RSIBucketB6, obs.Bucket, "observation %d bucket", i)
		assert.Equal(t, wantRegime[i], obs.Regime, "observation %d regime", i)
		_ = wantEMA // EMA itself is not exported on RSIObservation; regime is its observable projection.
	}

	// Forward returns: bars has 7 entries (indices 0-6). horizon 1d and
	// 2d are only computed when the future index exists within bars.
	//
	//	idx2 (close=101): 1d->idx3(105)=(105-101)/101; 2d->idx4(103)=(103-101)/101
	//	idx3 (close=105): 1d->idx4(103)=(103-105)/105; 2d->idx5(103)=(103-105)/105
	//	idx4 (close=103): 1d->idx5(103)=0;             2d->idx6(108)=(108-103)/103
	//	idx5 (close=103): 1d->idx6(108)=(108-103)/103; 2d->idx7 does not exist, excluded
	//	idx6 (close=108): 1d->idx7 does not exist;     2d->idx8 does not exist, both excluded
	require.Len(t, result.ForwardReturns, 7, "2+2+2+1+0 = 7")

	byIndexAndHorizon := map[int]map[string]float64{}
	for _, fr := range result.ForwardReturns {
		if byIndexAndHorizon[fr.Observation.Index] == nil {
			byIndexAndHorizon[fr.Observation.Index] = map[string]float64{}
		}
		byIndexAndHorizon[fr.Observation.Index][fr.Horizon.Label] = fr.Return
	}

	assert.InDelta(t, (105.0-101)/101, byIndexAndHorizon[2]["1d"], tol)
	assert.InDelta(t, (103.0-101)/101, byIndexAndHorizon[2]["2d"], tol)
	assert.InDelta(t, (103.0-105)/105, byIndexAndHorizon[3]["1d"], tol)
	assert.InDelta(t, (103.0-105)/105, byIndexAndHorizon[3]["2d"], tol)
	assert.InDelta(t, 0.0, byIndexAndHorizon[4]["1d"], tol)
	assert.InDelta(t, (108.0-103)/103, byIndexAndHorizon[4]["2d"], tol)
	assert.InDelta(t, (108.0-103)/103, byIndexAndHorizon[5]["1d"], tol)
	_, has2d5 := byIndexAndHorizon[5]["2d"]
	assert.False(t, has2d5, "idx5's 2d horizon must be excluded: its future bar is outside the supplied partition")
	assert.Empty(t, byIndexAndHorizon[6], "idx6 has no forward returns at all: both horizons fall outside the partition")
}

// TestRunRSIRegimeEventStudy_PartitionEndExclusion isolates the same
// exclusion mechanism as a standalone assertion, mirroring the issue's
// own explicit "forward labels must never cross the partition boundary"
// requirement: a bars slice ending mid-sequence must never fabricate a
// forward return by reaching past its own end.
func TestRunRSIRegimeEventStudy_PartitionEndExclusion(t *testing.T) {
	bars := rsiBarsFromCloses(t, []string{"100", "102", "101", "105", "103"})
	cfg := baseRSIRegimeConfig(t, day(1), day(5))

	result, err := RunRSIRegimeEventStudy(bars, cfg)
	require.NoError(t, err)

	for _, fr := range result.ForwardReturns {
		future := fr.Observation.Index + fr.Horizon.Bars
		assert.Less(t, future, len(bars), "forward return must never reference a bar outside the supplied partition")
	}
	// The 5-day horizon can never be satisfied by a 5-bar partition
	// (max index 4, plus 5 bars would need index 9) — every observation
	// must exclude it.
	for _, fr := range result.ForwardReturns {
		assert.NotEqual(t, "5d", fr.Horizon.Label)
	}
}

// TestRunRSIRegimeEventStudy_PositiveRegimeFiltering proves
// PositiveRegimeStats only ever aggregates ForwardReturns whose
// Observation.Regime is RegimePositive, while AllRegimeStats aggregates
// every regime — the frozen protocol's own "must never be blended"
// requirement, checked directly against the hand-calculated fixture
// above (index 3 is the only RegimePositive observation with any
// ForwardReturns).
func TestRunRSIRegimeEventStudy_PositiveRegimeFiltering(t *testing.T) {
	const tol = 1e-9
	bars := rsiBarsFromCloses(t, []string{"100", "102", "101", "105", "103", "103", "108"})
	cfg := baseRSIRegimeConfig(t, day(1))

	result, err := RunRSIRegimeEventStudy(bars, cfg)
	require.NoError(t, err)

	require.Len(t, result.PositiveRegimeStats, 1, "only bucket B6/1d has any positive-regime forward returns")
	posCell := result.PositiveRegimeStats[0]
	assert.Equal(t, RSIBucketB6, posCell.Bucket)
	assert.Equal(t, "1d", posCell.Horizon.Label)
	require.Equal(t, 1, posCell.Count, "only index 3's observation is RegimePositive with a 1d forward return")
	assert.InDelta(t, (103.0-105)/105, posCell.MeanReturn, tol)

	require.Len(t, result.AllRegimeStats, 1, "every 1d forward return shares bucket B6 in this fixture")
	allCell := result.AllRegimeStats[0]
	assert.Equal(t, RSIBucketB6, allCell.Bucket)
	assert.Equal(t, 4, allCell.Count, "indices 2,3,4,5 all have a 1d forward return")
}

func TestRunRSIRegimeEventStudy_MinObservationsInsufficientFlag(t *testing.T) {
	bars := rsiBarsFromCloses(t, []string{"100", "102", "101", "105", "103", "103", "108"})

	cfgStrict := baseRSIRegimeConfig(t, day(1))
	cfgStrict.MinObservations = 5
	resultStrict, err := RunRSIRegimeEventStudy(bars, cfgStrict)
	require.NoError(t, err)
	require.Len(t, resultStrict.AllRegimeStats, 1)
	assert.True(t, resultStrict.AllRegimeStats[0].Insufficient, "count 4 < MinObservations 5")

	cfgLoose := baseRSIRegimeConfig(t, day(1))
	cfgLoose.MinObservations = 4
	resultLoose, err := RunRSIRegimeEventStudy(bars, cfgLoose)
	require.NoError(t, err)
	require.Len(t, resultLoose.AllRegimeStats, 1)
	assert.False(t, resultLoose.AllRegimeStats[0].Insufficient, "count 4 >= MinObservations 4")
}

// TestRunRSIRegimeEventStudy_DeterministicBootstrap proves the
// bootstrap confidence interval is reproducible: two independent runs
// of the identical config against identical bars produce bit-identical
// CILower/CIUpper for every cell.
func TestRunRSIRegimeEventStudy_DeterministicBootstrap(t *testing.T) {
	bars := rsiBarsFromCloses(t, []string{"100", "102", "101", "105", "103", "103", "108", "110", "107", "112"})
	cfg := baseRSIRegimeConfig(t, day(1), day(2), day(3))
	cfg.Bootstrap = BootstrapConfig{Resamples: 500, Seed1: 123, Seed2: 456}

	result1, err := RunRSIRegimeEventStudy(bars, cfg)
	require.NoError(t, err)
	result2, err := RunRSIRegimeEventStudy(bars, cfg)
	require.NoError(t, err)

	require.Equal(t, len(result1.AllRegimeStats), len(result2.AllRegimeStats))
	for i := range result1.AllRegimeStats {
		assert.Equal(t, result1.AllRegimeStats[i].CILower, result2.AllRegimeStats[i].CILower)
		assert.Equal(t, result1.AllRegimeStats[i].CIUpper, result2.AllRegimeStats[i].CIUpper)
	}
}

// TestRSIRegimeEventStudyResult_JSONRoundTrip proves the provenance-
// carrying Result serializes and deserializes without error — issue
// #319's own "provenance/output serialization" verification item.
func TestRSIRegimeEventStudyResult_JSONRoundTrip(t *testing.T) {
	bars := rsiBarsFromCloses(t, []string{"100", "102", "101", "105", "103", "103", "108"})
	cfg := baseRSIRegimeConfig(t, day(1), day(2))

	result, err := RunRSIRegimeEventStudy(bars, cfg)
	require.NoError(t, err)

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var got RSIRegimeEventStudyResult
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, result.BarCount, got.BarCount)
	require.Len(t, got.AllRegimeStats, len(result.AllRegimeStats))
	for i := range result.AllRegimeStats {
		assert.Equal(t, result.AllRegimeStats[i].Bucket, got.AllRegimeStats[i].Bucket)
		assert.Equal(t, result.AllRegimeStats[i].Count, got.AllRegimeStats[i].Count)
	}
}

func TestNewDayHorizon(t *testing.T) {
	h, err := NewDayHorizon(5)
	require.NoError(t, err)
	assert.Equal(t, "5d", h.Label)
	assert.Equal(t, 5, h.Bars)

	_, err = NewDayHorizon(0)
	assert.ErrorIs(t, err, ErrInvalidHorizon)
}

func TestEQR01Horizons(t *testing.T) {
	horizons := EQR01Horizons()
	require.Len(t, horizons, 5)
	wantBars := []int{1, 2, 3, 5, 10}
	for i, h := range horizons {
		assert.Equal(t, wantBars[i], h.Bars)
	}
}
