package trader

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Key.compare TF branches (currently at 87.9% - missing TF < and TF > cases)
// ---------------------------------------------------------------------------

func TestKeyCompareTF(t *testing.T) {
	t.Parallel()

	base := Key{Source: "candles", Instrument: "EURUSD", Kind: KindCandle, TF: H1, Year: 2026, Month: 1}

	t.Run("TF smaller returns -1", func(t *testing.T) {
		t.Parallel()
		other := base
		other.TF = D1 // D1 > H1
		require.Equal(t, -1, base.compare(other))
	})

	t.Run("TF larger returns 1", func(t *testing.T) {
		t.Parallel()
		other := base
		other.TF = M1 // M1 < H1
		require.Equal(t, 1, base.compare(other))
	})
}

// ---------------------------------------------------------------------------
// parseCandlePath: bad month in filename (fileMonth invalid)
// ---------------------------------------------------------------------------

func TestParseCandlePath_BadFilenameMonth(t *testing.T) {
	t.Parallel()

	// filename month doesn't match directory month
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

// ---------------------------------------------------------------------------
// parseTickPath: bad year
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// ReadCSV: file not found (key is valid but file missing)
// ---------------------------------------------------------------------------

func TestReadCSV_FileNotFound(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	k := Key{Instrument: "EURUSD", Source: "test", Kind: KindCandle, TF: M1, Year: 2026, Month: 3}
	_, err := s.ReadCSV(k)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// WriteCSV: source defaults to empty string (writeMetadata path)
// ---------------------------------------------------------------------------

func TestWriteCSV_EmptySource(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	start := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	cs, err := newMonthlyCandleSet(
		"EURUSD",
		M1,
		FromTime(start),
		PriceScale,
		"", // empty source
	)
	require.NoError(t, err)
	err = s.WriteCSV(cs)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Keymap.Put nil map path (zero-value Keymap)
// ---------------------------------------------------------------------------

func TestKeymapPut_NilMap(t *testing.T) {
	t.Parallel()

	km := Keymap[int]{} // nil internal map
	k := Key{Instrument: "EURUSD"}
	km.Put(k, 1) // should initialize map
	v, ok := km.Get(k)
	require.True(t, ok)
	require.Equal(t, 1, v)
}

// ---------------------------------------------------------------------------
// candleSetIterator: already closed returns false
// ---------------------------------------------------------------------------

func TestCandleSetIterator_AlreadyClosed(t *testing.T) {
	t.Parallel()

	s := useTempStore(t)
	cs := makeTestCandleSet(t, "EURUSD", 2026, time.January, H1)
	_ = s

	it := newCandleSetIterator(cs, TimeRange{})
	require.NoError(t, it.Close())
	require.False(t, it.Next())
	require.NoError(t, it.Close()) // idempotent
}

// ---------------------------------------------------------------------------
// candleSetIterator: already done returns false
// ---------------------------------------------------------------------------

func TestCandleSetIterator_AfterDone(t *testing.T) {
	t.Parallel()

	s := useTempStore(t)
	cs := makeTestCandleSet(t, "EURUSD", 2026, time.January, H1)
	_ = s

	it := newCandleSetIterator(cs, TimeRange{})
	// Drain all items
	for it.Next() {
	}
	// Calling Next again after done should return false
	require.False(t, it.Next())
	require.NoError(t, it.Err())
}

// ---------------------------------------------------------------------------
// Key.Range: tick key with hour=0
// ---------------------------------------------------------------------------

func TestKeyRange_TickHour0(t *testing.T) {
	t.Parallel()

	k := Key{
		Kind:  KindTick,
		Year:  2026,
		Month: 1,
		Day:   5,
		Hour:  0,
	}
	rng := k.Range()
	start := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	require.Equal(t, Timestamp(start.Unix()), rng.Start)
	require.Equal(t, Timestamp(start.Add(time.Hour).Unix()), rng.End)
}
