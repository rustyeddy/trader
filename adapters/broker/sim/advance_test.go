package sim

import (
	"context"
	"testing"
	"time"

	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var barTime = testStart.Add(time.Hour)

func TestAdvanceRejectsInvalidObservation(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)

	err = b.Advance(ctx, Observation{}) // zero value: no listing, no time
	require.ErrorIs(t, err, ErrInvalidObservation)
}

func TestAdvanceRejectsClosedBroker(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	require.NoError(t, b.Close())

	obs := mustObservation(t, mustEurUsdListing(t), "1.10000", "1.10500", "1.09500", "1.10200", barTime)
	err = b.Advance(ctx, obs)
	require.ErrorIs(t, err, brokerpkg.ErrClosed)
}

// TestAdvanceBuyLimitFillsAtOpenOnFavorableGap covers ADR-026's limit
// gap rule: a buy limit's price is a maximum, so an open already at or
// below it must fill at the better open price, not the limit.
func TestAdvanceBuyLimitFillsAtOpenOnFavorableGap(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustLimitRequest(t, deps.IDs, accountID, order.Buy, "1000", "1.10000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.09000", "1.09500", "1.08800", "1.09300", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Empty(t, snap.OpenOrders())
	require.Len(t, snap.Positions(), 1)
	assert.True(t, snap.Positions()[0].AvgPrice.Equal(num.MustParsePrice("1.09000")), "must fill at the better open, not the requested limit")
}

// TestAdvanceBuyLimitFillsAtLimitOnNormalTouch covers the non-gap case:
// the bar's low reaches the limit but the open never did.
func TestAdvanceBuyLimitFillsAtLimitOnNormalTouch(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustLimitRequest(t, deps.IDs, accountID, order.Buy, "1000", "1.10000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.10500", "1.10600", "1.09900", "1.10300", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.Positions(), 1)
	assert.True(t, snap.Positions()[0].AvgPrice.Equal(num.MustParsePrice("1.10000")))
}

func TestAdvanceBuyLimitDoesNotTriggerWhenUnreached(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustLimitRequest(t, deps.IDs, accountID, order.Buy, "1000", "1.05000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.10000", "1.10500", "1.09500", "1.10200", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.OpenOrders(), 1)
	assert.Empty(t, snap.Positions())
}

func TestAdvanceSellLimitFillsAtOpenOnFavorableGap(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustLimitRequest(t, deps.IDs, accountID, order.Sell, "1000", "1.10000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.11000", "1.11500", "1.10800", "1.11300", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.Positions(), 1)
	assert.True(t, snap.Positions()[0].AvgPrice.Equal(num.MustParsePrice("1.11000")))
}

func TestAdvanceSellLimitFillsAtLimitOnNormalTouch(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustLimitRequest(t, deps.IDs, accountID, order.Sell, "1000", "1.10000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.09500", "1.10200", "1.09400", "1.09800", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.Positions(), 1)
	assert.True(t, snap.Positions()[0].AvgPrice.Equal(num.MustParsePrice("1.10000")))
}

func TestAdvanceBuyStopFillsAtOpenOnAdverseGap(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustStopRequest(t, deps.IDs, accountID, order.Buy, "1000", "1.10000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.10500", "1.10800", "1.10300", "1.10600", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.Positions(), 1)
	assert.True(t, snap.Positions()[0].AvgPrice.Equal(num.MustParsePrice("1.10500")), "an adverse gap must fill at the worse open, not the requested stop")
}

func TestAdvanceBuyStopFillsAtStopOnNormalTouch(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustStopRequest(t, deps.IDs, accountID, order.Buy, "1000", "1.10000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.09500", "1.10200", "1.09400", "1.10100", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.Positions(), 1)
	assert.True(t, snap.Positions()[0].AvgPrice.Equal(num.MustParsePrice("1.10000")))
}

func TestAdvanceSellStopFillsAtOpenOnAdverseGap(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustStopRequest(t, deps.IDs, accountID, order.Sell, "1000", "1.10000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.09500", "1.09800", "1.09200", "1.09600", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.Positions(), 1)
	assert.True(t, snap.Positions()[0].AvgPrice.Equal(num.MustParsePrice("1.09500")))
}

func TestAdvanceSellStopFillsAtStopOnNormalTouch(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustStopRequest(t, deps.IDs, accountID, order.Sell, "1000", "1.10000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.10300", "1.10500", "1.09900", "1.10100", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.Positions(), 1)
	assert.True(t, snap.Positions()[0].AvgPrice.Equal(num.MustParsePrice("1.10000")))
}

func TestObservationValidate(t *testing.T) {
	listing := mustEurUsdListing(t)
	tests := []struct {
		name string
		obs  Observation
	}{
		{name: "zero listing", obs: Observation{Time: barTime, Open: num.MustParsePrice("1"), High: num.MustParsePrice("1"), Low: num.MustParsePrice("1"), Close: num.MustParsePrice("1")}},
		{name: "zero time", obs: Observation{Listing: listing, Open: num.MustParsePrice("1"), High: num.MustParsePrice("1"), Low: num.MustParsePrice("1"), Close: num.MustParsePrice("1")}},
		{name: "high below low", obs: Observation{Listing: listing, Time: barTime, Open: num.MustParsePrice("1"), High: num.MustParsePrice("0.9"), Low: num.MustParsePrice("1"), Close: num.MustParsePrice("1")}},
		{name: "open above high", obs: Observation{Listing: listing, Time: barTime, Open: num.MustParsePrice("1.2"), High: num.MustParsePrice("1.1"), Low: num.MustParsePrice("1"), Close: num.MustParsePrice("1.05")}},
		{name: "open below low", obs: Observation{Listing: listing, Time: barTime, Open: num.MustParsePrice("0.9"), High: num.MustParsePrice("1.1"), Low: num.MustParsePrice("1"), Close: num.MustParsePrice("1.05")}},
		{name: "close above high", obs: Observation{Listing: listing, Time: barTime, Open: num.MustParsePrice("1.05"), High: num.MustParsePrice("1.1"), Low: num.MustParsePrice("1"), Close: num.MustParsePrice("1.2")}},
		{name: "close below low", obs: Observation{Listing: listing, Time: barTime, Open: num.MustParsePrice("1.05"), High: num.MustParsePrice("1.1"), Low: num.MustParsePrice("1"), Close: num.MustParsePrice("0.9")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, tt.obs.validate(), ErrInvalidObservation)
		})
	}
}

