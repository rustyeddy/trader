package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/examples/emax/emax"
	"github.com/rustyeddy/trader/internal/id"
	"github.com/rustyeddy/trader/internal/results"
	"github.com/rustyeddy/trader/internal/store/storetest"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/provider/oanda"
	"github.com/rustyeddy/trader/strategy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// quote is a raw hourly candle of the instrument at the given bid close with a
// spread of two ticks, so its mid is a whole tick (an odd spread has a half-tick
// mid).
func quote(t time.Time, bid string, spreadTicks int64) oanda.Record {
	b := num.MustParsePrice(bid)
	tick := num.MustParsePrice("0.00001")
	a := b
	for range spreadTicks {
		var err error
		a, err = a.Add(tick)
		if err != nil {
			panic(err)
		}
	}
	hi, lo := num.MustParsePrice("0.00100"), num.MustParsePrice("0.00100")
	bh, _ := b.Add(hi)
	bl, _ := b.Sub(lo)
	ah, _ := a.Add(hi)
	al, _ := a.Sub(lo)
	return oanda.Record{Time: t, BidOpen: b, BidHigh: bh, BidLow: bl, BidClose: b,
		AskOpen: a, AskHigh: ah, AskLow: al, AskClose: a, Volume: 100, Complete: true}
}

// month puts one raw month of hourly quotes for the instrument in the store: the
// bids follow the given points (ticks above the base, in ticks), two ticks of spread.
func month(t *testing.T, instrument string, m time.Month, base int64, points ...int64) {
	t.Helper()
	monthSpread(t, instrument, m, base, 2, points...)
}

// monthSpread is month with the spread given in ticks: an odd one makes the mid a
// half tick, which is how real quotes usually look.
func monthSpread(t *testing.T, instrument string, m time.Month, base, spreadTicks int64, points ...int64) {
	t.Helper()
	var rows []oanda.Record
	for h, p := range points {
		v := base + p
		rows = append(rows, quote(utc(2024, m, 2, h), fmt.Sprintf("%d.%05d", v/100000, v%100000), spreadTicks))
	}
	putRaw(t, instrument, 2024, m, rows...)
}

var wave = []int64{0, 0, 0, 0, 500, 1000, 1500, 1000, 500, 0, -500, -1000}

// backtestReq is a request for January 2024, journaled under a temporary results root.
func backtestReq(t *testing.T, instruments ...string) BacktestRequest {
	t.Helper()
	return BacktestRequest{Instruments: instruments, Interval: "h1", From: utc(2024, 1, 1, 0), To: utc(2024, 2, 1, 0), ResultsRoot: t.TempDir()}
}

func buildCanonical(t *testing.T, req BuildRequest) {
	t.Helper()
	_, err := run(t, req)
	require.NoError(t, err)
}

func TestRunBacktest_LoadsTwoInstrumentsFromCanonicalStorageAndRunsThem(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	month(t, "GBPUSD", time.January, 125000, wave...)
	buildCanonical(t, janToMar("EURUSD", "GBPUSD"))

	res, err := RunBacktest(context.Background(), nil, backtestReq(t, "EURUSD", "GBPUSD"))

	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, res.Write(&out))
	assert.Contains(t, out.String(), "oanda EURUSD,GBPUSD h1 [2024-01-01T00:00:00Z, 2024-02-01T00:00:00Z): 24 bars")
	assert.Contains(t, out.String(), "orders: submitted")
	assert.NotContains(t, out.String(), "submitted 0,", "the crossovers produced orders on the shared account")
}

func TestRunBacktest_ARangeIsCutByOpeningTime(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	buildCanonical(t, janToMar("EURUSD"))
	req := backtestReq(t, "EURUSD")
	req.From, req.To = utc(2024, 1, 2, 2), utc(2024, 1, 2, 8) // opening times 02:00 to 07:00

	res, err := RunBacktest(context.Background(), nil, req)

	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, res.Write(&out))
	assert.Contains(t, out.String(), ": 6 bars")
}

func TestRunBacktest_AMissingMonthFailsUnlessAllowed(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	buildCanonical(t, janToMar("EURUSD"))
	req := backtestReq(t, "EURUSD")
	req.To = utc(2024, 3, 1, 0) // February has no data

	res, err := RunBacktest(context.Background(), nil, req)

	require.ErrorIs(t, err, ErrNotLoadable)
	if res != nil { // a run that failed after it was made comes back with its RunID, never a result
		assert.Nil(t, res.Result)
	}
	for _, want := range []string{"oanda EURUSD h1", "2024-02 missing"} {
		assert.Contains(t, err.Error(), want, "the error names source, instrument, interval and the month")
	}
	req.AllowMissing = true
	res, err = RunBacktest(context.Background(), nil, req)
	require.NoError(t, err)
	assert.NotNil(t, res)
}

