package datamanager

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

func makeParentsAndFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

func TestNewDataManager(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	instruments := []string{" EURUSD ", "gbpusd", "", "EUR_USD"}

	dm := NewDataManager(instruments, start, end)
	require.NotNil(t, dm)
	require.Equal(t, start, dm.Start)
	require.Equal(t, end, dm.End)
	require.Equal(t, []string{"EURUSD", "GBPUSD"}, dm.Instruments)
}

func TestGetDataManager_DefaultInstruments(t *testing.T) {
	t.Parallel()

	dm := GetDataManager()
	require.Equal(t, []string{"EURUSD", "USDJPY", "GBPUSD"}, dm.Instruments)
}

func TestDataManagerInit(t *testing.T) {
	t.Parallel()

	dm := &DataManager{}
	require.Nil(t, dm.downloader)

	dm.Init()
	require.NotNil(t, dm.downloader)

	// Second Init is idempotent
	d := dm.downloader
	dm.Init()
	require.Equal(t, d, dm.downloader)
}

func TestCandleRequestKey(t *testing.T) {
	t.Parallel()

	cr := CandleRequest{
		Instrument: "EURUSD",
		Source:     market.SourceCandles,
		Range: types.TimeRange{
			TF: types.H1,
		},
	}
	k := cr.Key()
	require.Equal(t, "EURUSD", k.Instrument)
	require.Equal(t, market.SourceCandles, k.Source)
	require.Equal(t, KindCandle, k.Kind)
	require.Equal(t, types.H1, k.TF)
}

func TestCandleRequestKey_DefaultSource(t *testing.T) {
	t.Parallel()

	k := (CandleRequest{
		Instrument: "EURUSD",
		Range: types.TimeRange{
			TF: types.H1,
		},
	}).Key()

	require.Equal(t, market.SourceOanda, k.Source)
}

// ---------------------------------------------------------------------------
// readNextBI5Tick
// ---------------------------------------------------------------------------

func makeBi5Record(msOffset, askU, bidU uint32, askVol, bidVol float32) []byte {
	buf := make([]byte, 20)
	binary.BigEndian.PutUint32(buf[0:4], msOffset)
	binary.BigEndian.PutUint32(buf[4:8], askU)
	binary.BigEndian.PutUint32(buf[8:12], bidU)
	binary.BigEndian.PutUint32(buf[12:16], math.Float32bits(askVol))
	binary.BigEndian.PutUint32(buf[16:20], math.Float32bits(bidVol))
	return buf
}

func TestReadNextBI5Tick_Valid(t *testing.T) {
	t.Parallel()

	baseMS := types.TimeMillis(1_000_000)
	msOffset := uint32(500)
	askU := uint32(12345)
	bidU := uint32(12340)
	askVol := float32(1.5)
	bidVol := float32(0.5)

	rec := makeBi5Record(msOffset, askU, bidU, askVol, bidVol)
	r := bytes.NewReader(rec)

	tick, ok, err := readNextBI5Tick(r, "test.bi5", baseMS, 1)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, baseMS+types.TimeMillis(msOffset), tick.TimeMillis)
	require.Equal(t, types.Price(askU), tick.Ask)
	require.Equal(t, types.Price(bidU), tick.Bid)
	require.InDelta(t, float64(askVol), float64(tick.AskVol), 0.001)
	require.InDelta(t, float64(bidVol), float64(tick.BidVol), 0.001)
}

