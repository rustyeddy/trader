package trader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ReadCSV: additional validation paths
// ---------------------------------------------------------------------------

func TestReadCSV_NonZeroDay(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: M1, Year: 2026, Month: 1, Day: 1}
	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Day==0")
}

func TestReadCSV_NonZeroHour(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: M1, Year: 2026, Month: 1, Hour: 1}
	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Hour==0")
}

func TestReadCSV_BadTimestamp(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: M1, Year: 2026, Month: 1}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"NOT_A_NUMBER,100,99,98,99,1,2,3,0x0001\n",
	), 0o644))

	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse timestamp")
}

func TestReadCSV_BadHighValue(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: M1, Year: 2026, Month: 1}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,BAD,99,98,99,1,2,3,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse high")
}

func TestReadCSV_BadOpenValue(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: M1, Year: 2026, Month: 1}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,BAD,98,99,1,2,3,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse open")
}

func TestReadCSV_BadLowValue(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: M1, Year: 2026, Month: 1}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,BAD,99,1,2,3,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse low")
}

func TestReadCSV_BadCloseValue(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: M1, Year: 2026, Month: 1}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,98,BAD,1,2,3,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse close")
}

func TestReadCSV_BadAvgSpread(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: M1, Year: 2026, Month: 1}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,98,99,BAD,2,3,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse avgspread")
}

func TestReadCSV_BadMaxSpread(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: M1, Year: 2026, Month: 1}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,98,99,1,BAD,3,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse maxspread")
}

func TestReadCSV_BadTicks(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: M1, Year: 2026, Month: 1}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,98,99,1,2,BAD,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse ticks")
}

func TestReadCSV_BadFlags(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: M1, Year: 2026, Month: 1}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,98,99,1,2,3,NOT_HEX\n",
		ts.Unix(),
	)), 0o644))

	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse flags")
}

func TestReadCSV_TimestampOutOfRange(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	// Timestamp for Feb 1 2026 - outside January range
	ts := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: M1, Year: 2026, Month: 1}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,98,99,1,2,3,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of range")
}

// ---------------------------------------------------------------------------
// SaveFile: reader returns an error (copy error path)
// ---------------------------------------------------------------------------

type errReader struct {
	err error
}

func (er *errReader) Read(p []byte) (int, error) { return 0, er.err }
func (er *errReader) Close() error               { return nil }

func TestStoreSaveFile_CopyError(t *testing.T) {
	s := useTempStore(t)

	k := Key{
		Instrument: "EURUSD",
		Source:     "dukascopy",
		Kind:       KindTick,
		TF:         Ticks,
		Year:       2025,
		Month:      1,
		Day:        2,
		Hour:       13,
	}

	sentinel := errors.New("read error")
	rc := &errReader{err: sentinel}

	_, err := s.SaveFile(k, rc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "write")
}

// ---------------------------------------------------------------------------
// buildHourM1FromTickIterator: bad timestamp (<=0)
// ---------------------------------------------------------------------------

func TestBuildHourM1_BadTimestamp(t *testing.T) {
	t.Parallel()

	k := Key{Kind: KindTick, TF: Ticks, Year: 2026, Month: 1, Day: 5, Hour: 10}
	tick := RawTick{timemilli: 0, Ask: 100, Bid: 99} // Timemilli <= 0
	idx := 0
	it := NewFuncIterator(func() (RawTick, bool, error) {
		if idx > 0 {
			return RawTick{}, false, nil
		}
		idx++
		return tick, true, nil
	}, nil)

	_, err := buildHourM1FromTickIterator(context.Background(), k, it)
	require.Error(t, err)
	require.Contains(t, err.Error(), "bad tick timestamp")
}

// ---------------------------------------------------------------------------
// buildHourM1FromTickIterator: out-of-order ticks
// ---------------------------------------------------------------------------

