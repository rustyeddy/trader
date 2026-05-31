package trader

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- helpers ----------------------------------------------------------------

func makeCT(open, high, low, close int32, avgSpread int32, unixSec int64) *CandleTime {
	return &CandleTime{
		Candle: Candle{
			Open: Price(open), High: Price(high),
			Low: Price(low), Close: Price(close),
			AvgSpread: Price(avgSpread),
		},
		Timestamp: Timestamp(unixSec),
	}
}

// sliceIter is a minimal CandleIterator backed by a []CandleTime.
type sliceIter struct {
	candles []CandleTime
	idx     int
}

func newSliceIter(candles ...CandleTime) CandleIterator {
	return &sliceIter{candles: candles, idx: -1}
}

func (it *sliceIter) Next() bool {
	it.idx++
	return it.idx < len(it.candles)
}
func (it *sliceIter) CandleTime() CandleTime { return it.candles[it.idx] }
func (it *sliceIter) Err() error             { return nil }
func (it *sliceIter) Close() error           { return nil }

// ---- percentile -------------------------------------------------------------

func TestPercentile_Empty(t *testing.T) {
	assert.Equal(t, 0.0, percentile(nil, 50))
}

func TestPercentile_Single(t *testing.T) {
	assert.Equal(t, 7.0, percentile([]float64{7}, 50))
}

func TestPercentile_OddLength(t *testing.T) {
	// [1,2,3,4,5] p50 → index 2.0 → 3
	assert.InDelta(t, 3.0, percentile([]float64{1, 2, 3, 4, 5}, 50), 1e-9)
}

func TestPercentile_Interpolation(t *testing.T) {
	// [0,10] p25 → index 0.25 → 0*(0.75) + 10*(0.25) = 2.5
	assert.InDelta(t, 2.5, percentile([]float64{0, 10}, 25), 1e-9)
}

// ---- SwingAnalyzer ----------------------------------------------------------

func TestSwingAnalyzer_Empty(t *testing.T) {
	a := NewSwingAnalyzer(GetInstrument("EURUSD"))
	stats := a.Stats()
	require.Len(t, stats, 1)
	assert.Equal(t, "count", stats[0].Name)
	assert.Equal(t, "0", stats[0].Value)
}

func TestSwingAnalyzer_ZeroRangeSkipped(t *testing.T) {
	a := NewSwingAnalyzer(GetInstrument("EURUSD"))
	a.Update(makeCT(100, 100, 100, 100, 0, 0)) // High==Low, skip
	assert.Len(t, a.Stats(), 1)
	assert.Equal(t, "0", a.Stats()[0].Value)
}

func TestSwingAnalyzer_SingleCandle(t *testing.T) {
	// High−Low = 20 price units; unitsPerPip=10 → 2 pips
	a := NewSwingAnalyzer(GetInstrument("EURUSD"))
	a.Update(makeCT(100, 120, 100, 110, 0, 0))
	stats := statMap(a.Stats())
	assert.Equal(t, "1", stats["count"])
	assert.Equal(t, "2.0 pips", stats["mean"])
	assert.Equal(t, "2.0 pips", stats["min"])
	assert.Equal(t, "2.0 pips", stats["max"])
	assert.Equal(t, "2.0 pips", stats["p50"])
}

func TestSwingAnalyzer_MultipleCandles(t *testing.T) {
	a := NewSwingAnalyzer(GetInstrument("EURUSD"))
	// ranges in price units: 10, 20, 30 → pips: 1, 2, 3
	a.Update(makeCT(0, 10, 0, 5, 0, 0))
	a.Update(makeCT(0, 20, 0, 10, 0, 0))
	a.Update(makeCT(0, 30, 0, 15, 0, 0))
	stats := statMap(a.Stats())
	assert.Equal(t, "3", stats["count"])
	assert.Equal(t, "2.0 pips", stats["mean"])
	assert.Equal(t, "1.0 pips", stats["min"])
	assert.Equal(t, "3.0 pips", stats["max"])
	assert.Equal(t, "2.0 pips", stats["p50"])
}

// ---- SpreadAnalyzer ---------------------------------------------------------

func TestSpreadAnalyzer_Empty(t *testing.T) {
	a := NewSpreadAnalyzer(GetInstrument("EURUSD"))
	stats := a.Stats()
	require.Len(t, stats, 1)
	assert.Equal(t, "0", stats[0].Value)
}

func TestSpreadAnalyzer_ZeroSpreadSkipped(t *testing.T) {
	a := NewSpreadAnalyzer(GetInstrument("EURUSD"))
	a.Update(makeCT(100, 110, 90, 105, 0, 0))
	assert.Equal(t, "0", a.Stats()[0].Value)
}

