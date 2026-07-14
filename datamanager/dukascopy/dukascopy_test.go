package dukascopy

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

const dukascopyTestsEnv = "TRADER_RUN_DUKASCOPY_TESTS"

func requireDukascopyTests(t *testing.T) {
	t.Helper()
	if os.Getenv(dukascopyTestsEnv) == "1" {
		return
	}
	t.Skip("skipping Dukascopy tests; set TRADER_RUN_DUKASCOPY_TESTS=1 to enable")
}

func TestNewFile(t *testing.T) {
	t.Parallel()
	requireDukascopyTests(t)

	sym := "EURUSD"
	ts := time.Date(2025, 1, 2, 13, 45, 30, 0, time.UTC)
	f := NewFile(sym, ts)

	require.NotNil(t, f)
	require.Equal(t, sym, f.symbol)
	require.Equal(t, time.Date(2025, 1, 2, 13, 0, 0, 0, time.UTC), f.t)
}

func TestFileKey(t *testing.T) {
	t.Parallel()
	requireDukascopyTests(t)

	sym := "EURUSD"
	ts := time.Date(2025, 1, 2, 13, 0, 0, 0, time.UTC)
	f := NewFile(sym, ts)

	k := f.Key()
	require.Equal(t, sym, k.Instrument)
	require.Equal(t, "dukascopy", k.Source)
	require.Equal(t, datamanager.KindTick, k.Kind)
	require.Equal(t, types.Ticks, k.TF)
	require.Equal(t, 2025, k.Year)
	require.Equal(t, 1, k.Month)
	require.Equal(t, 2, k.Day)
	require.Equal(t, 13, k.Hour)

	k2 := f.Key()
	require.Equal(t, k, k2)
}

func TestFileInstrument(t *testing.T) {
	t.Parallel()
	requireDukascopyTests(t)

	f := NewFile("GBPUSD", time.Now())
	require.Equal(t, "GBPUSD", f.Instrument())
}

func TestFileURL(t *testing.T) {
	t.Parallel()
	requireDukascopyTests(t)

	sym := "EURUSD"
	ts := time.Date(2025, 1, 2, 13, 0, 0, 0, time.UTC)
	f := NewFile(sym, ts)

	url := f.URL()
	want := fmt.Sprintf(
		"https://datafeed.dukascopy.com/datafeed/%s/%04d/%02d/%02d/%02dh_ticks.bi5",
		sym, 2025, 0, 2, 13,
	)
	require.Equal(t, want, url)
}

func TestFileInstrument_Ungated(t *testing.T) {
	t.Parallel()

	f := NewFile("GBPUSD", time.Date(2026, 1, 6, 12, 0, 0, 0, time.UTC))
	require.Equal(t, "GBPUSD", f.Instrument())
}
