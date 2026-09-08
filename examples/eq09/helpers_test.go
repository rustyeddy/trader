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
	"errors"
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

// awaitFillEvidence drains reader until it has observed BOTH orderID
// transitioning to order.StatusFilled AND a canonical EventKindFill
// event naming orderID, or the overall timeout elapses first — issue
// #302 asks to verify order state AND fill state, and Order.Status
// alone is not that proof: an adapter could (in principle) report a
// status without ever having emitted the corresponding Fill event, or
// vice versa, and this smoke test's own job is to demonstrate both are
// observable through the canonical broker.Event stream, not just one
// (PR #314 review). It returns early, before the timeout, once orderID
// reaches a terminal non-fill status (StatusRejected/Canceled/Expired)
// — no further event for this order can ever supply the missing
// evidence at that point. The timeout bounds total wait time across
// every intervening event (including other orders' events replayed
// from the same correlator, and this adapter's own poll-interval
// cadence) via one shared context, not a fresh full-length allowance
// per Next call.
//
// Only waitCtx's own deadline expiring is treated as "still working,
// just slow." Any other error — broker.EventReader's own io.EOF once
// its producer has ended (Broker.Close), or any other failure — is a
// real stream/broker failure, not a benign timeout, and is reported
// via t.Fatalf so it cannot be silently masked as an environment/timing
// skip (PR #314 review).
func awaitFillEvidence(t *testing.T, ctx context.Context, reader brokerpkg.EventReader, orderID id.OrderID, timeout time.Duration) (statusFilled, fillObserved bool) {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		if statusFilled && fillObserved {
			return true, true
		}
		ev, err := reader.Next(waitCtx)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return statusFilled, fillObserved
			}
			t.Fatalf("event reader failed while waiting for order %s: %v", orderID, err)
			return false, false
		}
		switch ev.Kind {
		case brokerpkg.EventKindOrder:
			if ev.Order == nil || ev.Order.Request.OrderID != orderID {
				continue
			}
			t.Logf("observed order %s transition to status %s", orderID, ev.Order.Status)
			switch ev.Order.Status {
			case order.StatusFilled:
				statusFilled = true
			case order.StatusRejected, order.StatusCanceled, order.StatusExpired:
				return false, fillObserved
			}
		case brokerpkg.EventKindFill:
			if ev.Fill != nil && ev.Fill.OrderID == orderID {
				t.Logf("observed fill event for order %s: %s shares @ %s", orderID, ev.Fill.Quantity, ev.Fill.Price)
				fillObserved = true
			}
		}
	}
}

// waitForFill reports whether order orderID reached order.StatusFilled
// with a corresponding EventKindFill event observed, within timeout.
// See awaitFillEvidence for the full contract.
func waitForFill(t *testing.T, ctx context.Context, reader brokerpkg.EventReader, orderID id.OrderID, timeout time.Duration) bool {
	t.Helper()
	statusFilled, fillObserved := awaitFillEvidence(t, ctx, reader, orderID, timeout)
	return statusFilled && fillObserved
}

func mustFreshEventID(t *testing.T, ids *id.Generator) id.EventID {
	t.Helper()
	eventID, err := id.GenerateEventID(ids)
	require.NoError(t, err)
	return eventID
}

// cancelAndAwaitTerminal cancels orderID and determines its true
// terminal ADR-018 status — never assuming the synchronous
// CancelResult.Status is automatically that final state, but also
// never ignoring it when it already is one. Alpaca cancels
// asynchronously, so the order can still race to a fill after the
// cancel request is accepted; this function determines what genuinely
// happened rather than letting a caller act on a stale assumption
// (PR #314 review).
//
// If result.Status is itself already terminal — most commonly because
// Alpaca declined the cancel outright and reported the order's real
// current state (see Cancel's own doc comment in cancel_replace.go:
// "CancelResult.Status reflects the order's actual state" on a
// decline) — that is trusted directly, without waiting for a further
// status-change event. This matters beyond avoiding a redundant wait:
// the terminal transition may already have been consumed from reader
// by an earlier awaitFillEvidence call on this same reader instance
// (whose own internal loop keeps draining until ITS timeout, past
// whatever terminal order event arrives), in which case waiting here
// for a "new" terminal event would hang until this function's own
// timeout for an event that will never be redelivered (PR #314
// review, second finding).
func cancelAndAwaitTerminal(t *testing.T, ctx context.Context, acc brokerpkg.Account, reader brokerpkg.EventReader, ids *id.Generator, orderID id.OrderID, timeout time.Duration) order.Status {
	t.Helper()
	result, err := acc.Cancel(ctx, order.CancelRequest{
		OrderID:  orderID,
		Metadata: id.Metadata{EventID: mustFreshEventID(t, ids)},
	})
	require.NoError(t, err)
	if result.Status.Terminal() {
		t.Logf("cancel reported order %s already in terminal status %s", orderID, result.Status)
		return result.Status
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		ev, err := reader.Next(waitCtx)
		if err != nil {
			t.Fatalf("could not reconcile order %s to a terminal status after cancel (%v); the paper account needs manual review", orderID, err)
		}
		if ev.Kind != brokerpkg.EventKindOrder || ev.Order == nil || ev.Order.Request.OrderID != orderID {
			continue
		}
		t.Logf("observed order %s transition to status %s while reconciling after cancel", orderID, ev.Order.Status)
		if ev.Order.Status.Terminal() {
			return ev.Order.Status
		}
	}
}
