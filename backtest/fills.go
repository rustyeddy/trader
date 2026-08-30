package backtest

import (
	"context"
	"errors"
	"fmt"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/order"
)

// ErrEventReaderNotFinite reports that an Account's EventReader does
// not implement broker.FiniteEventReader — Runner requires it, since
// a backtest run's trade derivation needs to drain exactly what the
// run produced, deterministically, without racing the reader's
// ordinary live-stream blocking behavior (see drainFills' own doc
// comment).
var ErrEventReaderNotFinite = errors.New("backtest: event reader does not support finite draining")

// drainFills reads every order.Fill event reader has recorded so far,
// in delivery (Sequence) order, and returns their order.Fill payloads.
//
// broker.EventReader.Next blocks indefinitely waiting for a future
// event rather than returning io.EOF merely because it has caught up
// (ADR-024) — correct for a live/paper stream, but Runner needs a
// deterministic way to know it has drained exactly this run's own
// fills, not a timing-dependent guess. drainFills therefore requires
// reader to implement broker.FiniteEventReader and uses AtEnd to
// decide when to stop: AtEnd is checked before every Next call, so the
// loop only ever calls Next when a buffered event is already known to
// be waiting, and never blocks. This makes drainFills' termination a
// property of the account's own recorded state, not of wall-clock
// timing or Runner's own execution model.
func drainFills(ctx context.Context, reader broker.EventReader) ([]order.Fill, error) {
	finite, ok := reader.(broker.FiniteEventReader)
	if !ok {
		return nil, fmt.Errorf("%w: %T", ErrEventReaderNotFinite, reader)
	}

	var fills []order.Fill
	for !finite.AtEnd() {
		event, err := reader.Next(ctx)
		if err != nil {
			return nil, err
		}
		if event.Kind == broker.EventKindFill && event.Fill != nil {
			fills = append(fills, *event.Fill)
		}
	}
	return fills, nil
}
