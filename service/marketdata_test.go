package service

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	mdservice "github.com/rustyeddy/trader/marketdata/service"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/provider/oanda"
	"github.com/rustyeddy/trader/store"
	"github.com/rustyeddy/trader/store/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain points the store at a temporary directory for the whole package, so
// a test that forgets storetest.Root still never touches the real data root.
func TestMain(m *testing.M) { os.Exit(storetest.Main(m)) }

func utc(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.UTC) }

func rec(t time.Time, complete bool) oanda.Record {
	return oanda.Record{
		Time:    t,
		BidOpen: num.MustParsePrice("1.10000"), BidHigh: num.MustParsePrice("1.10100"),
		BidLow: num.MustParsePrice("1.09900"), BidClose: num.MustParsePrice("1.10050"),
		AskOpen: num.MustParsePrice("1.10010"), AskHigh: num.MustParsePrice("1.10110"),
		AskLow: num.MustParsePrice("1.09910"), AskClose: num.MustParsePrice("1.10060"),
		Volume: 100, Complete: complete,
	}
}

func putRaw(t *testing.T, symbol string, year int, month time.Month, records ...oanda.Record) {
	t.Helper()
	key := oanda.PartitionKey{Symbol: symbol, Interval: oanda.RawH1, Year: year, Month: month}
	require.NoError(t, oanda.WritePartition(context.Background(), key, records, true))
}

func run(t *testing.T, req BuildRequest) ([]PartitionResult, error) {
	t.Helper()
	var got []PartitionResult
	err := BuildCanonical(context.Background(), nil, req, func(r PartitionResult) { got = append(got, r) })
	return got, err
}

func janToMar(instruments ...string) BuildRequest {
	return BuildRequest{Instruments: instruments, Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 3, 1, 0), BuiltAt: utc(2026, 11, 15, 12)}
}

func TestBuildCanonical_BuildsEachMonthInOrderAndReportsAMissingRawMonth(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.January, rec(utc(2024, 1, 2, 0), true), rec(utc(2024, 1, 2, 1), true))
	putRaw(t, "GBPUSD", 2024, time.January, rec(utc(2024, 1, 2, 0), true))
	putRaw(t, "GBPUSD", 2024, time.February, rec(utc(2024, 2, 1, 0), true))

	got, err := run(t, janToMar("EURUSD", "GBPUSD"))

	require.NoError(t, err)
	require.Len(t, got, 4)
	assert.Equal(t, PartitionResult{Instrument: "EURUSD", Year: 2024, Month: time.January,
		Path: "oanda/canonical/EURUSD/2024/01/EURUSD-2024-01-h1.csv", Action: Built, Good: 2}, got[0])
	assert.Equal(t, PartitionResult{Instrument: "EURUSD", Year: 2024, Month: time.February, Action: Missing}, got[1],
		"no raw month is reported, not an error")
	assert.Equal(t, "GBPUSD", got[2].Instrument)
	assert.Equal(t, Built, got[2].Action)
	assert.Equal(t, Built, got[3].Action)
	files, err := store.List("oanda/canonical")
	require.NoError(t, err)
	assert.Len(t, files, 3)
}

func TestBuildCanonical_ASecondRunIsUnchangedAndNewRawRowsReplace(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.January, rec(utc(2024, 1, 2, 0), true))
	req := janToMar("EURUSD")

	_, err := run(t, req)
	require.NoError(t, err)
	again, err := run(t, req)
	require.NoError(t, err)
	require.NoError(t, oanda.UpdatePartition(context.Background(), oanda.PartitionKey{Symbol: "EURUSD", Interval: oanda.RawH1, Year: 2024, Month: time.January},
		func(existing []oanda.Record) ([]oanda.Record, error) {
			return append(existing, rec(utc(2024, 1, 2, 1), true)), nil
		}))
	changed, err := run(t, req)

	require.NoError(t, err)
	assert.Equal(t, Unchanged, again[0].Action)
	assert.Equal(t, Replaced, changed[0].Action)
	assert.Equal(t, 2, changed[0].Good)
}