func TestSpreadAnalyzer_Values(t *testing.T) {
	// spreads: 5 and 15 price units at 10 units/pip → 0.5 and 1.5 pips
	a := NewSpreadAnalyzer(GetInstrument("EURUSD"))
	a.Update(makeCT(100, 110, 90, 105, 5, 0))
	a.Update(makeCT(100, 110, 90, 105, 15, 0))
	stats := statMap(a.Stats())
	assert.Equal(t, "2", stats["count (with spread)"])
	assert.Equal(t, "1.00 pips", stats["mean"])
	assert.Equal(t, "1.50 pips", stats["max"])
}

// ---- TrendAnalyzer ----------------------------------------------------------

func TestTrendAnalyzer_Empty(t *testing.T) {
	a := NewTrendAnalyzer()
	stats := a.Stats()
	require.Len(t, stats, 1)
	assert.Equal(t, "0", stats[0].Value)
}

func TestTrendAnalyzer_ZeroRangeSkipped(t *testing.T) {
	a := NewTrendAnalyzer()
	a.Update(makeCT(100, 100, 100, 100, 0, 0))
	assert.Equal(t, 0, a.total)
}

func TestTrendAnalyzer_AllTrending(t *testing.T) {
	// body=range → ratio=1.0 → trending
	a := NewTrendAnalyzer()
	a.Update(makeCT(0, 10, 0, 10, 0, 0)) // Open=0 Close=10 High=10 Low=0
	stats := statMap(a.Stats())
	assert.Equal(t, "1", stats["count"])
	assert.Equal(t, "1.000", stats["mean body/range"])
	assert.Contains(t, stats["trending  (>0.6)"], "100.0%")
	assert.Contains(t, stats["consolidating  (<0.3)"], "0.0%")
}

func TestTrendAnalyzer_AllConsolidating(t *testing.T) {
	// body=0 → ratio=0 → consolidating
	a := NewTrendAnalyzer()
	a.Update(makeCT(5, 10, 0, 5, 0, 0)) // Open==Close, ratio=0
	stats := statMap(a.Stats())
	assert.Contains(t, stats["consolidating  (<0.3)"], "100.0%")
	assert.Contains(t, stats["trending  (>0.6)"], "0.0%")
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
	assert.Contains(t, stats["trending  (>0.6)"], "33.3%")
	assert.Contains(t, stats["consolidating  (<0.3)"], "33.3%")
	assert.Contains(t, stats["mixed  (0.3–0.6)"], "33.3%")
}

// ---- SessionAnalyzer --------------------------------------------------------

func TestSessionAnalyzer_Empty(t *testing.T) {
	a := NewSessionAnalyzer(GetInstrument("EURUSD"))
	assert.Empty(t, a.Stats())
}

func TestSessionAnalyzer_ZeroRangeSkipped(t *testing.T) {
	a := NewSessionAnalyzer(GetInstrument("EURUSD"))
	a.Update(makeCT(100, 100, 100, 100, 0, 0))
	assert.Empty(t, a.Stats())
}

func TestSessionAnalyzer_BucketsByHour(t *testing.T) {
	a := NewSessionAnalyzer(GetInstrument("EURUSD"))
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

// ---- RunAnalysis ------------------------------------------------------------

func TestRunAnalysis_WalksAllCandles(t *testing.T) {
	swing := NewSwingAnalyzer(GetInstrument("EURUSD"))
	candles := []CandleTime{
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

	swing := NewSwingAnalyzer(GetInstrument("EURUSD"))
	itr := newSliceIter(*makeCT(0, 10, 0, 5, 0, 0))
	err := RunAnalysis(ctx, itr, []Analyzer{swing})
	assert.ErrorIs(t, err, context.Canceled)
}

// ---- Pips field -------------------------------------------------------------

func TestSwingAnalyzer_PipsFieldSet(t *testing.T) {
	a := NewSwingAnalyzer(GetInstrument("EURUSD"))
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
	a := NewSpreadAnalyzer(GetInstrument("EURUSD"))
	a.Update(makeCT(0, 20, 0, 10, 10, 0)) // spread=10 price units = 1 pip
	for _, s := range a.Stats() {
		if s.Name == "count (with spread)" {
			assert.Equal(t, 0.0, s.Pips)
		} else {
			assert.Equal(t, 1.0, s.Pips, "stat %q should have Pips=1.0", s.Name)
		}
	}
}

func TestSessionAnalyzer_PipsFieldSet(t *testing.T) {
	a := NewSessionAnalyzer(GetInstrument("EURUSD"))
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