// TestAdvanceEmitsFillThenFilledOrderEventsCausedByObservation covers
// the event shape: unlike a market fill (three events; #149), a
// triggered limit/stop fill emits exactly two — the pending order was
// already accepted by an earlier Submit call — with a zero
// CausationID on the fill event since nothing Trader-internal caused
// it.
func TestAdvanceEmitsFillThenFilledOrderEventsCausedByObservation(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustLimitRequest(t, deps.IDs, accountID, order.Buy, "1000", "1.10000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.10500", "1.10600", "1.09900", "1.10300", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	reader, err := acc.Events(ctx, "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	events := drainEvents(t, reader, 3) // Submit's accept event, then Advance's fill + filled-order events
	assertNoMoreEventsSoon(t, reader)

	require.Equal(t, brokerpkg.EventKindOrder, events[0].Kind)
	assert.Equal(t, order.StatusWorking, events[0].Order.Status)

	require.Equal(t, brokerpkg.EventKindFill, events[1].Kind)
	assert.True(t, events[1].Metadata.CausationID.IsZero(), "a market-observation-triggered fill is not caused by any preceding Trader event")

	require.Equal(t, brokerpkg.EventKindOrder, events[2].Kind)
	assert.Equal(t, order.StatusFilled, events[2].Order.Status)
	assert.Equal(t, events[1].Metadata.EventID, events[2].Metadata.CausationID)
}

// TestAdvanceRejectsAmbiguousIntrabarTriggerByDefault covers ADR-026's
// default IntrabarRejectAmbiguous policy: two independent pending
// orders on the same account/listing both trigger *within the bar*
// (neither at the observation's Open, so their relative order is
// genuinely unknown), so neither fills.
func TestAdvanceRejectsAmbiguousIntrabarTriggerByDefault(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	buyLimit := mustLimitRequest(t, deps.IDs, accountID, order.Buy, "1000", "1.10000")
	_, err = acc.Submit(ctx, buyLimit)
	require.NoError(t, err)
	sellStop := mustStopRequest(t, deps.IDs, accountID, order.Sell, "500", "1.09800")
	_, err = acc.Submit(ctx, sellStop)
	require.NoError(t, err)

	// Open (1.10050) is above the buy limit and above the sell stop, so
	// neither triggers at the open; both only trigger later, via Low
	// (1.09700) dipping through both prices. OHLC alone cannot say
	// which the market actually reached first within the bar.
	obs := mustObservation(t, mustEurUsdListing(t), "1.10050", "1.10200", "1.09700", "1.09900", barTime)
	err = b.Advance(ctx, obs)
	require.ErrorIs(t, err, ErrAmbiguousIntrabarOrder)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	assert.Len(t, snap.OpenOrders(), 2, "neither conflicting order may be filled")
	assert.Empty(t, snap.Positions())
}

func TestAdvanceReportsUnsupportedForIntrabarPessimistic(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	deps.IntrabarPolicy = IntrabarPessimistic
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	buyLimit := mustLimitRequest(t, deps.IDs, accountID, order.Buy, "1000", "1.10000")
	_, err = acc.Submit(ctx, buyLimit)
	require.NoError(t, err)
	sellStop := mustStopRequest(t, deps.IDs, accountID, order.Sell, "500", "1.09800")
	_, err = acc.Submit(ctx, sellStop)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.10050", "1.10200", "1.09700", "1.09900", barTime)
	err = b.Advance(ctx, obs)
	require.ErrorIs(t, err, brokerpkg.ErrUnsupported)
}

// TestAdvanceFillsAtOpenOrdersSequentiallyNotAsAmbiguous covers the
// distinction ADR-026 draws between "more than one order triggers
// within one bar" (potentially ambiguous) and "more than one order
// triggers at the observation's Open" (never ambiguous, since Open is
// the bar's single known first instant). Both orders here resolve at
// Open; the first fills and opens a Position, and the second — a Sell
// against that new Long position — correctly fails with
// ErrPositionUpdateUnsupported, not ErrAmbiguousIntrabarOrder: OHLC
// ordering was never the uncertainty.
func TestAdvanceFillsAtOpenOrdersSequentiallyNotAsAmbiguous(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	buyLimit := mustLimitRequest(t, deps.IDs, accountID, order.Buy, "1000", "1.10000")
	_, err = acc.Submit(ctx, buyLimit)
	require.NoError(t, err)
	sellStop := mustStopRequest(t, deps.IDs, accountID, order.Sell, "500", "1.09800")
	_, err = acc.Submit(ctx, sellStop)
	require.NoError(t, err)

	// Open (1.09900) is below the buy limit (1.10000 <= condition met
	// at open) and above the sell stop (1.09900 > 1.09800, so the stop
	// does NOT trigger at open — it only triggers later via Low
	// reaching 1.09700). Only the buy limit is at-open here.
	obs := mustObservation(t, mustEurUsdListing(t), "1.09900", "1.10100", "1.09700", "1.09950", barTime)
	err = b.Advance(ctx, obs)
	require.ErrorIs(t, err, ErrPositionUpdateUnsupported)
	require.NotErrorIs(t, err, ErrAmbiguousIntrabarOrder)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.Positions(), 1)
	assert.Equal(t, order.Long, snap.Positions()[0].Side)
	assert.True(t, snap.Positions()[0].Quantity.Equal(num.MustParseQuantity("1000")), "the buy limit must have filled")
	require.Len(t, snap.OpenOrders(), 1, "the sell stop remains pending, rejected only by the existing-position boundary")
	assert.Equal(t, sellStop.OrderID, snap.OpenOrders()[0].Request.OrderID)
}

func TestAdvanceHonorsCanceledContext(t *testing.T) {
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(context.Background(), accountID)
	require.NoError(t, err)

	req := mustLimitRequest(t, deps.IDs, accountID, order.Buy, "1000", "1.10000")
	_, err = acc.Submit(context.Background(), req)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	obs := mustObservation(t, mustEurUsdListing(t), "1.09000", "1.09500", "1.08800", "1.09300", barTime)
	err = b.Advance(ctx, obs)
	require.ErrorIs(t, err, context.Canceled)

	snap, err := acc.Snapshot(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.OpenOrders(), 1, "a canceled context must leave the pending order untouched")
	assert.Empty(t, snap.Positions())

	reader, err := acc.Events(context.Background(), "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	drainEvents(t, reader, 1)         // the Submit accept event
	assertNoMoreEventsSoon(t, reader) // Advance must not have emitted anything
}

func TestAdvanceIgnoresMarketAndStopLimitOrders(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	limitPrice := num.MustParsePrice("1.10000")
	stopPrice := num.MustParsePrice("1.10000")
	proposal, err := order.NewProposal(order.Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   accountID,
		Side:        order.Buy,
		Type:        order.StopLimit,
		TimeInForce: order.GTC,
		Quantity:    num.MustParseQuantity("1000"),
		LimitPrice:  &limitPrice,
		StopPrice:   &stopPrice,
		Metadata:    id.Metadata{EventID: mustEventID(t, deps.IDs)},
	})
	require.NoError(t, err)
	stopLimitReq, err := order.NewRequest(proposal, mustOrderID(t, deps.IDs))
	require.NoError(t, err)
	_, err = acc.Submit(ctx, stopLimitReq)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.09000", "1.11000", "1.08500", "1.10500", barTime)
	require.NoError(t, b.Advance(ctx, obs))

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.OpenOrders(), 1, "StopLimit is not evaluated by Advance in this package yet")
	assert.Equal(t, order.StatusWorking, snap.OpenOrders()[0].Status)
	assert.Empty(t, snap.Positions())
}

