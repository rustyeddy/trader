package datamanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- helpers ----------------------------------------------------------------

func makeCT(open, high, low, close int32, avgSpread int32, unixSec int64) *market.CandleTime {
	return &market.CandleTime{
		Candle: market.Candle{
			Open: market.Price(open), High: market.Price(high),
			Low: market.Price(low), Close: market.Price(close),
			AvgSpread: market.Price(avgSpread),
		},
		Timestamp: market.Timestamp(unixSec),
	}
}

// sliceIter is a minimal CandleIterator backed by a []CandleTime.
type sliceIter struct {
	candles  []market.CandleTime
	idx      int
	err      error
	closeErr error
}

func newSliceIter(candles ...market.CandleTime) market.CandleIterator {
	return &sliceIter{candles: candles, idx: -1}
}

func (it *sliceIter) Next() (market.CandleTime, bool) {
	it.idx++
	if it.idx >= len(it.candles) {
		return market.CandleTime{}, false
	}
	return it.candles[it.idx], true
}
func (it *sliceIter) Err() error   { return it.err }
func (it *sliceIter) Close() error { return it.closeErr }

// ---- priceDistribution ------------------------------------------------------

func TestPriceDistribution_PercentileEmpty(t *testing.T) {
	var d priceDistribution
	assert.Equal(t, 0.0, d.PercentilePips(50, 10))
}

func TestPriceDistribution_PercentileSingle(t *testing.T) {
	var d priceDistribution
	d.Add(market.Price(70))
	assert.Equal(t, 7.0, d.PercentilePips(50, 10))
}

func TestPriceDistribution_PercentileOddLength(t *testing.T) {
	// [1,2,3,4,5] p50 → index 2.0 → 3
	var d priceDistribution
	for _, p := range []market.Price{10, 20, 30, 40, 50} {
		d.Add(p)
	}
	assert.InDelta(t, 3.0, d.PercentilePips(50, 10), 1e-9)
}

func TestPriceDistribution_PercentileInterpolation(t *testing.T) {
	// [0,10] p25 → index 0.25 → 0*(0.75) + 10*(0.25) = 2.5
	var d priceDistribution
	d.Add(market.Price(0))
	d.Add(market.Price(100))
	assert.InDelta(t, 2.5, d.PercentilePips(25, 10), 1e-9)
}

func TestPriceDistribution_PercentileClampsOutOfRange(t *testing.T) {
	var d priceDistribution
	d.Add(market.Price(10))
	d.Add(market.Price(20))
	d.Add(market.Price(30))

	assert.Equal(t, 1.0, d.PercentilePips(-10, 10))
	assert.Equal(t, 3.0, d.PercentilePips(110, 10))
}

// ---- SwingAnalyzer ----------------------------------------------------------

func TestSwingAnalyzer_Empty(t *testing.T) {
	a := NewSwingAnalyzer(market.GetInstrument("EURUSD"))
	stats := a.Stats()
	require.Len(t, stats, 1)
	assert.Equal(t, "Swing Range Distribution", a.Name())
	assert.Equal(t, "count", stats[0].Name)
	assert.Equal(t, "0", stats[0].Value)
}

func TestSwingAnalyzer_NilInstrument(t *testing.T) {
	a := NewSwingAnalyzer(nil)
	a.Update(makeCT(0, 20, 0, 10, 0, 0))
	assertMissingInstrument(t, a.Stats())
}

func TestSwingAnalyzer_ZeroRangeSkipped(t *testing.T) {
	a := NewSwingAnalyzer(market.GetInstrument("EURUSD"))
	a.Update(makeCT(100, 100, 100, 100, 0, 0)) // High==Low, skip
	assert.Len(t, a.Stats(), 1)
	assert.Equal(t, "0", a.Stats()[0].Value)
}

func TestSwingAnalyzer_InvalidOHLCSkipped(t *testing.T) {
	a := NewSwingAnalyzer(market.GetInstrument("EURUSD"))
	a.Update(makeCT(130, 120, 100, 110, 0, 0))
	assert.Equal(t, "0", a.Stats()[0].Value)
}

