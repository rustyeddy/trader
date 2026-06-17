package trader

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bufferWriteCloser represents a trader domain type.
type bufferWriteCloser struct {
	bytes.Buffer
}

// Close is an internal helper for trader type processing.
func (b *bufferWriteCloser) Close() error { return nil }

// TestCandleFormattingHelpers verifies expected behavior for this component.
func TestCandleFormattingHelpers(t *testing.T) {
	t.Parallel()

	c := Candle{Open: 1, High: 2, Low: 3, Close: 4, AvgSpread: 5, MaxSpread: 6, Ticks: 7}
	assert.Equal(t, "0.00001, 0.00002, 0.00003, 0.00004", c.String())
	assert.Equal(t, "0.00001, 0.00002, 0.00003, 0.00004: avg spread 0.00005, max spread 0.00006, ticks: 7", c.FullString())

	ct := candleTime{Candle: c, Timestamp: Timestamp(100)}
	assert.Equal(t, c.String(), ct.String())
	assert.Equal(t, c.String(), fmt.Sprint(ct))
}

// TestCandleSetFilenameTimeAndBitHelpers verifies expected behavior for this component.
func TestCandleSetFilenameTimeAndBitHelpers(t *testing.T) {
	t.Parallel()

	start := Timestamp(time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC).Unix())
	h1 := &candleSet{Instrument: "EURUSD", Start: start, Timeframe: H1}
	d1 := &candleSet{Instrument: "EURUSD", Start: start, Timeframe: D1}

	assert.Equal(t, "eurusd-h1-2026", h1.Filename())
	assert.Equal(t, "eurusd-d1-all", d1.Filename())
	assert.Equal(t, time.Date(2026, time.March, 1, 1, 0, 0, 0, time.UTC), h1.Time(1))

	valid := make([]uint64, 2)
	bitSet(valid, 5)
	bitSet(valid, 70)
	assert.True(t, bitIsSet(valid, 5))
	assert.True(t, bitIsSet(valid, 70))
	assert.False(t, bitIsSet(valid, 6))
}

// TestCandleSetPrintStatsAndConversions verifies expected behavior for this component.
func TestCandleSetPrintStatsAndConversions(t *testing.T) {
	t.Parallel()

	cs := &candleSet{
		Instrument: "EURUSD",
		Start:      Timestamp(time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC).Unix()),
		Timeframe:  M1,
		Scale:      PriceScale,
		Candles:    make([]Candle, 20),
		Valid:      make([]uint64, 1),
	}
	bitSet(cs.Valid, 0)
	bitSet(cs.Valid, 12)

	buf := &bufferWriteCloser{}
	cs.PrintStats(buf)
	out := buf.String()
	assert.Contains(t, out, "CandleSet Stats")
	assert.Contains(t, out, "Timeframe: m1")
	assert.Contains(t, out, "Total Bars: 20")
	assert.Contains(t, out, "Present Bars: 2")
	assert.Contains(t, out, "Missing Bars: 18")
	assert.Contains(t, out, "Longest Gap")
}

// TestCandleSetIteratorAccessors verifies expected behavior for this component.
func TestCandleSetIteratorAccessors(t *testing.T) {
	t.Parallel()

	start := Timestamp(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Unix())
	cs := &candleSet{
		Instrument: "EURUSD",
		Start:      start,
		Timeframe:  H1,
		Scale:      PriceScale,
		Candles: []Candle{
			{Open: 10, High: 20, Low: 5, Close: 15, Ticks: 1},
			{Open: 30, High: 40, Low: 25, Close: 35, Ticks: 2},
		},
		Valid: make([]uint64, 1),
	}
	cs.SetValid(0)
	cs.SetValid(1)

	it := cs.Iterator()
	first, ok := it.NextCandle()
	require.True(t, ok)
	assert.Equal(t, cs.Candles[0], first)
	assert.Equal(t, 0, it.Index())
	assert.Equal(t, start, it.StartTime())
	assert.Equal(t, start, it.Timestamp())
	assert.Equal(t, time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), it.Time())

	second, ok := it.NextCandle()
	require.True(t, ok)
	assert.Equal(t, cs.Candles[1], second)
	assert.Equal(t, 1, it.Index())

	_, ok = it.NextCandle()
	assert.False(t, ok)
}
