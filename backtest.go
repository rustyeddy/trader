package trader

import (
	"context"
	"fmt"
	"io"
	"iter"
	"strings"
	"time"
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

	realized, unrealized, balance, equity money
	open                                  []position
}

func newResult(scenario string, bars int, tr *trader) *result {
	act := tr.account
	r := &result{
		scenario: scenario, bars: bars, rejected: len(tr.rejected),
		balance: act.balance, unrealized: act.unrealizedPnL(), equity: act.equity(),
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
		switch {
		case t.pnl > 0:
			r.wins++
		case t.pnl < 0:
			r.losses++
		default:
			r.breakEven++
		}
		r.realized += t.pnl
	}
	for _, instrument := range act.positionSymbols() {
		r.open = append(r.open, *act.positions[instrument])
	}
	return r
}

// write prints the summary. It holds nothing that varies between runs.
func (r *result) write(w io.Writer) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d bars\n", r.scenario, r.bars)
	fmt.Fprintf(&b, "  orders: submitted %d, filled %d, rejected %d, cancelled %d, pending %d\n",
		r.submitted, r.filled, r.rejected, r.cancelled, r.pending)
	fmt.Fprintf(&b, "  trades: closed %d, wins %d, losses %d, break-even %d\n",
		r.trades, r.wins, r.losses, r.breakEven)
	fmt.Fprintf(&b, "  money: realized %s, unrealized %s, balance %s, equity %s\n",
		formatMoney(r.realized), formatMoney(r.unrealized), formatMoney(r.balance), formatMoney(r.equity))
	if len(r.open) == 0 {
		b.WriteString("  open: none\n")
	}
	for _, p := range r.open {
		fmt.Fprintf(&b, "  open: %s %d %s from %s, marked %s\n",
			p.direction, p.units, p.instrument, formatPrice(p.entryPrice), formatPrice(p.markPrice))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func formatMoney(m money) string {
	sign, cents := "", uint64(m)
	if m < 0 {
		sign, cents = "-", -cents
	}
	return fmt.Sprintf("%s$%d.%02d", sign, cents/uint64(moneyScale), cents%uint64(moneyScale))
}

func formatPrice(p price) string {
	return fmt.Sprintf("%d.%05d", p/priceScale, p%priceScale)
}
