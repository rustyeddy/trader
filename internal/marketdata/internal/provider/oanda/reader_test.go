package oanda

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixture(name string) string { return filepath.Join("testdata", name) }

func TestReadFileEURUSDH1(t *testing.T) {
	meta, records, err := ReadFile(context.Background(), fixture("EURUSD-2020-05-h1.csv"))
	require.NoError(t, err)

	assert.Equal(t, RawH1, meta.Interval)
	assert.Equal(t, 2020, meta.Year)
	assert.Equal(t, time.May, meta.Month)
	assert.Equal(t, "EURUSD", meta.Symbol)
	assert.Equal(t,
		instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD")),
		meta.Instrument)

	require.Len(t, records, 6)

	// First row, exact values — prices never routed through float64.
	first := records[0]
	assert.Equal(t, time.Date(2020, time.May, 1, 0, 0, 0, 0, time.UTC), first.Time)
	assert.Equal(t, num.MustParsePrice("1.09439"), first.BidOpen)
	assert.Equal(t, num.MustParsePrice("1.09548"), first.BidHigh)
	assert.Equal(t, num.MustParsePrice("1.09339"), first.BidLow)
	assert.Equal(t, num.MustParsePrice("1.09452"), first.BidClose)
	assert.Equal(t, num.MustParsePrice("1.09457"), first.AskOpen)
	assert.Equal(t, num.MustParsePrice("1.09466"), first.AskClose)
	assert.Equal(t, int64(2235), first.Volume)
	assert.True(t, first.Complete)
}

func TestReadFileJPYPairExactDecimals(t *testing.T) {
	meta, records, err := ReadFile(context.Background(), fixture("USDJPY-2020-05-h1.csv"))
	require.NoError(t, err)
	assert.Equal(t, "USDJPY", meta.Symbol)
	require.NotEmpty(t, records)
	// JPY pairs carry larger integer parts; parsing must stay exact.
	assert.Equal(t, num.MustParsePrice("107.27200"), records[0].BidOpen)
}

// The daily partition names the interval d1 in the file name but tf=d in the
// schema comment; both resolve to D1 and the cross-check must accept it.
func TestReadFileDailyIntervalTokenReconciled(t *testing.T) {
	meta, records, err := ReadFile(context.Background(), fixture("EURUSD-2020-05-d1.csv"))
	require.NoError(t, err)
	assert.Equal(t, RawD1, meta.Interval)
	require.NotEmpty(t, records)
}

func TestOpenXAUUSDOutOfScope(t *testing.T) {
	r, err := Open(fixture("XAUUSD-2026-06-h4.csv"))
	assert.Nil(t, r)
	assert.ErrorIs(t, err, ErrInstrumentOutOfScope)
}

func TestOpenW1Unsupported(t *testing.T) {
	r, err := Open(fixture("EURUSD-2020-05-w1.csv"))
	assert.Nil(t, r)
	assert.ErrorIs(t, err, ErrUnsupportedInterval)
}

func TestMeta(t *testing.T) {
	r, err := Open(fixture("EURUSD-2020-05-h1.csv"))
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	m := r.Meta()
	assert.Equal(t, "EURUSD", m.Symbol)
	assert.Equal(t, RawH1, m.Interval)
	assert.Equal(t, 2020, m.Year)
	assert.Equal(t, time.May, m.Month)
}

// The malformed fixtures have valid 4-part file names so parsing reaches the
// row parser; each carries one bad data row of a distinct kind.
func TestReadFileMalformedRows(t *testing.T) {
	cases := map[string]string{
		"EURUSD-2020-06-h1.csv": "unparseable price",
		"EURUSD-2020-07-h1.csv": "short row",
		"EURUSD-2020-08-h1.csv": "unparseable time",
		"EURUSD-2020-11-h1.csv": "unparseable volume",
		"EURUSD-2020-12-h1.csv": "unparseable complete",
		"EURUSD-2021-01-h1.csv": "missing column header",
	}
	for name, kind := range cases {
		_, _, err := ReadFile(context.Background(), fixture(name))
		assert.ErrorIs(t, err, ErrMalformedData, kind)
	}
}

// A header with the same columns in a different order must be rejected, not
// read positionally — otherwise bid_h values would be assigned to bid_o.
func TestOpenReorderedHeaderRejected(t *testing.T) {
	r, err := Open(fixture("EURUSD-2021-02-h1.csv"))
	if r != nil {
		_ = r.Close()
	}
	assert.ErrorIs(t, err, ErrMalformedData)
}

func TestOpenBadYearMonthInFileName(t *testing.T) {
	// parsePathMeta runs before opening the file, so these need not exist.
	for _, name := range []string{
		"EURUSD-YYYY-05-h1.csv", // non-numeric year
		"EURUSD-2020-13-h1.csv", // month out of range
		"EURUSD-2020-00-h1.csv", // month out of range
	} {
		_, err := Open(fixture(name))
		assert.ErrorIs(t, err, ErrMalformedData, name)
	}
}

func TestOpenSchemaInstrumentMismatch(t *testing.T) {
	// File name says EURUSD; schema comment says GBPUSD.
	r, err := Open(fixture("EURUSD-2020-09-h1.csv"))
	if r != nil {
		_ = r.Close()
	}
	assert.ErrorIs(t, err, ErrMalformedData)
}

func TestOpenSchemaMonthMismatch(t *testing.T) {
	// File name says month 10; schema comment says month 05.
	r, err := Open(fixture("EURUSD-2020-10-h1.csv"))
	if r != nil {
		_ = r.Close()
	}
	assert.ErrorIs(t, err, ErrMalformedData)
}

func TestReaderStreamingAndClose(t *testing.T) {
	r, err := Open(fixture("EURUSD-2020-05-h1.csv"))
	require.NoError(t, err)

	// Stream two records, then close early — Close must release the file and
	// be safe to call again.
	_, err = r.Next(context.Background())
	require.NoError(t, err)
	_, err = r.Next(context.Background())
	require.NoError(t, err)

	require.NoError(t, r.Close())
	require.NoError(t, r.Close(), "Close is idempotent")

	_, err = r.Next(context.Background())
	assert.Error(t, err, "Next after Close reports an error")
}

func TestReaderNextReturnsEOF(t *testing.T) {
	r, err := Open(fixture("EURUSD-2020-05-h1.csv"))
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	count := 0
	for {
		_, err := r.Next(context.Background())
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		count++
	}
	assert.Equal(t, 6, count)
}

func TestReaderHonorsContextCancellation(t *testing.T) {
	r, err := Open(fixture("EURUSD-2020-05-h1.csv"))
	require.NoError(t, err)
	defer func() { _ = r.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = r.Next(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestOpenBadFileName(t *testing.T) {
	_, err := Open(fixture("not-a-valid-name.csv"))
	assert.Error(t, err)
}

// newReaderFromBytes resolves its own metadata from path independently
// of any caller-side check, the same way Open does; a bad name is
// rejected the same way regardless of entry point.
func TestNewReaderFromBytesBadFileName(t *testing.T) {
	_, err := newReaderFromBytes("not-a-valid-name.csv", []byte("irrelevant"))
	assert.Error(t, err)
}

func TestOpenMissingFile(t *testing.T) {
	// A well-formed, in-scope name that does not exist on disk.
	_, err := Open(fixture("EURUSD-1999-01-h1.csv"))
	assert.Error(t, err)
}