func TestSwingAnalyzer_SingleCandle(t *testing.T) {
	// High−Low = 20 price units; PriceUnitsPerPip=10 → 2 pips
	a := NewSwingAnalyzer(market.GetInstrument("EURUSD"))
	a.Update(makeCT(100, 120, 100, 110, 0, 0))
	stats := statMap(a.Stats())
	assert.Equal(t, "1", stats["count"])
	assert.Equal(t, "2.0 pips", stats["mean"])
	assert.Equal(t, "2.0 pips", stats["p0"])
	assert.Equal(t, "2.0 pips", stats["max"])
	assert.Equal(t, "2.0 pips", stats["p50"])
}

func TestSwingAnalyzer_MultipleCandles(t *testing.T) {
	a := NewSwingAnalyzer(market.GetInstrument("EURUSD"))
	// ranges in price units: 10, 20, 30 → pips: 1, 2, 3
	a.Update(makeCT(0, 10, 0, 5, 0, 0))
	a.Update(makeCT(0, 20, 0, 10, 0, 0))
	a.Update(makeCT(0, 30, 0, 15, 0, 0))
	stats := statMap(a.Stats())
	assert.Equal(t, "3", stats["count"])
	assert.Equal(t, "2.0 pips", stats["mean"])
	assert.Equal(t, "1.0 pips", stats["p0"])
	assert.Equal(t, "3.0 pips", stats["max"])
	assert.Equal(t, "2.0 pips", stats["p50"])
}

func TestSwingAnalyzer_AggregatesDuplicateRanges(t *testing.T) {
	a := NewSwingAnalyzer(market.GetInstrument("EURUSD"))
	a.Update(makeCT(0, 20, 0, 10, 0, 0))
	a.Update(makeCT(0, 20, 0, 10, 0, 0))
	a.Update(makeCT(0, 20, 0, 10, 0, 0))

	stats := statMap(a.Stats())
	assert.Equal(t, "3", stats["count"])
	assert.Equal(t, "2.0 pips", stats["mean"])
	assert.Len(t, a.ranges.counts, 1)
}

func TestSwingAnalyzer_JPYPipConversion(t *testing.T) {
	a := NewSwingAnalyzer(market.GetInstrument("USDJPY"))
	a.Update(makeCT(0, 2000, 0, 1000, 0, 0))
	stats := statMap(a.Stats())
	assert.Equal(t, "2.0 pips", stats["mean"])
}

// ---- SpreadAnalyzer ---------------------------------------------------------

func TestSpreadAnalyzer_Empty(t *testing.T) {
	a := NewSpreadAnalyzer(market.GetInstrument("EURUSD"))
	stats := a.Stats()
	require.Len(t, stats, 1)
	assert.Equal(t, "Avg Spread Distribution", a.Name())
	assert.Equal(t, "0", stats[0].Value)
}

func TestSpreadAnalyzer_NilInstrument(t *testing.T) {
	a := NewSpreadAnalyzer(nil)
	a.Update(makeCT(100, 110, 90, 105, 10, 0))
	assertMissingInstrument(t, a.Stats())
}

func TestSpreadAnalyzer_ZeroSpreadSkipped(t *testing.T) {
	a := NewSpreadAnalyzer(market.GetInstrument("EURUSD"))
	a.Update(makeCT(100, 110, 90, 105, 0, 0))
	assert.Equal(t, "0", a.Stats()[0].Value)
}

func TestSpreadAnalyzer_InvalidOHLCSkipped(t *testing.T) {
	a := NewSpreadAnalyzer(market.GetInstrument("EURUSD"))
	a.Update(makeCT(100, 110, 90, 120, 10, 0))
	assert.Equal(t, "0", a.Stats()[0].Value)
}