func TestRunBacktest_ABadMonthFailsUnlessAllowedAndNothingRunsWhenOneInstrumentFails(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	// GBPUSD's raw month has a candle still forming: a bad record.
	rows := []oanda.Record{quote(utc(2024, 1, 2, 0), "1.25000", 2), quote(utc(2024, 1, 2, 1), "1.25000", 2)}
	rows[1].Complete = false
	putRaw(t, "GBPUSD", 2024, time.January, rows...)
	buildCanonical(t, janToMar("EURUSD", "GBPUSD"))

	res, err := RunBacktest(context.Background(), nil, backtestReq(t, "EURUSD", "GBPUSD"))

	require.ErrorIs(t, err, ErrNotLoadable)
	if res != nil { // a run that failed after it was made comes back with its RunID, never a result
		assert.Nil(t, res.Result)
	}
	assert.Contains(t, err.Error(), "GBPUSD")
	assert.Contains(t, err.Error(), "2024-01 bad")
	assert.NotContains(t, err.Error(), "EURUSD: ", "the instrument that loaded is not blamed")

	req := backtestReq(t, "EURUSD", "GBPUSD")
	req.AllowBad = true
	res, err = RunBacktest(context.Background(), nil, req)
	require.NoError(t, err)
	assert.NotNil(t, res)
}