func TestReadNextBI5Tick_EOF(t *testing.T) {
	t.Parallel()

	r := bytes.NewReader([]byte{})
	_, ok, err := readNextBI5Tick(r, "test.bi5", 0, 1)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestReadNextBI5Tick_TruncatedRecord(t *testing.T) {
	t.Parallel()

	// Only 10 bytes, not a full 20-byte record
	r := bytes.NewReader(make([]byte, 10))
	_, ok, err := readNextBI5Tick(r, "test.bi5", 0, 1)
	require.Error(t, err)
	require.False(t, ok)
}

func TestReadNextBI5Tick_BadMsOffset(t *testing.T) {
	t.Parallel()

	// msOffset >= 3600*1000 is invalid
	rec := makeBi5Record(3600*1000, 100, 99, 1.0, 1.0)
	r := bytes.NewReader(rec)

	_, ok, err := readNextBI5Tick(r, "test.bi5", 0, 1)
	require.Error(t, err)
	require.False(t, ok)
	require.Contains(t, err.Error(), "bad msOffset")
}

// ---------------------------------------------------------------------------
// store.baseScanDir, store.Delete, store.IsUsableTickFile
// ---------------------------------------------------------------------------

func TestStoreBaseScanDir(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	require.Equal(t, s.basedir, s.baseScanDir())
}

func TestStoreIsUsableTickFile_Missing(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{
		Instrument: "EURUSD",
		Source:     "dukascopy",
		Kind:       KindTick,
		TF:         types.Ticks,
		Year:       2025,
		Month:      1,
		Day:        2,
		Hour:       13,
	}
	require.False(t, s.IsUsableTickFile(k))
}

func TestStoreIsUsableTickFile_EmptyFile(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{
		Instrument: "EURUSD",
		Source:     "dukascopy",
		Kind:       KindTick,
		TF:         types.Ticks,
		Year:       2025,
		Month:      1,
		Day:        2,
		Hour:       13,
	}

	path, err := s.KeyPath(k)
	require.NoError(t, err)
	require.NoError(t, makeParentsAndFile(path, nil))

	require.False(t, s.IsUsableTickFile(k))
}

func TestStoreIsUsableTickFile_NonEmptyFile(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{
		Instrument: "EURUSD",
		Source:     "dukascopy",
		Kind:       KindTick,
		TF:         types.Ticks,
		Year:       2025,
		Month:      1,
		Day:        2,
		Hour:       13,
	}

	path, err := s.KeyPath(k)
	require.NoError(t, err)
	require.NoError(t, makeParentsAndFile(path, []byte("data")))

	require.True(t, s.IsUsableTickFile(k))
}

// ---------------------------------------------------------------------------
// store.SaveFile
// ---------------------------------------------------------------------------

func TestStoreSaveFile(t *testing.T) {
	s := newTestStore(t)
	k := Key{
		Instrument: "EURUSD",
		Source:     "dukascopy",
		Kind:       KindTick,
		TF:         types.Ticks,
		Year:       2025,
		Month:      1,
		Day:        2,
		Hour:       13,
	}

	content := []byte("fake bi5 data")
	rc := io.NopCloser(bytes.NewReader(content))

	path, err := s.SaveFile(k, rc)
	require.NoError(t, err)
	require.NotEmpty(t, path)

	exists, err := s.Exists(k)
	require.NoError(t, err)
	require.True(t, exists)
}

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (t *trackingReadCloser) Close() error {
	t.closed = true
	return nil
}

func TestStoreSaveFile_ClosesReader(t *testing.T) {
	s := newTestStore(t)
	k := Key{
		Instrument: "EURUSD",
		Source:     market.SourceDukascopy,
		Kind:       KindTick,
		TF:         types.Ticks,
		Year:       2025,
		Month:      1,
		Day:        2,
		Hour:       13,
	}

	rc := &trackingReadCloser{Reader: bytes.NewReader([]byte("fake bi5 data"))}
	_, err := s.SaveFile(k, rc)
	require.NoError(t, err)
	require.True(t, rc.closed)
}

// ---------------------------------------------------------------------------
// scanFiles
// ---------------------------------------------------------------------------

func TestStoreScanFiles_Empty(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	inv := NewInventory()
	require.NoError(t, s.scanFiles(inv))
	require.Equal(t, 0, inv.Len())
}

func TestStoreScanFiles_WithCSV(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	// Write a valid CSV file so scanFiles finds it
	cs := makeTestCandleSet(t, "EURUSD", 2026, time.January, types.H1)
	require.NoError(t, s.WriteCSV(cs))

	inv := NewInventory()
	require.NoError(t, s.scanFiles(inv))
	require.Greater(t, inv.Len(), 0)
}

func TestRawTickMid(t *testing.T) {
	t.Parallel()

	tick := RawTick{
		Ask: types.Price(110),
		Bid: types.Price(100),
	}
	require.Equal(t, types.Price(105), tick.Mid())
}

func TestRawTickMidOdd(t *testing.T) {
	t.Parallel()

	tick := RawTick{
		Ask: types.Price(101),
		Bid: types.Price(100),
	}
	require.Equal(t, types.Price(101), tick.Mid())
}

func TestRawTickSpread(t *testing.T) {
	t.Parallel()

	tick := RawTick{
		Ask: types.Price(110),
		Bid: types.Price(100),
	}
	require.Equal(t, types.Price(10), tick.Spread())
}

func TestRawTickMinute(t *testing.T) {
	t.Parallel()

	tick := RawTick{
		TimeMillis: types.TimeMillis(90_500),
	}
	require.Equal(t, types.TimeMillis(60_000), tick.Minute())

	tick2 := RawTick{
		TimeMillis: types.TimeMillis(60_000),
	}
	require.Equal(t, types.TimeMillis(60_000), tick2.Minute())
}

type errReadAfter struct {
	data []byte
	pos  int
	err  error
}

func (r *errReadAfter) Read(p []byte) (int, error) {
	if r.pos < len(r.data) {
		n := copy(p, r.data[r.pos:])
		r.pos += n
		return n, nil
	}
	return 0, r.err
}

func TestReadNextBI5Tick_GeneralReadError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("disk error")
	partial := make([]byte, 10)
	r := &errReadAfter{data: partial, err: sentinel}
	_, ok, err := readNextBI5Tick(r, "test.bi5", 0, 1)
	require.Error(t, err)
	require.False(t, ok)
	require.Contains(t, err.Error(), "test.bi5")
}

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
		TF:         types.Ticks,
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