func TestAdvanceFillAgainstExistingPositionIsUnsupported(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	market := mustMarketRequest(t, deps.IDs, accountID, order.Buy, "1000")
	_, err = acc.Submit(ctx, market)
	require.NoError(t, err)

	limitReq := mustLimitRequest(t, deps.IDs, accountID, order.Buy, "500", "1.10000")
	_, err = acc.Submit(ctx, limitReq)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.09500", "1.10200", "1.09400", "1.10100", barTime)
	err = b.Advance(ctx, obs)
	require.ErrorIs(t, err, ErrPositionUpdateUnsupported)

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.Positions(), 1)
	assert.True(t, snap.Positions()[0].Quantity.Equal(num.MustParseQuantity("1000")), "the rejected limit fill must not have changed the existing position")
	require.Len(t, snap.OpenOrders(), 1, "the limit order remains pending, not consumed by the failed attempt")
}

func TestAdvanceRepeatedCallsDoNotRefillATerminalOrder(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	accountID := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000")})
	require.NoError(t, err)
	acc, err := b.OpenAccount(ctx, accountID)
	require.NoError(t, err)

	req := mustLimitRequest(t, deps.IDs, accountID, order.Buy, "1000", "1.10000")
	_, err = acc.Submit(ctx, req)
	require.NoError(t, err)

	obs := mustObservation(t, mustEurUsdListing(t), "1.09000", "1.09500", "1.08800", "1.09300", barTime)
	require.NoError(t, b.Advance(ctx, obs))
	require.NoError(t, b.Advance(ctx, obs)) // same observation again

	snap, err := acc.Snapshot(ctx)
	require.NoError(t, err)
	require.Len(t, snap.Positions(), 1)
	assert.True(t, snap.Positions()[0].Quantity.Equal(num.MustParseQuantity("1000")), "a second Advance must not fill the already-terminal order again")
}

