package service

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
// service in its place (ADR-091): within the module it may import the public
// market-data model, the market-data domain service and a provider, never a transport, the root engine, the store or
// the layout directly. File data goes through the domain service and the provider.
func TestService_ImportsOnlyTheDomainServiceAndProviders(t *testing.T) {
	const mod = "github.com/rustyeddy/trader"
	allowed := map[string]bool{mod + "/marketdata": true, mod + "/marketdata/service": true, mod + "/provider/oanda": true}
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

func loadReq(instruments ...string) LoadRequest {
	return LoadRequest{Instruments: instruments, Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 3, 1, 0)}
}

func TestLoadSeries_LoadsEachInstrumentInRequestOrder(t *testing.T) {
	storetest.Root(t)
	buildAll(t, "EURUSD", map[time.Month][]int{time.January: {2, 3}, time.February: {1}})
	buildAll(t, "GBPUSD", map[time.Month][]int{time.January: {2}, time.February: {1}})

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
	buildAll(t, "EURUSD", map[time.Month][]int{time.January: {2}, time.February: {1}})
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