func TestBuildInventory_WithBi5(t *testing.T) {
	s := useTempStore(t)

	bi5Path := s.basedir + "/dukascopy/EURUSD/2025/01/02/13h_ticks.bi5"
	require.NoError(t, makeParentsAndFile(bi5Path, []byte("fake bi5")))

	inv, err := BuildInventory(context.Background())
	require.NoError(t, err)
	require.NotNil(t, inv)
	require.Equal(t, 1, inv.Len())
}

func TestStoreScanFiles_PartialCandleMonthIncomplete(t *testing.T) {
	s := useTempStore(t)

	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, s.WriteMonthlyCandles(market.SourceOanda, "EURUSD", types.H1, start, []market.Candle{
		{Open: 100, High: 101, Low: 99, Close: 100, Ticks: 1},
	}))

	inv := NewInventory()
	require.NoError(t, s.scanFiles(inv))

	key := Key{Instrument: "EURUSD", Source: market.SourceOanda, Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}
	asset, ok := inv.Get(key)
	require.True(t, ok)
	require.False(t, asset.Complete)
	require.Greater(t, asset.MissingInputs, 0)
	require.Contains(t, asset.Reason, "expected candles missing")
}

func TestStoreScanFiles_ClosedOnlyDailyGapsRemainComplete(t *testing.T) {
	s := useTempStore(t)

	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	cs, err := NewMonthlyCandleSet("EURUSD", types.D1, types.FromTime(start), types.PriceScale, market.SourceOanda)
	require.NoError(t, err)

	step := time.Duration(cs.Timeframe) * time.Second
	monthStart := time.Unix(int64(cs.Start), 0).UTC()
	for i := range cs.Candles {
		slotStart := monthStart.Add(time.Duration(i) * step)
		slotEnd := slotStart.Add(step)
		if !timeRangeMayHaveForexData(slotStart, slotEnd) {
			continue
		}
		cs.Candles[i].Candle = market.Candle{Open: 100, High: 101, Low: 99, Close: 100, Ticks: 1}
		cs.SetValid(i)
	}
	require.NoError(t, s.WriteCSV(cs))

	inv := NewInventory()
	require.NoError(t, s.scanFiles(inv))

	key := Key{Instrument: "EURUSD", Source: market.SourceOanda, Kind: KindCandle, TF: types.D1, Year: 2026, Month: 1}
	asset, ok := inv.Get(key)
	require.True(t, ok)
	require.True(t, asset.Complete)
	require.Zero(t, asset.MissingInputs)
}

