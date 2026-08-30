package backtest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// tradesFixture bundles the identifiers/generator every DeriveTrades
// test needs, so each test only states what actually varies.
type tradesFixture struct {
	ids       *id.Generator
	accountID id.AccountID
	orderID   id.OrderID
}

func newTradesFixture(t *testing.T, seed uint64) tradesFixture {
	t.Helper()
	ids := id.NewGenerator(clock.NewSimulated(time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)), id.NewDeterministic(seed, seed+1))
	accountID, err := id.GenerateAccountID(ids)
	require.NoError(t, err)
	orderID, err := id.GenerateOrderID(ids)
	require.NoError(t, err)
	return tradesFixture{ids: ids, accountID: accountID, orderID: orderID}
}

func (f tradesFixture) fill(t *testing.T, listing instrument.Listing, side order.Side, price, quantity string, commission string, ts time.Time) order.Fill {
	t.Helper()
	fillID, err := id.GenerateFillID(f.ids)
	require.NoError(t, err)

	params := order.Fill{
		FillID:    fillID,
		OrderID:   f.orderID,
		AccountID: f.accountID,
		Listing:   listing,
		Side:      side,
		Price:     num.MustParsePrice(price),
		Quantity:  num.MustParseQuantity(quantity),
		Timestamp: ts,
	}
	if commission != "" {
		c := num.MustParseMoney(commission, listing.Spec().SettlementCurrency())
		params.Commission = &c
	}
	fill, err := order.NewFill(params)
	require.NoError(t, err)
	return fill
}

func tradesUSD(s string) num.Money {
	return num.MustParseMoney(s, num.MustParseCurrency("USD"))
}

func TestDeriveTrades_SimpleOpenAndClose(t *testing.T) {
	f := newTradesFixture(t, 10)
	listing := simListing(t, "EUR", "USD", "EUR_USD")
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	open := f.fill(t, listing, order.Buy, "1.10000", "1000", "1.00", t0)
	closeFill := f.fill(t, listing, order.Sell, "1.12000", "1000", "1.00", t0.Add(time.Hour))

	set, err := backtest.DeriveTrades([]order.Fill{open, closeFill})
	require.NoError(t, err)
	require.Empty(t, set.Open)
	require.Len(t, set.Closed, 1)

	tr := set.Closed[0]
	assert.Equal(t, order.Long, tr.Side)
	assert.Equal(t, []id.FillID{open.FillID}, tr.EntryFillIDs)
	assert.Equal(t, []id.FillID{closeFill.FillID}, tr.ExitFillIDs)
	assert.Equal(t, t0, tr.OpenedAt)
	assert.Equal(t, t0.Add(time.Hour), tr.ClosedAt)
	assert.True(t, tr.RealizedPnL.Equal(tradesUSD("20")), "(1.12000-1.10000)*1000 = 20.00 USD profit")
	assert.True(t, tr.Costs.Equal(tradesUSD("2.00")), "1.00 entry + 1.00 exit commission")
}

func TestDeriveTrades_PartialReduceThenClose(t *testing.T) {
	f := newTradesFixture(t, 20)
	listing := simListing(t, "EUR", "USD", "EUR_USD")
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	open := f.fill(t, listing, order.Buy, "1.10000", "1000", "", t0)
	reduce := f.fill(t, listing, order.Sell, "1.11000", "400", "", t0.Add(time.Hour))
	closeRest := f.fill(t, listing, order.Sell, "1.12000", "600", "", t0.Add(2*time.Hour))

	set, err := backtest.DeriveTrades([]order.Fill{open, reduce, closeRest})
	require.NoError(t, err)
	require.Empty(t, set.Open)
	require.Len(t, set.Closed, 1)

	tr := set.Closed[0]
	assert.Equal(t, []id.FillID{open.FillID}, tr.EntryFillIDs)
	assert.Equal(t, []id.FillID{reduce.FillID, closeRest.FillID}, tr.ExitFillIDs)
	assert.Equal(t, t0.Add(2*time.Hour), tr.ClosedAt)
	// (1.11000-1.10000)*400 + (1.12000-1.10000)*600 = 4 + 12 = 16
	assert.True(t, tr.RealizedPnL.Equal(tradesUSD("16")))
	assert.True(t, tr.Costs.IsZero())
}

func TestDeriveTrades_MultiLotAveragingEntry(t *testing.T) {
	f := newTradesFixture(t, 30)
	listing := simListing(t, "EUR", "USD", "EUR_USD")
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	first := f.fill(t, listing, order.Buy, "1.10000", "1000", "", t0)
	second := f.fill(t, listing, order.Buy, "1.09500", "500", "", t0.Add(time.Hour))
	closeAll := f.fill(t, listing, order.Sell, "1.10500", "1500", "", t0.Add(2*time.Hour))

	set, err := backtest.DeriveTrades([]order.Fill{first, second, closeAll})
	require.NoError(t, err)
	require.Len(t, set.Closed, 1)

	tr := set.Closed[0]
	assert.Equal(t, []id.FillID{first.FillID, second.FillID}, tr.EntryFillIDs)
	// weighted avg entry = (1.10000*1000 + 1.09500*500)/1500 = 1.09833333
	// pnl = (1.10500 - 1.09833333) * 1500 = 10.000005 ~ rounds per num's scale
	cmp, err := tr.RealizedPnL.Cmp(tradesUSD("0"))
	require.NoError(t, err)
	assert.True(t, cmp > 0, "closing above the weighted-average entry must be profitable")
}

