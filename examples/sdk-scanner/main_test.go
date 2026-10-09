package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/sdk"
)

func TestScannerPicksWidestRangeAndReportsMissing(t *testing.T) {
	s := newScanner()
	d := s.Describe()
	require.Len(t, d.Requirements, 3)

	ts := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	bar := func(high, low string) marketdata.Bar {
		return marketdata.Bar{Time: ts, High: num.MustParsePrice(high), Low: num.MustParsePrice(low)}
	}
	ev := sdk.BarsEvent{
		Boundary: ts, Interval: s.interval,
		Bars: []sdk.BarEvent{
			{Instrument: s.universe[0], Interval: s.interval, Bar: bar("1.1010", "1.1000")},
			{Instrument: s.universe[1], Interval: s.interval, Bar: bar("1.2600", "1.2500")},
		},
		Missing: s.universe[2:],
	}
	intents, signals, err := s.OnBars(context.Background(), ev, nil)
	require.NoError(t, err)
	require.Empty(t, intents, "a scanner emits no intents")
	require.Len(t, signals, 1)
	require.Equal(t, s.universe[1].String(), signals[0].Values["widest"])
	require.Equal(t, "1", signals[0].Values["missing"])
	require.Equal(t, "2024-01-02", signals[0].Values["boundary"])
}

func TestScannerEmptySnapshotEmitsNothing(t *testing.T) {
	s := newScanner()
	intents, signals, err := s.OnBars(context.Background(), sdk.BarsEvent{Interval: s.interval, Missing: s.universe}, nil)
	require.NoError(t, err)
	require.Empty(t, intents)
	require.Empty(t, signals)
}
