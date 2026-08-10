package account

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validParams(t *testing.T) SnapshotParams {
	t.Helper()
	accountID := mustAccountID(t)
	listing := mustEurUsdListing(t)
	return SnapshotParams{
		AccountID:       accountID,
		Broker:          "OANDA",
		Currency:        num.MustParseCurrency("USD"),
		AsOf:            time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC),
		Cursor:          "cursor-1",
		CashBalances:    []num.Money{usd("10000"), eur("500")},
		Equity:          usd("10500"),
		BuyingPower:     usd("9000"),
		MarginUsed:      usd("1500"),
		MarginAvailable: usd("8500"),
		RealizedPnL:     usd("250"),
		UnrealizedPnL:   usd("-50"),
		Fees:            usd("12.34"),
		Financing:       usd("1.10"),
		Positions:       []order.Position{mustPosition(t, accountID, listing)},
		OpenOrders:      []order.Order{mustWorkingOrder(t, accountID, listing)},
	}
}

func TestNewSnapshotValid(t *testing.T) {
	p := validParams(t)
	s, err := NewSnapshot(p)
	require.NoError(t, err)

	assert.Equal(t, p.AccountID, s.AccountID())
	assert.Equal(t, "OANDA", s.Broker())
	assert.Equal(t, "USD", s.Currency().String())
	assert.True(t, p.AsOf.Equal(s.AsOf()))
	assert.Equal(t, "cursor-1", s.Cursor())
	assert.Equal(t, p.Equity, s.Equity())
	assert.Equal(t, p.BuyingPower, s.BuyingPower())
	assert.Equal(t, p.MarginUsed, s.MarginUsed())
	assert.Equal(t, p.MarginAvailable, s.MarginAvailable())
	assert.Equal(t, p.RealizedPnL, s.RealizedPnL())
	assert.Equal(t, p.UnrealizedPnL, s.UnrealizedPnL())
	assert.Equal(t, p.Fees, s.Fees())
	assert.Equal(t, p.Financing, s.Financing())
	assert.Len(t, s.CashBalances(), 2)
	assert.Len(t, s.Positions(), 1)
	assert.Len(t, s.OpenOrders(), 1)
}

// TestSnapshotClonesEveryOrderPointerField exercises cloneOrder against
// an order carrying every pointer/slice field it must independently
// clone (Request.LimitPrice/StopPrice, AcceptedLimitPrice/
// AcceptedStopPrice, AppliedFillIDs, AppliedBrokerFillIDs), by mutating
// the returned copy through each pointer and slice and confirming a
// fresh accessor call is unaffected.
func TestSnapshotClonesEveryOrderPointerField(t *testing.T) {
	p := validParams(t)
	listing := mustEurUsdListing(t)
	o := mustPartiallyFilledLimitOrder(t, p.AccountID, listing)
	p.OpenOrders = []order.Order{o}
	// A partial fill leaves the order carrying an accepted quantity
	// larger than its filled quantity, so it remains open/non-terminal.
	p.Positions = []order.Position{mustPosition(t, p.AccountID, listing)}

	s, err := NewSnapshot(p)
	require.NoError(t, err)

	got := s.OpenOrders()
	require.Len(t, got, 1)
	require.NotNil(t, got[0].Request.LimitPrice)
	require.NotNil(t, got[0].Request.StopPrice)
	require.NotNil(t, got[0].AcceptedLimitPrice)
	require.NotNil(t, got[0].AcceptedStopPrice)
	require.NotEmpty(t, got[0].AppliedFillIDs)
	require.NotEmpty(t, got[0].AppliedBrokerFillIDs)

	origLimit := *got[0].Request.LimitPrice
	origStop := *got[0].Request.StopPrice
	origAcceptedLimit := *got[0].AcceptedLimitPrice
	origAcceptedStop := *got[0].AcceptedStopPrice
	origFillIDCount := len(got[0].AppliedFillIDs)
	origBrokerFillIDCount := len(got[0].AppliedBrokerFillIDs)

	mutated := num.MustParsePrice("0.00001")
	*got[0].Request.LimitPrice = mutated
	*got[0].Request.StopPrice = mutated
	*got[0].AcceptedLimitPrice = mutated
	*got[0].AcceptedStopPrice = mutated
	got[0].AppliedFillIDs = append(got[0].AppliedFillIDs, id.FillID{})
	got[0].AppliedBrokerFillIDs = append(got[0].AppliedBrokerFillIDs, "tampered")

	again := s.OpenOrders()
	require.Len(t, again, 1)
	assert.Equal(t, origLimit, *again[0].Request.LimitPrice)
	assert.Equal(t, origStop, *again[0].Request.StopPrice)
	assert.Equal(t, origAcceptedLimit, *again[0].AcceptedLimitPrice)
	assert.Equal(t, origAcceptedStop, *again[0].AcceptedStopPrice)
	assert.Len(t, again[0].AppliedFillIDs, origFillIDCount)
	assert.Len(t, again[0].AppliedBrokerFillIDs, origBrokerFillIDCount)
}