func TestDeriveTrades_Reversal(t *testing.T) {
	f := newTradesFixture(t, 40)
	listing := simListing(t, "EUR", "USD", "EUR_USD")
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	open := f.fill(t, listing, order.Buy, "1.10000", "1000", "", t0)
	reverse := f.fill(t, listing, order.Sell, "1.11000", "1500", "3.00", t0.Add(time.Hour))

	set, err := backtest.DeriveTrades([]order.Fill{open, reverse})
	require.NoError(t, err)
	require.Len(t, set.Closed, 1)
	require.Len(t, set.Open, 1)

	closedTrade := set.Closed[0]
	openTrade := set.Open[0]

	assert.Equal(t, order.Long, closedTrade.Side)
	assert.Equal(t, []id.FillID{open.FillID}, closedTrade.EntryFillIDs)
	assert.Equal(t, []id.FillID{reverse.FillID}, closedTrade.ExitFillIDs, "the reversal fill closes the old trade")
	assert.Equal(t, t0.Add(time.Hour), closedTrade.ClosedAt)
	assert.True(t, closedTrade.RealizedPnL.Equal(tradesUSD("10")), "(1.11000-1.10000)*1000 = 10.00 USD profit on the closed portion")

	assert.Equal(t, order.Short, openTrade.Side)
	assert.Equal(t, []id.FillID{reverse.FillID}, openTrade.EntryFillIDs, "the same reversal fill also opens the new trade")
	assert.True(t, openTrade.ClosedAt.IsZero())
	assert.Equal(t, t0.Add(time.Hour), openTrade.OpenedAt)
	assert.True(t, openTrade.RealizedPnL.IsZero())

	// Commission (3.00) splits pro-rata by quantity: 1000/1500 closed,
	// 500/1500 opened.
	total, err := closedTrade.Costs.Add(openTrade.Costs)
	require.NoError(t, err)
	assert.True(t, total.Equal(tradesUSD("3.00")), "the two shares must sum back to the original commission exactly")
	cmp, err := closedTrade.Costs.Cmp(openTrade.Costs)
	require.NoError(t, err)
	assert.True(t, cmp > 0, "the closed 1000 units carry a larger share than the opened 500")
}

func TestDeriveTrades_MultiInstrumentInterleaving(t *testing.T) {
	f := newTradesFixture(t, 50)
	eurusd := simListing(t, "EUR", "USD", "EUR_USD")
	gbpusd := simListing(t, "GBP", "USD", "GBP_USD")
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	eOpen := f.fill(t, eurusd, order.Buy, "1.10000", "1000", "", t0)
	gOpen := f.fill(t, gbpusd, order.Sell, "1.30000", "1000", "", t0)
	eClose := f.fill(t, eurusd, order.Sell, "1.11000", "1000", "", t0.Add(time.Hour))
	gClose := f.fill(t, gbpusd, order.Buy, "1.29000", "1000", "", t0.Add(time.Hour))

	set, err := backtest.DeriveTrades([]order.Fill{eOpen, gOpen, eClose, gClose})
	require.NoError(t, err)
	require.Empty(t, set.Open)
	require.Len(t, set.Closed, 2)

	byListing := map[string]order.Trade{}
	for _, tr := range set.Closed {
		byListing[tr.Listing.Symbol()] = tr
	}
	require.Contains(t, byListing, "EUR_USD")
	require.Contains(t, byListing, "GBP_USD")
	assert.True(t, byListing["EUR_USD"].RealizedPnL.Equal(tradesUSD("10")))
	assert.True(t, byListing["GBP_USD"].RealizedPnL.Equal(tradesUSD("10")), "short GBP/USD profits as price falls")
}

func TestDeriveTrades_StillOpenAtEndOfStream(t *testing.T) {
	f := newTradesFixture(t, 60)
	listing := simListing(t, "EUR", "USD", "EUR_USD")
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	open := f.fill(t, listing, order.Buy, "1.10000", "1000", "", t0)

	set, err := backtest.DeriveTrades([]order.Fill{open})
	require.NoError(t, err)
	require.Empty(t, set.Closed)
	require.Len(t, set.Open, 1)
	assert.True(t, set.Open[0].ClosedAt.IsZero())
	assert.True(t, set.Open[0].RealizedPnL.IsZero())
}

func TestDeriveTrades_RejectsMixedAccounts(t *testing.T) {
	f1 := newTradesFixture(t, 70)
	f2 := newTradesFixture(t, 71)
	listing := simListing(t, "EUR", "USD", "EUR_USD")
	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	a := f1.fill(t, listing, order.Buy, "1.10000", "1000", "", t0)
	b := f2.fill(t, listing, order.Buy, "1.10000", "1000", "", t0)

	_, err := backtest.DeriveTrades([]order.Fill{a, b})
	require.ErrorIs(t, err, backtest.ErrMixedAccountFills)
}