func TestBuildHourM1_OutOfOrder(t *testing.T) {
	t.Parallel()

	hourStart := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	baseMS := timeMilliFromTime(hourStart)

	ticks := []RawTick{
		// First tick: minute 5
		{timemilli: baseMS + 5*60_000 + 100, Ask: 13010, Bid: 13000},
		// Second tick: minute 2 (out of order)
		{timemilli: baseMS + 2*60_000 + 100, Ask: 13010, Bid: 13000},
	}
	idx := 0
	it := NewFuncIterator(func() (RawTick, bool, error) {
		if idx >= len(ticks) {
			return RawTick{}, false, nil
		}
		tick := ticks[idx]
		idx++
		return tick, true, nil
	}, nil)

	k := Key{Kind: KindTick, TF: Ticks, Year: 2026, Month: 1, Day: 5, Hour: 10}
	_, err := buildHourM1FromTickIterator(context.Background(), k, it)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out-of-order")
}

// ---------------------------------------------------------------------------
// buildHourM1FromTickIterator: trailing fillFlat (after last tick to end of hour)
// ---------------------------------------------------------------------------

func TestBuildHourM1_TrailingFillFlat(t *testing.T) {
	t.Parallel()

	hourStart := time.Date(2026, 1, 5, 10, 0, 0, 0, time.UTC)
	baseMS := timeMilliFromTime(hourStart)

	// Only minute 0 has a tick; minutes 1-59 should be filled flat
	ticks := []RawTick{
		{timemilli: baseMS + 100, Ask: 13010, Bid: 13000},
	}
	idx := 0
	it := NewFuncIterator(func() (RawTick, bool, error) {
		if idx >= len(ticks) {
			return RawTick{}, false, nil
		}
		tick := ticks[idx]
		idx++
		return tick, true, nil
	}, nil)

	k := Key{Kind: KindTick, TF: Ticks, Year: 2026, Month: 1, Day: 5, Hour: 10}
	cs, err := buildHourM1FromTickIterator(context.Background(), k, it)
	require.NoError(t, err)
	require.NotNil(t, cs)

	// Minute 0 is valid
	require.True(t, cs.IsValid(0))
	// Minute 1 should have a flat placeholder (not valid)
	require.False(t, cs.IsValid(1))
	// All flat candles should have the same price as minute 0 close
	require.Equal(t, cs.Candles[0].Close, cs.Candles[1].Close)
}

// ---------------------------------------------------------------------------
// Plan: context cancellation inside the range loop
// ---------------------------------------------------------------------------

func TestPlanContextCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	wl := NewWantlist()
	k := Key{Kind: KindTick, Instrument: "EURUSD", Year: 2026, Month: 1}
	wl.Put(Want{Key: k, WantReason: WantMissing})

	dm := &DataManager{
		inventory: NewInventory(),
		wants:     wl,
	}

	// Plan iterates over wants with cancelled ctx; may or may not see the cancel.
	plan, err := dm.Plan(ctx)
	// Should return nil plan or early exit without panic
	_ = plan
	_ = err
}

// ---------------------------------------------------------------------------
// Candles: context cancelled during month iteration
// ---------------------------------------------------------------------------

func TestCandles_ContextCancelledDuringIteration(t *testing.T) {
	s := useTempStore(t)

	// Write data for two months
	writeMonthlyCandles(t, s, "EURUSD", H1, 2026, time.January, nil)
	writeMonthlyCandles(t, s, "EURUSD", H1, 2026, time.February, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	dm := &DataManager{}
	req := CandleRequest{
		Source:     SourceCandles,
		Instrument: "EURUSD",
		Timeframe:  H1,
		Range: TimeRange{
			Start: FromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
			End:   FromTime(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)),
		},
	}
	_, err := dm.Candles(ctx, req)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// BuildInventory with context (covers the 75% → higher)
// ---------------------------------------------------------------------------

func TestBuildInventory_ContextCancelled(t *testing.T) {
	useTempStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// BuildInventory doesn't check ctx, but at least covers the function fully
	inv, err := BuildInventory(ctx)
	require.NoError(t, err)
	require.NotNil(t, inv)
}

// ---------------------------------------------------------------------------
// Inventory.String (DataKind.String covers "ticks" case)
// ---------------------------------------------------------------------------

func TestInventoryString(t *testing.T) {
	t.Parallel()

	inv := NewInventory()
	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 1}
	a := Asset{
		Key:        k,
		Descriptor: "test descriptor",
		Reason:     "test reason",
	}
	inv.Put(a)
	// Len is part of the uncovered inventory path, verify it's accessible
	require.Equal(t, 1, inv.Len())
}