func TestSpreadAnalyzer_Values(t *testing.T) {
	// spreads: 5 and 15 price units at 10 units/pip → 0.5 and 1.5 pips
	a := NewSpreadAnalyzer(market.GetInstrument("EURUSD"))
	a.Update(makeCT(100, 110, 90, 105, 5, 0))
	a.Update(makeCT(100, 110, 90, 105, 15, 0))
	stats := statMap(a.Stats())
	assert.Equal(t, "2", stats["count"])
	assert.Equal(t, "1.00 pips", stats["mean"])
	assert.Equal(t, "0.50 pips", stats["p0"])
	assert.Equal(t, "0.75 pips", stats["p25"])
	assert.Equal(t, "1.00 pips", stats["p50"])
	assert.Equal(t, "1.25 pips", stats["p75"])
	assert.Equal(t, "1.40 pips", stats["tail (p90)"])
	assert.Equal(t, "1.50 pips", stats["max"])
}

func TestSpreadAnalyzer_AggregatesDuplicateSpreads(t *testing.T) {
	a := NewSpreadAnalyzer(market.GetInstrument("EURUSD"))
	a.Update(makeCT(100, 110, 90, 105, 10, 0))
	a.Update(makeCT(100, 110, 90, 105, 10, 0))
	a.Update(makeCT(100, 110, 90, 105, 10, 0))

	stats := statMap(a.Stats())
	assert.Equal(t, "3", stats["count"])
	assert.Equal(t, "1.00 pips", stats["mean"])
	assert.Len(t, a.spreads.counts, 1)
}

func TestSpreadAnalyzer_JPYPipConversion(t *testing.T) {
	a := NewSpreadAnalyzer(market.GetInstrument("USDJPY"))
	a.Update(makeCT(1000, 2000, 1000, 1500, 1000, 0))
	stats := statMap(a.Stats())
	assert.Equal(t, "1.00 pips", stats["mean"])
}

// ---- TrendAnalyzer ----------------------------------------------------------

func TestTrendAnalyzer_Empty(t *testing.T) {
	a := NewTrendAnalyzer()
	stats := a.Stats()
	require.Len(t, stats, 1)
	assert.Equal(t, "Trend Distribution", a.Name())
	assert.Equal(t, "0", stats[0].Value)
}

func TestTrendAnalyzer_ZeroRangeSkipped(t *testing.T) {
	a := NewTrendAnalyzer()
	a.Update(makeCT(100, 100, 100, 100, 0, 0))
	assert.Equal(t, 0, a.total)
}

func TestTrendAnalyzer_InvalidOHLCSkipped(t *testing.T) {
	a := NewTrendAnalyzer()
	a.Update(makeCT(100, 110, 90, 120, 0, 0))
	assert.Equal(t, 0, a.total)
}

func TestTrendAnalyzer_AllTrending(t *testing.T) {
	// body=range → ratio=1.0 → trending
	a := NewTrendAnalyzer()
	a.Update(makeCT(0, 10, 0, 10, 0, 0)) // Open=0 Close=10 High=10 Low=0
	stats := statMap(a.Stats())
	assert.Equal(t, "1", stats["count"])
	assert.Equal(t, "1.000", stats["mean body/range"])
	assert.Contains(t, stats["trending (>0.6)"], "100.0%")
	assert.Contains(t, stats["consolidating (<0.3)"], "0.0%")
}

func TestTrendAnalyzer_AllConsolidating(t *testing.T) {
	// body=0 → ratio=0 → consolidating
	a := NewTrendAnalyzer()
	a.Update(makeCT(5, 10, 0, 5, 0, 0)) // Open==Close, ratio=0
	stats := statMap(a.Stats())
	assert.Contains(t, stats["consolidating (<0.3)"], "100.0%")
	assert.Contains(t, stats["trending (>0.6)"], "0.0%")
}

func TestTrendAnalyzer_Mixed(t *testing.T) {
	a := NewTrendAnalyzer()
	// ratio=1.0 → trending
	a.Update(makeCT(0, 10, 0, 10, 0, 0))
	// ratio=0 → consolidating
	a.Update(makeCT(5, 10, 0, 5, 0, 0))
	// ratio=0.5 → mixed
	a.Update(makeCT(0, 10, 0, 5, 0, 0))
	stats := statMap(a.Stats())
	assert.Equal(t, "3", stats["count"])
	assert.Contains(t, stats["trending (>0.6)"], "33.3%")
	assert.Contains(t, stats["consolidating (<0.3)"], "33.3%")
	assert.Contains(t, stats["mixed (0.3–0.6)"], "33.3%")
}

