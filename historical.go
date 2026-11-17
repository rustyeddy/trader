package trader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// Result is the summary of one finished run.
type Result struct{ r *result }

// Write prints the summary: orders, trades, money and open positions. It holds
// nothing that varies between runs.
func (r *Result) Write(w io.Writer) error { return r.r.write(w) }

// The fixed configuration of RunSeries: the one the EMAX scenarios were worked out
// for, applied to each instrument of the run. Choosing a strategy and an account
// is a later piece of work.
const (
	runShort    = 2
	runLong     = 4
	runBalance  = "10000" // USD
	runStrategy = "emax"
)

// RunSeries backtests the series, which a reader has already loaded, on one
// shared account and broker: EMAX on each instrument, one strategy callback per
// distinct availability instant (see backtest.runSeries). name labels the summary.
//
// The engine opens no file and knows no provider. It validates every series
// (marketdata.Series.Validate) before replaying, so a caller cannot skip that; a
// series of an instrument the engine has no market for, with prices off the
// market's tick, or of another interval than the rest, is refused with an error
// that names it. The series are not changed. Series are of different
// instruments; an empty series contributes nothing.
func RunSeries(ctx context.Context, name string, series ...marketdata.Series) (*Result, error) {
	if len(series) == 0 {
		return nil, errors.New("run: no series")
	}
	markets, err := defaultCatalog()
	if err != nil {
		return nil, err
	}
	run := make([]marketdata.Series, len(series))
	instruments := make([]string, 0, len(series))
	bars := 0
	for i, s := range series {
		// Normalizing rewrites the bars, so work on a copy: the caller's are left alone.
		s.Bars = slices.Clone(s.Bars)
		if err := markets.normalize(&s); err != nil {
			return nil, fmt.Errorf("run: %s: %w", s.Instrument, err)
		}
		run[i] = s
		instruments = append(instruments, s.Instrument)
		bars += len(s.Bars)
	}
	slices.Sort(instruments)

	strat := &emaxEach{id: runStrategy, instruments: instruments, short: runShort, long: runLong,
		stopPct: rateOnePercent, markets: markets}
	if err := strat.init(); err != nil {
		return nil, err
	}
	br := newBroker("sim", markets)
	tr := &trader{
		broker:   br,
		account:  br.addAccount("sim123", num.MustParseMoney(runBalance, usdCurrency)),
		strategy: strat,
	}
	if err := (&backtest{}).runSeries(ctx, tr, run...); err != nil {
		return nil, err
	}
	res, err := newResult(name, bars, tr)
	if err != nil {
		return nil, err
	}
	return &Result{r: res}, nil
}
