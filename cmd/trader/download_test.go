package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/marketdata/oanda"
	"github.com/rustyeddy/trader/num"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func utc(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }

func rec(t time.Time, bidOpen string, complete bool) oanda.Record {
	p := num.MustParsePrice
	return oanda.Record{Time: t, BidOpen: p(bidOpen), BidHigh: p("1.10100"), BidLow: p("1.09900"), BidClose: p("1.10050"),
		AskOpen: p("1.10010"), AskHigh: p("1.10110"), AskLow: p("1.09910"), AskClose: p("1.10060"), Volume: 100, Complete: complete}
}

// fakeFetcher serves records from a fixed series, as the provider would for a
// [From, To) window, and remembers every request.
type fakeFetcher struct {
	mu     sync.Mutex
	series []oanda.Record
	reqs   []oanda.CandleRequest
	err    error
}

func (f *fakeFetcher) FetchCandles(_ context.Context, req oanda.CandleRequest) ([]oanda.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	if f.err != nil {
		return nil, f.err
	}
	var out []oanda.Record
	for _, r := range f.series {
		if !r.Time.Before(req.From) && r.Time.Before(req.To) {
			out = append(out, r)
		}
	}
	return out, nil
}

func hourly(from time.Time, n int) []oanda.Record {
	var out []oanda.Record
	for i := range n {
		out = append(out, rec(from.Add(time.Duration(i)*time.Hour), "1.10000", true))
	}
	return out
}

func run(t *testing.T, f candleFetcher, root string, req downloadRequest) []partitionResult {
	t.Helper()
	var got []partitionResult
	require.NoError(t, downloadMonths(context.Background(), quiet(), f, root, req, func(r partitionResult) { got = append(got, r) }))
	return got
}

func TestDownloadMonths_WritesOneFilePerMonthInTheNewLayout(t *testing.T) {
	root := t.TempDir()
	f := &fakeFetcher{series: hourly(utc(2024, 1, 30, 0), 72)} // Jan 30 .. Feb 1, 23:00
	req := downloadRequest{Symbols: []string{"EURUSD"}, Interval: oanda.RawH1, From: utc(2024, 1, 30, 0), To: utc(2024, 2, 2, 0)}

	got := run(t, f, root, req)

	require.Len(t, got, 2)
	assert.Equal(t, 48, got[0].Added, "January holds Jan 30 and Jan 31")
	assert.Equal(t, 24, got[1].Added, "February holds Feb 1")
	assert.Equal(t, root+"/oanda/raw/EURUSD/2024/01/EURUSD-2024-01-h1.csv", got[0].Path)
	jan, err := oanda.ReadPartitionRecords(context.Background(), root, got[0].Key)
	require.NoError(t, err)
	assert.Len(t, jan, 48)
	assert.Equal(t, []oanda.CandleRequest{
		{Symbol: "EURUSD", Interval: oanda.RawH1, From: utc(2024, 1, 30, 0), To: utc(2024, 2, 1, 0)},
		{Symbol: "EURUSD", Interval: oanda.RawH1, From: utc(2024, 2, 1, 0), To: utc(2024, 2, 2, 0)},
	}, f.reqs, "each month is its own bounded request")
}

func TestDownloadMonths_ARerunChangesNothing(t *testing.T) {
	root := t.TempDir()
	f := &fakeFetcher{series: hourly(utc(2024, 1, 1, 0), 24)}
	req := downloadRequest{Symbols: []string{"EURUSD"}, Interval: oanda.RawH1, From: utc(2024, 1, 1, 0), To: utc(2024, 1, 2, 0)}
	first := run(t, f, root, req)
	before, err := os.Stat(first[0].Path)
	require.NoError(t, err)
	bytesBefore, err := os.ReadFile(first[0].Path)
	require.NoError(t, err)

	second := run(t, f, root, req)

	assert.Equal(t, 0, second[0].Added)
	assert.Equal(t, 24, second[0].Kept)
	after, err := os.Stat(first[0].Path)
	require.NoError(t, err)
	assert.Equal(t, before.ModTime(), after.ModTime(), "a file that gains nothing is not rewritten")
	bytesAfter, err := os.ReadFile(first[0].Path)
	require.NoError(t, err)
	assert.Equal(t, bytesBefore, bytesAfter)
}