func TestTrendAnalyzer_ThresholdBoundaries(t *testing.T) {
	a := NewTrendAnalyzer()
	a.Update(makeCT(0, 10, 0, 6, 0, 0))     // exactly 0.6 → mixed
	a.Update(makeCT(0, 1000, 0, 601, 0, 0)) // just above 0.6 → trending
	a.Update(makeCT(0, 10, 0, 3, 0, 0))     // exactly 0.3 → mixed
	a.Update(makeCT(0, 1000, 0, 299, 0, 0)) // just below 0.3 → consolidating
	stats := statMap(a.Stats())
	assert.Contains(t, stats["trending (>0.6)"], "25.0%")
	assert.Contains(t, stats["mixed (0.3–0.6)"], "50.0%")
	assert.Contains(t, stats["consolidating (<0.3)"], "25.0%")
}

func TestTrendAnalyzer_MeanUsesHigherPrecision(t *testing.T) {
	a := NewTrendAnalyzer()
	a.Update(makeCT(0, 3, 0, 2, 0, 0))
	stats := statMap(a.Stats())
	assert.Equal(t, "0.667", stats["mean body/range"])
}

// ---- SessionAnalyzer --------------------------------------------------------

func TestSessionAnalyzer_Empty(t *testing.T) {
	a := NewSessionAnalyzer(market.GetInstrument("EURUSD"))
	assert.Empty(t, a.Stats())
}

func TestSessionAnalyzer_NilInstrument(t *testing.T) {
	a := NewSessionAnalyzer(nil)
	a.Update(makeCT(0, 20, 0, 10, 0, 0))
	assertMissingInstrument(t, a.Stats())
}

func TestSessionAnalyzer_ZeroRangeSkipped(t *testing.T) {
	a := NewSessionAnalyzer(market.GetInstrument("EURUSD"))
	a.Update(makeCT(100, 100, 100, 100, 0, 0))
	assert.Empty(t, a.Stats())
}

func TestSessionAnalyzer_InvalidOHLCSkipped(t *testing.T) {
	a := NewSessionAnalyzer(market.GetInstrument("EURUSD"))
	a.Update(makeCT(80, 110, 90, 100, 0, 0))
	assert.Empty(t, a.Stats())
}

func TestSessionAnalyzer_BucketsByHour(t *testing.T) {
	a := NewSessionAnalyzer(market.GetInstrument("EURUSD"))
	// UTC 08:00 on 2024-01-01
	h8 := time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC).Unix()
	// UTC 14:00 on 2024-01-01
	h14 := time.Date(2024, 1, 1, 14, 0, 0, 0, time.UTC).Unix()

	// hour 8: range=20 → 2 pips
	a.Update(makeCT(0, 20, 0, 10, 0, h8))
	// hour 14: two candles, ranges 10 and 30 → avg 2.0 pips
	a.Update(makeCT(0, 10, 0, 5, 0, h14))
	a.Update(makeCT(0, 30, 0, 15, 0, h14))

	stats := statMap(a.Stats())
	assert.Contains(t, stats["08:00 UTC"], "count=1")
	assert.Contains(t, stats["08:00 UTC"], "2.0 pips")
	assert.Contains(t, stats["14:00 UTC"], "count=2")
	assert.Contains(t, stats["14:00 UTC"], "2.0 pips")
}

func TestSessionAnalyzer_JPYPipConversion(t *testing.T) {
	a := NewSessionAnalyzer(market.GetInstrument("USDJPY"))
	h8 := time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC).Unix()
	a.Update(makeCT(0, 2000, 0, 1000, 0, h8))
	stats := statMap(a.Stats())
	assert.Contains(t, stats["08:00 UTC"], "2.0 pips")
}

// ---- RunAnalysis ------------------------------------------------------------