// TestStoreScanFiles_DSTTransitionMonthNotFalselyIncomplete is the
// regression case for a bug found while validating #179's regen: scanFiles
// (via inspectCandleAsset -> candleSetMissingExpectedSlots) used to
// reconstruct each slot's time as Start+idx*step instead of reading the
// slot's own true timestamp, so every D1/H4 month past a DST transition
// (every March/November) drifted an hour and falsely reported market-hours
// slots as missing for the rest of the month.
func TestStoreScanFiles_DSTTransitionMonthNotFalselyIncomplete(t *testing.T) {
	s := useTempStore(t)

	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	boundaries := SlotBoundaries(monthStart, types.D1, 31)
	candles := make([]market.CandleTime, len(boundaries))
	for i, b := range boundaries {
		candles[i].Timestamp = types.FromTime(b)
		if timeRangeMayHaveForexData(b, b.Add(24*time.Hour)) {
			candles[i].Candle = market.Candle{Open: 100, High: 101, Low: 99, Close: 100, Ticks: 1}
		}
	}
	require.NoError(t, s.WriteMonthlyCandleTimes(market.SourceOanda, "EURUSD", types.D1, monthStart, candles))

	inv := NewInventory()
	require.NoError(t, s.scanFiles(inv))

	key := Key{Instrument: "EURUSD", Source: market.SourceOanda, Kind: KindCandle, TF: types.D1, Year: 2026, Month: 3}
	asset, ok := inv.Get(key)
	require.True(t, ok)
	require.True(t, asset.Complete, "reason: %s", asset.Reason)
	require.Zero(t, asset.MissingInputs)
}

// TestStoreScanFiles_NextMonthSpilloverNotFalselyIncomplete is the
// regression case for a bug found while re-validating the #179 regen: a
// month's last trading day's true daily-alignment window can run a few
// hours into the next calendar month (e.g. May's last H4 slots landing on
// June 1st), but that data structurally lives in June's own raw/canonical
// file, not May's — writeRawMonth/DeriveCanonicalFromRaw never expect to
// fill it from May's raw source. candleSetMissingExpectedSlots used to
// count those trailing slots as "expected," falsely flagging essentially
// every H4/D1 month in the entire store as incomplete once true
// timestamps were being read correctly (the previous Start+idx*step bug
// coincidentally masked this). Only the last H4 slot here (dated the 1st
// of the next month) is left unfilled; the file must still report
// complete.
func TestStoreScanFiles_NextMonthSpilloverNotFalselyIncomplete(t *testing.T) {
	s := useTempStore(t)

	monthStart := time.Date(2021, time.May, 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	boundaries := SlotBoundaries(monthStart, types.H4, 186)
	candles := make([]market.CandleTime, len(boundaries))
	for i, b := range boundaries {
		candles[i].Timestamp = types.FromTime(b)
		if b.Before(monthEnd) && timeRangeMayHaveForexData(b, b.Add(4*time.Hour)) {
			candles[i].Candle = market.Candle{Open: 100, High: 101, Low: 99, Close: 100, Ticks: 1}
		}
		// Slots at/after monthEnd (June 1st spillover) are intentionally
		// left unfilled, matching what a real raw file would look like.
	}
	require.NoError(t, s.WriteMonthlyCandleTimes(market.SourceOanda, "EURUSD", types.H4, monthStart, candles))

	inv := NewInventory()
	require.NoError(t, s.scanFiles(inv))

	key := Key{Instrument: "EURUSD", Source: market.SourceOanda, Kind: KindCandle, TF: types.H4, Year: 2021, Month: 5}
	asset, ok := inv.Get(key)
	require.True(t, ok)
	require.True(t, asset.Complete, "reason: %s", asset.Reason)
	require.Zero(t, asset.MissingInputs)
}

func TestParseCandlePath_BadFilenameMonth(t *testing.T) {
	t.Parallel()

	path := "/data/candles/test/EURUSD/2026/01/EURUSD-2026-13-h1.csv"
	_, ok := parseCandlePath(path)
	require.False(t, ok)
}

func TestParseCandlePath_BadFilenameYear(t *testing.T) {
	t.Parallel()

	path := "/data/candles/test/EURUSD/2026/01/EURUSD-XXXX-01-h1.csv"
	_, ok := parseCandlePath(path)
	require.False(t, ok)
}

func TestParseTickPath_BadYear(t *testing.T) {
	t.Parallel()

	path := "/data/dukascopy/EURUSD/XXXX/01/02/13h_ticks.bi5"
	_, ok := parseTickPath(path)
	require.False(t, ok)
}

func TestParseTickPath_BadDay(t *testing.T) {
	t.Parallel()

	path := "/data/dukascopy/EURUSD/2025/01/XX/13h_ticks.bi5"
	_, ok := parseTickPath(path)
	require.False(t, ok)
}

func TestParseTickPath_BadHourNotNumeric(t *testing.T) {
	t.Parallel()

	path := "/data/dukascopy/EURUSD/2025/01/02/XXh_ticks.bi5"
	_, ok := parseTickPath(path)
	require.False(t, ok)
}

func TestReadCSV_FileNotFound(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 3}
	_, err := s.ReadCSV(k)
	require.Error(t, err)
}