func TestRunBacktest_RefusesWhatCannotBeRun(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	buildCanonical(t, janToMar("EURUSD"))
	reversed := backtestReq(t, "EURUSD")
	reversed.From, reversed.To = reversed.To, reversed.From
	otherSource := backtestReq(t, "EURUSD")
	otherSource.Source = "alpaca"
	noInstrument := backtestReq(t)
	badInterval := backtestReq(t, "EURUSD")
	badInterval.Interval = "w1"
	nothing := backtestReq(t, "EURUSD")
	nothing.From, nothing.To = utc(2024, 1, 20, 0), utc(2024, 1, 21, 0) // the month loads, no bar opens in it
	for name, tc := range map[string]struct {
		req  BacktestRequest
		want string
	}{
		"reversed range": {reversed, "--to must be after --from"},
		"other source":   {otherSource, `source "alpaca" is not supported`},
		"no instrument":  {noInstrument, "no instrument"},
		"bad interval":   {badInterval, "w1"},
		"no bars":        {nothing, "no bars in the range"},
	} {
		t.Run(name, func(t *testing.T) {
			res, err := RunBacktest(context.Background(), nil, tc.req)

			require.Error(t, err)
			if res != nil { // a run that failed after it was made comes back with its RunID, never a result
				assert.Nil(t, res.Result)
			}
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestRunBacktest_AnInstrumentTheEngineHasNoMarketForIsRefusedByTheEngine(t *testing.T) {
	storetest.Root(t)
	month(t, "USDJPY", time.January, 14000000, wave...)
	buildCanonical(t, janToMar("USDJPY"))

	res, err := RunBacktest(context.Background(), nil, backtestReq(t, "USDJPY"))

	require.Error(t, err)
	if res != nil { // a run that failed after it was made comes back with its RunID, never a result
		assert.Nil(t, res.Result)
	}
	assert.Contains(t, err.Error(), "USDJPY")
	assert.Contains(t, err.Error(), "oanda USDJPY h1")
}

func TestRunBacktest_ACancelledContextStopsTheRun(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	buildCanonical(t, janToMar("EURUSD"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := RunBacktest(ctx, nil, backtestReq(t, "EURUSD"))

	require.ErrorIs(t, err, context.Canceled)
}

// The review of #668: realistic quotes, a one-tick spread and so a half-tick mid,
// go from raw through BuildCanonical and LoadSeries to RunBacktest. The canonical
// mid stays as the file has it; the engine rounds to the nearest tick on its copy.
func TestRunBacktest_ARealisticOneTickSpreadRunsAndKeepsTheHalfTickMid(t *testing.T) {
	storetest.Root(t)
	monthSpread(t, "EURUSD", time.January, 110000, 1, wave...)
	monthSpread(t, "GBPUSD", time.January, 125000, 1, wave...)
	buildCanonical(t, janToMar("EURUSD", "GBPUSD"))
	loaded, err := LoadSeries(context.Background(), nil, LoadRequest{Instruments: []string{"EURUSD"}, Interval: "h1",
		From: utc(2024, 1, 1, 0), To: utc(2024, 2, 1, 0)})
	require.NoError(t, err)
	onTick, err := loaded[0].Series.Bars[0].Mid.Close.DivisibleBy(num.MustParsePrice("0.00001"))
	require.NoError(t, err)
	require.False(t, onTick, "the canonical mid sits between two ticks: %s", loaded[0].Series.Bars[0].Mid.Close)

	res, err := RunBacktest(context.Background(), nil, backtestReq(t, "EURUSD", "GBPUSD"))

	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, res.Write(&out))
	assert.Contains(t, out.String(), ": 24 bars")
	assert.NotContains(t, out.String(), "submitted 0,", "orders were placed")
	assert.NotContains(t, out.String(), "rejected 1", "none was refused for its price")
	reloaded, err := LoadSeries(context.Background(), nil, LoadRequest{Instruments: []string{"EURUSD"}, Interval: "h1",
		From: utc(2024, 1, 1, 0), To: utc(2024, 2, 1, 0)})
	require.NoError(t, err)
	assert.Equal(t, loaded[0].Series.Bars[0].Mid, reloaded[0].Series.Bars[0].Mid, "the canonical data is as it was")
}

// February is traded and has only two bars: too few for the long EMA. The bars of
// January, the month before, prefill it when they can be loaded.
func TestRunBacktest_TheMonthBeforeTheRangePrefillsTheIndicators(t *testing.T) {
	febReq := func() BacktestRequest {
		r := backtestReq(t, "EURUSD")
		r.From, r.To = utc(2024, 2, 1, 0), utc(2024, 3, 1, 0)
		return r
	}
	summary := func(t *testing.T, req BacktestRequest) string {
		t.Helper()
		res, err := RunBacktest(context.Background(), nil, req)
		require.NoError(t, err)
		var out bytes.Buffer
		require.NoError(t, res.Write(&out))
		return out.String()
	}

	t.Run("January available", func(t *testing.T) {
		storetest.Root(t)
		month(t, "EURUSD", time.January, 110000, wave...)
		month(t, "EURUSD", time.February, 110000, 0, 500)
		buildCanonical(t, janToMar("EURUSD"))

		out := summary(t, febReq())

		assert.Contains(t, out, ": 2 bars", "only February's bars are counted")
		assert.NotContains(t, out, "submitted 0,", "the EMAs were seeded by January")
	})
	t.Run("January missing: no warm-up, the run still goes", func(t *testing.T) {
		storetest.Root(t)
		month(t, "EURUSD", time.February, 110000, 0, 500)
		buildCanonical(t, janToMar("EURUSD"))

		out := summary(t, febReq())

		assert.Contains(t, out, ": 2 bars")
		assert.Contains(t, out, "submitted 0,", "cold, two bars are too few")
	})
	t.Run("January has bad records: it is not used as a warm-up", func(t *testing.T) {
		storetest.Root(t)
		rows := []oanda.Record{quote(utc(2024, 1, 2, 0), "1.10000", 2), quote(utc(2024, 1, 2, 1), "1.10000", 2),
			quote(utc(2024, 1, 2, 2), "1.10500", 2), quote(utc(2024, 1, 2, 3), "1.11000", 2)}
		rows[3].Complete = false // a candle still forming: a bad record
		putRaw(t, "EURUSD", 2024, time.January, rows...)
		month(t, "EURUSD", time.February, 110000, 0, 500)
		buildCanonical(t, janToMar("EURUSD"))

		out := summary(t, febReq())

		assert.Contains(t, out, ": 2 bars")
		assert.Contains(t, out, "submitted 0,", "the bad month is skipped, not trusted")
	})
}

func TestRunBacktest_AnInteriorGapTheCalendarDoesNotExplainFailsUnlessAllowed(t *testing.T) {
	storetest.Root(t)
	var rows []oanda.Record
	for _, h := range []int{0, 1, 2, 3, 8, 9} { // 04:00 to 07:00 are missing
		rows = append(rows, quote(utc(2024, time.January, 9, h), "1.10000", 2))
	}
	putRaw(t, "EURUSD", 2024, time.January, rows...)
	buildCanonical(t, janToMar("EURUSD"))

	res, err := RunBacktest(context.Background(), nil, backtestReq(t, "EURUSD"))

	require.ErrorIs(t, err, ErrNotLoadable)
	if res != nil {
		assert.Nil(t, res.Result, "nothing is replayed over a hole")
	}
	assert.Contains(t, err.Error(), "gap unexplained 2024-01-09T04:00Z 2024-01-09T08:00Z, 4 bars")
	req := backtestReq(t, "EURUSD")
	req.AllowMissing = true
	res, err = RunBacktest(context.Background(), nil, req)
	require.NoError(t, err)
	assert.NotNil(t, res)
}

// A run, as a client such as Investor reads it: two files, nothing else.
type runFiles struct {
	rec    map[string]any
	events []map[string]any
}

func readRunDir(t *testing.T, dir string) runFiles {
	t.Helper()
	var r runFiles
	data, err := os.ReadFile(filepath.Join(dir, results.RunFile))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &r.rec))
	f, err := os.Open(filepath.Join(dir, results.EventsFile))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e map[string]any
		require.NoError(t, json.Unmarshal(sc.Bytes(), &e), sc.Text())
		r.events = append(r.events, e)
	}
	require.NoError(t, sc.Err())
	return r
}

// onlyRun is the one run directory under root.
func onlyRun(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	_, err = id.ParseRunID(entries[0].Name())
	require.NoError(t, err, "a run directory is named by its RunID")
	return filepath.Join(root, entries[0].Name())
}

func TestRunBacktest_JournalsTheRunInADirectoryNamedByItsRunID(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	buildCanonical(t, janToMar("EURUSD"))
	req := backtestReq(t, "EURUSD")

	res, err := RunBacktest(context.Background(), nil, req)
	require.NoError(t, err)

	assert.Equal(t, onlyRun(t, req.ResultsRoot), res.Dir)
	assert.Equal(t, filepath.Base(res.Dir), res.RunID.String())

	// From here on only the files are read: no trader object.
	run := readRunDir(t, res.Dir)
	assert.Equal(t, res.RunID.String(), run.rec["run_id"])
	assert.Equal(t, "backtest", run.rec["kind"])
	assert.Equal(t, map[string]any{"name": "emax", "transport": "in-process"}, run.rec["strategy"])
	assert.Equal(t, "oanda", run.rec["source"])
	assert.Equal(t, "h1", run.rec["interval"])
	assert.Equal(t, []any{"EURUSD"}, run.rec["instruments"])
	assert.Equal(t, "2024-01-01T00:00:00Z", run.rec["from"])
	assert.Equal(t, "2024-02-01T00:00:00Z", run.rec["to"])
	assert.Equal(t, "completed", run.rec["status"])
	assert.NotContains(t, run.rec, "error")
	started, err := time.Parse(time.RFC3339Nano, run.rec["started_at"].(string))
	require.NoError(t, err)
	ended, err := time.Parse(time.RFC3339Nano, run.rec["ended_at"].(string))
	require.NoError(t, err)
	assert.False(t, ended.Before(started))
	runStarted, err := res.RunID.Time()
	require.NoError(t, err)
	assert.WithinDuration(t, started, runStarted, time.Second, "the RunID carries the time the run began")

	// An opening request, its admission, its fill, and a closing fill.
	byOrder := map[float64][]map[string]any{}
	for i, e := range run.events {
		assert.EqualValues(t, i+1, e["seq"])
		byOrder[e["order"].(float64)] = append(byOrder[e["order"].(float64)], e)
	}
	open := byOrder[1]
	require.GreaterOrEqual(t, len(open), 4)
	for i, want := range []string{"requested", "sized", "submitted", "filled"} {
		assert.Equal(t, want, open[i]["state"])
	}
	assert.Equal(t, "open", open[0]["action"])
	assert.Equal(t, "EURUSD", open[0]["instrument"])
	assert.Equal(t, "strategy", open[0]["cause"])
	assert.NotEmpty(t, open[0]["stop_loss"], "the stop the strategy asked for")
	assert.NotEmpty(t, open[1]["units"], "the size the account chose")
	assert.NotEmpty(t, open[3]["fill_price"])

	var closed bool
	for _, e := range run.events {
		if e["state"] == "filled" && e["action"] == "close" {
			closed = true
		}
	}
	assert.True(t, closed, "a closing fill, by the strategy or the broker's stop")

	// The record's result is the engine's summary, and agrees with the journal: an
	// order is submitted once, and no run here amends a stop or trails one.
	result := run.rec["result"].(map[string]any)
	assert.Equal(t, "emax", result["strategy"])
	settings := result["settings"].(map[string]any)
	assert.Equal(t, run.rec["settings"], result["settings"], "the snapshot from the start is the summary's")
	assert.Equal(t, "default", settings["margin_policy"])
	assert.Equal(t, "0.01", settings["risk_per_trade"], "run.json says what sized the orders")
	assert.Equal(t, "0.5", settings["margin_closeout_level"])
	assert.Equal(t, map[string]any{"amount": "50000000", "currency": "USD"}, settings["max_order_notional"])
	assert.Len(t, settings["instruments"], 2)
	var submitted float64
	for _, e := range run.events {
		if e["state"] == "submitted" {
			submitted++
		}
	}
	assert.Equal(t, result["orders"].(map[string]any)["submitted"], submitted,
		"every order the engine counts as submitted has one submitted line")
}

// assertSettings checks a run's record holds the engine's settings.
func assertSettings(t *testing.T, run runFiles) {
	t.Helper()
	settings, err := trader.RunSettings()
	require.NoError(t, err)
	data, err := json.Marshal(settings)
	require.NoError(t, err)
	got, err := json.Marshal(run.rec["settings"])
	require.NoError(t, err)
	assert.JSONEq(t, string(data), string(got))
}

func TestRunBacktest_ARunThatFailsIsStillJournaledAsFailed(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	buildCanonical(t, janToMar("EURUSD"))
	req := backtestReq(t, "EURUSD")
	req.To = utc(2024, 3, 1, 0) // February was never built

	res, err := RunBacktest(context.Background(), nil, req)

	require.ErrorIs(t, err, ErrNotLoadable)
	if res != nil { // a run that failed after it was made comes back with its RunID, never a result
		assert.Nil(t, res.Result)
	}
	dir := onlyRun(t, req.ResultsRoot)
	assert.ErrorContains(t, err, dir, "the error says where the run is")
	run := readRunDir(t, dir)
	assert.Equal(t, "failed", run.rec["status"])
	assert.Contains(t, run.rec["error"], "EURUSD")
	assert.NotContains(t, run.rec, "result")
	assertSettings(t, run)
	assert.Equal(t, "backtest", run.rec["kind"])
	assert.NotEmpty(t, run.rec["ended_at"])
	assert.Empty(t, run.events, "it never got as far as an order")
}

func TestRunBacktest_ARequestTheEngineRefusesIsAFailedRunToo(t *testing.T) {
	storetest.Root(t)
	req := backtestReq(t)
	req.Interval = "m5"

	_, err := RunBacktest(context.Background(), nil, req)

	require.Error(t, err)
	run := readRunDir(t, onlyRun(t, req.ResultsRoot))
	assert.Equal(t, "failed", run.rec["status"])
	assert.NotEmpty(t, run.rec["error"])
	assertSettings(t, run)
}

func TestRunBacktest_ACancelledRunIsJournaledAsCancelled(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	buildCanonical(t, janToMar("EURUSD"))
	req := backtestReq(t, "EURUSD")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := RunBacktest(ctx, nil, req)

	require.ErrorIs(t, err, context.Canceled)
	run := readRunDir(t, onlyRun(t, req.ResultsRoot))
	assert.Equal(t, "cancelled", run.rec["status"])
	assertSettings(t, run)
}

func TestRunBacktest_NothingRunsWithoutSomewhereToRecordIt(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	buildCanonical(t, janToMar("EURUSD"))

	t.Run("no results root", func(t *testing.T) {
		req := backtestReq(t, "EURUSD")
		req.ResultsRoot = ""
		_, err := RunBacktest(context.Background(), nil, req)
		assert.ErrorContains(t, err, "no results root")
	})
	t.Run("a root that cannot be made", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
		req := backtestReq(t, "EURUSD")
		req.ResultsRoot = file
		_, err := RunBacktest(context.Background(), nil, req)
		assert.ErrorContains(t, err, "results root")
	})
	t.Run("an unsupported source is refused before a run exists", func(t *testing.T) {
		req := backtestReq(t, "EURUSD")
		req.Source = "stooq"
		_, err := RunBacktest(context.Background(), nil, req)
		assert.ErrorContains(t, err, "not supported")
		entries, rerr := os.ReadDir(req.ResultsRoot)
		require.NoError(t, rerr)
		assert.Empty(t, entries, "no run, so no directory")
	})
}

func TestRunBacktest_EveryRunGetsItsOwnDirectory(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	buildCanonical(t, janToMar("EURUSD"))
	req := backtestReq(t, "EURUSD")

	first, err := RunBacktest(context.Background(), nil, req)
	require.NoError(t, err)
	second, err := RunBacktest(context.Background(), nil, req)
	require.NoError(t, err)

	assert.NotEqual(t, first.Dir, second.Dir)
	assert.Less(t, first.RunID.String(), second.RunID.String(), "later runs sort later")
	a, b := readRunDir(t, first.Dir), readRunDir(t, second.Dir)
	assert.Equal(t, a.events, b.events, "the same run writes the same journal; only run.json carries a run's own identity")
}

// walkPoints is a seeded walk in ticks above a base, hour by hour, for a month of
// quotes with many crossovers.
func walkPoints(seed uint64, hours int) []int64 {
	rng := rand.New(rand.NewPCG(seed, seed*7+1))
	points := make([]int64, hours)
	var p int64
	for i := range points {
		p += rng.Int64N(301) - 150
		points[i] = p
	}
	return points
}

// emaxClient is examples/emax as a strategy client's two functions, through the
// public SDK only. If failAfter is positive it gives up on that group, as a
// crashing process would.
func emaxClient(t *testing.T, failAfter int) (onBars func([]marketdata.Bar, []strategy.Event) ([]strategy.Order, error), warm func([]marketdata.Bar)) {
	t.Helper()
	s, err := emax.New(emax.Config{Short: 2, Long: 4, StopPct: num.MustParseRate("0.01"), Tick: num.MustParsePrice("0.00001")})
	require.NoError(t, err)
	groups := 0
	return func(bars []marketdata.Bar, events []strategy.Event) ([]strategy.Order, error) {
		groups++
		if failAfter > 0 && groups > failAfter {
			return nil, errors.New("strategy crashed")
		}
		return s.OnBars(bars, events)
	}, s.Warm
}

// clientRun is one backtest asked for by a client: what Trader's service made of
// it, and what the client was told.
type clientRun struct {
	run       *BacktestRun // the service's side
	runErr    error
	result    *strategy.Result // the client's side
	clientErr error
	client    string // the client's address, as Trader saw it
}

// runForClient serves req to a client the way trader serve does: a strategy
// listener accepts the client's strategy.Run and its handler runs RunBacktest with
// the session as the strategy. The client trades with onBars and warm.
func runForClient(t *testing.T, req BacktestRequest, onBars func([]marketdata.Bar, []strategy.Event) ([]strategy.Order, error), warm func([]marketdata.Bar)) clientRun {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	var out clientRun
	handled := make(chan struct{})
	go func() {
		served <- strategy.ServeRuns(ctx, ln, func(ctx context.Context, _ strategy.RunRequest, s *strategy.Session) (strategy.Result, error) {
			defer close(handled)
			req.Remote, req.RemoteAddr, req.OnStart = s.Client, s.Remote, s.Accept
			out.client = s.Remote
			out.run, out.runErr = RunBacktest(ctx, nil, req)
			if out.run == nil {
				return strategy.Result{}, out.runErr
			}
			res := strategy.Result{RunID: out.run.RunID.String(), Status: out.run.Status}
			if out.runErr != nil {
				res.Error = out.runErr.Error()
				return res, nil
			}
			summary, err := json.Marshal(out.run.Summary())
			res.Summary = summary
			return res, err
		})
	}()
	out.result, out.clientErr = strategy.Run(context.Background(), ln.Addr().String(),
		strategy.RunRequest{Instruments: req.Instruments, Interval: req.Interval, From: req.From, To: req.To},
		onBars, strategy.WithWarmup(warm))
	select {
	case <-handled:
	case <-time.After(10 * time.Second):
		t.Fatal("the run handler did not finish")
	}
	cancel()
	require.NoError(t, <-served)
	return out
}

// A real month, built into canonical bars through the store, then run with the
// built-in EMAX and with the EMAX example as a client that asked for the run, from
// the same data and settings, journaled each.
func TestRunBacktest_RemoteEMAXMatchesTheBuiltInEMAX(t *testing.T) {
	storetest.Root(t)
	// An odd spread gives half-tick mids, which the engine rounds: the parity holds
	// through that too.
	monthSpread(t, "EURUSD", time.January, 110000, 3, walkPoints(1, 600)...)
	monthSpread(t, "GBPUSD", time.January, 125000, 3, walkPoints(2, 600)...)
	buildCanonical(t, janToMar("EURUSD", "GBPUSD"))
	builtinReq := backtestReq(t, "EURUSD", "GBPUSD")
	builtin, err := RunBacktest(context.Background(), nil, builtinReq)
	require.NoError(t, err)

	onBars, warm := emaxClient(t, 0)
	got := runForClient(t, backtestReq(t, "EURUSD", "GBPUSD"), onBars, warm)
	require.NoError(t, got.runErr)
	require.NoError(t, got.clientErr, "the client ends on the run's result")

	b, r := readRunDir(t, builtin.Dir), readRunDir(t, got.run.Dir)

	// What names the run or its strategy, or reads the wall clock, may differ. Nothing else.
	assert.Equal(t, map[string]any{"name": "emax", "transport": "in-process"}, b.rec["strategy"])
	assert.Equal(t, map[string]any{"name": "remote", "transport": "tcp", "addr": got.client}, r.rec["strategy"])
	for _, rec := range []map[string]any{b.rec, r.rec} {
		delete(rec, "run_id")
		delete(rec, "strategy")
		delete(rec, "started_at")
		delete(rec, "ended_at")
		delete(rec["result"].(map[string]any), "strategy")
	}
	assert.Equal(t, b.rec, r.rec, "status, instruments, range and the engine's result: orders, trades, balance, equity, open positions")
	assert.Equal(t, "completed", b.rec["status"])
	assert.Equal(t, b.events, r.events, "every order, fill and protective exit, in order, to the price")
	require.Greater(t, len(b.events), 100, "a real comparison, not an empty one")
	assert.Positive(t, b.rec["result"].(map[string]any)["trades"])

	// The client was told the RunID before anything else, and the result is the summary.
	assert.Equal(t, got.run.RunID.String(), got.result.RunID)
	assert.Equal(t, strategy.StatusCompleted, got.result.Status)
	var summary map[string]any
	require.NoError(t, json.Unmarshal(got.result.Summary, &summary))
	assert.Equal(t, r.rec["result"].(map[string]any)["balance"], summary["balance"], "the result is the summary run.json holds")
	require.NotEmpty(t, summary["settings"])
	assert.Equal(t, r.rec["result"].(map[string]any)["settings"], summary["settings"])
}

// January is the month before From, so the built-in EMAX is warmed by it and the
// client is sent it as warm-up: the comparison covers a normal historical run. It
// is not vacuous: the same February without January trades differently.
func TestRunBacktest_RemoteEMAXMatchesTheBuiltInEMAXWhenTheMonthBeforeIsWarmup(t *testing.T) {
	febReq := func() BacktestRequest {
		r := backtestReq(t, "EURUSD", "GBPUSD")
		r.From, r.To = utc(2024, 2, 1, 0), utc(2024, 3, 1, 0)
		return r
	}
	storetest.Root(t)
	for _, m := range []time.Month{time.January, time.February} {
		monthSpread(t, "EURUSD", m, 110000, 3, walkPoints(uint64(m), 600)...)
		monthSpread(t, "GBPUSD", m, 125000, 3, walkPoints(uint64(m)+10, 600)...)
	}
	buildCanonical(t, janToMar("EURUSD", "GBPUSD"))
	builtin, err := RunBacktest(context.Background(), nil, febReq())
	require.NoError(t, err)

	onBars, warm := emaxClient(t, 0)
	got := runForClient(t, febReq(), onBars, warm)
	require.NoError(t, got.runErr)
	require.NoError(t, got.clientErr)

	b, r := readRunDir(t, builtin.Dir), readRunDir(t, got.run.Dir)
	assert.Equal(t, b.events, r.events, "every order, fill and exit, to the price")
	assert.Equal(t, b.rec["result"].(map[string]any)["balance"], r.rec["result"].(map[string]any)["balance"])
	assert.Equal(t, b.rec["result"].(map[string]any)["open"], r.rec["result"].(map[string]any)["open"])
	require.Greater(t, len(b.events), 100)

	// Without January the built-in run is cold, and it trades differently: the
	// warm-up is what the client was compared on.
	storetest.Root(t)
	monthSpread(t, "EURUSD", time.February, 110000, 3, walkPoints(uint64(time.February), 600)...)
	monthSpread(t, "GBPUSD", time.February, 125000, 3, walkPoints(uint64(time.February)+10, 600)...)
	buildCanonical(t, janToMar("EURUSD", "GBPUSD"))
	cold, err := RunBacktest(context.Background(), nil, febReq())
	require.NoError(t, err)
	assert.NotEqual(t, b.events, readRunDir(t, cold.Dir).events)
}

func TestRunBacktest_ARemoteRunThatLosesItsClientKeepsTheRunIDAndThePartialJournal(t *testing.T) {
	storetest.Root(t)
	monthSpread(t, "EURUSD", time.January, 110000, 3, walkPoints(1, 600)...)
	buildCanonical(t, janToMar("EURUSD"))
	full, err := RunBacktest(context.Background(), nil, backtestReq(t, "EURUSD"))
	require.NoError(t, err)

	onBars, warm := emaxClient(t, 250) // dies on its 251st group
	req := backtestReq(t, "EURUSD")
	got := runForClient(t, req, onBars, warm)

	require.Error(t, got.runErr)
	require.Error(t, got.clientErr, "the client that crashed is told so by its own error")
	assert.ErrorContains(t, got.runErr, "strategy remote: reading orders", "the failure says it was the client")
	require.NotNil(t, got.run, "the failed run is returned with its RunID")
	assert.Equal(t, results.Failed, got.run.Status)
	assert.Nil(t, got.run.Result)

	run := readRunDir(t, onlyRun(t, req.ResultsRoot))
	assert.Equal(t, got.run.RunID.String(), run.rec["run_id"])
	assert.Equal(t, "failed", run.rec["status"])
	assert.Contains(t, run.rec["error"], "strategy remote: reading orders")
	assert.NotContains(t, run.rec, "result")
	assertSettings(t, run) // it failed while replaying
	assert.Equal(t, map[string]any{"name": "remote", "transport": "tcp", "addr": got.client}, run.rec["strategy"])

	whole := readRunDir(t, full.Dir).events
	require.NotEmpty(t, run.events, "what was recorded before the client died is kept")
	require.Less(t, len(run.events), len(whole), "and the run did not get to the end")
	assert.Equal(t, whole[:len(run.events)], run.events, "the partial journal is the start of the full one: nothing lost, nothing invented")
}

// The RunID goes to the client before any bars, and a run that is then refused by
// the OnStart hook still keeps its directory, its RunID and a failed record.
func TestRunBacktest_OnStartGetsTheRunIDBeforeAnythingIsLoadedAndAFailureKeepsTheRun(t *testing.T) {
	storetest.Root(t) // nothing is built: loading would fail, and must not be reached
	req := backtestReq(t, "EURUSD")
	var got string
	req.OnStart = func(runID string) error {
		got = runID
		return errors.New("the client is gone")
	}

	run, err := RunBacktest(context.Background(), nil, req)

	require.Error(t, err)
	assert.ErrorContains(t, err, "the client is gone")
	require.NotNil(t, run)
	assert.Equal(t, run.RunID.String(), got)
	assert.Equal(t, results.Failed, run.Status)
	rec := readRunDir(t, onlyRun(t, req.ResultsRoot))
	assert.Equal(t, "failed", rec.rec["status"])
	assert.Equal(t, got, rec.rec["run_id"])
	assert.Empty(t, rec.events)
}

// A run that fails while loading is a failed run with a RunID, returned with the error.
func TestRunBacktest_AFailureAfterTheRunWasMadeReturnsTheRun(t *testing.T) {
	storetest.Root(t) // nothing built: the bars cannot be loaded
	req := backtestReq(t, "EURUSD")

	run, err := RunBacktest(context.Background(), nil, req)

	require.Error(t, err)
	require.NotNil(t, run)
	assert.Equal(t, results.Failed, run.Status)
	assert.NotEmpty(t, run.Dir)
	assert.Nil(t, run.Result)
}

// A cancelled run is returned too, with the cancelled status.
func TestRunBacktest_ACancelledRunIsReturnedWithTheCancelledStatus(t *testing.T) {
	storetest.Root(t)
	monthSpread(t, "EURUSD", time.January, 110000, 3, walkPoints(1, 100)...)
	buildCanonical(t, janToMar("EURUSD"))
	ctx, cancel := context.WithCancel(context.Background())
	req := backtestReq(t, "EURUSD")
	req.OnStart = func(string) error { cancel(); return nil }

	run, err := RunBacktest(ctx, nil, req)

	require.ErrorIs(t, err, context.Canceled)
	require.NotNil(t, run)
	assert.Equal(t, results.Cancelled, run.Status)
}

// A request Trader refuses before a run exists has no RunID and no run.
func TestRunBacktest_ARefusedRequestReturnsNoRun(t *testing.T) {
	storetest.Root(t)
	req := backtestReq(t, "EURUSD")
	req.Source = "stooq"

	run, err := RunBacktest(context.Background(), nil, req)

	require.Error(t, err)
	assert.Nil(t, run)
}

// The account journal is read as a client reads it, from the files alone: the
// summary's maximum drawdown in run.json equals a replay of account.jsonl, and the
// last line is the account run.json reports.
func TestRunBacktest_AccountJournalReconcilesWithTheRecord(t *testing.T) {
	storetest.Root(t)
	month(t, "EURUSD", time.January, 110000, wave...)
	buildCanonical(t, janToMar("EURUSD"))
	req := backtestReq(t, "EURUSD")

	res, err := RunBacktest(context.Background(), nil, req)
	require.NoError(t, err)

	var recFile struct {
		Result struct {
			Balance, Equity struct{ Amount string }
			MaxDrawdown     struct {
				Amount     struct{ Amount string }
				Pct        string
				PeakTime   time.Time `json:"peak_time"`
				TroughTime time.Time `json:"trough_time"`
			} `json:"max_drawdown"`
			MaxPercentDrawdown struct {
				Pct        string
				PeakTime   time.Time `json:"peak_time"`
				TroughTime time.Time `json:"trough_time"`
			} `json:"max_percent_drawdown"`
		}
	}
	data, err := os.ReadFile(filepath.Join(res.Dir, results.RunFile))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &recFile))

	f, err := os.Open(filepath.Join(res.Dir, results.AccountFile))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	type line struct {
		Time    time.Time `json:"time"`
		Kind    string    `json:"kind"`
		Balance struct{ Amount string }
		Equity  struct{ Amount string }
	}
	var lines []line
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var l line
		require.NoError(t, json.Unmarshal(sc.Bytes(), &l), sc.Text())
		lines = append(lines, l)
	}
	require.NoError(t, sc.Err())
	require.Greater(t, len(lines), 1)
	assert.Equal(t, "initial", lines[0].Kind)

	rat := func(s string) *big.Rat {
		r, ok := new(big.Rat).SetString(s)
		require.True(t, ok, s)
		return r
	}
	peak, peakAt := rat(lines[0].Equity.Amount), lines[0].Time
	depth, peakOf, troughAt := new(big.Rat), peakAt, peakAt
	pct, pctPeakOf, pctTroughAt := new(big.Rat), peakAt, peakAt
	for _, l := range lines[1:] {
		e := rat(l.Equity.Amount)
		if e.Cmp(peak) > 0 {
			peak, peakAt = e, l.Time
		}
		d := new(big.Rat).Sub(peak, e)
		if d.Cmp(depth) > 0 {
			depth, peakOf, troughAt = d, peakAt, l.Time
		}
		if p := new(big.Rat).Quo(d, peak); p.Cmp(pct) > 0 {
			pct, pctPeakOf, pctTroughAt = p, peakAt, l.Time
		}
	}
	dd := recFile.Result.MaxDrawdown
	assert.Equal(t, 0, depth.Cmp(rat(dd.Amount.Amount)), "drawdown %s vs run.json %s", depth.FloatString(8), dd.Amount.Amount)
	assert.True(t, peakOf.Equal(dd.PeakTime))
	assert.True(t, troughAt.Equal(dd.TroughTime))
	pctDD := recFile.Result.MaxPercentDrawdown
	percent := new(big.Rat).Mul(pct, big.NewRat(100, 1))
	assert.Equal(t, percent.FloatString(6), rat(pctDD.Pct).FloatString(6))
	assert.True(t, pctPeakOf.Equal(pctDD.PeakTime))
	assert.True(t, pctTroughAt.Equal(pctDD.TroughTime))
	assert.Positive(t, depth.Sign(), "the wave loses money at some point, so the check is not vacuous")
	last := lines[len(lines)-1]
	assert.Equal(t, 0, rat(last.Equity.Amount).Cmp(rat(recFile.Result.Equity.Amount)))
	assert.Equal(t, 0, rat(last.Balance.Amount).Cmp(rat(recFile.Result.Balance.Amount)))
}