func TestBuildCanonical_BadRowsAreCountedNotHidden(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.January, rec(utc(2024, 1, 2, 0), true), rec(utc(2024, 1, 2, 1), false))

	got, err := run(t, janToMar("EURUSD"))

	require.NoError(t, err)
	assert.Equal(t, 1, got[0].Good)
	assert.Equal(t, 1, got[0].Bad)
	assert.Equal(t, Built, got[0].Action, "a partial partition is still built; the counts say it is not complete")
}

func TestBuildCanonical_AFailedPartitionDoesNotStopTheRest(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.February, rec(utc(2024, 2, 1, 0), true))
	damaged, err := oanda.PartitionKey{Symbol: "EURUSD", Interval: oanda.RawH1, Year: 2024, Month: time.January}.Path()
	require.NoError(t, err)
	require.NoError(t, store.Write(context.Background(), damaged, []byte("not a raw file\n")))

	got, err := run(t, janToMar("EURUSD"))

	require.ErrorIs(t, err, ErrPartitionsFailed)
	require.Len(t, got, 2)
	assert.Equal(t, Failed, got[0].Action)
	assert.ErrorIs(t, got[0].Err, oanda.ErrMalformedData)
	assert.Equal(t, Built, got[1].Action, "February is built despite January failing")
	_, rerr := store.Read("oanda/canonical/EURUSD/2024/01/EURUSD-2024-01-h1.csv")
	assert.ErrorIs(t, rerr, os.ErrNotExist, "a failed partition writes nothing")
}

func TestBuildCanonical_AnIntervalTheDomainDoesNotBuildFailsEveryPartition(t *testing.T) {
	storetest.Root(t)
	key := oanda.PartitionKey{Symbol: "EURUSD", Interval: oanda.RawH4, Year: 2024, Month: time.January}
	require.NoError(t, oanda.WritePartition(context.Background(), key, []oanda.Record{rec(utc(2024, 1, 2, 0), true)}, true))
	req := janToMar("EURUSD")
	req.Interval = "h4"

	got, err := run(t, req)

	require.ErrorIs(t, err, ErrPartitionsFailed)
	assert.Equal(t, Failed, got[0].Action)
	assert.Equal(t, Missing, got[1].Action)
}

func TestBuildCanonical_RejectsARequestItCannotRun(t *testing.T) {
	storetest.Root(t)
	for name, mutate := range map[string]func(*BuildRequest){
		"gold":           func(r *BuildRequest) { r.Instruments = []string{"XAUUSD"} },
		"weekly":         func(r *BuildRequest) { r.Interval = "w1" },
		"traversal":      func(r *BuildRequest) { r.Instruments = []string{"../EURUSD"} },
		"range reversed": func(r *BuildRequest) { r.From, r.To = r.To, r.From },
		"empty range":    func(r *BuildRequest) { r.To = r.From },
	} {
		t.Run(name, func(t *testing.T) {
			req := janToMar("EURUSD")
			mutate(&req)
			called := false

			err := BuildCanonical(context.Background(), nil, req, func(PartitionResult) { called = true })

			assert.Error(t, err)
			assert.False(t, called, "nothing is built for a request that cannot run")
		})
	}
}

func TestBuildCanonical_CancellationStopsTheRun(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.January, rec(utc(2024, 1, 2, 0), true))
	ctx, cancel := context.WithCancel(context.Background())
	var got []PartitionResult

	err := BuildCanonical(ctx, nil, janToMar("EURUSD"), func(r PartitionResult) { got = append(got, r); cancel() })

	require.ErrorIs(t, err, context.Canceled)
	assert.Len(t, got, 1, "the run stops after the partition that was in progress")
}

