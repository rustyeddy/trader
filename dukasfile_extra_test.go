package trader

import (
	"fmt"
	"testing"
	"time"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

func TestNewDatafile(t *testing.T) {
	t.Parallel()

	sym := "EURUSD"
	ts := time.Date(2025, 1, 2, 13, 45, 30, 0, time.UTC)
	df := newDatafile(sym, ts)

	require.NotNil(t, df)
	require.Equal(t, sym, df.symbol)
	// Time is truncated to whole hour in UTC
	require.Equal(t, time.Date(2025, 1, 2, 13, 0, 0, 0, time.UTC), df.Time)
}

func TestDukasfileKey(t *testing.T) {
	t.Parallel()

	sym := "EURUSD"
	ts := time.Date(2025, 1, 2, 13, 0, 0, 0, time.UTC)
	df := newDatafile(sym, ts)

	k := df.Key()
	require.Equal(t, sym, k.Instrument)
	require.Equal(t, "dukascopy", k.Source)
	require.Equal(t, KindTick, k.Kind)
	require.Equal(t, types.Ticks, k.TF)
	require.Equal(t, 2025, k.Year)
	require.Equal(t, 1, k.Month)
	require.Equal(t, 2, k.Day)
	require.Equal(t, 13, k.Hour)

	// Second call returns cached key
	k2 := df.Key()
	require.Equal(t, k, k2)
}

func TestDukasfileInstrument(t *testing.T) {
	t.Parallel()

	df := newDatafile("GBPUSD", time.Now())
	require.Equal(t, "GBPUSD", df.Instrument())
}

func TestDukasfileURL(t *testing.T) {
	t.Parallel()

	sym := "EURUSD"
	ts := time.Date(2025, 1, 2, 13, 0, 0, 0, time.UTC)
	df := newDatafile(sym, ts)

	url := df.URL()
	// Format: https://datafeed.dukascopy.com/datafeed/EURUSD/2025/00/02/13h_ticks.bi5
	// Month is 0-indexed in the URL (January = 00)
	want := fmt.Sprintf(
		"https://datafeed.dukascopy.com/datafeed/%s/%04d/%02d/%02d/%02dh_ticks.bi5",
		sym, 2025, 0, 2, 13,
	)
	require.Equal(t, want, url)
}