func TestNewSnapshotRejectsZeroAccountID(t *testing.T) {
	p := validParams(t)
	p.AccountID = id.AccountID{}
	_, err := NewSnapshot(p)
	require.ErrorIs(t, err, ErrInvalidSnapshot)
}

func TestNewSnapshotRejectsMissingBroker(t *testing.T) {
	p := validParams(t)
	p.Broker = ""
	_, err := NewSnapshot(p)
	require.ErrorIs(t, err, ErrInvalidSnapshot)
}

func TestNewSnapshotRejectsInvalidCurrency(t *testing.T) {
	p := validParams(t)
	p.Currency = num.Currency{}
	_, err := NewSnapshot(p)
	require.ErrorIs(t, err, ErrInvalidSnapshot)
}

func TestNewSnapshotRejectsZeroAsOf(t *testing.T) {
	p := validParams(t)
	p.AsOf = time.Time{}
	_, err := NewSnapshot(p)
	require.ErrorIs(t, err, ErrInvalidSnapshot)
}

func TestNewSnapshotRejectsDuplicateCashBalanceCurrency(t *testing.T) {
	p := validParams(t)
	p.CashBalances = []num.Money{usd("100"), usd("200")}
	_, err := NewSnapshot(p)
	require.ErrorIs(t, err, ErrInvalidSnapshot)
}

func TestNewSnapshotRejectsInvalidCashBalance(t *testing.T) {
	p := validParams(t)
	p.CashBalances = []num.Money{{}}
	_, err := NewSnapshot(p)
	require.ErrorIs(t, err, ErrInvalidSnapshot)
}

func TestNewSnapshotRejectsHomeCurrencyMismatch(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*SnapshotParams)
	}{
		{"equity", func(p *SnapshotParams) { p.Equity = eur("1") }},
		{"buying power", func(p *SnapshotParams) { p.BuyingPower = eur("1") }},
		{"margin used", func(p *SnapshotParams) { p.MarginUsed = eur("1") }},
		{"margin available", func(p *SnapshotParams) { p.MarginAvailable = eur("1") }},
		{"realized pnl", func(p *SnapshotParams) { p.RealizedPnL = eur("1") }},
		{"unrealized pnl", func(p *SnapshotParams) { p.UnrealizedPnL = eur("1") }},
		{"fees", func(p *SnapshotParams) { p.Fees = eur("1") }},
		{"financing", func(p *SnapshotParams) { p.Financing = eur("1") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validParams(t)
			tt.apply(&p)
			_, err := NewSnapshot(p)
			require.ErrorIs(t, err, ErrInvalidSnapshot)
		})
	}
}

func TestNewSnapshotRejectsInvalidHomeCurrencyField(t *testing.T) {
	p := validParams(t)
	p.Equity = num.Money{}
	_, err := NewSnapshot(p)
	require.ErrorIs(t, err, ErrInvalidSnapshot)
}

func TestNewSnapshotRejectsPositionAccountMismatch(t *testing.T) {
	p := validParams(t)
	other := mustAccountID(t)
	p.Positions = []order.Position{mustPosition(t, other, mustEurUsdListing(t))}
	_, err := NewSnapshot(p)
	require.ErrorIs(t, err, ErrInvalidSnapshot)
}

func TestNewSnapshotRejectsInvalidPosition(t *testing.T) {
	p := validParams(t)
	p.Positions = []order.Position{{AccountID: p.AccountID}}
	_, err := NewSnapshot(p)
	require.ErrorIs(t, err, ErrInvalidSnapshot)
}

func TestNewSnapshotRejectsDuplicateListingPosition(t *testing.T) {
	p := validParams(t)
	listing := mustEurUsdListing(t)
	p.Positions = []order.Position{
		mustPosition(t, p.AccountID, listing),
		mustPosition(t, p.AccountID, listing),
	}
	_, err := NewSnapshot(p)
	require.ErrorIs(t, err, ErrInvalidSnapshot)
}

func TestNewSnapshotAllowsMultipleDistinctPositions(t *testing.T) {
	p := validParams(t)
	listing := mustEurUsdListing(t)
	other := mustGbpUsdListing(t)
	p.Positions = []order.Position{
		mustPosition(t, p.AccountID, listing),
		mustPosition(t, p.AccountID, other),
	}
	s, err := NewSnapshot(p)
	require.NoError(t, err)
	assert.Len(t, s.Positions(), 2)
}