func TestBuildCanonical_NeverChangesRaw(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.January, rec(utc(2024, 1, 2, 0), true), rec(utc(2024, 1, 2, 1), false))
	rawPath, err := oanda.PartitionKey{Symbol: "EURUSD", Interval: oanda.RawH1, Year: 2024, Month: time.January}.Path()
	require.NoError(t, err)
	before, err := store.Read(rawPath)
	require.NoError(t, err)

	_, err = run(t, janToMar("EURUSD"))
	require.NoError(t, err)

	after, err := store.Read(rawPath)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestBuildCanonical_BuiltAtDefaultsToNow(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.January, rec(utc(2024, 1, 2, 0), true))
	req := janToMar("EURUSD")
	req.BuiltAt = time.Time{}
	start := time.Now().UTC().Add(-time.Second)

	got, err := run(t, req)

	require.NoError(t, err)
	data, err := store.Read(got[0].Path)
	require.NoError(t, err)
	m := regexp.MustCompile(`"built_at":"([^"]+)"`).FindSubmatch(data)
	require.NotNil(t, m, "the meta line records the build time")
	builtAt, err := time.Parse(time.RFC3339Nano, string(m[1]))
	require.NoError(t, err)
	assert.WithinDuration(t, start, builtAt, time.Minute)
}

// TestService_ImportsOnlyTheDomainServiceAndProviders keeps the application
// service in its place (ADR-091): within the module it may import the engine (the
// root package), the public market-data model, the market-data domain service and a
// provider, never a transport, the store or the layout directly. File data goes
// through the domain service and the provider. The one exception is migrate.go: the
// layout migration (#642) is an operation on the archive's directory structure, not
// on market data, so it uses the foundational store and layout packages directly
// (ADR-091 lets foundational packages be imported without a service wrapper); no
// other file may.
func TestService_ImportsOnlyTheDomainServiceAndProviders(t *testing.T) {
	const mod = "github.com/rustyeddy/trader"
	allowed := map[string]bool{mod: true, mod + "/marketdata": true, mod + "/marketdata/service": true, mod + "/provider/oanda": true}
	foundational := map[string]bool{"migrate.go": true}
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	checked := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		checked++
		f, perr := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		require.NoError(t, perr)
		for _, imp := range f.Imports {
			path, uerr := strconv.Unquote(imp.Path.Value)
			require.NoError(t, uerr)
			if foundational[file] && (path == mod+"/store" || path == mod+"/internal/marketdata/layout") {
				continue
			}
			if path == mod || strings.HasPrefix(path, mod+"/") {
				assert.True(t, allowed[path], "%s imports %s", file, path)
			}
		}
	}
	assert.Positive(t, checked)
}

// TestBuildCanonical_FromTheArchiveFixtures builds real archive months (the raw
// fixtures copied from the archive) and checks the canonical bars against the raw
// rows they came from: every row is accounted for, and a good bar's bid, ask and
// volume are the raw row's, unchanged.
func TestBuildCanonical_FromTheArchiveFixtures(t *testing.T) {
	for _, tc := range []struct {
		file, symbol string
		interval     oanda.RawInterval
		year         int
		month        time.Month
	}{
		{"EURUSD-2020-05-h1.csv", "EURUSD", oanda.RawH1, 2020, time.May},
		{"EURUSD-2020-05-d1.csv", "EURUSD", oanda.RawD1, 2020, time.May},
		{"USDJPY-2020-05-h1.csv", "USDJPY", oanda.RawH1, 2020, time.May},
	} {
		t.Run(tc.file, func(t *testing.T) {
			storetest.Root(t)
			ctx := context.Background()
			key := oanda.PartitionKey{Symbol: tc.symbol, Interval: tc.interval, Year: tc.year, Month: tc.month}
			rawPath, err := key.Path()
			require.NoError(t, err)
			data, err := os.ReadFile(filepath.Join("..", "provider", "oanda", "testdata", tc.file))
			require.NoError(t, err)
			require.NoError(t, store.Write(ctx, rawPath, data))
			raw, err := oanda.ReadPartitionRecords(ctx, key)
			require.NoError(t, err)

			var got []PartitionResult
			err = BuildCanonical(ctx, nil, BuildRequest{Instruments: []string{tc.symbol}, Interval: string(tc.interval),
				From: utc(tc.year, tc.month, 1, 0), To: utc(tc.year, tc.month+1, 1, 0), BuiltAt: utc(2026, 11, 15, 12)},
				func(r PartitionResult) { got = append(got, r) })

			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, Built, got[0].Action)
			assert.Equal(t, len(raw), got[0].Good+got[0].Bad, "every raw row is either a bar or a bad record")
			assert.Positive(t, got[0].Good)
			t.Logf("%s: %d raw rows, %d good, %d bad", tc.file, len(raw), got[0].Good, got[0].Bad)
		})
	}
}

