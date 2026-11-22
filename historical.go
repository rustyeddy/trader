package trader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

// Result is the summary of one finished run.
type Result struct{ r *result }

// Write prints the summary: orders, trades, money and open positions. It holds
// nothing that varies between runs.
func (r *Result) Write(w io.Writer) error { return r.r.write(w) }

// Summary is what a finished run came to, as data: the numbers Write prints, exact,
// for a run record to keep. It is read from the result, not calculated again.
type Summary struct {
	Strategy       string     `json:"strategy"`
	Account        string     `json:"account"`
	InitialBalance num.Money  `json:"initial_balance"`
	Bars           int        `json:"bars"`
	Orders         OrderCount `json:"orders"`
	Trades         int        `json:"trades"`
	Wins           int        `json:"wins"`
	Losses         int        `json:"losses"`
	BreakEven      int        `json:"break_even"`
	Realized       num.Money  `json:"realized"`
	Unrealized     num.Money  `json:"unrealized"`
	Balance        num.Money  `json:"balance"`
	Equity         num.Money  `json:"equity"`
	Open           []Position `json:"open"`
}

// OrderCount is how many orders ended where, as in Write's orders line.
type OrderCount struct {
	Submitted int `json:"submitted"`
	Filled    int `json:"filled"`
	Rejected  int `json:"rejected"`
	Cancelled int `json:"cancelled"`
	Pending   int `json:"pending"`
}

// Position is a position still open when the run ended.
type Position struct {
	Instrument string       `json:"instrument"`
	Direction  string       `json:"direction"`
	Units      num.Quantity `json:"units"`
	EntryPrice num.Price    `json:"entry_price"`
	MarkPrice  num.Price    `json:"mark_price"`
}

// Summary returns the run's numbers.
func (r *Result) Summary() Summary {
	x := r.r
	s := Summary{
		Strategy: x.strategy, Account: runAccount, InitialBalance: num.MustParseMoney(runBalance, usdCurrency),
		Bars:   x.bars,
		Orders: OrderCount{Submitted: x.submitted, Filled: x.filled, Rejected: x.rejected, Cancelled: x.cancelled, Pending: x.pending},
		Trades: x.trades, Wins: x.wins, Losses: x.losses, BreakEven: x.breakEven,
		Realized: x.realized, Unrealized: x.unrealized, Balance: x.balance, Equity: x.equity,
		Open: []Position{},
	}
	for _, p := range x.open {
		s.Open = append(s.Open, Position{Instrument: p.instrument, Direction: p.direction.String(), Units: p.units,
			EntryPrice: p.entryPrice, MarkPrice: p.markPrice})
	}
	return s
}

// The fixed configuration of RunSeries: the one the EMAX scenarios were worked out
// for, applied to each instrument of the run. Choosing a strategy and an account
// is a later piece of work.
const (
	runShort   = 2
	runLong    = 4
	runBalance = "10000" // USD

	// StrategyEMAX names the strategy RunSeries trades unless RunOptions names a
	// worker, which is StrategyRemote.
	StrategyEMAX   = "emax"
	StrategyRemote = "remote"
	runStrategy    = StrategyEMAX
	runAccount     = "sim123"
)

// RunOptions are the optional parts of a run.
//
// Journaling is the application's rule, not the engine's: every run started through
// service.RunBacktest (which is what `trader backtest` calls) is journaled, with a
// RunID and a results directory. RunSeries and RunSeriesWith are the engine's
// library entry points and write a journal only if handed a writer, which is how
// tests and any other caller that does not want a run directory use them. The
// scenario regression (Run, the six EMAX scenarios) is not a run in that sense: it
// is the baseline the journaled runs are compared with, and has no directory.
type RunOptions struct {
	// Journal, when not nil, is written one JSON line for every transition of every
	// order of the run, in the order they happen (see journal). The engine opens no
	// file; the caller owns the writer. If a write fails the run stops with that
	// error, and what was written before it stays.
	Journal io.Writer

	// Worker, when not empty, is the loopback host:port of a worker process that
	// trades instead of the built-in EMAX: Trader connects to it and, for every group
	// of bars, asks what it wants (package strategy). Everything else is the same run:
	// the same engine loop, account sizing, broker and journal. The bars before
	// tradeFrom reach it as warm-up (Client.Warm): it prefills its indicators from
	// them, and an order in its answer to one is an error.
	Worker string
}

