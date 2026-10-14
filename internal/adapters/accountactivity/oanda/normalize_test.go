package oanda

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

var (
	usd    = num.MustParseCurrency("USD")
	stream = accountactivity.SyncKey{Provider: Provider, Environment: accountactivity.EnvironmentPractice, ProviderAccountID: testAccount}
)

func norm(t *testing.T, f *fakeOANDA, id int) accountactivity.Activity {
	t.Helper()
	a, err := normalize(rawTx(t, f, id), stream, usd)
	require.NoError(t, err)
	a.ImportedAt = time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC) // the sync loop stamps this
	require.NoError(t, a.Validate(), "transaction %d must normalize to a valid activity", id)
	return a
}

func money(t *testing.T, v accountactivity.Value[num.Money]) string {
	t.Helper()
	m, ok := v.Get()
	require.True(t, ok, "value is unknown")
	return m.String()
}

func TestNormalize_OrderFillPreservesIdentityTimeAndCosts(t *testing.T) {
	f := loadFixture(t)
	a := norm(t, f, 3)

	assert.Equal(t, accountactivity.KindFill, a.Kind)
	assert.Equal(t, "ORDER_FILL", a.ProviderType)
	assert.Equal(t, "3", a.ProviderActivityID)
	assert.Equal(t, "2", a.BrokerOrderID)
	assert.Equal(t, time.Date(2024, 3, 5, 14, 30, 0, 200_000_000, time.UTC), a.OccurredAt, "the broker's own timestamp, with sub-second precision")
	assert.Equal(t, "fx:EUR/USD", a.Instrument.String())
	assert.Equal(t, order.Buy, a.Side)

	q, _ := a.Quantity.Get()
	p, _ := a.Price.Get()
	assert.Equal(t, "1000", q.String())
	assert.Equal(t, "1.08532", p.String())
	assert.Equal(t, accountactivity.BasisReported, a.Price.Basis())
	assert.Equal(t, "price", a.Price.Source())

	// OANDA reports halfSpreadCost as a positive cost; Trader holds costs
	// as negative account-centric money, still broker-reported.
	assert.Equal(t, "-0.25 USD", money(t, a.SpreadCost))
	assert.Equal(t, accountactivity.BasisReported, a.SpreadCost.Basis())
	assert.Equal(t, "halfSpreadCost", a.SpreadCost.Source())

	// Reported zeros stay reported zeros, not unknown.
	for name, v := range map[string]accountactivity.Value[num.Money]{"pl": a.RealizedPL, "financing": a.Financing, "commission": a.Commission, "fee": a.OtherFees} {
		got, ok := v.Get()
		require.True(t, ok, name)
		assert.True(t, got.IsZero(), name)
	}
	assert.Equal(t, []accountactivity.TradeLink{{BrokerTradeID: "3", Role: accountactivity.TradeRoleOpened}}, a.TradeLinks)
}

func TestNormalize_SellFillWithMixedCostSignsAndTradeLinks(t *testing.T) {
	a := norm(t, loadFixture(t), 7)
	assert.Equal(t, order.Sell, a.Side, "negative units sell")
	q, _ := a.Quantity.Get()
	assert.Equal(t, "1500", q.String(), "quantity is the absolute size")

	assert.Equal(t, "5.8 USD", money(t, a.RealizedPL), "pl is already signed")
	assert.Equal(t, "-0.1 USD", money(t, a.Financing), "financing is already signed")
	assert.Equal(t, "-0.05 USD", money(t, a.Commission), "a positive commission is a cost")
	assert.Equal(t, "-0.3 USD", money(t, a.SpreadCost))
	assert.False(t, a.OtherFees.Known(), "an absent field is unknown, not zero")

	// A single fill can close one trade and open another.
	assert.Equal(t, []accountactivity.TradeLink{
		{BrokerTradeID: "7", Role: accountactivity.TradeRoleOpened},
		{BrokerTradeID: "3", Role: accountactivity.TradeRoleClosed},
	}, a.TradeLinks)
}

func TestNormalize_ReducedAndClosedTradesAndMissingFieldsStayUnknown(t *testing.T) {
	a := norm(t, loadFixture(t), 12)
	assert.Equal(t, []accountactivity.TradeLink{
		{BrokerTradeID: "7", Role: accountactivity.TradeRoleReduced},
		{BrokerTradeID: "8888", Role: accountactivity.TradeRoleClosed},
	}, a.TradeLinks)
	assert.Equal(t, "1.2 USD", money(t, a.RealizedPL))
	for name, v := range map[string]accountactivity.Value[num.Money]{"financing": a.Financing, "commission": a.Commission, "spread": a.SpreadCost} {
		assert.False(t, v.Known(), name)
	}
}

