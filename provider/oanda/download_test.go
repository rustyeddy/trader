package oanda

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/store"
	"github.com/rustyeddy/trader/store/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func rec(t time.Time, bidOpen string, complete bool) Record {
	p := num.MustParsePrice
	return Record{Time: t, BidOpen: p(bidOpen), BidHigh: p("1.10100"), BidLow: p("1.09900"), BidClose: p("1.10050"),
		AskOpen: p("1.10010"), AskHigh: p("1.10110"), AskLow: p("1.09910"), AskClose: p("1.10060"), Volume: 100, Complete: complete}
}

func hourly(from time.Time, n int) []Record {
	var out []Record
	for i := range n {
		out = append(out, rec(from.Add(time.Duration(i)*time.Hour), "1.10000", true))
	}
	return out
}

// fakeFetcher serves records from a fixed series for a [From, To) window, as the
// provider would, and remembers every request.
type fakeFetcher struct {
	mu     sync.Mutex
	series []Record
	reqs   []CandleRequest
	err    error
}

func (f *fakeFetcher) FetchCandles(_ context.Context, req CandleRequest) ([]Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reqs = append(f.reqs, req)
	if f.err != nil {
		return nil, f.err
	}
	var out []Record
	for _, r := range f.series {
		if !r.Time.Before(req.From) && r.Time.Before(req.To) {
			out = append(out, r)
		}
	}
	return out, nil
}

// strayFetcher ignores the window, like a fetcher that breaks the contract.
type strayFetcher struct{ out []Record }

func (s strayFetcher) FetchCandles(context.Context, CandleRequest) ([]Record, error) {
	return s.out, nil
}

var h1Jan = PartitionKey{Symbol: "EURUSD", Interval: RawH1, Year: 2024, Month: time.January}

func TestDownloadPartition_WritesTheMonthFileAndReportsItsRelativePath(t *testing.T) {
	f := &fakeFetcher{series: hourly(at(2024, 1, 30, 0), 48)}

	res, err := DownloadPartition(context.Background(), quiet(), f, h1Jan, at(2024, 1, 30, 0), at(2024, 2, 1, 0))

	require.NoError(t, err)
	assert.Equal(t, 48, res.Added)
	assert.Equal(t, 48, res.Fetched)
	assert.Equal(t, filepath.Join("oanda", "raw", "EURUSD", "2024", "01", "EURUSD-2024-01-h1.csv"), res.Path)
	assert.Equal(t, h1Jan, res.Key)
	rows, err := ReadPartitionRecords(context.Background(), h1Jan)
	require.NoError(t, err)
	assert.Len(t, rows, 48)
	assert.Equal(t, []CandleRequest{{Symbol: "EURUSD", Interval: RawH1, From: at(2024, 1, 30, 0), To: at(2024, 2, 1, 0)}}, f.reqs)
}

func TestDownloadPartition_ARerunChangesNothing(t *testing.T) {
	storetest.Root(t)
	f := &fakeFetcher{series: hourly(at(2024, 1, 1, 0), 24)}
	first, err := DownloadPartition(context.Background(), quiet(), f, h1Jan, at(2024, 1, 1, 0), at(2024, 1, 2, 0))
	require.NoError(t, err)
	path := filepath.Join(store.Root(), first.Path)
	before, err := os.Stat(path)
	require.NoError(t, err)
	bytesBefore, err := os.ReadFile(path)
	require.NoError(t, err)

	second, err := DownloadPartition(context.Background(), quiet(), f, h1Jan, at(2024, 1, 1, 0), at(2024, 1, 2, 0))

	require.NoError(t, err)
	assert.Equal(t, 0, second.Added)
	assert.Equal(t, 24, second.Kept)
	after, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, before.ModTime(), after.ModTime(), "a file that gains nothing is not rewritten")
	bytesAfter, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, bytesBefore, bytesAfter)
}

func TestDownloadPartition_ExtendsAnExistingFileWithoutTouchingItsRows(t *testing.T) {
	storetest.Root(t)
	stored := rec(at(2024, 1, 1, 0), "1.20000", true) // differs from what the provider reports now
	require.NoError(t, WritePartition(context.Background(), h1Jan, []Record{stored}, true))
	f := &fakeFetcher{series: hourly(at(2024, 1, 1, 0), 3)}

	res, err := DownloadPartition(context.Background(), quiet(), f, h1Jan, at(2024, 1, 1, 0), at(2024, 1, 2, 0))

	require.NoError(t, err)
	assert.Equal(t, 2, res.Added)
	assert.Equal(t, 1, res.Kept)
	assert.Equal(t, 1, res.Differing)
	rows, err := ReadPartitionRecords(context.Background(), h1Jan)
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, "1.2", rows[0].BidOpen.String(), "the stored row keeps its own values")
}

