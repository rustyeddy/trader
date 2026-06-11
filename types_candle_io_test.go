package trader

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
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

// writeCandleSourceFile is an internal helper for trader type processing.
func writeCandleSourceFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// TestCandleFormattingHelpers verifies expected behavior for this component.
func TestCandleFormattingHelpers(t *testing.T) {
	t.Parallel()

	c := Candle{Open: 1, High: 2, Low: 3, Close: 4, AvgSpread: 5, MaxSpread: 6, Ticks: 7}
	assert.Equal(t, "1, 2, 3, 4", c.String())
	assert.Equal(t, "1, 2, 3, 4: avg spread 5, max spread 6, ticks: 7", c.FullString())

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

// TestCandleSetScanBoundsAndBuildDenseFromFile verifies expected behavior for this component.
func TestCandleSetScanBoundsAndBuildDenseFromFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeCandleSourceFile(t, dir, "candles.csv", "# comment\n"+
		"time;open;high;low;close;avgspread;maxspread;ticks;valid\n"+
		"2024-01-01T00:00:00Z;1.100000;1.101000;1.099000;1.100500;0.000100;0.000200;3;1\n"+
		"bad line\n"+
		"2024-01-01T00:01:00Z;1.100500;1.102000;1.100000;1.101500;0.000100;0.000200;4;1\n"+
		"2024-01-01T00:01:00Z;1.100500;1.102000;1.100000;1.101500;0.000100;0.000200;4;1\n"+
		"2024-01-01T00:02:00Z;1.101500;1.103000;1.101000;1.102500;0.000100;0.000200;5;0\n")

	cs := &candleSet{Filepath: path, Instrument: "EURUSD"}
	minTs, maxTs, err := cs.scanBounds()
	require.NoError(t, err)
	assert.Equal(t, Timestamp(time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()), minTs)
	assert.Equal(t, Timestamp(time.Date(2024, time.January, 1, 0, 2, 0, 0, time.UTC).Unix()), maxTs)

	require.NoError(t, cs.buildDenseFromFile())
	assert.Equal(t, M1, cs.Timeframe)
	assert.Equal(t, Scale6(1_000_000), cs.Scale)
	assert.Equal(t, minTs, cs.Start)
	require.Len(t, cs.Candles, 3)
	assert.True(t, cs.IsValid(0))
	assert.True(t, cs.IsValid(1))
	assert.False(t, cs.IsValid(2))
	assert.Equal(t, 1, cs.duplicates)
	assert.Equal(t, 1, cs.badLines)
	assert.Equal(t, 0, cs.outOfRange)
	assert.Equal(t, Price(1100000), cs.Candles[0].Open)
	assert.Equal(t, Price(1102500), cs.Candles[2].Close)
}

// TestCandleSetScanBoundsNoValidRows verifies expected behavior for this component.
func TestCandleSetScanBoundsNoValidRows(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeCandleSourceFile(t, dir, "empty.csv", "# comment\n"+
		"time;open;high;low;close;avgspread;maxspread;ticks;valid\n"+
		"not-a-date;1;2;3;4;5;6;7;1\n")

	cs := &candleSet{Filepath: path}
	_, _, err := cs.scanBounds()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no valid timestamps found")
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

	assert.InDelta(t, 12.34567, cs.Float64(1234567), 1e-9)
	assert.Equal(t, int32(123457), cs.Int32(1.234567))
	assert.InDelta(t, 0.0001, cs.PipSize(), 1e-12)
	assert.InDelta(t, 10.0, cs.UnitsPerPip(), 1e-9)
	assert.InDelta(t, 25.0, cs.DeltaToPips(250), 1e-9)
	assert.Equal(t, int32(25), cs.PipsToDelta(2.5))

	buf := &bufferWriteCloser{}
	cs.PrintStats(buf)
	out := buf.String()
	assert.Contains(t, out, "CandleSet Stats")
	assert.Contains(t, out, "Total Minutes: 20")
	assert.Contains(t, out, "Present Minutes: 2")
	assert.Contains(t, out, "Missing Minutes: 18")
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
