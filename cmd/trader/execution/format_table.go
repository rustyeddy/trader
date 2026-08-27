package execution

import (
	"fmt"
	"io"

	svcexecution "github.com/rustyeddy/trader/service/execution"
)

// tableFormatter is the default, human-readable Formatter: a plain
// text line per record, using errWriter's sticky-error idiom so a
// broken output pipe is reported through the same error return
// jsonFormatter already uses, not silently ignored — the same
// convention cmd/trader/broker's own tableFormatter establishes.
type tableFormatter struct{}

// errWriter wraps an io.Writer and remembers the first error any
// Write call returns, turning every write after that into a no-op —
// duplicated from cmd/trader/broker's own errWriter rather than
// shared: each CLI command family package is deliberately independent
// (issue #201).
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

// writeCommon renders the fields FormatEvaluate/FormatSubmit share:
// Proposal (once planning succeeds) and Decision (once risk evaluation
// completes) — always present unless resp is entirely zero, which
// only happens on a structural failure this formatter is never called
// for.
func writeCommon(ew *errWriter, resp svcexecution.SubmitResponse) {
	p := resp.Proposal
	_, _ = fmt.Fprintf(ew, "instrument=%s  side=%s  quantity=%s\n", p.Listing.Symbol(), p.Side, p.Quantity)

	d := resp.Decision
	_, _ = fmt.Fprintf(ew, "allowed=%t\n", d.Allowed)
	if len(d.Violations) == 0 {
		_, _ = fmt.Fprintln(ew, "violations: (none)")
	} else {
		_, _ = fmt.Fprintln(ew, "violations:")
		for _, v := range d.Violations {
			_, _ = fmt.Fprintf(ew, "  %s: %s (measured=%s limit=%s)\n", v.Rule, v.Message, v.Measured, v.Limit)
		}
	}
	if len(d.Warnings) > 0 {
		_, _ = fmt.Fprintln(ew, "warnings:")
		for _, w := range d.Warnings {
			_, _ = fmt.Fprintf(ew, "  %s: %s\n", w.Rule, w.Message)
		}
	}

	if d.Allowed {
		r := resp.Request
		_, _ = fmt.Fprintf(ew, "request: order_id=%s  type=%s  time_in_force=%s\n", r.OrderID, r.Type, r.TimeInForce)
	}
}

func (tableFormatter) FormatEvaluate(w io.Writer, resp svcexecution.SubmitResponse) error {
	ew := &errWriter{w: w}
	writeCommon(ew, resp)
	return ew.err
}

func (tableFormatter) FormatSubmit(w io.Writer, resp svcexecution.SubmitResponse) error {
	ew := &errWriter{w: w}
	writeCommon(ew, resp)
	if resp.Decision.Allowed {
		o := resp.Order
		_, _ = fmt.Fprintf(ew, "order: broker_order_id=%s  status=%s  filled_qty=%s\n", o.BrokerOrderID, o.Status, o.FilledQuantity)
	}
	return ew.err
}