func TestDownloadPartition_NeverStoresAFormingCandleAndCreatesNoEmptyFile(t *testing.T) {
	storetest.Root(t)
	f := &fakeFetcher{series: []Record{rec(at(2024, 1, 1, 0), "1.10000", true), rec(at(2024, 1, 1, 1), "1.10000", false)}}

	res, err := DownloadPartition(context.Background(), quiet(), f, h1Jan, at(2024, 1, 1, 0), at(2024, 1, 2, 0))

	require.NoError(t, err)
	assert.Equal(t, 1, res.Added)
	assert.Equal(t, 1, res.Incomplete)
	rows, err := ReadPartitionRecords(context.Background(), h1Jan)
	require.NoError(t, err)
	assert.Len(t, rows, 1)

	storetest.Root(t) // a fresh, empty root
	only := &fakeFetcher{series: []Record{rec(at(2024, 1, 1, 0), "1.10000", false)}}
	res, err = DownloadPartition(context.Background(), quiet(), only, h1Jan, at(2024, 1, 1, 0), at(2024, 1, 2, 0))
	require.NoError(t, err)
	assert.Equal(t, 0, res.Added)
	_, statErr := os.Stat(filepath.Join(store.Root(), res.Path))
	assert.ErrorIs(t, statErr, os.ErrNotExist, "a month with nothing to store leaves no file")
}

func TestDownloadPartition_RejectsACandleOutsideTheRequestedWindowBeforeWriting(t *testing.T) {
	for name, stray := range map[string]time.Time{
		"before the window":    at(2024, 1, 9, 23),
		"at the exclusive end": at(2024, 1, 11, 0),
		"another month":        at(2024, 2, 1, 0),
	} {
		t.Run(name, func(t *testing.T) {
			root := storetest.Root(t)
			f := strayFetcher{out: []Record{rec(at(2024, 1, 10, 1), "1.10000", true), rec(stray, "1.10000", true)}}

			_, err := DownloadPartition(context.Background(), quiet(), f, h1Jan, at(2024, 1, 10, 0), at(2024, 1, 11, 0))

			require.Error(t, err)
			assert.Contains(t, err.Error(), "outside the requested window")
			entries, rerr := os.ReadDir(root)
			require.NoError(t, rerr)
			assert.Empty(t, entries, "nothing is written")
		})
	}
}

func TestDownloadPartition_ErrorsLeaveTheFileUntouched(t *testing.T) {
	storetest.Root(t)
	boom := errors.New("provider down")
	_, err := DownloadPartition(context.Background(), quiet(), &fakeFetcher{err: boom}, h1Jan, at(2024, 1, 1, 0), at(2024, 1, 2, 0))
	require.ErrorIs(t, err, boom)

	_, err = DownloadPartition(context.Background(), nil, &fakeFetcher{}, PartitionKey{Symbol: "XAUUSD", Interval: RawH1, Year: 2024, Month: time.January}, at(2024, 1, 1, 0), at(2024, 1, 2, 0))
	require.Error(t, err, "a key that is not in scope never reaches the provider")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &fakeFetcher{series: hourly(at(2024, 1, 1, 0), 2)}
	_, err = DownloadPartition(ctx, quiet(), f, h1Jan, at(2024, 1, 1, 0), at(2024, 1, 2, 0))
	require.ErrorIs(t, err, context.Canceled)
	_, statErr := os.Stat(filepath.Join(store.Root(), "oanda"))
	assert.ErrorIs(t, statErr, os.ErrNotExist, "no failed run wrote a file")
}

func TestSameCandle_IgnoresTheLocationOfTheTime(t *testing.T) {
	a := rec(at(2024, 1, 1, 0), "1.10000", true)
	b := a
	b.Time = a.Time.In(time.FixedZone("x", 3600))

	assert.True(t, sameCandle(a, b))
	b.Volume++
	assert.False(t, sameCandle(a, b))
}

// forming is a row as an older downloader stored it: a candle that was still
// forming, with the values it had then.
func forming(t time.Time) Record {
	r := rec(t, "1.20000", false)
	r.Volume = 7
	return r
}

func TestDownloadPartition_ReplacesAStoredFormingRowWithTheClosedCandle(t *testing.T) {
	storetest.Root(t)
	stored := []Record{rec(at(2024, 1, 1, 0), "1.10000", true), forming(at(2024, 1, 1, 1)), rec(at(2024, 1, 1, 2), "1.10000", true)}
	require.NoError(t, WritePartition(context.Background(), h1Jan, stored, true))
	f := &fakeFetcher{series: hourly(at(2024, 1, 1, 0), 3)}

	res, err := DownloadPartition(context.Background(), quiet(), f, h1Jan, at(2024, 1, 1, 0), at(2024, 1, 2, 0))

	require.NoError(t, err)
	assert.Equal(t, 1, res.Replaced)
	assert.Equal(t, 0, res.Added)
	assert.Equal(t, 2, res.Kept, "the two complete rows are kept, as they were")
	assert.Equal(t, 0, res.Differing)
	rows, err := ReadPartitionRecords(context.Background(), h1Jan)
	require.NoError(t, err)
	require.Len(t, rows, 3, "replaced in place: no row added or lost")
	assert.True(t, rows[1].Complete, "the stored row is now the closed candle")
	assert.Equal(t, "1.1", rows[1].BidOpen.String(), "with the provider's values")
	assert.Equal(t, int64(100), rows[1].Volume)
	assert.True(t, rows[1].Time.Equal(at(2024, 1, 1, 1)))
}

