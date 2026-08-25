package sim

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/account"
	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// verticalSliceResult is every observable output
// runVerticalSliceScenario produces, gathered in one place so
// TestVerticalSlice_DeterministicAcrossRuns can diff two independent
// runs field by field.
type verticalSliceResult struct {
	SnapA, SnapB     account.Snapshot
	EventsA, EventsB []brokerpkg.Event
}

// runVerticalSliceScenario is issue #157/M3-14's own primary
// deliverable: one deterministic, end-to-end scenario across two
// isolated accounts sharing a single Broker, exercised entirely
// through the public brokerpkg.Broker/Account port -- never through
// simulator-internal fields -- covering every M3-14 acceptance
// criterion in one coherent story: known starting capital, account
// discovery, a market fill, two pending orders, a cancel, a replace,
// an Advance-triggered stop fill that partially closes a position, and
// final account state, followed by a second, entirely independent
// account proving isolation.
//
// Every input -- Deps.Clock, Deps.IDs, prices, and the Observation
// Advance evaluates -- is fully deterministic (clock.NewSimulated,
// id.NewDeterministic, a fixed/mutable price map, literal Observation
// values), so two calls to this function from independent Broker
// instances must produce byte-identical results; that equality is what
// TestVerticalSlice_DeterministicAcrossRuns actually asserts.
func runVerticalSliceScenario(t *testing.T) verticalSliceResult {
	t.Helper()
	ctx := context.Background()

	deps := testDeps()
	prices := &mutablePriceSource{prices: map[string]num.Price{
		"EUR_USD": num.MustParsePrice("1.10000"),
		"GBP_USD": num.MustParsePrice("1.25000"),
	}}
	deps.Prices = prices

	accountA := mustAccountID(t, deps.IDs)
	accountB := mustAccountID(t, deps.IDs)

	b, err := NewBroker("sim", deps,
		AccountConfig{AccountID: accountA, StartingCash: usd("10000")},
		AccountConfig{AccountID: accountB, StartingCash: usd("5000")},
	)
	require.NoError(t, err)

	// Account discovery covers both configured accounts, from known
	// starting capital.
	refs, err := b.Accounts(ctx)
	require.NoError(t, err)
	require.Len(t, refs, 2)

	accA, err := b.OpenAccount(ctx, accountA)
	require.NoError(t, err)
	accB, err := b.OpenAccount(ctx, accountB)
	require.NoError(t, err)

	// --- Account A: a market fill establishing a long position, two
	// pending orders against it, a cancel, a replace, and an
	// Advance-triggered partial close. ---
	buy := mustMarketRequest(t, deps.IDs, accountA, order.Buy, "1000")
	_, err = accA.Submit(ctx, buy) // fills @1.10000
	require.NoError(t, err)

	limitSell := mustLimitRequest(t, deps.IDs, accountA, order.Sell, "500", "1.15000")
	_, err = accA.Submit(ctx, limitSell) // stays Working
	require.NoError(t, err)

	stopSell := mustStopRequest(t, deps.IDs, accountA, order.Sell, "500", "1.05000")
	_, err = accA.Submit(ctx, stopSell) // stays Working
	require.NoError(t, err)

	_, err = accA.Cancel(ctx, mustCancelRequest(t, deps.IDs, limitSell.OrderID))
	require.NoError(t, err)

	_, err = accA.Replace(ctx, mustReplaceRequest(t, deps.IDs, stopSell.OrderID, "300"))
	require.NoError(t, err)

	listing := mustEurUsdListing(t)
	obs := mustObservation(t, listing, "1.06000", "1.07000", "1.04000", "1.04500", testStart.Add(time.Hour))
	require.NoError(t, b.Advance(ctx, obs)) // triggers the replaced stop within-bar @1.05000

	snapA, err := accA.Snapshot(ctx)
	require.NoError(t, err)

	readerA, err := accA.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = readerA.Close() }()
	// market buy (3) + limit submit (1) + stop submit (1) + cancel (2)
	// + replace (2) + Advance-triggered fill (2) = 11.
	eventsA := drainEvents(t, readerA, 11)
	assertNoMoreEventsSoon(t, readerA)

	// --- Account B: an entirely separate market order on a different
	// listing, never touched by anything just done on A. ---
	buyB := mustMarketRequestFor(t, deps.IDs, accountB, mustGbpUsdListing(t), order.Buy, "200")
	_, err = accB.Submit(ctx, buyB) // fills @1.25000
	require.NoError(t, err)

	snapB, err := accB.Snapshot(ctx)
	require.NoError(t, err)

	readerB, err := accB.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = readerB.Close() }()
	eventsB := drainEvents(t, readerB, 3)
	assertNoMoreEventsSoon(t, readerB)

	return verticalSliceResult{SnapA: snapA, SnapB: snapB, EventsA: eventsA, EventsB: eventsB}
}

