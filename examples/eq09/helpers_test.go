package eq09

// This file holds the pure, network-free logic smoke_test.go's own
// "alpacasmoke"-gated live test relies on — deliberately kept in an
// untagged _test.go file (compiled by every `go test
// ./examples/eq09/...` invocation, with or without -tags alpacasmoke)
// so it gets real, always-run unit test coverage rather than living
// entirely behind an opt-in, real-credential-gated build tag that
// almost never runs in CI.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/account"
	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/order"
)

// isFlatSPY reports whether snap holds no open (non-Flat) position for
// spyListing's instrument.
func isFlatSPY(snap account.Snapshot, spyListing instrument.Listing) bool {
	for _, pos := range snap.Positions() {
		if pos.Listing.InstrumentID() == spyListing.InstrumentID() && pos.Side != order.Flat {
			return false
		}
	}
	return true
}

func assertFlatSPYPosition(t *testing.T, snap account.Snapshot, spyListing instrument.Listing) {
	t.Helper()
	assert.True(t, isFlatSPY(snap, spyListing), "expected no open SPY position after flattening, got positions: %+v", snap.Positions())
}

// waitForFill drains reader until it observes orderID transition to
// order.StatusFilled, or the overall timeout elapses first — returning
// false in the latter case so the caller can run its own
// cancel-safety-net path rather than treating a slow fill as an
// architecture failure. The timeout bounds total wait time across every
// intervening event (including other orders' events replayed from the
// same correlator, and this adapter's own poll-interval cadence) via
// one shared context, not a fresh full-length allowance per Next call.
func waitForFill(t *testing.T, ctx context.Context, reader brokerpkg.EventReader, orderID id.OrderID, timeout time.Duration) bool {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		ev, err := reader.Next(waitCtx)
		if err != nil {
			return false // overall timeout elapsed, or the reader/broker closed
		}
		if ev.Kind != brokerpkg.EventKindOrder || ev.Order == nil || ev.Order.Request.OrderID != orderID {
			continue
		}
		t.Logf("observed order %s transition to status %s", orderID, ev.Order.Status)
		switch ev.Order.Status {
		case order.StatusFilled:
			return true
		case order.StatusRejected, order.StatusCanceled, order.StatusExpired:
			return false
		}
	}
}

func mustFreshEventID(t *testing.T, ids *id.Generator) id.EventID {
	t.Helper()
	eventID, err := id.GenerateEventID(ids)
	require.NoError(t, err)
	return eventID
}