func TestReadCSV_NonZeroDay(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1, Day: 1}
	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Day==0")
}

func TestReadCSV_NonZeroHour(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1, Hour: 1}
	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Hour==0")
}

func TestReadCSV_BadTimestamp(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}
	path, err := s.KeyPath(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(
		"Timestamp,Open,High,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"NOT_A_NUMBER,100,99,98,99,1,2,3,0x0001\n",
	), 0o644))

	_, err = s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse timestamp")
}

func TestReadCSV_BadHighValue(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}
	path, err := s.KeyPath(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,Open,High,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,99,BAD,98,99,1,2,3,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err = s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse high")
}

func TestReadCSV_BadOpenValue(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}
	path, err := s.KeyPath(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,Open,High,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,BAD,100,98,99,1,2,3,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err = s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse open")
}

func TestReadCSV_BadLowValue(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}
	path, err := s.KeyPath(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,Open,High,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,BAD,99,1,2,3,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err = s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse low")
}

func TestReadCSV_BadCloseValue(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}
	path, err := s.KeyPath(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,Open,High,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,98,BAD,1,2,3,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err = s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse close")
}

func TestReadCSV_BadAvgSpread(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}
	path, err := s.KeyPath(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,Open,High,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,98,99,BAD,2,3,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err = s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse avgspread")
}

func TestReadCSV_BadMaxSpread(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}
	path, err := s.KeyPath(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,Open,High,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,98,99,1,BAD,3,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err = s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse maxspread")
}

func TestReadCSV_BadTicks(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}
	path, err := s.KeyPath(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,Open,High,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,98,99,1,2,BAD,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err = s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse ticks")
}

func TestReadCSV_BadFlags(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	ts := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: types.M1, Year: 2026, Month: 1}
	path, err := s.KeyPath(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,Open,High,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,98,99,1,2,3,NOT_HEX\n",
		ts.Unix(),
	)), 0o644))

	_, err = s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse flags")
}

func TestReadCSV_TimestampOutOfRange(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	// Rows are now placed positionally (row N is slot N-1, see store.go
	// readCSVUncached), so "out of range" now means more data rows than
	// January (D1) has slots (31), not a single row with a far-future
	// timestamp.
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: types.D1, Year: 2026, Month: 1}
	path, err := s.KeyPath(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))

	var sb strings.Builder
	sb.WriteString("Timestamp,Open,High,Low,Close,avgspread,maxspread,ticks,flags\n")
	day := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 32; i++ { // January only has 31 days
		fmt.Fprintf(&sb, "%d,100,99,98,99,1,2,3,0x0001\n", day.AddDate(0, 0, i).Unix())
	}
	require.NoError(t, os.WriteFile(path, []byte(sb.String()), 0o644))

	_, err = s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too many rows")
}