func TestDownloadMonths_ExtendsAnExistingFileWithoutTouchingItsRows(t *testing.T) {
	root := t.TempDir()
	key := oanda.PartitionKey{Symbol: "EURUSD", Interval: oanda.RawH1, Year: 2024, Month: time.January}
	stored := rec(utc(2024, 1, 1, 0), "1.20000", true) // differs from what the provider reports now
	require.NoError(t, oanda.WritePartition(context.Background(), root, key, []oanda.Record{stored}, true))
	f := &fakeFetcher{series: hourly(utc(2024, 1, 1, 0), 3)}

	got := run(t, f, root, downloadRequest{Symbols: []string{"EURUSD"}, Interval: oanda.RawH1, From: utc(2024, 1, 1, 0), To: utc(2024, 1, 2, 0)})

	assert.Equal(t, 2, got[0].Added)
	assert.Equal(t, 1, got[0].Kept)
	assert.Equal(t, 1, got[0].Differing)
	rows, err := oanda.ReadPartitionRecords(context.Background(), root, key)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, "1.2", rows[0].BidOpen.String(), "the stored row keeps its own values")
}

func TestDownloadMonths_NeverStoresAFormingCandleAndCreatesNoEmptyFile(t *testing.T) {
	root := t.TempDir()
	f := &fakeFetcher{series: []oanda.Record{rec(utc(2024, 1, 1, 0), "1.10000", true), rec(utc(2024, 1, 1, 1), "1.10000", false)}}

	got := run(t, f, root, downloadRequest{Symbols: []string{"EURUSD"}, Interval: oanda.RawH1, From: utc(2024, 1, 1, 0), To: utc(2024, 1, 2, 0)})

	assert.Equal(t, 1, got[0].Added)
	assert.Equal(t, 1, got[0].Incomplete)
	rows, err := oanda.ReadPartitionRecords(context.Background(), root, got[0].Key)
	require.NoError(t, err)
	assert.Len(t, rows, 1)

	empty := t.TempDir()
	only := &fakeFetcher{series: []oanda.Record{rec(utc(2024, 1, 1, 0), "1.10000", false)}}
	res := run(t, only, empty, downloadRequest{Symbols: []string{"EURUSD"}, Interval: oanda.RawH1, From: utc(2024, 1, 1, 0), To: utc(2024, 1, 2, 0)})
	assert.Equal(t, 0, res[0].Added)
	_, statErr := os.Stat(res[0].Path)
	assert.ErrorIs(t, statErr, os.ErrNotExist, "a month with nothing to store leaves no file")
}

// Daily bars open at 21:00 or 22:00 UTC, so the last bar of a month opens on its
// last evening and belongs to that month's file (ADR-089).
func TestDownloadMonths_DailyBarsLandInTheirUTCOpeningMonth(t *testing.T) {
	root := t.TempDir()
	f := &fakeFetcher{series: []oanda.Record{
		rec(utc(2020, 4, 29, 21), "1.10000", true),
		rec(utc(2020, 4, 30, 21), "1.10000", true), // opens in April, trades into May
		rec(utc(2020, 5, 3, 21), "1.10000", true),
	}}

	got := run(t, f, root, downloadRequest{Symbols: []string{"EURUSD"}, Interval: oanda.RawD1, From: utc(2020, 4, 1, 0), To: utc(2020, 5, 10, 0)})

	require.Len(t, got, 2)
	april, err := oanda.ReadPartitionRecords(context.Background(), root, got[0].Key)
	require.NoError(t, err)
	assert.Len(t, april, 2)
	assert.True(t, april[1].Time.Equal(utc(2020, 4, 30, 21)))
	may, err := oanda.ReadPartitionRecords(context.Background(), root, got[1].Key)
	require.NoError(t, err)
	assert.Len(t, may, 1)
}