func TestAdvanceAcrossAccountsIsIsolatedAndOneFailureDoesNotBlockAnother(t *testing.T) {
	ctx := context.Background()
	deps := testDeps()
	a1 := mustAccountID(t, deps.IDs)
	a2 := mustAccountID(t, deps.IDs)
	b, err := NewBroker("sim", deps,
		AccountConfig{AccountID: a1, StartingCash: usd("10000")},
		AccountConfig{AccountID: a2, StartingCash: usd("10000")},
	)
	require.NoError(t, err)
	acc1, err := b.OpenAccount(ctx, a1)
	require.NoError(t, err)
	acc2, err := b.OpenAccount(ctx, a2)
	require.NoError(t, err)

	// Account 1 has two conflicting orders (will be ambiguous);
	// account 2 has one clean order (will fill normally).
	buyLimit := mustLimitRequest(t, deps.IDs, a1, order.Buy, "1000", "1.10000")
	_, err = acc1.Submit(ctx, buyLimit)
	require.NoError(t, err)
	sellStop := mustStopRequest(t, deps.IDs, a1, order.Sell, "500", "1.09800")
	_, err = acc1.Submit(ctx, sellStop)
	require.NoError(t, err)

	cleanLimit := mustLimitRequest(t, deps.IDs, a2, order.Buy, "1000", "1.10000")
	_, err = acc2.Submit(ctx, cleanLimit)
	require.NoError(t, err)

	// Same genuinely-ambiguous observation as
	// TestAdvanceRejectsAmbiguousIntrabarTriggerByDefault: neither of
	// account 1's two orders triggers at Open, so they conflict; account
	// 2's single order triggers within the bar too, but with nothing
	// else to conflict with, it is not ambiguous.
	obs := mustObservation(t, mustEurUsdListing(t), "1.10050", "1.10200", "1.09700", "1.09900", barTime)
	err = b.Advance(ctx, obs)
	require.ErrorIs(t, err, ErrAmbiguousIntrabarOrder)

	snap1, err := acc1.Snapshot(ctx)
	require.NoError(t, err)
	assert.Len(t, snap1.OpenOrders(), 2)

	snap2, err := acc2.Snapshot(ctx)
	require.NoError(t, err)
	assert.Empty(t, snap2.OpenOrders(), "account 2's clean fill must not be blocked by account 1's ambiguity")
	require.Len(t, snap2.Positions(), 1)
}
