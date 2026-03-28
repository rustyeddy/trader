package data

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// parseCandlePath
// ---------------------------------------------------------------------------

func TestParseCandlePath_Valid(t *testing.T) {
	t.Parallel()

	path := "/data/candles/test/EURUSD/2026/01/EURUSD-2026-01-h1.csv"
	k, ok := parseCandlePath(path)
	require.True(t, ok)
	require.Equal(t, "EURUSD", k.Instrument)
	require.Equal(t, "test", k.Source)
	require.Equal(t, KindCandle, k.Kind)
	require.Equal(t, 2026, k.Year)
	require.Equal(t, 1, k.Month)
	require.Equal(t, types.H1, k.TF)
}

func TestParseCandlePath_M1(t *testing.T) {
	t.Parallel()

	path := "/data/candles/candles/GBPUSD/2025/06/GBPUSD-2025-06-m1.csv"
	k, ok := parseCandlePath(path)
	require.True(t, ok)
	require.Equal(t, "GBPUSD", k.Instrument)
	require.Equal(t, "candles", k.Source)
	require.Equal(t, types.M1, k.TF)
	require.Equal(t, 2025, k.Year)
	require.Equal(t, 6, k.Month)
}

func TestParseCandlePath_D1(t *testing.T) {
	t.Parallel()

	path := "/basedir/src/USDJPY/2024/12/USDJPY-2024-12-d1.csv"
	k, ok := parseCandlePath(path)
	require.True(t, ok)
	require.Equal(t, types.D1, k.TF)
}

func TestParseCandlePath_TooFewParts(t *testing.T) {
	t.Parallel()

	_, ok := parseCandlePath("short/path.csv")
	require.False(t, ok)
}

func TestParseCandlePath_InvalidMonth(t *testing.T) {
	t.Parallel()

	path := "/data/candles/test/EURUSD/2026/13/EURUSD-2026-13-h1.csv"
	_, ok := parseCandlePath(path)
	require.False(t, ok)
}

func TestParseCandlePath_MismatchedInstrument(t *testing.T) {
	t.Parallel()

	path := "/data/candles/test/EURUSD/2026/01/GBPUSD-2026-01-h1.csv"
	_, ok := parseCandlePath(path)
	require.False(t, ok)
}

func TestParseCandlePath_UnknownTF(t *testing.T) {
	t.Parallel()

	path := "/data/candles/test/EURUSD/2026/01/EURUSD-2026-01-w1.csv"
	_, ok := parseCandlePath(path)
	require.False(t, ok)
}

func TestParseCandlePath_BadYear(t *testing.T) {
	t.Parallel()

	path := "/data/candles/test/EURUSD/XXXX/01/EURUSD-XXXX-01-h1.csv"
	_, ok := parseCandlePath(path)
	require.False(t, ok)
}

func TestParseCandlePath_FilenameWrongPartCount(t *testing.T) {
	t.Parallel()

	// filename has only 3 dashes-parts
	path := "/data/candles/test/EURUSD/2026/01/EURUSD-2026-h1.csv"
	_, ok := parseCandlePath(path)
	require.False(t, ok)
}

// ---------------------------------------------------------------------------
// parseTickPath
// ---------------------------------------------------------------------------

func TestParseTickPath_Valid(t *testing.T) {
	t.Parallel()

	path := "/data/dukascopy/EURUSD/2025/01/02/13h_ticks.bi5"
	k, ok := parseTickPath(path)
	require.True(t, ok)
	require.Equal(t, "EURUSD", k.Instrument)
	require.Equal(t, "dukascopy", k.Source)
	require.Equal(t, KindTick, k.Kind)
	require.Equal(t, 2025, k.Year)
	require.Equal(t, 1, k.Month)
	require.Equal(t, 2, k.Day)
	require.Equal(t, 13, k.Hour)
}

func TestParseTickPath_TooFewParts(t *testing.T) {
	t.Parallel()

	_, ok := parseTickPath("a/b/c")
	require.False(t, ok)
}

func TestParseTickPath_BadHourSuffix(t *testing.T) {
	t.Parallel()

	path := "/data/dukascopy/EURUSD/2025/01/02/13h_other.bi5"
	_, ok := parseTickPath(path)
	require.False(t, ok)
}

func TestParseTickPath_InvalidMonth(t *testing.T) {
	t.Parallel()

	path := "/data/dukascopy/EURUSD/2025/13/02/13h_ticks.bi5"
	_, ok := parseTickPath(path)
	require.False(t, ok)
}

func TestParseTickPath_InvalidDay(t *testing.T) {
	t.Parallel()

	path := "/data/dukascopy/EURUSD/2025/01/32/13h_ticks.bi5"
	_, ok := parseTickPath(path)
	require.False(t, ok)
}

func TestParseTickPath_InvalidHour(t *testing.T) {
	t.Parallel()

	path := "/data/dukascopy/EURUSD/2025/01/02/25h_ticks.bi5"
	_, ok := parseTickPath(path)
	require.False(t, ok)
}

// ---------------------------------------------------------------------------
// pathForHourlyTick via PathForAsset
// ---------------------------------------------------------------------------

func TestPathForAsset_TickKey(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{
		Instrument: "EUR_USD",
		Source:     "dukascopy",
		Kind:       KindTick,
		TF:         types.Ticks,
		Year:       2025,
		Month:      1,
		Day:        2,
		Hour:       13,
	}
	p := s.PathForAsset(k)
	require.Contains(t, p, "EURUSD")
	require.Contains(t, p, "2025")
	require.Contains(t, p, "13h_ticks.bi5")
}

func TestPathForAsset_PanicOnUnsupported(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{
		Instrument: "EURUSD",
		Source:     "test",
		Kind:       KindUnknown,
		Year:       2026,
		Month:      1,
	}
	require.Panics(t, func() { s.PathForAsset(k) })
}

// ---------------------------------------------------------------------------
// RelDir
// ---------------------------------------------------------------------------

func TestStoreRelDir(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{
		Instrument: "EUR_USD",
		TF:         types.H1,
		Year:       2026,
	}
	rel := s.RelDir(k)
	require.Equal(t, filepath.Join("EURUSD", "H1", "2026"), rel)
}

// ---------------------------------------------------------------------------
// Exists
// ---------------------------------------------------------------------------

func TestStoreExists_Missing(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{
		Instrument: "EURUSD",
		Source:     "test",
		Kind:       KindCandle,
		TF:         types.M1,
		Year:       2026,
		Month:      1,
	}
	exists, err := s.Exists(k)
	require.NoError(t, err)
	require.False(t, exists)
}

func TestStoreExists_Present(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{
		Instrument: "EURUSD",
		Source:     "test",
		Kind:       KindCandle,
		TF:         types.M1,
		Year:       2026,
		Month:      1,
	}
	path := s.PathForAsset(k)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("data"), 0o644))

	exists, err := s.Exists(k)
	require.NoError(t, err)
	require.True(t, exists)
}
