package marketdata

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
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
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
		Range: market.TimeRange{
			TF: market.H1,
		},
	}
	k := cr.Key()
	require.Equal(t, "EURUSD", k.Instrument)
	require.Equal(t, market.SourceCandles, k.Source)
	require.Equal(t, KindCandle, k.Kind)
	require.Equal(t, market.H1, k.TF)
}

func TestCandleRequestKey_DefaultSource(t *testing.T) {
	t.Parallel()

	k := (CandleRequest{
		Instrument: "EURUSD",
		Range: market.TimeRange{
			TF: market.H1,
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

	baseMS := market.TimeMillis(1_000_000)
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
	require.Equal(t, baseMS+market.TimeMillis(msOffset), tick.TimeMillis)
	require.Equal(t, market.Price(askU), tick.Ask)
	require.Equal(t, market.Price(bidU), tick.Bid)
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
// Store.baseScanDir, Store.Delete, Store.IsUsableTickFile
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
		TF:         market.Ticks,
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
		TF:         market.Ticks,
		Year:       2025,
		Month:      1,
		Day:        2,
		Hour:       13,
	}

	path, err := s.PathForAsset(k)
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
		TF:         market.Ticks,
		Year:       2025,
		Month:      1,
		Day:        2,
		Hour:       13,
	}

	path, err := s.PathForAsset(k)
	require.NoError(t, err)
	require.NoError(t, makeParentsAndFile(path, []byte("data")))

	require.True(t, s.IsUsableTickFile(k))
}

// ---------------------------------------------------------------------------
// Store.SaveFile
// ---------------------------------------------------------------------------

func TestStoreSaveFile(t *testing.T) {
	s := newTestStore(t)
	k := Key{
		Instrument: "EURUSD",
		Source:     "dukascopy",
		Kind:       KindTick,
		TF:         market.Ticks,
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
		TF:         market.Ticks,
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
	cs := makeTestCandleSet(t, "EURUSD", 2026, time.January, market.H1)
	require.NoError(t, s.WriteCSV(cs))

	inv := NewInventory()
	require.NoError(t, s.scanFiles(inv))
	require.Greater(t, inv.Len(), 0)
}

func TestRawTickMid(t *testing.T) {
	t.Parallel()

	tick := RawTick{
		Ask: market.Price(110),
		Bid: market.Price(100),
	}
	require.Equal(t, market.Price(105), tick.Mid())
}

func TestRawTickMidOdd(t *testing.T) {
	t.Parallel()

	tick := RawTick{
		Ask: market.Price(101),
		Bid: market.Price(100),
	}
	require.Equal(t, market.Price(101), tick.Mid())
}

func TestRawTickSpread(t *testing.T) {
	t.Parallel()

	tick := RawTick{
		Ask: market.Price(110),
		Bid: market.Price(100),
	}
	require.Equal(t, market.Price(10), tick.Spread())
}

func TestRawTickMinute(t *testing.T) {
	t.Parallel()

	tick := RawTick{
		TimeMillis: market.TimeMillis(90_500),
	}
	require.Equal(t, market.TimeMillis(60_000), tick.Minute())

	tick2 := RawTick{
		TimeMillis: market.TimeMillis(60_000),
	}
	require.Equal(t, market.TimeMillis(60_000), tick2.Minute())
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
		TF:         market.Ticks,
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
	require.NoError(t, s.WriteMonthlyCandles(market.SourceOanda, "EURUSD", market.H1, start, []market.Candle{
		{Open: 100, High: 101, Low: 99, Close: 100, Ticks: 1},
	}))

	inv := NewInventory()
	require.NoError(t, s.scanFiles(inv))

	key := Key{Instrument: "EURUSD", Source: market.SourceOanda, Kind: KindCandle, TF: market.H1, Year: 2026, Month: 1}
	asset, ok := inv.Get(key)
	require.True(t, ok)
	require.False(t, asset.Complete)
	require.Greater(t, asset.MissingInputs, 0)
	require.Contains(t, asset.Reason, "expected candles missing")
}

func TestStoreScanFiles_ClosedOnlyDailyGapsRemainComplete(t *testing.T) {
	s := useTempStore(t)

	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	cs, err := newMonthlyCandleSet("EURUSD", market.D1, market.FromTime(start), market.PriceScale, market.SourceOanda)
	require.NoError(t, err)

	step := time.Duration(cs.Timeframe) * time.Second
	monthStart := time.Unix(int64(cs.Start), 0).UTC()
	for i := range cs.Candles {
		slotStart := monthStart.Add(time.Duration(i) * step)
		slotEnd := slotStart.Add(step)
		if !timeRangeMayHaveForexData(slotStart, slotEnd) {
			continue
		}
		cs.Candles[i] = market.Candle{Open: 100, High: 101, Low: 99, Close: 100, Ticks: 1}
		cs.SetValid(i)
	}
	require.NoError(t, s.WriteCSV(cs))

	inv := NewInventory()
	require.NoError(t, s.scanFiles(inv))

	key := Key{Instrument: "EURUSD", Source: market.SourceOanda, Kind: KindCandle, TF: market.D1, Year: 2026, Month: 1}
	asset, ok := inv.Get(key)
	require.True(t, ok)
	require.True(t, asset.Complete)
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
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.M1, Year: 2026, Month: 3}
	_, err := s.ReadCSV(k)
	require.Error(t, err)
}

func TestReadCSV_NonZeroDay(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: market.M1, Year: 2026, Month: 1, Day: 1}
	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Day==0")
}

