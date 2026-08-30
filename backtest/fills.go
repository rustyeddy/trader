package backtest

import (
	"context"
	"errors"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/order"
)

// eventDrainTimeout bounds each individual read from the account's
// event stream while drainFills drains it for one run's fills (see
// drainFills' own doc comment for why). It is not a real wait: every
// already-produced event returns from EventReader.Next immediately,
// since Next only blocks once its internal buffer is exhausted (see
// broker.EventReader's own doc comment) — so this timeout only ever
// elapses on the one call that confirms the stream has caught up,
// never as a race against genuine event delivery.
const eventDrainTimeout = 50 * time.Millisecond

// drainFills reads every order.Fill event reader has buffered, in
// delivery (Sequence) order, and returns their order.Fill payloads.
//
// It is only safe to call once no further event-producing activity
// will occur on the underlying Account — Runner guarantees this by
// calling it only after Scheduler.Run has already returned, and a
// backtest's synchronous, single-goroutine execution model (Scheduler
// drives every Pipeline.Submit call directly, with no asynchronous
// broker round-trip) guarantees every event this run will ever produce
// is already present in the account's own log by that point.
//
// broker.EventReader.Next blocks indefinitely waiting for a future
// event rather than returning io.EOF merely because it has caught up
// (ADR-024) — that behavior is correct for a live/paper stream, but
// Runner does not own the Account's underlying broker.Broker to close
// it and trigger a real io.EOF the way a finished session normally
// would. drainFills instead detects "nothing more is coming" by giving
// each Next call its own short, fixed deadline, deliberately
// independent of ctx's own deadline (see the loop body): a genuinely
// buffered event is always returned well within that deadline, so it
// only ever elapses on the one call made once the stream is caught up.
// ctx's own cancellation/expiry is still checked explicitly on every
// iteration and returned as a real error, never mistaken for "caught
// up."
func drainFills(ctx context.Context, reader broker.EventReader) ([]order.Fill, error) {
	var fills []order.Fill
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		callCtx, cancel := context.WithTimeout(context.Background(), eventDrainTimeout)
		event, err := reader.Next(callCtx)
		cancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return fills, nil
			}
			return nil, err
		}
		if event.Kind == broker.EventKindFill && event.Fill != nil {
			fills = append(fills, *event.Fill)
		}
	}
}
