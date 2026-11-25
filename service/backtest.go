package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/internal/clock"
	"github.com/rustyeddy/trader/internal/id"
	"github.com/rustyeddy/trader/internal/results"
	"github.com/rustyeddy/trader/marketdata"
	remotestrat "github.com/rustyeddy/trader/strategy"
)

// DefaultResultsRoot is where a run's results go when nothing else is configured.
const DefaultResultsRoot = results.DefaultRoot

// runIDs makes the RunID of every run.
var runIDs = id.NewGenerator(clock.Clock, id.Random{})

// BacktestRequest is what one historical backtest asks for: the canonical bars of
// every instrument from one source at one interval whose opening time is in
// [From, To), replayed together on one shared account. The values come from the
// transport. AllowBad and AllowMissing are LoadRequest's: by default a month that
// is missing or has bad records fails the run, and a stale month always does.
type BacktestRequest struct {
	// Source is the provider the bars came from; only "oanda" for now (empty means
	// oanda).
	Source      string
	Instruments []string
	// Interval is the lower-case token, h1 or d1: one interval per run.
	Interval     string
	From, To     time.Time
	AllowBad     bool
	AllowMissing bool

	// Remote, when not nil, is a strategy client connected to Trader that asked for
	// this run and trades it instead of the built-in EMAX (see
	// trader.RunOptions.Remote); RemoteAddr is its address, recorded in run.json. It
	// is warmed up from the months before From like the built-in strategy. The
	// connection is the caller's to close.
	Remote     *remotestrat.Client
	RemoteAddr string

	// OnStart, when not nil, is called with the RunID as soon as the run exists
	// (its directory and journal are made) and before anything is loaded or traded.
	// If it returns an error the run is not carried on: it ends as failed with that
	// error, and keeps its RunID.
	OnStart func(runID string) error

	// ResultsRoot is the directory the run's results go under (results.DefaultRoot
	// unless the transport was told otherwise). It is required: every run is
	// journaled, so there is no way to run without one.
	ResultsRoot string
}

// BacktestRun is a backtest: the engine's summary and where the run's results are.
// RunBacktest also returns one, without a Result, alongside the error of a run that
// failed or was cancelled after it was made, so the caller can say which run it was.
type BacktestRun struct {
	*trader.Result
	RunID id.RunID
	// Dir is the run's directory: run.json and events.jsonl.
	Dir string
	// Status is how the run ended in run.json: completed, failed or cancelled.
	Status string
}

// RunBacktest loads the canonical series for req (LoadSeries) and replays them on
// the engine (trader.RunSeriesWith), returning the engine's summary and where the
// run's results are. It coordinates the two and holds no rule of its own: what
// loads is the market-data service's policy, and what runs, and what it validates
// first, is the engine's.
//
// Every run is journaled. A run exists from the moment the request is well formed
// enough to describe (the source is supported and there is a results root): it gets
// a RunID and a directory under req.ResultsRoot with run.json, events.jsonl and
// account.jsonl, and it ends there with a status, completed, failed or cancelled, whatever happened
// after: bars that cannot be loaded, a request the engine refuses, a cancellation.
// Only an unsupported source, a missing results root or a directory that cannot be
// made stop it before a run exists, since there is then nowhere to record it.
//
// A request that cannot be run, or an instrument that cannot be loaded as asked,
// is an error that names the source, instrument, interval and range (and, for a
// load, the months and their states) and the run's directory; a load failure is
// also ErrNotLoadable, and nothing is replayed unless every instrument loaded. A
// range with no bars at all is an error: there is nothing to backtest. The bars of
// the month before From, when they can be loaded, prefill the strategy's indicators
// and are not traded (warmupBars). Cancellation stops the run and returns the
// context's error. log may be nil.
func RunBacktest(ctx context.Context, log *slog.Logger, req BacktestRequest) (*BacktestRun, error) {
	source := req.Source
	if source == "" {
		source = "oanda"
	}
	if source != "oanda" {
		return nil, fmt.Errorf("backtest: source %q is not supported (only oanda)", req.Source)
	}
	runID, err := runIDs.RunID()
	if err != nil {
		return nil, fmt.Errorf("backtest: run id: %w", err)
	}
	rec := results.Record{
		RunID: runID, Kind: "backtest", Source: source, Interval: req.Interval,
		Instruments: req.Instruments, From: req.From.UTC(), To: req.To.UTC(), StartedAt: clock.Clock(),
	}
	rec.Strategy.Name, rec.Strategy.Transport = trader.StrategyEMAX, "in-process"
	if req.Remote != nil {
		rec.Strategy.Name, rec.Strategy.Transport, rec.Strategy.Addr = trader.StrategyRemote, "tcp", req.RemoteAddr
	}
	run, err := results.Start(req.ResultsRoot, rec)
	if err != nil {
		return nil, fmt.Errorf("backtest: %w", err)
	}

	var res *trader.Result
	if req.OnStart != nil {
		err = req.OnStart(runID.String())
	}
	if err == nil {
		res, err = runBacktest(ctx, log, req, source, run.Journal(), run.AccountJournal())
	}
	var summary any
	if res != nil {
		summary = res.Summary()
	}
	err = run.Finish(err, summary)
	done := &BacktestRun{Result: res, RunID: runID, Dir: run.Dir(), Status: run.Status()}
	if err != nil {
		return done, fmt.Errorf("%w (run %s, journal in %s)", err, runID, run.Dir())
	}
	return done, nil
}