// TestVerticalSlice_FullScenarioProducesExpectedState is the concrete,
// hand-computed proof behind runVerticalSliceScenario: every balance,
// position, and event count below is derived by hand from the
// scenario's own fixed prices, so a real ordering or accounting
// regression fails at a specific, named assertion rather than merely
// "the snapshot changed."
func TestVerticalSlice_FullScenarioProducesExpectedState(t *testing.T) {
	result := runVerticalSliceScenario(t)

	t.Run("account A: final position and PnL match hand-computed accounting", func(t *testing.T) {
		snap := result.SnapA
		// Buy 1000 @1.10000, no realized PnL yet; Sell 300 @1.05000
		// (within-bar stop trigger, ADR-026) partially closes it:
		// realized = (1.05000-1.10000)*300 = -15.00. Cash carries only
		// realized PnL/fees, never notional (ADR-025/ADR-027's own
		// cash-accounting boundary) -- 10000 + (-15) = 9985.
		require.True(t, snap.RealizedPnL().Equal(usd("-15")), "realized PnL: %s", snap.RealizedPnL())
		require.True(t, snap.CashBalances()[0].Equal(usd("9985")), "cash: %s", snap.CashBalances()[0])

		// Remaining 700 units, still at the original 1.10000 average
		// (a partial close realizes PnL without moving the average
		// cost of what remains). The triggering fill's own trade price
		// (1.05000) becomes the new mark, superseding the bar's Close
		// (1.04500) revaluation that happens earlier in the same
		// Advance call: unrealized = (1.05000-1.10000)*700 = -35.00.
		require.True(t, snap.UnrealizedPnL().Equal(usd("-35")), "unrealized PnL: %s", snap.UnrealizedPnL())
		require.True(t, snap.Equity().Equal(usd("9950")), "equity: %s", snap.Equity())

		require.Len(t, snap.Positions(), 1)
		pos := snap.Positions()[0]
		assert.Equal(t, order.Long, pos.Side)
		assert.True(t, pos.Quantity.Equal(num.MustParseQuantity("700")), "remaining quantity: %s", pos.Quantity)
		require.NotNil(t, pos.AvgPrice)
		assert.True(t, pos.AvgPrice.Equal(num.MustParsePrice("1.10000")), "avg price: %s", pos.AvgPrice)

		assert.Empty(t, snap.OpenOrders(), "the limit was canceled and the replaced stop is now terminal (Filled)")
	})

	t.Run("account A: event stream reflects the full order lifecycle", func(t *testing.T) {
		events := result.EventsA
		require.Len(t, events, 11)

		// A fill event is published before the order's own terminal
		// Filled status event (both caused by the same fill), so the
		// market buy's own three events are Working, Fill, then Filled.
		require.Equal(t, brokerpkg.EventKindOrder, events[0].Kind)
		assert.Equal(t, order.StatusWorking, events[0].Order.Status, "the market buy's own accept event")
		require.Equal(t, brokerpkg.EventKindFill, events[1].Kind)
		require.NotNil(t, events[1].Fill)
		assert.True(t, events[1].Fill.Quantity.Equal(num.MustParseQuantity("1000")))
		assert.True(t, events[1].Fill.Price.Equal(num.MustParsePrice("1.10000")))
		require.Equal(t, brokerpkg.EventKindOrder, events[2].Kind)
		assert.Equal(t, order.StatusFilled, events[2].Order.Status)

		var fillCount, canceledCount int
		for _, e := range events {
			if e.Kind == brokerpkg.EventKindFill {
				fillCount++
			}
			if e.Kind == brokerpkg.EventKindOrder && e.Order.Status == order.StatusCanceled {
				canceledCount++
			}
		}
		assert.Equal(t, 2, fillCount, "the market buy and the triggered stop are the only two fills")
		assert.Equal(t, 1, canceledCount, "only the limit order was canceled")

		// The same Fill-before-Filled ordering applies to the
		// Advance-triggered stop: its own Fill event is second-to-last,
		// and the order's terminal Filled status is the final event.
		secondToLast, last := events[len(events)-2], events[len(events)-1]
		require.Equal(t, brokerpkg.EventKindFill, secondToLast.Kind)
		require.NotNil(t, secondToLast.Fill)
		assert.True(t, secondToLast.Fill.Quantity.Equal(num.MustParseQuantity("300")))
		assert.True(t, secondToLast.Fill.Price.Equal(num.MustParsePrice("1.05000")), "a within-bar stop trigger fills at the stop price (ADR-026)")
		require.Equal(t, brokerpkg.EventKindOrder, last.Kind)
		assert.Equal(t, order.StatusFilled, last.Order.Status)
	})

	t.Run("account B: isolated from every account A operation", func(t *testing.T) {
		snap := result.SnapB
		assert.True(t, snap.RealizedPnL().Equal(usd("0")), "account A's realized loss must not appear on B")
		assert.True(t, snap.CashBalances()[0].Equal(usd("5000")), "B's starting cash is untouched by A's activity")
		assert.True(t, snap.Equity().Equal(usd("5000")))

		require.Len(t, snap.Positions(), 1)
		pos := snap.Positions()[0]
		assert.Equal(t, order.Long, pos.Side)
		assert.True(t, pos.Quantity.Equal(num.MustParseQuantity("200")))
		assert.Equal(t, "GBP_USD", pos.Listing.Symbol(), "B never touches EUR_USD, A's own listing")
		assert.Empty(t, snap.OpenOrders())

		require.Len(t, result.EventsB, 3, "B's own three-event market fill, none of A's eleven")

		for _, p := range result.SnapA.Positions() {
			assert.NotEqual(t, "GBP_USD", p.Listing.Symbol(), "A must never carry B's own instrument")
		}
	})
}

// TestVerticalSlice_DeterministicAcrossRuns proves the entire scenario
// -- account snapshots and complete event streams alike -- is
// byte-for-byte reproducible: two independent runs, each constructing
// its own fresh Broker from identical deterministic inputs, must
// produce identical observable results (issue #157/M3-14's own
// "repeated runs are byte/value/order equivalent" acceptance
// criterion).
func TestVerticalSlice_DeterministicAcrossRuns(t *testing.T) {
	first := runVerticalSliceScenario(t)
	second := runVerticalSliceScenario(t)

	require.Equal(t, first.SnapA, second.SnapA, "account A's final snapshot must be identical across runs")
	require.Equal(t, first.SnapB, second.SnapB, "account B's final snapshot must be identical across runs")
	require.Equal(t, first.EventsA, second.EventsA, "account A's complete event stream must be identical across runs")
	require.Equal(t, first.EventsB, second.EventsB, "account B's complete event stream must be identical across runs")
}