// buildAll writes raw months and builds them, so there are canonical files to load.
func buildAll(t *testing.T, symbol string, months map[time.Month][]int) {
	t.Helper()
	for m, days := range months {
		var recs []oanda.Record
		for _, d := range days {
			recs = append(recs, rec(utc(2024, m, d, 0), true))
		}
		putRaw(t, symbol, 2024, m, recs...)
	}
	require.NoError(t, BuildCanonical(context.Background(), nil, janToMar(symbol), nil))
}

// buildHours writes n consecutive hourly raw bars from start (months split as the
// run crosses them) and builds them, so the canonical bars have no gap.
func buildHours(t *testing.T, symbol string, start time.Time, n int) {
	t.Helper()
	byMonth := map[time.Month][]oanda.Record{}
	for i := range n {
		at := start.Add(time.Duration(i) * time.Hour)
		byMonth[at.Month()] = append(byMonth[at.Month()], rec(at, true))
	}
	for m, recs := range byMonth {
		putRaw(t, symbol, start.Year(), m, recs...)
	}
	require.NoError(t, BuildCanonical(context.Background(), nil, janToMar(symbol), nil))
}

func loadReq(instruments ...string) LoadRequest {
	return LoadRequest{Instruments: instruments, Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 3, 1, 0)}
}

func TestLoadSeries_LoadsEachInstrumentInRequestOrder(t *testing.T) {
	storetest.Root(t)
	buildHours(t, "EURUSD", utc(2024, 1, 31, 22), 3) // Jan 31 22:00 to Feb 1 00:00
	buildHours(t, "GBPUSD", utc(2024, 1, 31, 23), 2)

	got, err := LoadSeries(context.Background(), nil, loadReq("GBPUSD", "EURUSD"))

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "GBPUSD", got[0].Instrument)
	assert.Len(t, got[0].Series.Bars, 2)
	assert.Equal(t, "EURUSD", got[1].Instrument)
	assert.Len(t, got[1].Series.Bars, 3)
	assert.Equal(t, "EURUSD", got[1].Series.Bars[0].Instrument)
	require.Len(t, got[1].Months, 2)
	assert.Equal(t, MonthLoaded, got[1].Months[0].State)
	assert.NoError(t, got[1].Err)
}

func TestLoadSeries_AnInstrumentThatCannotLoadDoesNotStopTheRest(t *testing.T) {
	storetest.Root(t)
	buildHours(t, "EURUSD", utc(2024, 1, 31, 23), 2)
	buildAll(t, "GBPUSD", map[time.Month][]int{time.January: {2}}) // February missing

	got, err := LoadSeries(context.Background(), nil, loadReq("GBPUSD", "EURUSD"))

	require.ErrorIs(t, err, ErrNotLoadable)
	require.Len(t, got, 2)
	assert.ErrorIs(t, got[0].Err, mdservice.ErrIncomplete)
	assert.Empty(t, got[0].Series.Bars)
	assert.Equal(t, []MonthState{MonthLoaded, MonthMissing}, []MonthState{got[0].Months[0].State, got[0].Months[1].State},
		"the failed instrument still reports every month")
	require.NoError(t, got[1].Err)
	assert.Len(t, got[1].Series.Bars, 2, "EURUSD loads")
}