func TestNewSnapshotRejectsOpenOrderAccountMismatch(t *testing.T) {
	p := validParams(t)
	other := mustAccountID(t)
	p.OpenOrders = []order.Order{mustWorkingOrder(t, other, mustEurUsdListing(t))}
	_, err := NewSnapshot(p)
	require.ErrorIs(t, err, ErrInvalidSnapshot)
}

func TestNewSnapshotRejectsInvalidOpenOrder(t *testing.T) {
	p := validParams(t)
	p.OpenOrders = []order.Order{{}}
	_, err := NewSnapshot(p)
	require.ErrorIs(t, err, ErrInvalidSnapshot)
}

func TestNewSnapshotRejectsTerminalOpenOrder(t *testing.T) {
	p := validParams(t)
	filled := mustFilledOrder(t, p.AccountID, mustEurUsdListing(t))
	p.OpenOrders = []order.Order{filled}
	_, err := NewSnapshot(p)
	require.ErrorIs(t, err, ErrInvalidSnapshot)
}

func TestNewSnapshotRejectsDuplicateOrderID(t *testing.T) {
	p := validParams(t)
	o := mustWorkingOrder(t, p.AccountID, mustEurUsdListing(t))
	p.OpenOrders = []order.Order{o, o}
	_, err := NewSnapshot(p)
	require.ErrorIs(t, err, ErrInvalidSnapshot)
}

// TestCloneOrderClonesRejectionAndAvgFillPrice exercises cloneOrder's
// Rejection and AvgFillPrice branches directly. Neither field can occur
// on a Snapshot's OpenOrders in practice — Rejection only coexists with
// a Terminal Status (order.NewOrder), which checkOpenOrders excludes,
// and order/transition.go's ApplyFill always clears AvgFillPrice — but
// cloneOrder is a general Order-copying helper and must not silently
// alias either pointer if some future caller populates them.
func TestCloneOrderClonesRejectionAndAvgFillPrice(t *testing.T) {
	avgFillPrice := num.MustParsePrice("1.10000")
	o := order.Order{
		AvgFillPrice: &avgFillPrice,
		Rejection: &order.Rejection{
			Reason:     order.ReasonInsufficientMargin,
			BrokerCode: "INSUFFICIENT_MARGIN",
		},
	}

	cloned := cloneOrder(o)
	require.NotNil(t, cloned.AvgFillPrice)
	require.NotNil(t, cloned.Rejection)
	assert.Equal(t, *o.AvgFillPrice, *cloned.AvgFillPrice)
	assert.Equal(t, *o.Rejection, *cloned.Rejection)

	*cloned.AvgFillPrice = num.MustParsePrice("999")
	cloned.Rejection.Detail = "tampered"

	assert.Equal(t, avgFillPrice, *o.AvgFillPrice)
	assert.Equal(t, "", o.Rejection.Detail)
}

func TestSnapshotCashBalancesIsDefensiveCopy(t *testing.T) {
	s, err := NewSnapshot(validParams(t))
	require.NoError(t, err)

	got := s.CashBalances()
	got[0] = num.Money{}

	again := s.CashBalances()
	assert.NotEqual(t, num.Money{}, again[0])
}

func TestSnapshotPositionsIsDeepDefensiveCopy(t *testing.T) {
	s, err := NewSnapshot(validParams(t))
	require.NoError(t, err)

	got := s.Positions()
	require.Len(t, got, 1)
	original := *got[0].AvgPrice

	// Mutate the pointed-to value in place, not merely the local
	// pointer, and mutate a plain field for good measure.
	*got[0].AvgPrice = num.MustParsePrice("999")
	got[0].Side = order.Short

	again := s.Positions()
	require.Len(t, again, 1)
	assert.Equal(t, order.Long, again[0].Side)
	assert.Equal(t, original, *again[0].AvgPrice)
}

func TestSnapshotOpenOrdersIsDeepDefensiveCopy(t *testing.T) {
	s, err := NewSnapshot(validParams(t))
	require.NoError(t, err)

	got := s.OpenOrders()
	require.Len(t, got, 1)
	originalAccepted := *got[0].AcceptedQuantity

	mutatedQty := num.MustParseQuantity("1")
	*got[0].AcceptedQuantity = mutatedQty
	got[0].AppliedFillIDs = append(got[0].AppliedFillIDs, id.FillID{})
	got[0].BrokerOrderID = "tampered"

	again := s.OpenOrders()
	require.Len(t, again, 1)
	assert.Equal(t, originalAccepted, *again[0].AcceptedQuantity)
	assert.Empty(t, again[0].AppliedFillIDs)
	assert.Equal(t, "broker-order-1", again[0].BrokerOrderID)
}
