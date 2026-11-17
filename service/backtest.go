package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/marketdata"
)

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
}

// RunBacktest loads the canonical series for req (LoadSeries) and replays them on
// the engine (trader.RunSeries), returning the engine's summary. It coordinates
// the two and holds no rule of its own: what loads is the market-data service's
// policy, and what runs, and what it validates first, is the engine's.
//
// A request that cannot be run, or an instrument that cannot be loaded as asked,
// is an error that names the source, instrument, interval and range (and, for a
// load, the months and their states); a load failure is also ErrNotLoadable, and
// nothing is replayed unless every instrument loaded. A range with no bars at all
// is an error: there is nothing to backtest. Cancellation stops the run and
// returns the context's error. Nothing is written. log may be nil.
func RunBacktest(ctx context.Context, log *slog.Logger, req BacktestRequest) (*trader.Result, error) {
	source := req.Source
	if source == "" {
		source = "oanda"
	}
	if source != "oanda" {
		return nil, fmt.Errorf("backtest: source %q is not supported (only oanda)", req.Source)
	}
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

	series := make([]marketdata.Series, 0, len(results))
	bars := 0
	for _, r := range results {
		series = append(series, r.Series)
		bars += len(r.Series.Bars)
	}
	if bars == 0 {
		return nil, fmt.Errorf("backtest %s: no bars in the range", what)
	}
	res, err := trader.RunSeries(ctx, what, series...)
	if err != nil {
		return nil, fmt.Errorf("backtest %s: %w", what, err)
	}
	return res, nil
}
