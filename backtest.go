package trader

import (
	"context"
	"fmt"
	"io"
	"iter"
	"strings"
	"time"

	"github.com/rustyeddy/trader/num"
)

type backtest struct {
}

func (bt *backtest) run(ctx context.Context, bars iter.Seq2[bar, error], tr *trader) error {
	var last bar // most recent bar processed, for error context
	for b, err := range bars {
		if err != nil {
			if last.instrument == "" {
				return fmt.Errorf("reading bars: %w", err)
			}
			return fmt.Errorf("reading bars after %s %s: %w", last.instrument, last.Format(time.DateOnly), err)
		}
		last = b

		if err := tr.onBar(ctx, b); err != nil {
			return err
		}
	}
	return nil
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
	r := &result{
		scenario: scenario, bars: bars, rejected: len(tr.rejected),
		balance: act.balance, unrealized: unrealized, equity: equity,
		realized: zeroMoney(act.currency),
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