func TestDownloadPartition_AReplacementAloneRewritesTheFileAndARerunChangesNothing(t *testing.T) {
	storetest.Root(t)
	require.NoError(t, WritePartition(context.Background(), h1Jan, []Record{rec(at(2024, 1, 1, 0), "1.10000", true), forming(at(2024, 1, 1, 1))}, true))
	f := &fakeFetcher{series: hourly(at(2024, 1, 1, 0), 2)}
	first, err := DownloadPartition(context.Background(), quiet(), f, h1Jan, at(2024, 1, 1, 0), at(2024, 1, 2, 0))
	require.NoError(t, err)
	require.Equal(t, 1, first.Replaced)
	path := filepath.Join(store.Root(), first.Path)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	stat, err := os.Stat(path)
	require.NoError(t, err)

	second, err := DownloadPartition(context.Background(), quiet(), f, h1Jan, at(2024, 1, 1, 0), at(2024, 1, 2, 0))

	require.NoError(t, err)
	assert.Equal(t, 0, second.Replaced)
	assert.Equal(t, 2, second.Kept)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after)
	again, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, stat.ModTime(), again.ModTime(), "nothing left to do, so the file is not rewritten")
}

func TestDownloadPartition_AStoredFormingRowStaysWhileTheProviderStillReportsItForming(t *testing.T) {
	storetest.Root(t)
	require.NoError(t, WritePartition(context.Background(), h1Jan, []Record{rec(at(2024, 1, 1, 0), "1.10000", true), forming(at(2024, 1, 1, 1))}, true))
	path := filepath.Join(store.Root(), "oanda", "raw", "EURUSD", "2024", "01", "EURUSD-2024-01-h1.csv")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	f := &fakeFetcher{series: []Record{rec(at(2024, 1, 1, 0), "1.10000", true), rec(at(2024, 1, 1, 1), "1.10000", false)}}

	res, err := DownloadPartition(context.Background(), quiet(), f, h1Jan, at(2024, 1, 1, 0), at(2024, 1, 2, 0))

	require.NoError(t, err)
	assert.Equal(t, 0, res.Replaced)
	assert.Equal(t, 1, res.Incomplete)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after, "a forming candle replaces nothing: its values will still change")
}

func TestDownloadPartition_ReplacesAndAddsInOneDownloadAndLeavesOtherRowsAlone(t *testing.T) {
	storetest.Root(t)
	stored := []Record{rec(at(2024, 1, 1, 0), "1.30000", true), forming(at(2024, 1, 1, 1))} // row 0 differs and is complete
	require.NoError(t, WritePartition(context.Background(), h1Jan, stored, true))
	f := &fakeFetcher{series: hourly(at(2024, 1, 1, 0), 4)}

	res, err := DownloadPartition(context.Background(), quiet(), f, h1Jan, at(2024, 1, 1, 0), at(2024, 1, 2, 0))

	require.NoError(t, err)
	assert.Equal(t, 2, res.Added)
	assert.Equal(t, 1, res.Replaced)
	assert.Equal(t, 1, res.Kept)
	assert.Equal(t, 1, res.Differing, "a complete row that differs is reported, not replaced")
	rows, err := ReadPartitionRecords(context.Background(), h1Jan)
	require.NoError(t, err)
	require.Len(t, rows, 4)
	assert.Equal(t, "1.3", rows[0].BidOpen.String(), "the complete row keeps its own values")
	assert.True(t, rows[1].Complete)
	assert.Equal(t, "1.1", rows[1].BidOpen.String())
}

func TestDownloadPartition_AFormingRowOutsideTheWindowIsNotTouched(t *testing.T) {
	storetest.Root(t)
	require.NoError(t, WritePartition(context.Background(), h1Jan, []Record{forming(at(2024, 1, 3, 1))}, true))
	f := &fakeFetcher{series: hourly(at(2024, 1, 1, 0), 3)}

	res, err := DownloadPartition(context.Background(), quiet(), f, h1Jan, at(2024, 1, 1, 0), at(2024, 1, 2, 0))

	require.NoError(t, err)
	assert.Equal(t, 3, res.Added)
	assert.Equal(t, 0, res.Replaced)
	rows, err := ReadPartitionRecords(context.Background(), h1Jan)
	require.NoError(t, err)
	require.Len(t, rows, 4)
	assert.False(t, rows[3].Complete, "the row the window did not cover is as it was")
}
