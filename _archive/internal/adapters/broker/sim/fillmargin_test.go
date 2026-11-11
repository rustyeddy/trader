package sim

import (
	"context"
	"testing"

	brokerpkg "github.com/rustyeddy/trader/internal/broker"
	"github.com/rustyeddy/trader/internal/id"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fillMarginAccount opens a $10,000 account at the given ratio whose
// EUR/USD market price can be moved between calls, with optional
// deps adjustments.
func fillMarginAccount(t *testing.T, ratio string, adjust func(*Deps)) (*Broker, Deps, *accountHandle, *mutablePriceSource) {
	t.Helper()
	deps := testDeps()
	prices := &mutablePriceSource{prices: map[string]num.Price{
		"EUR_USD": num.MustParsePrice("1.10000"),
		"GBP_USD": num.MustParsePrice("1.25000"),
	}}
	deps.Prices = prices
	if adjust != nil {
		adjust(&deps)
	}
	accountID := mustAccountID(t, deps.IDs)
	r := num.MustParseRate(ratio)
	b, err := NewBroker("sim", deps, AccountConfig{AccountID: accountID, StartingCash: usd("10000"), InitialMarginRatio: &r})
	require.NoError(t, err)
	acc, err := b.OpenAccount(context.Background(), accountID)
	require.NoError(t, err)
	return b, deps, acc.(*accountHandle), prices
}

// orderEvents drains every event so far and returns each order
// event's status, in sequence order.
func orderEvents(t *testing.T, h *accountHandle) []runtimeorder.Status {
	t.Helper()
	reader, err := h.Events(context.Background(), "")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	var statuses []runtimeorder.Status
	for i := 0; i < len(h.state.events); i++ {
		ev, err := reader.Next(context.Background())
		require.NoError(t, err)
		if ev.Kind == brokerpkg.EventKindOrder {
			statuses = append(statuses, ev.Order.Status)
		}
	}
	return statuses
}

func TestFillMargin_GapUpRejectsFullNotionalMarketOrder(t *testing.T) {
	ctx := context.Background()
	_, deps, h, prices := fillMarginAccount(t, "1", nil)
	// Sized at the 1.10000 reference: 9090 × 1.1 = 9999 fits. The next
	// open gaps to 1.10100: 9090 × 1.101 = 10008.09 does not.
	prices.set("EUR_USD", num.MustParsePrice("1.10100"))

	o, err := h.Submit(ctx, mustMarketRequestFor(t, deps.IDs, h.Reference().AccountID, mustEurUsdListing(t), order.Buy, "9090"))
	require.NoError(t, err, "a margin refusal is an order outcome, not an error")
	assert.Equal(t, runtimeorder.StatusRejected, o.Status)
	require.NotNil(t, o.Rejection)
	assert.Equal(t, runtimeorder.ReasonInsufficientMargin, o.Rejection.Reason)
	assert.Contains(t, o.Rejection.Detail, "required margin 10008.09 USD")
	assert.Contains(t, o.Rejection.Detail, "gross notional 10008.09 USD, up from 0 USD")
	assert.Contains(t, o.Rejection.Detail, "post-fill equity 10000 USD")

	s := snapshot(t, h)
	assert.Empty(t, s.Positions())
	assert.Empty(t, s.OpenOrders())
	assert.Equal(t, []runtimeorder.Status{runtimeorder.StatusRejected}, orderEvents(t, h), "one rejection event, no accept event")
}

func TestFillMargin_GapUpWithHeadroomFills(t *testing.T) {
	_, deps, h, prices := fillMarginAccount(t, "1", nil)
	prices.set("EUR_USD", num.MustParsePrice("1.10100"))
	// 9000 × 1.101 = 9909 fits.
	o, err := h.Submit(context.Background(), mustMarketRequestFor(t, deps.IDs, h.Reference().AccountID, mustEurUsdListing(t), order.Buy, "9000"))
	require.NoError(t, err)
	assert.Equal(t, runtimeorder.StatusFilled, o.Status)
	require.Len(t, snapshot(t, h).Positions(), 1)
}

func TestFillMargin_ExactLimitFills(t *testing.T) {
	_, deps, h, prices := fillMarginAccount(t, "1", nil)
	prices.set("EUR_USD", num.MustParsePrice("1.00000"))
	o, err := h.Submit(context.Background(), mustMarketRequestFor(t, deps.IDs, h.Reference().AccountID, mustEurUsdListing(t), order.Buy, "10000"))
	require.NoError(t, err)
	assert.Equal(t, runtimeorder.StatusFilled, o.Status, "exactly equal to equity is within the limit")
}

func TestFillMargin_EquityIsAfterTheFillsOwnCommission(t *testing.T) {
	for _, tc := range []struct {
		commission string
		want       runtimeorder.Status
	}{
		{"0", runtimeorder.StatusFilled},   // 9999 ≤ 10000
		{"1", runtimeorder.StatusFilled},   // 9999 ≤ 9999
		{"2", runtimeorder.StatusRejected}, // 9999 > 9998
	} {
		t.Run("commission "+tc.commission, func(t *testing.T) {
			_, deps, h, _ := fillMarginAccount(t, "1", func(d *Deps) {
				d.Commission = fixedCommission{amount: usd(tc.commission)}
			})
			o, err := h.Submit(context.Background(), mustMarketRequestFor(t, deps.IDs, h.Reference().AccountID, mustEurUsdListing(t), order.Buy, "9090"))
			require.NoError(t, err)
			assert.Equal(t, tc.want, o.Status)
		})
	}
}

func TestFillMargin_ShortEntryCountsGross(t *testing.T) {
	_, deps, h, _ := fillMarginAccount(t, "1", nil)
	// 10000 GBP short at 1.25 = 12500 of gross exposure.
	o, err := h.Submit(context.Background(), mustMarketRequestFor(t, deps.IDs, h.Reference().AccountID, mustGbpUsdListing(t), order.Sell, "10000"))
	require.NoError(t, err)
	assert.Equal(t, runtimeorder.StatusRejected, o.Status)
}

// twoRestingLimitsScenario submits two resting limit buys that each fit
// alone (5000 × 1.09 = 5450) but not together (10900 > 10000), then a
// bar that gaps below both so they trigger at the open in one Advance.
func twoRestingLimitsScenario(t *testing.T) (*accountHandle, []runtimeorder.Order) {
	t.Helper()
	ctx := context.Background()
	b, deps, h, _ := fillMarginAccount(t, "1", nil)
	eur := mustEurUsdListing(t)
	var ids []runtimeorder.Request
	for range 2 {
		req := mustLimitRequestFor(t, deps.IDs, h.Reference().AccountID, eur, order.Buy, "5000", "1.10000")
		_, err := h.Submit(ctx, req)
		require.NoError(t, err)
		ids = append(ids, req)
	}
	require.NoError(t, b.Advance(ctx, mustObservation(t, eur, "1.09000", "1.09500", "1.08500", "1.09200", barTime)))

	var orders []runtimeorder.Order
	for _, req := range ids {
		orders = append(orders, h.state.orders[req.OrderID])
	}
	return h, orders
}

func TestFillMargin_TwoRestingLimitsTogetherExceed(t *testing.T) {
	h, orders := twoRestingLimitsScenario(t)

	var filled, canceled []runtimeorder.Order
	for _, o := range orders {
		switch o.Status {
		case runtimeorder.StatusFilled:
			filled = append(filled, o)
		case runtimeorder.StatusCanceled:
			canceled = append(canceled, o)
		}
	}
	require.Len(t, filled, 1, "the first to be processed fills")
	require.Len(t, canceled, 1, "the second would exceed the limit and is canceled by the broker")
	require.NotNil(t, canceled[0].CancelReason)
	assert.Equal(t, runtimeorder.ReasonInsufficientMargin, canceled[0].CancelReason.Reason)
	assert.Nil(t, canceled[0].Rejection, "a canceled order carries CancelReason, never Rejection")

	s := snapshot(t, h)
	require.Len(t, s.Positions(), 1)
	assert.True(t, s.Positions()[0].Quantity.Equal(num.MustParseQuantity("5000")))
	assert.Empty(t, s.OpenOrders())
}

func TestFillMargin_DeterministicEvents(t *testing.T) {
	h1, orders1 := twoRestingLimitsScenario(t)
	h2, orders2 := twoRestingLimitsScenario(t)
	assert.Equal(t, orderEvents(t, h1), orderEvents(t, h2))
	for i := range orders1 {
		assert.Equal(t, orders1[i].Request.OrderID, orders2[i].Request.OrderID)
		assert.Equal(t, orders1[i].Status, orders2[i].Status)
	}
	// Two accepts, then one fill's Filled, then the other's
	// PendingCancel and Canceled.
	assert.Equal(t, []runtimeorder.Status{
		runtimeorder.StatusWorking, runtimeorder.StatusWorking,
		runtimeorder.StatusFilled,
		runtimeorder.StatusPendingCancel, runtimeorder.StatusCanceled,
	}, orderEvents(t, h1))
}

func TestFillMargin_ReduceOnlyStopFillsOnOverLimitAccount(t *testing.T) {
	ctx := context.Background()
	b, deps, h, _ := fillMarginAccount(t, "0.5", nil)
	eur := mustEurUsdListing(t)
	// 15000 × 1.1 = 16500 gross, 8250 margin: within 2× of 10000.
	_, err := h.Submit(ctx, mustMarketRequestFor(t, deps.IDs, h.Reference().AccountID, eur, order.Buy, "15000"))
	require.NoError(t, err)
	stop := mustReduceOnlyStopRequestFor(t, deps.IDs, h.Reference().AccountID, eur, order.Sell, "15000", "0.70000")
	_, err = h.Submit(ctx, stop)
	require.NoError(t, err)

	// Drift to 0.75 leaves the account over the limit: equity 4750,
	// margin 5625. No stop trigger yet.
	require.NoError(t, b.Advance(ctx, mustObservation(t, eur, "0.75000", "0.75000", "0.75000", "0.75000", barTime)))
	s := snapshot(t, h)
	assert.True(t, s.MarginAvailable().Equal(usd("-875")), "over the limit: %s", s.MarginAvailable())

	// The protective stop still fills: de-risking is never blocked.
	require.NoError(t, b.Advance(ctx, mustObservation(t, eur, "0.72000", "0.72000", "0.68000", "0.69000", barTime)))
	assert.Equal(t, runtimeorder.StatusFilled, h.state.orders[stop.OrderID].Status)
	assert.Empty(t, snapshot(t, h).Positions())
}

func TestFillMargin_DeRiskingMarketOrderFillsOnOverLimitAccount(t *testing.T) {
	ctx := context.Background()
	b, deps, h, prices := fillMarginAccount(t, "0.5", nil)
	eur := mustEurUsdListing(t)
	_, err := h.Submit(ctx, mustMarketRequestFor(t, deps.IDs, h.Reference().AccountID, eur, order.Buy, "15000"))
	require.NoError(t, err)
	require.NoError(t, b.Advance(ctx, mustObservation(t, eur, "0.75000", "0.75000", "0.75000", "0.75000", barTime)))

	prices.set("EUR_USD", num.MustParsePrice("0.75000"))
	// Equity 4750; 15000 at 0.75 needs 5625. Selling 1000 leaves 14000
	// needing 5250: still over the limit, but a reduction fills.
	o, err := h.Submit(ctx, mustMarketRequestFor(t, deps.IDs, h.Reference().AccountID, eur, order.Sell, "1000"))
	require.NoError(t, err)
	assert.Equal(t, runtimeorder.StatusFilled, o.Status, "a reduction fills even though the account stays over the limit")
	assert.True(t, snapshot(t, h).MarginAvailable().Equal(usd("-500")))

	// But adding to it is refused.
	o, err = h.Submit(ctx, mustMarketRequestFor(t, deps.IDs, h.Reference().AccountID, eur, order.Buy, "1"))
	require.NoError(t, err)
	assert.Equal(t, runtimeorder.StatusRejected, o.Status)
}

func TestFillMargin_NoModelFillsOverTheLimit(t *testing.T) {
	_, deps, h := marginAccount(t, "")
	o, err := h.Submit(context.Background(), mustMarketRequestFor(t, deps.IDs, h.Reference().AccountID, mustEurUsdListing(t), order.Buy, "40000"))
	require.NoError(t, err)
	assert.Equal(t, runtimeorder.StatusFilled, o.Status, "legacy behavior without a margin model")
}

// TestFillMargin_RefusedMarketOrderConsumesOnlyItsRejectionEventID pins
// that a margin refusal inside Submit consumes exactly one ID — its own
// rejection event's — and no discarded accept-event ID, so it never
// shifts later IDs.
func TestFillMargin_RefusedMarketOrderConsumesOnlyItsRejectionEventID(t *testing.T) {
	ctx := context.Background()

	// Scenario 1: submit an over-limit market order.
	_, deps1, h1, _ := fillMarginAccount(t, "1", nil)
	req1 := mustMarketRequestFor(t, deps1.IDs, h1.Reference().AccountID, mustEurUsdListing(t), order.Buy, "20000")
	_, err := h1.Submit(ctx, req1)
	require.NoError(t, err)
	require.Len(t, h1.state.events, 1)
	next1, err := id.GenerateEventID(deps1.IDs)
	require.NoError(t, err)

	// Scenario 2: identical setup, drawing IDs directly instead.
	_, deps2, h2, _ := fillMarginAccount(t, "1", nil)
	_ = mustMarketRequestFor(t, deps2.IDs, h2.Reference().AccountID, mustEurUsdListing(t), order.Buy, "20000")
	first2, err := id.GenerateEventID(deps2.IDs)
	require.NoError(t, err)
	next2, err := id.GenerateEventID(deps2.IDs)
	require.NoError(t, err)

	assert.Equal(t, first2, h1.state.events[0].Metadata.EventID, "the rejection event takes the first ID Submit could consume")
	assert.Equal(t, next2, next1, "and nothing else was consumed")
}

// TestFillMargin_CurrentGrossUsesFillPriceAfterGap: the refusal detail
// values the changed listing at the fill price in both states
// (ADR-066), so a price gap is never attributed to the order.
func TestFillMargin_CurrentGrossUsesFillPriceAfterGap(t *testing.T) {
	_, deps, h, prices := fillMarginAccount(t, "1", nil)
	eur := mustEurUsdListing(t)
	_, err := h.Submit(context.Background(), mustMarketRequestFor(t, deps.IDs, h.Reference().AccountID, eur, order.Buy, "5000")) // 5500 at 1.10
	require.NoError(t, err)

	// Gap to 1.30 with no bar observed, so the stored mark is still
	// 1.10. Equity at the fill price: 10000 + 5000 × 0.20 = 11000.
	// Adding 4000: 9000 × 1.30 = 11700 > 11000.
	prices.set("EUR_USD", num.MustParsePrice("1.30000"))
	o, err := h.Submit(context.Background(), mustMarketRequestFor(t, deps.IDs, h.Reference().AccountID, eur, order.Buy, "4000"))
	require.NoError(t, err)
	require.Equal(t, runtimeorder.StatusRejected, o.Status)
	assert.Contains(t, o.Rejection.Detail, "gross notional 11700 USD, up from 6500 USD",
		"current gross is 5000 × 1.30 at the fill price, not 5000 × 1.10 at the stale mark")
}
