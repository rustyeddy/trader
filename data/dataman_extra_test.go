package data

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

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
	instruments := []string{"EURUSD", "GBPUSD"}

	dm := NewDataManager(instruments, start, end)
	require.NotNil(t, dm)
	require.Equal(t, start, dm.Start)
	require.Equal(t, end, dm.End)
	require.Equal(t, instruments, dm.Instruments)
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
		Source:     SourceCandles,
		Timeframe:  types.H1,
	}
	k := cr.Key()
	require.Equal(t, "EURUSD", k.Instrument)
	require.Equal(t, "candles", k.Source)
	require.Equal(t, KindCandle, k.Kind)
	require.Equal(t, types.H1, k.TF)
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

	baseMS := types.Timemilli(1_000_000)
	msOffset := uint32(500)
	askU := uint32(12345)
	bidU := uint32(12340)
	askVol := float32(1.5)
	bidVol := float32(0.5)

	rec := makeBi5Record(msOffset, askU, bidU, askVol, bidVol)
	r := bytes.NewReader(rec)

	tick, ok, err := readNextBI5Tick(r, "test.bi5", baseMS)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, baseMS+types.Timemilli(msOffset), tick.Timemilli)
	require.Equal(t, types.Price(askU*10), tick.Ask)
	require.Equal(t, types.Price(bidU*10), tick.Bid)
	require.InDelta(t, float64(askVol), float64(tick.AskVol), 0.001)
	require.InDelta(t, float64(bidVol), float64(tick.BidVol), 0.001)
}

func TestReadNextBI5Tick_EOF(t *testing.T) {
	t.Parallel()

	r := bytes.NewReader([]byte{})
	_, ok, err := readNextBI5Tick(r, "test.bi5", 0)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestReadNextBI5Tick_TruncatedRecord(t *testing.T) {
	t.Parallel()

	// Only 10 bytes, not a full 20-byte record
	r := bytes.NewReader(make([]byte, 10))
	_, ok, err := readNextBI5Tick(r, "test.bi5", 0)
	require.Error(t, err)
	require.False(t, ok)
}

func TestReadNextBI5Tick_BadMsOffset(t *testing.T) {
	t.Parallel()

	// msOffset >= 3600*1000 is invalid
	rec := makeBi5Record(3600*1000, 100, 99, 1.0, 1.0)
	r := bytes.NewReader(rec)

	_, ok, err := readNextBI5Tick(r, "test.bi5", 0)
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

	path := s.PathForAsset(k)
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

	path := s.PathForAsset(k)
	require.NoError(t, makeParentsAndFile(path, []byte("data")))

	require.True(t, s.IsUsableTickFile(k))
}

// ---------------------------------------------------------------------------
// Store.SaveFile
// ---------------------------------------------------------------------------

func TestStoreSaveFile(t *testing.T) {
	// SaveFile uses key.Path() which reads from the global store,
	// so we must swap the global store to a temp dir.
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

	content := []byte("fake bi5 data")
	rc := io.NopCloser(bytes.NewReader(content))

	path, err := s.SaveFile(k, rc)
	require.NoError(t, err)
	require.NotEmpty(t, path)

	exists, err := s.Exists(k)
	require.NoError(t, err)
	require.True(t, exists)
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
	cs := newMonthlyCandleSet(t, "EURUSD", 2026, time.January, types.H1)
	require.NoError(t, s.WriteCSV(cs))

	inv := NewInventory()
	require.NoError(t, s.scanFiles(inv))
	require.Greater(t, inv.Len(), 0)
}