func TestLoadSeries_AllowFlagsReachTheDomainService(t *testing.T) {
	storetest.Root(t)
	buildAll(t, "GBPUSD", map[time.Month][]int{time.January: {2}})
	req := loadReq("GBPUSD")
	req.AllowMissing = true

	got, err := LoadSeries(context.Background(), nil, req)

	require.NoError(t, err)
	assert.Len(t, got[0].Series.Bars, 1)
	assert.Equal(t, MonthMissing, got[0].Months[1].State)
}

func TestLoadSeries_RefusesARequestItCannotRun(t *testing.T) {
	storetest.Root(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	_, noInstrument := LoadSeries(context.Background(), nil, loadReq())
	reversed := loadReq("EURUSD")
	reversed.From, reversed.To = reversed.To, reversed.From
	_, errRange := LoadSeries(context.Background(), nil, reversed)
	_, errCtx := LoadSeries(cancelled, nil, loadReq("EURUSD"))
	h4 := loadReq("EURUSD")
	h4.Interval = "h4"
	got, errInterval := LoadSeries(context.Background(), nil, h4)

	assert.Error(t, noInstrument)
	assert.Error(t, errRange)
	assert.ErrorIs(t, errCtx, context.Canceled)
	require.ErrorIs(t, errInterval, ErrNotLoadable, "the domain's refusal is the instrument's, reported in its result")
	assert.ErrorIs(t, got[0].Err, mdservice.ErrUnsupportedInterval)
}

// dlFetcher serves records from a fixed series for a [From, To) window, as the
// provider would, and remembers every request.
type dlFetcher struct {
	mu     sync.Mutex
	series []oanda.Record
	reqs   []oanda.CandleRequest
	err    error
}

func (f *dlFetcher) FetchCandles(_ context.Context, req oanda.CandleRequest) ([]oanda.Record, error) {
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

func dlHourly(from time.Time, n int) []oanda.Record {
	var out []oanda.Record
	for i := range n {
		out = append(out, rec(from.Add(time.Duration(i)*time.Hour), true))
	}
	return out
}

func download(t *testing.T, f CandleFetcher, req DownloadRequest) []DownloadResult {
	t.Helper()
	var got []DownloadResult
	require.NoError(t, DownloadRaw(context.Background(), nil, f, req, func(r DownloadResult) { got = append(got, r) }))
	return got
}

func TestDownloadRaw_OneBoundedRequestAndOneFilePerMonth(t *testing.T) {
	storetest.Root(t)
	f := &dlFetcher{series: dlHourly(utc(2024, 1, 30, 0), 72)} // Jan 30 .. Feb 1, 23:00

	got := download(t, f, DownloadRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: utc(2024, 1, 30, 0), To: utc(2024, 2, 2, 0)})

	require.Len(t, got, 2)
	assert.Equal(t, 48, got[0].Added, "January holds Jan 30 and Jan 31")
	assert.Equal(t, 24, got[1].Added, "February holds Feb 1")
	assert.Equal(t, filepath.Join("oanda", "raw", "EURUSD", "2024", "01", "EURUSD-2024-01-h1.csv"), got[0].Path)
	assert.Equal(t, "EURUSD", got[0].Instrument)
	assert.Equal(t, 2024, got[1].Year)
	assert.Equal(t, time.February, got[1].Month)
	assert.Equal(t, []oanda.CandleRequest{
		{Symbol: "EURUSD", Interval: oanda.RawH1, From: utc(2024, 1, 30, 0), To: utc(2024, 2, 1, 0)},
		{Symbol: "EURUSD", Interval: oanda.RawH1, From: utc(2024, 2, 1, 0), To: utc(2024, 2, 2, 0)},
	}, f.reqs, "the first and last months are clamped to the requested range")
}

func TestDownloadRaw_ARerunAddsNothing(t *testing.T) {
	storetest.Root(t)
	f := &dlFetcher{series: dlHourly(utc(2024, 1, 1, 0), 24)}
	req := DownloadRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 1, 2, 0)}
	download(t, f, req)

	second := download(t, f, req)

	assert.Equal(t, 0, second[0].Added)
	assert.Equal(t, 24, second[0].Kept)
}