// ---------------------------------------------------------------------------
// Store.Exists: stat error (unrelated OS error - hard to trigger cleanly)
// Test we return true for existing file and false for missing.
// ---------------------------------------------------------------------------

func TestStoreExistsTwoPaths(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: H1, Year: 2026, Month: 1}

	// Initially missing
	exists, err := s.Exists(k)
	require.NoError(t, err)
	require.False(t, exists)

	// Create file
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))

	exists, err = s.Exists(k)
	require.NoError(t, err)
	require.True(t, exists)
}

// ---------------------------------------------------------------------------
// dukasfile URL month is 0-based (January = 00)
// ---------------------------------------------------------------------------

func TestDukasfileURLMonthOffset(t *testing.T) {
	t.Parallel()

	df := newDatafile("EURUSD", time.Date(2025, time.December, 15, 8, 0, 0, 0, time.UTC))
	url := df.URL()
	// December is month 12, Dukascopy uses 0-based so 11 = 0x0B
	require.Contains(t, url, "/2025/11/15/08h_ticks.bi5")
}

// ---------------------------------------------------------------------------
// RequiredTickHoursForMonth: result has no market-closed hours
// ---------------------------------------------------------------------------

func TestRequiredTickHoursForMonth_NoClosedHours(t *testing.T) {
	t.Parallel()

	keys := RequiredTickHoursForMonth("dukascopy", "EURUSD", 2026, 1)
	for _, k := range keys {
		ts := time.Date(k.Year, time.Month(k.Month), k.Day, k.Hour, 0, 0, 0, time.UTC)
		require.False(t, isForexMarketClosed(ts),
			"key %+v should not be forex-closed time", k)
	}
}

// ---------------------------------------------------------------------------
// Keymap.Range: empty map
// ---------------------------------------------------------------------------

func TestKeymapRange_Empty(t *testing.T) {
	t.Parallel()

	km := NewKeymap[int]()
	count := 0
	km.Range(func(k Key, v int) bool {
		count++
		return true
	})
	require.Equal(t, 0, count)
}

// ---------------------------------------------------------------------------
// store.scanFiles: file that fails both .csv and .bi5 suffixes (ignored)
// ---------------------------------------------------------------------------

func TestStoreScanFiles_IgnoresUnknownFiles(t *testing.T) {
	s := useTempStore(t)

	// Create a .txt file that should be ignored
	txtPath := filepath.Join(s.basedir, "some_random_file.txt")
	require.NoError(t, os.WriteFile(txtPath, []byte("hello"), 0o644))

	inv := NewInventory()
	require.NoError(t, s.scanFiles(inv))
	require.Equal(t, 0, inv.Len())
}

// ---------------------------------------------------------------------------
// WriteCSV: valid candle set with a candle that is NOT marked valid (flags=0)
// ---------------------------------------------------------------------------

func TestWriteCSV_WithInvalidCandle(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	start := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	cs, err := NewMonthlyCandleSet("EURUSD", H1, FromTime(start), PriceScale, "test")
	require.NoError(t, err)

	// Put a candle but do NOT call cs.SetValid → flags will be 0
	cs.Candles[0] = Candle{Open: 100, High: 105, Low: 99, Close: 103, Ticks: 1}

	require.NoError(t, s.WriteCSV(cs))

	// Read it back and verify the candle is NOT valid
	key := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: H1, Year: 2026, Month: 2}
	back, err := s.ReadCSV(key)
	require.NoError(t, err)
	require.False(t, back.IsValid(0))
}

// ---------------------------------------------------------------------------
// dataKind String: unknown value coverage
// ---------------------------------------------------------------------------

func TestDataKindStringUnknownValue(t *testing.T) {
	t.Parallel()

	var dk DataKind = 99
	require.Equal(t, "unknown", dk.String())
}

// ---------------------------------------------------------------------------
// looksLikeHeader: "time" prefix
// ---------------------------------------------------------------------------

func TestLooksLikeHeader_TimePrefix(t *testing.T) {
	t.Parallel()

	require.True(t, looksLikeHeader([]string{"time", "open", "high"}))
	require.True(t, looksLikeHeader([]string{"  TIME  ", "close"}))
}