func TestNormalize_AccountLevelEvents(t *testing.T) {
	f := loadFixture(t)

	dep := norm(t, f, 1)
	assert.Equal(t, accountactivity.KindDeposit, dep.Kind)
	assert.Equal(t, "10000 USD", money(t, dep.Cash))

	wd := norm(t, f, 8)
	assert.Equal(t, accountactivity.KindWithdrawal, wd.Kind)
	assert.Equal(t, "-500 USD", money(t, wd.Cash))

	fin := norm(t, f, 5)
	assert.Equal(t, accountactivity.KindFinancing, fin.Kind)
	assert.Equal(t, "-1.37 USD", money(t, fin.Financing))

	div := norm(t, f, 11)
	assert.Equal(t, accountactivity.KindDividend, div.Kind)
	assert.Equal(t, "2.3 USD", money(t, div.Cash))
}

// Nothing is dropped: transactions Trader does not model keep their identity.
func TestNormalize_UnmodelledTransactionsAreKeptAsOther(t *testing.T) {
	f := loadFixture(t)

	cfg := norm(t, f, 4)
	assert.Equal(t, accountactivity.KindOther, cfg.Kind)
	assert.Equal(t, "CLIENT_CONFIGURE", cfg.ProviderType)
	assert.Equal(t, "4", cfg.ProviderActivityID)

	order := norm(t, f, 2)
	assert.Equal(t, accountactivity.KindOther, order.Kind)
	assert.Equal(t, "MARKET_ORDER", order.ProviderType)
	assert.Equal(t, "2", order.BrokerOrderID, "an order transaction's own ID is the order ID")
	assert.Equal(t, "fx:EUR/USD", order.Instrument.String())

	// A fill in an instrument Trader cannot identify is kept, with no economics claimed.
	cfd := norm(t, f, 10)
	assert.Equal(t, accountactivity.KindOther, cfd.Kind)
	assert.Equal(t, "ORDER_FILL", cfd.ProviderType)
	assert.Equal(t, "9", cfd.BrokerOrderID)
	assert.False(t, cfd.Price.Known())
	assert.False(t, cfd.RealizedPL.Known())
}

func TestNormalize_RecognizedButUnparseableIsAnError(t *testing.T) {
	cases := map[string]string{
		"units not a number":   `{"id":"3","type":"ORDER_FILL","time":"2024-03-05T14:30:00Z","instrument":"EUR_USD","units":"abc","price":"1.1"}`,
		"zero units":           `{"id":"3","type":"ORDER_FILL","time":"2024-03-05T14:30:00Z","instrument":"EUR_USD","units":"0","price":"1.1"}`,
		"price missing":        `{"id":"3","type":"ORDER_FILL","time":"2024-03-05T14:30:00Z","instrument":"EUR_USD","units":"1"}`,
		"money not a number":   `{"id":"3","type":"ORDER_FILL","time":"2024-03-05T14:30:00Z","instrument":"EUR_USD","units":"1","price":"1.1","pl":"x"}`,
		"too much precision":   `{"id":"3","type":"ORDER_FILL","time":"2024-03-05T14:30:00Z","instrument":"EUR_USD","units":"1","price":"1.123456789"}`,
		"bad time":             `{"id":"3","type":"ORDER_FILL","time":"1709649000.5","instrument":"EUR_USD","units":"1","price":"1.1"}`,
		"financing missing":    `{"id":"5","type":"DAILY_FINANCING","time":"2024-03-05T22:00:00Z"}`,
		"transfer missing":     `{"id":"1","type":"TRANSFER_FUNDS","time":"2024-03-05T22:00:00Z"}`,
		"dividend missing":     `{"id":"1","type":"DIVIDEND_ADJUSTMENT","time":"2024-03-05T22:00:00Z"}`,
		"no id":                `{"type":"CLIENT_CONFIGURE","time":"2024-03-05T22:00:00Z"}`,
		"duplicate trade link": `{"id":"3","type":"ORDER_FILL","time":"2024-03-05T14:30:00Z","instrument":"EUR_USD","units":"1","price":"1.1","tradesClosed":[{"tradeID":"1"},{"tradeID":"1"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			a, err := normalize(json.RawMessage(body), stream, usd)
			if err == nil {
				a.ImportedAt = time.Now()
				err = a.Validate()
			}
			assert.Error(t, err)
		})
	}
}

func TestNormalize_ZeroTransferIsOther(t *testing.T) {
	a, err := normalize(json.RawMessage(`{"id":"1","type":"TRANSFER_FUNDS","time":"2024-03-05T22:00:00Z","amount":"0.0000"}`), stream, usd)
	require.NoError(t, err)
	assert.Equal(t, accountactivity.KindOther, a.Kind)
}

// No amount goes through binary floating point: a value float64 cannot
// represent exactly survives intact.
func TestNormalize_ExactDecimalsSurvive(t *testing.T) {
	a, err := normalize(json.RawMessage(`{"id":"3","type":"ORDER_FILL","time":"2024-03-05T14:30:00Z","instrument":"EUR_USD","units":"92233720368","price":"0.12345678","pl":"92233720368.54775807"}`), stream, usd)
	require.NoError(t, err)
	p, _ := a.Price.Get()
	assert.Equal(t, "0.12345678", p.String())
	assert.Equal(t, "92233720368.54775807 USD", money(t, a.RealizedPL))
}