// Daily bars open at 21:00 or 22:00 UTC, so the last bar of a month opens on its
// last evening and belongs to that month's file (ADR-089).
func TestDownloadRaw_DailyBarsLandInTheirUTCOpeningMonth(t *testing.T) {
	storetest.Root(t)
	f := &dlFetcher{series: []oanda.Record{
		rec(utc(2020, 4, 29, 21), true),
		rec(utc(2020, 4, 30, 21), true), // opens in April, trades into May
		rec(utc(2020, 5, 3, 21), true),
	}}

	got := download(t, f, DownloadRequest{Instruments: []string{"EURUSD"}, Interval: "d1", From: utc(2020, 4, 1, 0), To: utc(2020, 5, 10, 0)})

	require.Len(t, got, 2)
	assert.Equal(t, 2, got[0].Added)
	assert.Equal(t, 1, got[1].Added)
	april, err := oanda.ReadPartitionRecords(context.Background(), oanda.PartitionKey{Symbol: "EURUSD", Interval: oanda.RawD1, Year: 2020, Month: time.April})
	require.NoError(t, err)
	require.Len(t, april, 2)
	assert.True(t, april[1].Time.Equal(utc(2020, 4, 30, 21)))
}

func TestDownloadRaw_SeveralInstrumentsInOrder(t *testing.T) {
	storetest.Root(t)
	f := &dlFetcher{series: dlHourly(utc(2024, 1, 1, 0), 2)}

	got := download(t, f, DownloadRequest{Instruments: []string{"GBPUSD", "EURUSD"}, Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 1, 2, 0)})

	require.Len(t, got, 2)
	assert.Equal(t, "GBPUSD", got[0].Instrument)
	assert.Equal(t, "EURUSD", got[1].Instrument)
}

func TestDownloadRaw_RefusesBadRequestsBeforeAnyFetch(t *testing.T) {
	day := func(d int) time.Time { return utc(2024, 1, d, 0) }
	for name, tc := range map[string]struct {
		req  DownloadRequest
		want string
	}{
		"not in scope":  {DownloadRequest{Instruments: []string{"EURUSD", "XAUUSD"}, Interval: "h1", From: day(1), To: day(2)}, "XAUUSD"},
		"bad interval":  {DownloadRequest{Instruments: []string{"EURUSD"}, Interval: "w1", From: day(1), To: day(2)}, "w1"},
		"empty range":   {DownloadRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: day(2), To: day(2)}, "--to must be after --from"},
		"reversed span": {DownloadRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: day(3), To: day(2)}, "--to must be after --from"},
	} {
		t.Run(name, func(t *testing.T) {
			f := &dlFetcher{series: dlHourly(day(1), 2)}

			err := DownloadRaw(context.Background(), nil, f, tc.req, nil)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Empty(t, f.reqs, "no request is made")
		})
	}
}

func TestDownloadRaw_AFailureStopsAndNamesThePartition(t *testing.T) {
	storetest.Root(t)
	boom := errors.New("provider down")
	f := &dlFetcher{err: boom}
	var reported int

	err := DownloadRaw(context.Background(), nil, f, DownloadRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 3, 1, 0)},
		func(DownloadResult) { reported++ })

	require.ErrorIs(t, err, boom)
	assert.Contains(t, err.Error(), "EURUSD 2024-01")
	assert.Zero(t, reported)
	assert.Len(t, f.reqs, 1, "the run stops at the first failure")
}