// runBacktest loads and replays, writing the engine's journal to journal.
func runBacktest(ctx context.Context, log *slog.Logger, req BacktestRequest, source string, journal, account io.Writer) (*trader.Result, error) {
	what := fmt.Sprintf("%s %s %s [%s, %s)", source, strings.Join(req.Instruments, ","), req.Interval,
		req.From.UTC().Format(time.RFC3339), req.To.UTC().Format(time.RFC3339))

	results, err := LoadSeries(ctx, log, LoadRequest{Instruments: req.Instruments, Interval: req.Interval,
		From: req.From, To: req.To, AllowBad: req.AllowBad, AllowMissing: req.AllowMissing})
	if err != nil {
		var causes []error
		for _, r := range results {
			if r.Err != nil {
				causes = append(causes, r.Err)
			}
		}
		if len(causes) == 0 {
			return nil, fmt.Errorf("backtest %s: %w", what, err)
		}
		return nil, fmt.Errorf("backtest %s: %w: %w", what, err, errors.Join(causes...))
	}

	bars := 0
	for _, r := range results {
		bars += len(r.Series.Bars)
	}
	if bars == 0 {
		return nil, fmt.Errorf("backtest %s: no bars in the range", what)
	}
	warm, err := warmupBars(ctx, log, req)
	if err != nil {
		return nil, err
	}
	series := make([]marketdata.Series, 0, len(results))
	for _, r := range results {
		s := r.Series
		if w := warm[r.Instrument]; len(w) > 0 {
			s.Bars = append(append(make([]marketdata.Bar, 0, len(w)+len(s.Bars)), w...), s.Bars...)
		}
		series = append(series, s)
	}
	res, err := trader.RunSeriesWith(ctx, trader.RunOptions{Journal: journal, Account: account, Remote: req.Remote}, what, req.From, series...)
	if err != nil {
		return nil, fmt.Errorf("backtest %s: %w", what, err)
	}
	return res, nil
}

// warmupLookback is how far before From the warm-up looks for bars.
const warmupLookback = 1 // months

// warmupBars returns, per instrument, the bars that opened in the month before From,
// for the strategy to prefill its indicators with: the run then starts with them
// ready instead of cold. They are not traded.
//
// Warm-up is best effort, "when it is available". It uses the same loader and the
// same policy as the run, except that a missing month is simply not there; an
// instrument whose earlier months cannot be loaded (stale, or bad when bad is not
// allowed) is run without a warm-up, and the log says so. Only cancellation is an
// error.
func warmupBars(ctx context.Context, log *slog.Logger, req BacktestRequest) (map[string][]marketdata.Bar, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	results, err := LoadSeries(ctx, log, LoadRequest{Instruments: req.Instruments, Interval: req.Interval,
		From: req.From.AddDate(0, -warmupLookback, 0), To: req.From, AllowBad: req.AllowBad, AllowMissing: true})
	if cerr := ctx.Err(); cerr != nil {
		return nil, cerr
	}
	if err != nil && !errors.Is(err, ErrNotLoadable) {
		return nil, err
	}
	warm := make(map[string][]marketdata.Bar, len(results))
	for _, r := range results {
		if r.Err != nil {
			log.Warn("no warm-up: the months before the range cannot be loaded", "symbol", r.Instrument, "error", r.Err)
			continue
		}
		warm[r.Instrument] = r.Series.Bars
		log.Info("warm-up bars", "symbol", r.Instrument, "bars", len(r.Series.Bars))
	}
	return warm, nil
}