func TestWriteMetadata_Output(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	start := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	cs, err := NewMonthlyCandleSet(
		"EURUSD", types.H1, types.FromTime(start), types.PriceScale, "test",
	)
	require.NoError(t, err)

	var buf bytes.Buffer
	err = s.writeMetadata(cs, &buf)
	require.NoError(t, err)

	out := buf.String()
	require.Contains(t, out, "schema=candle-v2")
	require.Contains(t, out, "EURUSD")
	require.Contains(t, out, "2026")
	require.Contains(t, out, "Timestamp,Open,High,Low,Close")
}

func TestWriteCSV_ValidFlag(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	start := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	cs, err := NewMonthlyCandleSet(
		"EURUSD", types.H1, types.FromTime(start), types.PriceScale, "test",
	)
	require.NoError(t, err)

	cs.Candles[5].Candle = market.Candle{Open: 200, High: 210, Low: 195, Close: 205, Ticks: 3}
	cs.SetValid(5)

	require.NoError(t, s.WriteCSV(cs))

	key := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 4}
	back, err := s.ReadCSV(key)
	require.NoError(t, err)
	require.True(t, back.IsValid(5))
	require.Equal(t, types.Price(210), back.Candles[5].High)
}

func TestWriteCSV_EmptySource(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	start := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	cs, err := NewMonthlyCandleSet(
		"EURUSD",
		types.M1,
		types.FromTime(start),
		types.PriceScale,
		"",
	)
	require.NoError(t, err)
	err = s.WriteCSV(cs)
	require.NoError(t, err)
}

func TestWriteCSV_WithInvalidCandle(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	start := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	cs, err := NewMonthlyCandleSet("EURUSD", types.H1, types.FromTime(start), types.PriceScale, "test")
	require.NoError(t, err)

	cs.Candles[0].Candle = market.Candle{Open: 100, High: 105, Low: 99, Close: 103, Ticks: 1}

	require.NoError(t, s.WriteCSV(cs))

	key := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 2}
	back, err := s.ReadCSV(key)
	require.NoError(t, err)
	require.False(t, back.IsValid(0))
}

func TestWriteMonthlyCandles_WrongCount(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	candles := make([]market.Candle, 31*24+1)

	err := s.WriteMonthlyCandles(market.SourceOanda, "EURUSD", types.H1, start, candles)
	require.Error(t, err)
	require.Contains(t, err.Error(), "wrong candle count")
}

func TestWriteMonthlyCandles_BadMonthStart(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	start := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
	candles := make([]market.Candle, 24)

	err := s.WriteMonthlyCandles(market.SourceOanda, "EURUSD", types.H1, start, candles)
	require.Error(t, err)
	require.Contains(t, err.Error(), "start of month")
}

func TestWriteMonthlyCandleTimes_PersistsEachCandlesOwnTimestamp(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	monthStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	// types.DailyAlignmentBoundary's true H4 grid start for January (EST,
	// UTC-5): 17:00 EST = 22:00 UTC.
	trueFirstSlot := monthStart.Add(22 * time.Hour)
	candles := make([]market.CandleTime, 6)
	for i := range candles {
		candles[i] = market.CandleTime{
			Candle:    market.Candle{Open: 1, High: 2, Low: 1, Close: 1, Ticks: 1},
			Timestamp: types.FromTime(trueFirstSlot.Add(time.Duration(i) * 4 * time.Hour)),
		}
	}

	require.NoError(t, s.WriteMonthlyCandleTimes(market.SourceOanda, "EURUSD", types.H4, monthStart, candles))

	key := Key{Instrument: "EURUSD", Source: market.SourceOanda, Kind: KindCandle, TF: types.H4, Year: 2026, Month: 1}
	cs, err := s.ReadCSV(key)
	require.NoError(t, err)
	for i := range candles {
		require.True(t, cs.IsValid(i))
		require.Equal(t, trueFirstSlot.Add(time.Duration(i)*4*time.Hour), cs.Time(i))
	}
}

