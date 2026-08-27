package broker

import (
	"fmt"
	"io"

	"github.com/rustyeddy/trader/account"
	svcbroker "github.com/rustyeddy/trader/service/broker"
)

// tableFormatter is the default, human-readable Formatter: a plain
// text line per record, using errWriter's sticky-error idiom so a
// broken output pipe is reported through the same error return
// jsonFormatter already uses, not silently ignored.
type tableFormatter struct{}

// errWriter wraps an io.Writer and remembers the first error any
// Write call returns, turning every write after that into a no-op —
// the same "sticky error" idiom cmd/trader/data's own format_table.go
// uses, duplicated here rather than shared: each CLI command family
// package is deliberately independent (issue #201), and this is a
// small enough type that sharing it is not worth a cross-family
// dependency.
type errWriter struct {
	w   io.Writer
	err error
}

func (e *errWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	n, err := e.w.Write(p)
	if err != nil {
		e.err = err
	}
	return n, err
}

func (tableFormatter) FormatAccounts(w io.Writer, resp svcbroker.AccountsResponse) error {
	ew := &errWriter{w: w}
	for _, ref := range resp.Accounts {
		_, _ = fmt.Fprintf(ew, "%s  broker=%s\n", ref.AccountID, ref.Broker)
	}
	return ew.err
}

func (tableFormatter) FormatSnapshot(w io.Writer, resp svcbroker.SnapshotResponse) error {
	ew := &errWriter{w: w}
	snap := resp.Snapshot
	_, _ = fmt.Fprintf(ew, "account=%s  broker=%s  as_of=%s\n", snap.AccountID(), snap.Broker(), snap.AsOf().Format("2006-01-02T15:04:05Z07:00"))
	_, _ = fmt.Fprintf(ew, "equity=%s  cash=%s  realized_pnl=%s  unrealized_pnl=%s  fees=%s\n",
		snap.Equity(), firstCashBalance(snap), snap.RealizedPnL(), snap.UnrealizedPnL(), snap.Fees())

	positions := snap.Positions()
	if len(positions) == 0 {
		_, _ = fmt.Fprintln(ew, "positions: (none)")
	} else {
		_, _ = fmt.Fprintln(ew, "positions:")
		for _, p := range positions {
			_, _ = fmt.Fprintf(ew, "  %s  %s  qty=%s  avg_price=%s\n", p.Listing.Symbol(), p.Side, p.Quantity, p.AvgPrice)
		}
	}

	orders := snap.OpenOrders()
	if len(orders) == 0 {
		_, _ = fmt.Fprintln(ew, "open orders: (none)")
	} else {
		_, _ = fmt.Fprintln(ew, "open orders:")
		for _, o := range orders {
			_, _ = fmt.Fprintf(ew, "  %s  %s  status=%s  qty=%s\n", o.Request.OrderID, o.Request.Listing.Symbol(), o.Status, o.Request.Quantity)
		}
	}
	return ew.err
}

func (tableFormatter) FormatSubmit(w io.Writer, resp svcbroker.SubmitResponse) error {
	ew := &errWriter{w: w}
	o := resp.Order
	_, _ = fmt.Fprintf(ew, "order_id=%s  broker_order_id=%s  status=%s\n", o.Request.OrderID, o.BrokerOrderID, o.Status)
	_, _ = fmt.Fprintf(ew, "symbol=%s  side=%s  filled_qty=%s\n", o.Request.Listing.Symbol(), o.Request.Side, o.FilledQuantity)
	return ew.err
}

// firstCashBalance returns snap's first cash balance, or "(none)" if
// it somehow reports none — every AccountConfig-constructed
// simulated account has exactly one, but a defensively empty slice
// must not panic a formatter.
func firstCashBalance(snap account.Snapshot) string {
	balances := snap.CashBalances()
	if len(balances) == 0 {
		return "(none)"
	}
	return balances[0].String()
}