func TestDownloadMonths_SeveralInstrumentsInOrder(t *testing.T) {
	root := t.TempDir()
	f := &fakeFetcher{series: hourly(utc(2024, 1, 1, 0), 2)}

	got := run(t, f, root, downloadRequest{Symbols: []string{"GBPUSD", "EURUSD"}, Interval: oanda.RawH1, From: utc(2024, 1, 1, 0), To: utc(2024, 1, 2, 0)})

	require.Len(t, got, 2)
	assert.Equal(t, "GBPUSD", got[0].Key.Symbol)
	assert.Equal(t, "EURUSD", got[1].Key.Symbol)
}

func TestDownloadMonths_FailuresStopAndNameThePartition(t *testing.T) {
	boom := errors.New("provider down")
	for name, tc := range map[string]struct {
		f    candleFetcher
		req  downloadRequest
		want string
	}{
		"fetch error":   {&fakeFetcher{err: boom}, downloadRequest{Symbols: []string{"EURUSD"}, Interval: oanda.RawH1, From: utc(2024, 1, 1, 0), To: utc(2024, 1, 2, 0)}, "EURUSD 2024-01"},
		"not in scope":  {&fakeFetcher{}, downloadRequest{Symbols: []string{"XAUUSD"}, Interval: oanda.RawH1, From: utc(2024, 1, 1, 0), To: utc(2024, 1, 2, 0)}, "XAUUSD"},
		"bad interval":  {&fakeFetcher{}, downloadRequest{Symbols: []string{"EURUSD"}, Interval: "w1", From: utc(2024, 1, 1, 0), To: utc(2024, 1, 2, 0)}, "EURUSD"},
		"empty range":   {&fakeFetcher{}, downloadRequest{Symbols: []string{"EURUSD"}, Interval: oanda.RawH1, From: utc(2024, 1, 2, 0), To: utc(2024, 1, 2, 0)}, "--to must be after --from"},
		"reversed span": {&fakeFetcher{}, downloadRequest{Symbols: []string{"EURUSD"}, Interval: oanda.RawH1, From: utc(2024, 1, 3, 0), To: utc(2024, 1, 2, 0)}, "--to must be after --from"},
	} {
		t.Run(name, func(t *testing.T) {
			err := downloadMonths(context.Background(), quiet(), tc.f, t.TempDir(), tc.req, nil)

			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), tc.want), err.Error())
		})
	}
}

func TestDownloadMonths_ACancelledContextWritesNothing(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeFetcher{series: hourly(utc(2024, 1, 1, 0), 2)}

	err := downloadMonths(ctx, quiet(), &cancelAware{f}, root, downloadRequest{Symbols: []string{"EURUSD"}, Interval: oanda.RawH1, From: utc(2024, 1, 1, 0), To: utc(2024, 1, 2, 0)}, nil)

	require.ErrorIs(t, err, context.Canceled)
	entries, rerr := os.ReadDir(root)
	require.NoError(t, rerr)
	assert.Empty(t, entries)
}

type cancelAware struct{ *fakeFetcher }

func (c *cancelAware) FetchCandles(ctx context.Context, r oanda.CandleRequest) ([]oanda.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.fakeFetcher.FetchCandles(ctx, r)
}

func TestSameCandle_IgnoresTheLocationOfTheTime(t *testing.T) {
	a := rec(utc(2024, 1, 1, 0), "1.10000", true)
	b := a
	b.Time = a.Time.In(time.FixedZone("x", 3600))

	assert.True(t, sameCandle(a, b))
	b.Volume++
	assert.False(t, sameCandle(a, b))
}