func TestWriteMonthlyCandleTimes_UnevenSpacingSurvivesRoundTrip(t *testing.T) {
	t.Parallel()

	// D1 across a DST transition legitimately isn't evenly spaced (the
	// transition day is 23 or 25 wall-clock hours) — WriteMonthlyCandleTimes
	// must not reject or "correct" this; each candle's own timestamp is
	// authoritative regardless of spacing from its neighbors.
	s := newTestStore(t)
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	day1 := time.Date(2026, time.March, 7, 22, 0, 0, 0, time.UTC) // EST boundary
	day2 := time.Date(2026, time.March, 8, 21, 0, 0, 0, time.UTC) // EDT boundary (23h later)

	// WriteMonthlyCandleTimes copies candles[i] into slot i positionally
	// (it does not place by timestamp), so the caller must supply a full
	// dense array with day1/day2's data at their real slot indices —
	// exactly what DeriveCanonicalFromRaw does.
	boundaries := SlotBoundaries(monthStart, types.D1, 31)
	candles := make([]market.CandleTime, len(boundaries))
	for i, b := range boundaries {
		candles[i].Timestamp = types.FromTime(b)
	}
	idx1 := SlotIndexForTime(monthStart, types.D1, day1)
	idx2 := SlotIndexForTime(monthStart, types.D1, day2)
	candles[idx1].Candle = market.Candle{Open: 1, High: 2, Low: 1, Close: 1, Ticks: 1}
	candles[idx2].Candle = market.Candle{Open: 1, High: 2, Low: 1, Close: 1, Ticks: 1}

	require.NoError(t, s.WriteMonthlyCandleTimes(market.SourceOanda, "EURUSD", types.D1, monthStart, candles))

	key := Key{Instrument: "EURUSD", Source: market.SourceOanda, Kind: KindCandle, TF: types.D1, Year: 2026, Month: 3}
	cs, err := s.ReadCSV(key)
	require.NoError(t, err)

	require.True(t, cs.IsValid(idx1))
	require.True(t, cs.IsValid(idx2))
	require.Equal(t, day1, cs.Time(idx1))
	require.Equal(t, day2, cs.Time(idx2))
}

func TestKeyPath_EmptySourceDefaults(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{
		Instrument: "EURUSD",
		Source:     "",
		Kind:       KindCandle,
		TF:         types.H1,
		Year:       2026,
		Month:      1,
	}
	path, err := s.KeyPath(k)
	require.NoError(t, err)
	require.Contains(t, path, "unknown")
}

func TestStoreExistsTwoPaths(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: types.H1, Year: 2026, Month: 1}

	exists, err := s.Exists(k)
	require.NoError(t, err)
	require.False(t, exists)

	path, err := s.KeyPath(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o644))

	exists, err = s.Exists(k)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestStoreScanFiles_IgnoresUnknownFiles(t *testing.T) {
	s := useTempStore(t)

	txtPath := filepath.Join(s.basedir, "some_random_file.txt")
	require.NoError(t, os.WriteFile(txtPath, []byte("hello"), 0o644))

	inv := NewInventory()
	require.NoError(t, s.scanFiles(inv))
	require.Equal(t, 0, inv.Len())
}

func TestDataKindStringUnknownValue(t *testing.T) {
	t.Parallel()

	var dk DataKind = 99
	require.Equal(t, "unknown", dk.String())
}

func TestLooksLikeHeader_TimePrefix(t *testing.T) {
	t.Parallel()

	require.True(t, looksLikeHeader([]string{"time", "open", "high"}))
	require.True(t, looksLikeHeader([]string{"  TIME  ", "close"}))
}