// RunSeries backtests the series, which a reader has already loaded, on one
// shared account and broker: EMAX on each instrument, one strategy callback per
// distinct availability instant (see backtest.runSeriesFrom). name labels the
// summary.
//
// tradeFrom, when not zero, is the first instant that is traded: a bar that opens
// before it only prefills the indicators (the strategy's warm-up) and is neither
// replayed on the broker nor counted in the summary. The warm-up bars come first in
// each series. With a zero tradeFrom every bar is traded.
//
// The engine opens no file and knows no provider. It validates every series
// (marketdata.Series.Validate) before replaying, so a caller cannot skip that; a
// series of an instrument the engine has no market for, or of another interval
// than the rest, is refused with an error that names it.
//
// Prices are rounded to the nearest tick of the market (a tie goes up) as the bars
// enter the engine, so a mid price between two ticks, which an odd bid/ask spread
// gives, trades like any other. The engine's copy of a bar carries just that one
// price series (as Mid); the rounding is on the copy, so the series passed in, and
// the canonical data it came from, are not changed. Series are of different
// instruments; an empty series contributes nothing.
func RunSeries(ctx context.Context, name string, tradeFrom time.Time, series ...marketdata.Series) (*Result, error) {
	return RunSeriesWith(ctx, RunOptions{}, name, tradeFrom, series...)
}

// RunSeriesWith is RunSeries with options. A run with the zero RunOptions is a
// RunSeries run, and what it trades does not depend on the options: a journal only
// records.
func RunSeriesWith(ctx context.Context, opts RunOptions, name string, tradeFrom time.Time, series ...marketdata.Series) (*Result, error) {
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
		// The series is checked as it was given, before the engine's copy drops the
		// series it does not trade (a crossed bid and ask would not survive that).
		if err := s.Validate(); err != nil {
			return nil, fmt.Errorf("run: %s: %w", s.Instrument, err)
		}
		// Rounding and normalizing rewrite the bars, so work on a copy: the caller's
		// are left alone.
		s.Bars = slices.Clone(s.Bars)
		if err := markets.roundToTicks(&s); err != nil {
			return nil, fmt.Errorf("run: %s: %w", s.Instrument, err)
		}
		if err := markets.normalize(&s); err != nil {
			return nil, fmt.Errorf("run: %s: %w", s.Instrument, err)
		}
		run[i] = s
		instruments = append(instruments, s.Instrument)
		for _, b := range s.Bars {
			if tradeFrom.IsZero() || !b.Time.Before(tradeFrom) {
				bars++
			}
		}
	}
	slices.Sort(instruments)

	var strat strategy
	if opts.Worker != "" {
		remote, err := dialRemote(StrategyRemote, opts.Worker)
		if err != nil {
			return nil, err
		}
		defer func() { _ = remote.close() }() // the worker sees the connection close and stops
		strat = remote
	} else {
		emax := &emaxEach{id: runStrategy, instruments: instruments, short: runShort, long: runLong,
			stopPct: rateOnePercent, markets: markets}
		if err := emax.init(); err != nil {
			return nil, err
		}
		strat = emax
	}
	br := newBroker("sim", markets)
	if opts.Journal != nil {
		br.journal = newJournal(opts.Journal)
	}
	tr := &trader{
		broker:   br,
		account:  br.addAccount(runAccount, num.MustParseMoney(runBalance, usdCurrency)),
		strategy: strat,
	}
	if err := (&backtest{}).runSeriesFrom(ctx, tr, tradeFrom, run...); err != nil {
		return nil, err
	}
	if err := br.journal.err(); err != nil { // the last frame's transitions too
		return nil, fmt.Errorf("journal: %w", err)
	}
	res, err := newResult(name, bars, tr)
	if err != nil {
		return nil, err
	}
	return &Result{r: res}, nil
}