func TestDownloadRaw_ACancelledContextFetchesAndWritesNothing(t *testing.T) {
	root := storetest.Root(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &dlFetcher{series: dlHourly(utc(2024, 1, 1, 0), 2)}

	err := DownloadRaw(ctx, nil, f, DownloadRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 1, 2, 0)}, nil)

	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, f.reqs)
	entries, rerr := os.ReadDir(root)
	require.NoError(t, rerr)
	assert.Empty(t, entries)
}

// TestService_ReadsNoFlagsEnvironmentOrCredentials keeps the application service
// free of transport concerns: it imports neither os, flag nor a command library,
// so configuration and the credential reach it only as plain values and as the
// fetcher the transport builds.
func TestService_ReadsNoFlagsEnvironmentOrCredentials(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, perr := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		require.NoError(t, perr)
		for _, imp := range f.Imports {
			path, uerr := strconv.Unquote(imp.Path.Value)
			require.NoError(t, uerr)
			assert.NotContains(t, []string{"os", "flag", "github.com/spf13/cobra"}, path, "%s imports %s", file, path)
		}
	}
}

func TestLoadSeries_AnUnexplainedGapFailsTheInstrumentAndIsInItsResult(t *testing.T) {
	storetest.Root(t)
	putRaw(t, "EURUSD", 2024, time.January, rec(utc(2024, 1, 9, 0), true), rec(utc(2024, 1, 9, 1), true), rec(utc(2024, 1, 9, 5), true))
	require.NoError(t, BuildCanonical(context.Background(), nil, janToMar("EURUSD"), nil))
	req := loadReq("EURUSD")
	req.To = utc(2024, 2, 1, 0)

	strict, err := LoadSeries(context.Background(), nil, req)
	req.AllowMissing = true
	allowed, aerr := LoadSeries(context.Background(), nil, req)

	require.ErrorIs(t, err, ErrNotLoadable)
	assert.ErrorIs(t, strict[0].Err, mdservice.ErrIncomplete)
	assert.Equal(t, []Gap{{Kind: GapUnexplained, Start: utc(2024, 1, 9, 2), End: utc(2024, 1, 9, 5), Bars: 3}}, strict[0].Gaps)
	assert.Empty(t, strict[0].Series.Bars)
	require.NoError(t, aerr)
	assert.Equal(t, strict[0].Gaps, allowed[0].Gaps)
	assert.Len(t, allowed[0].Series.Bars, 3)
}

type fixedToken string

func (f fixedToken) Token(context.Context) (string, error) { return string(f), nil }

func TestDownload_RefusesAProviderItCannotBuildBeforeAnyFetch(t *testing.T) {
	storetest.Root(t)
	req := DownloadRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 1, 2, 0)}
	for name, cfg := range map[string]ProviderConfig{
		"no credential": {BaseURL: "https://api-fxpractice.oanda.com"},
		"no base URL":   {Credential: fixedToken("secret-token-value")},
		"not https":     {BaseURL: "http://api-fxpractice.oanda.com", Credential: fixedToken("secret-token-value")},
	} {
		t.Run(name, func(t *testing.T) {
			err := Download(context.Background(), nil, cfg, req, func(DownloadResult) { t.Fatal("nothing is downloaded") })

			require.Error(t, err)
			assert.NotContains(t, err.Error(), "secret-token-value", "the token is never in an error")
		})
	}
}

func TestDownload_StillValidatesTheRequestAfterBuildingTheClient(t *testing.T) {
	storetest.Root(t)
	cfg := ProviderConfig{BaseURL: "https://api-fxpractice.oanda.com", Credential: fixedToken("t")}

	err := Download(context.Background(), nil, cfg, DownloadRequest{Instruments: []string{"EURUSD"}, Interval: "h1", From: utc(2024, 2, 1, 0), To: utc(2024, 1, 1, 0)}, nil)

	assert.ErrorContains(t, err, "--to must be after --from")
}