func TestReadCSV_NonZeroHour(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Kind: KindCandle, TF: market.M1, Year: 2026, Month: 1, Hour: 1}
	_, err := s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "Hour==0")
}

func TestReadCSV_BadTimestamp(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.M1, Year: 2026, Month: 1}
	path, err := s.PathForAsset(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
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
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.M1, Year: 2026, Month: 1}
	path, err := s.PathForAsset(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,BAD,99,98,99,1,2,3,0x0001\n",
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
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.M1, Year: 2026, Month: 1}
	path, err := s.PathForAsset(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,BAD,98,99,1,2,3,0x0001\n",
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
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.M1, Year: 2026, Month: 1}
	path, err := s.PathForAsset(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
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
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.M1, Year: 2026, Month: 1}
	path, err := s.PathForAsset(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
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
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.M1, Year: 2026, Month: 1}
	path, err := s.PathForAsset(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
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
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.M1, Year: 2026, Month: 1}
	path, err := s.PathForAsset(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
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
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.M1, Year: 2026, Month: 1}
	path, err := s.PathForAsset(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
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
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.M1, Year: 2026, Month: 1}
	path, err := s.PathForAsset(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
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
	ts := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.M1, Year: 2026, Month: 1}
	path, err := s.PathForAsset(k)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n"+
			"%d,100,99,98,99,1,2,3,0x0001\n",
		ts.Unix(),
	)), 0o644))

	_, err = s.ReadCSV(k)
	require.Error(t, err)
	require.Contains(t, err.Error(), "out of range")
}

func TestWriteMetadata_Output(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	start := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	cs, err := newMonthlyCandleSet(
		"EURUSD", market.H1, market.FromTime(start), market.PriceScale, "test",
	)
	require.NoError(t, err)

	var buf bytes.Buffer
	err = s.writeMetadata(cs, &buf)
	require.NoError(t, err)

	out := buf.String()
	require.Contains(t, out, "schema=v1")
	require.Contains(t, out, "EURUSD")
	require.Contains(t, out, "2026")
	require.Contains(t, out, "Timestamp,High,Open,Low,Close")
}

func TestWriteCSV_ValidFlag(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	start := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	cs, err := newMonthlyCandleSet(
		"EURUSD", market.H1, market.FromTime(start), market.PriceScale, "test",
	)
	require.NoError(t, err)

	cs.Candles[5] = market.Candle{Open: 200, High: 210, Low: 195, Close: 205, Ticks: 3}
	cs.SetValid(5)

	require.NoError(t, s.WriteCSV(cs))

	key := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.H1, Year: 2026, Month: 4}
	back, err := s.ReadCSV(key)
	require.NoError(t, err)
	require.True(t, back.IsValid(5))
	require.Equal(t, market.Price(210), back.Candles[5].High)
}

func TestWriteCSV_EmptySource(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	start := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	cs, err := newMonthlyCandleSet(
		"EURUSD",
		market.M1,
		market.FromTime(start),
		market.PriceScale,
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
	cs, err := newMonthlyCandleSet("EURUSD", market.H1, market.FromTime(start), market.PriceScale, "test")
	require.NoError(t, err)

	cs.Candles[0] = market.Candle{Open: 100, High: 105, Low: 99, Close: 103, Ticks: 1}

	require.NoError(t, s.WriteCSV(cs))

	key := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.H1, Year: 2026, Month: 2}
	back, err := s.ReadCSV(key)
	require.NoError(t, err)
	require.False(t, back.IsValid(0))
}

func TestWriteMonthlyCandles_WrongCount(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	start := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	candles := make([]market.Candle, 31*24+1)

	err := s.WriteMonthlyCandles(market.SourceOanda, "EURUSD", market.H1, start, candles)
	require.Error(t, err)
	require.Contains(t, err.Error(), "wrong candle count")
}

func TestWriteMonthlyCandles_BadMonthStart(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	start := time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC)
	candles := make([]market.Candle, 24)

	err := s.WriteMonthlyCandles(market.SourceOanda, "EURUSD", market.H1, start, candles)
	require.Error(t, err)
	require.Contains(t, err.Error(), "start of month")
}

func TestPathForAsset_EmptySourceDefaults(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{
		Instrument: "EURUSD",
		Source:     "",
		Kind:       KindCandle,
		TF:         market.H1,
		Year:       2026,
		Month:      1,
	}
	path, err := s.PathForAsset(k)
	require.NoError(t, err)
	require.Contains(t, path, "unknown")
}

func TestStoreExistsTwoPaths(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: market.H1, Year: 2026, Month: 1}

	exists, err := s.Exists(k)
	require.NoError(t, err)
	require.False(t, exists)

	path, err := s.PathForAsset(k)
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