func TestRunAnalysis_WalksAllCandles(t *testing.T) {
	swing := NewSwingAnalyzer(market.GetInstrument("EURUSD"))
	candles := []market.CandleTime{
		*makeCT(0, 10, 0, 5, 0, 0),
		*makeCT(0, 20, 0, 10, 0, 3600),
		*makeCT(0, 30, 0, 15, 0, 7200),
	}
	itr := newSliceIter(candles...)
	err := RunAnalysis(context.Background(), itr, []Analyzer{swing})
	require.NoError(t, err)
	stats := statMap(swing.Stats())
	assert.Equal(t, "3", stats["count"])
}

func TestRunAnalysis_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled immediately

	swing := NewSwingAnalyzer(market.GetInstrument("EURUSD"))
	itr := &sliceIter{candles: []market.CandleTime{*makeCT(0, 10, 0, 5, 0, 0)}, idx: -1}
	err := RunAnalysis(ctx, itr, []Analyzer{swing})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, -1, itr.idx)
}

func TestRunAnalysis_ReturnsCloseError(t *testing.T) {
	sentinel := errors.New("close failed")
	itr := &sliceIter{idx: -1, closeErr: sentinel}

	err := RunAnalysis(context.Background(), itr, nil)

	assert.ErrorIs(t, err, sentinel)
}

func TestRunAnalysis_ReturnsIteratorError(t *testing.T) {
	sentinel := errors.New("iterator failed")
	itr := &sliceIter{idx: -1, err: sentinel}

	err := RunAnalysis(context.Background(), itr, nil)

	assert.ErrorIs(t, err, sentinel)
}

func TestRunAnalysis_NilIterator(t *testing.T) {
	err := RunAnalysis(context.Background(), nil, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil candle iterator")
}

// ---- Pips field -------------------------------------------------------------

func TestSwingAnalyzer_PipsFieldSet(t *testing.T) {
	a := NewSwingAnalyzer(market.GetInstrument("EURUSD"))
	a.Update(makeCT(0, 20, 0, 10, 0, 0)) // range=20 price units = 2 pips
	for _, s := range a.Stats() {
		if s.Name == "count" {
			assert.Equal(t, 0.0, s.Pips)
		} else {
			assert.Equal(t, 2.0, s.Pips, "stat %q should have Pips=2.0", s.Name)
		}
	}
}

func TestSpreadAnalyzer_PipsFieldSet(t *testing.T) {
	a := NewSpreadAnalyzer(market.GetInstrument("EURUSD"))
	a.Update(makeCT(0, 20, 0, 10, 10, 0)) // spread=10 price units = 1 pip
	for _, s := range a.Stats() {
		if s.Name == "count" {
			assert.Equal(t, 0.0, s.Pips)
		} else {
			assert.Equal(t, 1.0, s.Pips, "stat %q should have Pips=1.0", s.Name)
		}
	}
}

func TestSessionAnalyzer_PipsFieldSet(t *testing.T) {
	a := NewSessionAnalyzer(market.GetInstrument("EURUSD"))
	h8 := time.Date(2024, 1, 1, 8, 0, 0, 0, time.UTC).Unix()
	a.Update(makeCT(0, 20, 0, 10, 0, h8)) // range=20 price units = 2 pips
	stats := a.Stats()
	require.Len(t, stats, 1)
	assert.Equal(t, 2.0, stats[0].Pips)
}

func TestTrendAnalyzer_PipsAlwaysZero(t *testing.T) {
	a := NewTrendAnalyzer()
	a.Update(makeCT(0, 10, 0, 10, 0, 0))
	for _, s := range a.Stats() {
		assert.Equal(t, 0.0, s.Pips, "TrendAnalyzer stat %q should never set Pips", s.Name)
	}
}

// ---- helper -----------------------------------------------------------------

// statMap converts []Stat to a map for easy assertions.
func statMap(stats []Stat) map[string]string {
	m := make(map[string]string, len(stats))
	for _, s := range stats {
		m[s.Name] = s.Value
	}
	return m
}

func assertMissingInstrument(t *testing.T, stats []Stat) {
	t.Helper()
	require.Len(t, stats, 1)
	assert.Equal(t, "error", stats[0].Name)
	assert.Equal(t, "missing instrument", stats[0].Value)
}
