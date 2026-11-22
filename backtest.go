package trader

import (
	"context"
	"fmt"
	"io"
	"iter"
	"strings"
	"time"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

type backtest struct {
}

// run replays bars through the trader one bar at a time: each bar is a group of
// its own. It is the single-bar access the research tests and studies use;
// runSeries is the path that groups bars by availability.
func (bt *backtest) run(ctx context.Context, bars iter.Seq2[marketdata.Bar, error], tr *trader) error {
	var last marketdata.Bar // most recent bar processed, for error context
	for b, err := range bars {
		if err != nil {
			return readError(last, err)
		}
		last = b

		if err := tr.onBar(ctx, b); err != nil {
			return err
		}
	}
	return nil
}

// runSeries replays already loaded series on one trader: runSeriesFrom with no
// warm-up.
func (bt *backtest) runSeries(ctx context.Context, tr *trader, series ...marketdata.Series) error {
	return bt.runSeriesFrom(ctx, tr, time.Time{}, series...)
}

// runSeriesFrom replays already loaded series on one trader, with one shared account
// and broker: the bars merged by availability (mergedBars), delivered as groups.
// Every bar that becomes available at the same instant is delivered together,
// once: the trader advances the broker through the whole group, then the
// strategy sees the whole group in one call. A group holds only fresh bars, so an
// instrument with no bar at that instant is absent, never carried forward. A
// single-instrument run is the same path with one-bar groups.
//
// A bar that opens before tradeFrom (when it is not zero) is a warm-up bar: the
// strategy is shown it only to prefill its indicators (warmer), the broker never
// sees it, and nothing is traded on it. The warm-up bars come first in each series.
//
// runSeriesFrom owns the validation of what it replays: every series must pass
// marketdata.Series.Validate (named, valid bars of its instrument, strictly
// ascending opening times and availability), so no caller can skip it.
func (bt *backtest) runSeriesFrom(ctx context.Context, tr *trader, tradeFrom time.Time, series ...marketdata.Series) error {
	for _, s := range series {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("replay %s: %w", s.Instrument, err)
		}
	}
	w, _ := tr.strategy.(warmer)
	var last marketdata.Bar
	for f, err := range frames(mergedBars(ctx, series...)) {
		if err != nil {
			return readError(last, err)
		}
		last = f.bars[len(f.bars)-1]

		if !tradeFrom.IsZero() {
			var warm []marketdata.Bar
			live := make([]marketdata.Bar, 0, len(f.bars))
			for _, b := range f.bars {
				if b.Time.Before(tradeFrom) {
					warm = append(warm, b)
				} else {
					live = append(live, b)
				}
			}
			if len(warm) > 0 && w != nil {
				if err := w.warm(ctx, warm); err != nil {
					return fmt.Errorf("warm-up: %w", err)
				}
			}
			if len(live) == 0 {
				continue
			}
			f.bars = live
		}
		if err := tr.onFrame(ctx, f); err != nil {
			return err
		}
	}
	return nil
}

// readError says a read failed, and after which bar (none: before the first).
func readError(last marketdata.Bar, err error) error {
	if last.Instrument == "" {
		return fmt.Errorf("reading bars: %w", err)
	}
	return fmt.Errorf("reading bars after %s %s: %w", last.Instrument, last.Time.Format(time.DateOnly), err)
}

// result is what one finished run reports. A trade is a position that closed:
// an open position is listed on its own and is neither a win nor a loss, and a
// closed trade with zero P/L is a break-even.
//
// The order counts cover this trader's account and come from where the orders
// ended up. rejected includes the opens the trader kept after sizing refused
// them, which never reached the
// broker; submitted is everything the broker accepted into its queue, its own
// stops included; pending is what is still waiting when the data ends.
type result struct {
	scenario string
	strategy string // the name of the strategy that traded
	bars     int

	submitted, filled, rejected, cancelled, pending int
	trades, wins, losses, breakEven                 int

	// The money is exact; it is floored to cents only in write (ADR-086).
	realized, unrealized, balance, equity num.Money
	open                                  []position
}

func newResult(scenario string, bars int, tr *trader) (*result, error) {
	act := tr.account
	unrealized, err := act.unrealizedPnL()
	if err != nil {
		return nil, err
	}
	equity, err := act.equity()
	if err != nil {
		return nil, err
	}
	realized, err := zeroMoneyOf(act.currency)
	if err != nil {
		return nil, err
	}
	name := ""
	if tr.strategy != nil { // a trader built without one, as some tests do
		name = tr.strategy.name()
	}
	r := &result{
		scenario: scenario, strategy: name, bars: bars, rejected: len(tr.rejected),
		balance: act.balance, unrealized: unrealized, equity: equity,
		realized: realized,
	}

	for _, o := range tr.broker.completed {
		if o.accountID != act.id {
			continue
		}
		r.submitted++
		switch o.state {
		case stateFilled:
			r.filled++
		case stateRejected:
			r.rejected++
		case stateCancelled:
			r.cancelled++
		}
	}
	for _, waiting := range tr.broker.orders {
		for _, o := range waiting {
			if o.accountID == act.id {
				r.submitted++
				r.pending++
			}
		}
	}

	for _, t := range act.trades {
		r.trades++
		sign, err := moneySign(t.pnl)
		if err != nil {
			return nil, err
		}
		switch {
		case sign > 0:
			r.wins++
		case sign < 0:
			r.losses++
		default:
			r.breakEven++
		}
		if r.realized, err = r.realized.Add(t.pnl); err != nil {
			return nil, err
		}
	}
	for _, instrument := range act.positionSymbols() {
		r.open = append(r.open, *act.positions[instrument])
	}
	return r, nil
}

// write prints the summary. It holds nothing that varies between runs.
//
// Money is printed in cents, each amount floored toward negative infinity
// (ADR-086). Equity is printed as the sum of the floored balance and
// unrealized amounts, so the printed figures add up; it can be a cent below the
// exact equity.
func (r *result) write(w io.Writer) error {
	realized, err := floorCents(r.realized)
	if err != nil {
		return err
	}
	unrealized, err := floorCents(r.unrealized)
	if err != nil {
		return err
	}
	balance, err := floorCents(r.balance)
	if err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d bars\n", r.scenario, r.bars)
	fmt.Fprintf(&b, "  orders: submitted %d, filled %d, rejected %d, cancelled %d, pending %d\n",
		r.submitted, r.filled, r.rejected, r.cancelled, r.pending)
	fmt.Fprintf(&b, "  trades: closed %d, wins %d, losses %d, break-even %d\n",
		r.trades, r.wins, r.losses, r.breakEven)
	fmt.Fprintf(&b, "  money: realized %s, unrealized %s, balance %s, equity %s\n",
		formatCents(realized), formatCents(unrealized), formatCents(balance), formatCents(balance+unrealized))
	if len(r.open) == 0 {
		b.WriteString("  open: none\n")
	}
	for _, p := range r.open {
		fmt.Fprintf(&b, "  open: %s %s %s from %s, marked %s\n",
			p.direction, p.units, p.instrument, formatPrice(p.entryPrice), formatPrice(p.markPrice))
	}
	_, err = io.WriteString(w, b.String())
	return err
}
