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
	assert.Equal(t, "2", a.BrokerBatchID, "the broker's batch grouping is kept")
	assert.Equal(t, "EUR_USD", a.ProviderInstrument)
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
	assert.Equal(t, "price (deprecated aggregate)", a.Price.Source(), "the top-level price is recorded as non-authoritative")

	// halfSpreadCost keeps the sign OANDA reports (it can be either).
	assert.Equal(t, "0.25 USD", money(t, a.SpreadCost))
	assert.Equal(t, accountactivity.BasisReported, a.SpreadCost.Basis())
	assert.Equal(t, "halfSpreadCost", a.SpreadCost.Source())

	// Reported zeros stay reported zeros, not unknown.
	for name, v := range map[string]accountactivity.Value[num.Money]{"pl": a.RealizedPL, "financing": a.Financing, "commission": a.Commission, "fee": a.OtherFees} {
		got, ok := v.Get()
		require.True(t, ok, name)
		assert.True(t, got.IsZero(), name)
	}
	assert.Equal(t, []string{"3:opened"}, effectKeys(a))
}

func TestNormalize_SellFillWithMixedCostSignsAndTradeEffects(t *testing.T) {
	a := norm(t, loadFixture(t), 7)
	assert.Equal(t, order.Sell, a.Side, "negative units sell")
	q, _ := a.Quantity.Get()
	assert.Equal(t, "1500", q.String(), "quantity is the absolute size")

	assert.Equal(t, "5.8 USD", money(t, a.RealizedPL), "pl is already signed")
	assert.Equal(t, "-0.1 USD", money(t, a.Financing), "financing is already signed")
	assert.Equal(t, "-0.05 USD", money(t, a.Commission), "a positive commission is a cost")
	assert.Equal(t, "0.3 USD", money(t, a.SpreadCost), "halfSpreadCost is not negated")
	assert.False(t, a.OtherFees.Known(), "an absent field is unknown, not zero")

	// A single fill can close one trade and open another.
	assert.Equal(t, []string{"7:opened", "3:closed"}, effectKeys(a))
}

func TestNormalize_ReduceAndMultiTradeCloseWithMissingFieldsStayUnknown(t *testing.T) {
	a := norm(t, loadFixture(t), 12)
	assert.Equal(t, []string{"7:reduced", "8888:closed", "9999:closed"}, effectKeys(a))
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

// halfSpreadCost is signed by OANDA, either way; it is never forced to a sign.
func TestNormalize_HalfSpreadCostKeepsTheProviderSign(t *testing.T) {
	for in, want := range map[string]string{"0.2500": "0.25 USD", "-0.2500": "-0.25 USD", "0.0000": "0 USD"} {
		a, err := normalize(json.RawMessage(`{"id":"3","type":"ORDER_FILL","time":"2024-03-05T14:30:00Z","instrument":"EUR_USD","units":"1","price":"1.1","halfSpreadCost":"`+in+`"}`), stream, usd)
		require.NoError(t, err, in)
		assert.Equal(t, want, money(t, a.SpreadCost), in)
		assert.Equal(t, "halfSpreadCost", a.SpreadCost.Source())
	}
	// Commission, by contrast, is documented as a positive cost and is negated.
	a, err := normalize(json.RawMessage(`{"id":"3","type":"ORDER_FILL","time":"2024-03-05T14:30:00Z","instrument":"EUR_USD","units":"1","price":"1.1","commission":"0.0500"}`), stream, usd)
	require.NoError(t, err)
	assert.Equal(t, "-0.05 USD", money(t, a.Commission))
}

// Batch identity is kept for every transaction type that carries it, and
// transactions created together share it.
func TestNormalize_BatchIDsAreKeptAndShared(t *testing.T) {
	f := loadFixture(t)
	order, fill := norm(t, f, 2), norm(t, f, 3)
	assert.Equal(t, "2", order.BrokerBatchID)
	assert.Equal(t, order.BrokerBatchID, fill.BrokerBatchID, "an order and its fill share a batch")
	assert.Equal(t, "6", norm(t, f, 6).BrokerBatchID)
	assert.Equal(t, "6", norm(t, f, 7).BrokerBatchID)
	assert.Empty(t, norm(t, f, 4).BrokerBatchID, "absent stays absent")
}

// Only real fiat pairs are forex. A metal, commodity or index CFD must not
// become an fx: instrument just because its code looks like a currency; the
// raw name is kept either way.
func TestNormalize_OnlyFiatPairsAreForexAndTheRawInstrumentIsAlwaysKept(t *testing.T) {
	fill := func(inst string) string {
		return `{"id":"3","type":"ORDER_FILL","time":"2024-03-05T14:30:00Z","orderID":"2","instrument":"` + inst + `","units":"1","price":"1.1","pl":"2.0000"}`
	}
	for inst, wantID := range map[string]string{"EUR_USD": "fx:EUR/USD", "USD_CNH": "fx:USD/CNH", "USD_ZAR": "fx:USD/ZAR", "GBP_JPY": "fx:GBP/JPY"} {
		a, err := normalize(json.RawMessage(fill(inst)), stream, usd)
		require.NoError(t, err, inst)
		assert.Equal(t, accountactivity.KindFill, a.Kind, inst)
		assert.Equal(t, wantID, a.Instrument.String(), inst)
		assert.Equal(t, inst, a.ProviderInstrument)
	}
	for _, inst := range []string{"XAU_USD", "XAG_EUR", "WTICO_USD", "BCO_USD", "SPX500_USD", "DE30_EUR", "USB10Y_USD", "XYZ_USD", "EURUSD"} {
		a, err := normalize(json.RawMessage(fill(inst)), stream, usd)
		require.NoError(t, err, inst)
		assert.Equal(t, accountactivity.KindOther, a.Kind, inst)
		assert.True(t, a.Instrument.IsZero(), "%s must not get an fx: identity", inst)
		assert.Equal(t, inst, a.ProviderInstrument, "the provider-native name survives for later re-normalization")
		assert.Equal(t, "ORDER_FILL", a.ProviderType)
		assert.False(t, a.Price.Known())
	}
}

// effectKeys lists a fill's trade effects as "id:role" in the provider's order.
func effectKeys(a accountactivity.Activity) []string {
	var out []string
	for _, e := range a.TradeEffects {
		out = append(out, e.BrokerTradeID+":"+e.Role.String())
	}
	return out
}

func qty(t *testing.T, v accountactivity.Value[num.Quantity]) string {
	t.Helper()
	q, ok := v.Get()
	require.True(t, ok, "units unknown")
	return q.String()
}

func price(t *testing.T, v accountactivity.Value[num.Price]) string {
	t.Helper()
	p, ok := v.Get()
	require.True(t, ok, "price unknown")
	return p.String()
}

// reconcile checks that a fill's aggregates equal the sums of its embedded
// per-trade values, where both sides were supplied. It reports mismatches; it
// never alters the data (broker data is never "corrected" in the ledger).
func reconcile(a accountactivity.Activity) []string {
	var problems []string
	sum := func(pick func(accountactivity.TradeEffect) accountactivity.Value[num.Money]) (num.Money, bool) {
		var total num.Money
		any := false
		for _, e := range a.TradeEffects {
			v, ok := pick(e).Get()
			if !ok {
				continue
			}
			if !any {
				total, any = v, true
				continue
			}
			var err error
			if total, err = total.Add(v); err != nil {
				problems = append(problems, err.Error())
			}
		}
		return total, any
	}
	check := func(name string, agg accountactivity.Value[num.Money], pick func(accountactivity.TradeEffect) accountactivity.Value[num.Money]) {
		want, hasAgg := agg.Get()
		got, hasSum := sum(pick)
		if hasAgg && hasSum && !want.Equal(got) {
			problems = append(problems, name+": fill "+want.String()+" != sum of trades "+got.String())
		}
	}
	check("halfSpreadCost", a.SpreadCost, func(e accountactivity.TradeEffect) accountactivity.Value[num.Money] { return e.SpreadCost })
	check("realized P/L", a.RealizedPL, func(e accountactivity.TradeEffect) accountactivity.Value[num.Money] { return e.RealizedPL })
	check("financing", a.Financing, func(e accountactivity.TradeEffect) accountactivity.Value[num.Money] { return e.Financing })
	check("guaranteed execution fee", a.OtherFees, func(e accountactivity.TradeEffect) accountactivity.Value[num.Money] {
		return e.GuaranteedExecutionFee
	})

	// Units: the effects together account for the fill's size.
	if q, ok := a.Quantity.Get(); ok {
		var total num.Quantity
		units := false
		for _, e := range a.TradeEffects {
			if u, ok := e.Units.Get(); ok {
				var err error
				if total, err = total.Add(u); err != nil {
					problems = append(problems, err.Error())
				}
				units = true
			}
		}
		if units && !total.Equal(q) {
			problems = append(problems, "units: fill "+q.String()+" != sum of trades "+total.String())
		}
	}
	return problems
}

// OANDA says the fill-level halfSpreadCost is the sum of the embedded trade
// values; P/L, financing, fee and size reconcile the same way. Every fill
// fixture, including a reversal and a multi-trade close, must reconcile.
func TestNormalize_FillAggregatesReconcileWithPerTradeValues(t *testing.T) {
	f := loadFixture(t)
	for _, id := range []int{3, 7, 12} {
		a := norm(t, f, id)
		assert.Empty(t, reconcile(a), "fill %d", id)
	}
}

// A mismatch is visible and is never corrected: the aggregate and the parts
// are stored exactly as the broker sent them.
func TestNormalize_AMismatchIsVisibleAndNeverCorrected(t *testing.T) {
	a, err := normalize(json.RawMessage(`{"id":"7","type":"ORDER_FILL","time":"2024-03-06T10:00:00Z","instrument":"EUR_USD","units":"-1500","price":"1.087",
		"pl":"9.9000","halfSpreadCost":"0.3000",
		"tradeOpened":{"tradeID":"7","units":"-500","price":"1.087","halfSpreadCost":"0.1000"},
		"tradesClosed":[{"tradeID":"3","units":"-1000","price":"1.087","realizedPL":"5.8000","halfSpreadCost":"0.2000"}]}`), stream, usd)
	require.NoError(t, err)

	assert.Equal(t, "9.9 USD", money(t, a.RealizedPL), "the broker's aggregate is kept as sent")
	m, _ := a.TradeEffects[1].RealizedPL.Get()
	assert.Equal(t, "5.8 USD", m.String())
	problems := reconcile(a)
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "realized P/L")
}

func TestNormalize_ReversalCarriesPerTradeExecutionFacts(t *testing.T) {
	a := norm(t, loadFixture(t), 7)
	require.Equal(t, []string{"7:opened", "3:closed"}, effectKeys(a))
	opened, closed := a.TradeEffects[0], a.TradeEffects[1]

	assert.Equal(t, "500", qty(t, opened.Units), "units are sizes, the sign is the fill's side")
	assert.Equal(t, "1.087", price(t, opened.Price))
	assert.Equal(t, "tradeOpened.price", opened.Price.Source())
	assert.False(t, opened.RealizedPL.Known(), "nothing is realized on open")
	assert.Equal(t, "0.1 USD", money(t, opened.SpreadCost))

	assert.Equal(t, "1000", qty(t, closed.Units))
	assert.Equal(t, "5.8 USD", money(t, closed.RealizedPL))
	assert.Equal(t, "-0.1 USD", money(t, closed.Financing))
	assert.Equal(t, "0.2 USD", money(t, closed.SpreadCost))
	assert.Equal(t, "tradesClosed.realizedPL", closed.RealizedPL.Source())
}

func TestNormalize_PartialReduceAndMultiTradeCloseAreEachPreserved(t *testing.T) {
	a := norm(t, loadFixture(t), 12)
	require.Equal(t, []string{"7:reduced", "8888:closed", "9999:closed"}, effectKeys(a))
	assert.Equal(t, "300", qty(t, a.TradeEffects[0].Units))
	assert.Equal(t, "0.7 USD", money(t, a.TradeEffects[0].RealizedPL))
	assert.Equal(t, "100", qty(t, a.TradeEffects[1].Units))
	assert.Equal(t, "0.3 USD", money(t, a.TradeEffects[1].RealizedPL))
	assert.Equal(t, "0.2 USD", money(t, a.TradeEffects[2].RealizedPL))
	assert.False(t, a.TradeEffects[0].Financing.Known(), "absent per-trade fields are unknown")
	assert.Equal(t, "1.09", price(t, a.TradeEffects[2].Price))
}

// The guaranteed-execution fee is a positive cost in OANDA's data, per trade
// as at the fill level, and is negated to Trader's sign. fullVWAP is the
// reference price "absent guaranteed execution", so it differs from the
// actual per-trade price and must not replace it.
func TestNormalize_GuaranteedExecutionFeeAndFullVWAPStaySeparateFromTheActualPrice(t *testing.T) {
	a, err := normalize(json.RawMessage(`{"id":"3","type":"ORDER_FILL","time":"2024-03-05T14:30:00Z","instrument":"EUR_USD","units":"1000","price":"1.08540","fullVWAP":"1.08534",
		"guaranteedExecutionFee":"0.5000","halfSpreadCost":"0.2500",
		"tradeOpened":{"tradeID":"3","units":"1000","price":"1.08540","guaranteedExecutionFee":"0.5000","halfSpreadCost":"0.2500"}}`), stream, usd)
	require.NoError(t, err)
	a.ImportedAt = time.Now()
	require.NoError(t, a.Validate())

	assert.Equal(t, "-0.5 USD", money(t, a.OtherFees), "a fee is a cost: negative")
	assert.Equal(t, "-0.5 USD", money(t, a.TradeEffects[0].GuaranteedExecutionFee))
	assert.Equal(t, "1.0854", price(t, a.TradeEffects[0].Price), "the actual price is the per-trade price")
	assert.Equal(t, "1.08534", price(t, a.FullVWAP), "fullVWAP is kept separately")
	assert.Equal(t, "fullVWAP", a.FullVWAP.Source())
	assert.Equal(t, accountactivity.BasisReported, a.FullVWAP.Basis())
	assert.Empty(t, reconcile(a))
	assert.False(t, a.Slippage.Known(), "nothing is relabelled as slippage")
	assert.False(t, a.Benchmark.Known())
}

func TestNormalize_FullPriceKeepsTheWholeLadderAndCloseouts(t *testing.T) {
	a := norm(t, loadFixture(t), 3)
	q, ok := a.ExecutionQuote.Get()
	require.True(t, ok)
	assert.Equal(t, "fullPrice", a.ExecutionQuote.Source())
	assert.Equal(t, accountactivity.BasisReported, a.ExecutionQuote.Basis())
	assert.Equal(t, time.Date(2024, 3, 5, 14, 30, 0, 150_000_000, time.UTC), q.At)

	require.Len(t, q.Bids, 2, "the ladder is not truncated to the top of the book")
	assert.Equal(t, "1.0853", q.Bids[0].Price.String())
	assert.Equal(t, "1000000", q.Bids[0].Liquidity.String())
	assert.Equal(t, "1.08528", q.Bids[1].Price.String())
	assert.Equal(t, "2000000", q.Bids[1].Liquidity.String())
	require.Len(t, q.Asks, 1)
	assert.Equal(t, "1.08534", q.Asks[0].Price.String())
	assert.Equal(t, "1.0852", price(t, q.CloseoutBid))
	assert.Equal(t, "1.08544", price(t, q.CloseoutAsk))
	assert.Equal(t, "fullPrice.closeoutBid", q.CloseoutBid.Source())
	assert.Equal(t, "1.08534", price(t, a.FullVWAP))
}

// The deprecated top-level price is optional once per-trade prices exist,
// and a fill with no price anywhere is an error.
func TestNormalize_ThePriceMayComeFromTheTradesAlone(t *testing.T) {
	a, err := normalize(json.RawMessage(`{"id":"3","type":"ORDER_FILL","time":"2024-03-05T14:30:00Z","instrument":"EUR_USD","units":"1000",
		"tradeOpened":{"tradeID":"3","units":"1000","price":"1.0854"}}`), stream, usd)
	require.NoError(t, err)
	assert.False(t, a.Price.Known(), "the deprecated aggregate is not invented")
	assert.Equal(t, "1.0854", price(t, a.TradeEffects[0].Price))

	_, err = normalize(json.RawMessage(`{"id":"3","type":"ORDER_FILL","time":"2024-03-05T14:30:00Z","instrument":"EUR_USD","units":"1000",
		"tradeOpened":{"tradeID":"3","units":"1000"}}`), stream, usd)
	assert.Error(t, err)
}

func TestNormalize_MalformedPerTradeDetailIsAnError(t *testing.T) {
	base := func(trade string) json.RawMessage {
		return json.RawMessage(`{"id":"3","type":"ORDER_FILL","time":"2024-03-05T14:30:00Z","instrument":"EUR_USD","units":"1","price":"1.1",` + trade + `}`)
	}
	for name, trade := range map[string]string{
		"units":           `"tradeOpened":{"tradeID":"3","units":"x"}`,
		"price":           `"tradeOpened":{"tradeID":"3","price":"1.1234567891"}`,
		"realizedPL":      `"tradesClosed":[{"tradeID":"3","realizedPL":"q"}]`,
		"no trade id":     `"tradeOpened":{"units":"1"}`,
		"fee":             `"tradeReduced":{"tradeID":"3","guaranteedExecutionFee":"?"}`,
		"vwap":            `"fullVWAP":"zz"`,
		"quote price":     `"fullPrice":{"bids":[{"price":"zz","liquidity":1}]}`,
		"quote liquidity": `"fullPrice":{"asks":[{"price":"1.1","liquidity":-5}]}`,
		"quote closeout":  `"fullPrice":{"closeoutBid":"zz"}`,
		"quote time":      `"fullPrice":{"time":"yesterday"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := normalize(base(trade), stream, usd)
			assert.Error(t, err)
		})
	}
}

// fullPrice is preserved whole or not at all: a snapshot missing its time,
// either ladder or either closeout is malformed provider data, and missing
// is not the same as an explicitly empty ladder.
func TestNormalize_AnIncompleteFullPriceIsMalformedNotPartial(t *testing.T) {
	fill := func(fullPrice string) json.RawMessage {
		return json.RawMessage(`{"id":"3","type":"ORDER_FILL","time":"2024-03-05T14:30:00Z","instrument":"EUR_USD","units":"1","price":"1.1","fullPrice":` + fullPrice + `}`)
	}
	complete := `{"time":"2024-03-05T14:30:00Z","bids":[{"price":"1.1","liquidity":100}],"asks":[{"price":"1.2","liquidity":100}],"closeoutBid":"1.09","closeoutAsk":"1.21"}`
	_, err := normalize(fill(complete), stream, usd)
	require.NoError(t, err)

	for name, body := range map[string]string{
		"no time":            `{"bids":[],"asks":[],"closeoutBid":"1.09","closeoutAsk":"1.21"}`,
		"null time":          `{"time":null,"bids":[],"asks":[],"closeoutBid":"1.09","closeoutAsk":"1.21"}`,
		"no bids":            `{"time":"2024-03-05T14:30:00Z","asks":[],"closeoutBid":"1.09","closeoutAsk":"1.21"}`,
		"null bids":          `{"time":"2024-03-05T14:30:00Z","bids":null,"asks":[],"closeoutBid":"1.09","closeoutAsk":"1.21"}`,
		"no asks":            `{"time":"2024-03-05T14:30:00Z","bids":[],"closeoutBid":"1.09","closeoutAsk":"1.21"}`,
		"null asks":          `{"time":"2024-03-05T14:30:00Z","bids":[],"asks":null,"closeoutBid":"1.09","closeoutAsk":"1.21"}`,
		"no closeout bid":    `{"time":"2024-03-05T14:30:00Z","bids":[],"asks":[],"closeoutAsk":"1.21"}`,
		"null closeout bid":  `{"time":"2024-03-05T14:30:00Z","bids":[],"asks":[],"closeoutBid":null,"closeoutAsk":"1.21"}`,
		"no closeout ask":    `{"time":"2024-03-05T14:30:00Z","bids":[],"asks":[],"closeoutBid":"1.09"}`,
		"only a bid":         `{"bids":[{"price":"1.1","liquidity":100}]}`,
		"rung without price": `{"time":"2024-03-05T14:30:00Z","bids":[{"liquidity":100}],"asks":[],"closeoutBid":"1.09","closeoutAsk":"1.21"}`,
		"rung without liq":   `{"time":"2024-03-05T14:30:00Z","bids":[{"price":"1.1"}],"asks":[],"closeoutBid":"1.09","closeoutAsk":"1.21"}`,
		"rung null liq":      `{"time":"2024-03-05T14:30:00Z","bids":[{"price":"1.1","liquidity":null}],"asks":[],"closeoutBid":"1.09","closeoutAsk":"1.21"}`,
		"rung zero liq":      `{"time":"2024-03-05T14:30:00Z","bids":[{"price":"1.1","liquidity":0}],"asks":[],"closeoutBid":"1.09","closeoutAsk":"1.21"}`,
		"rung zero price":    `{"time":"2024-03-05T14:30:00Z","bids":[{"price":"0","liquidity":5}],"asks":[],"closeoutBid":"1.09","closeoutAsk":"1.21"}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := normalize(fill(body), stream, usd)
			assert.Error(t, err)
		})
	}

	// Explicitly empty ladders are a legitimate snapshot (a closed market).
	a, err := normalize(fill(`{"time":"2024-03-05T14:30:00Z","bids":[],"asks":[],"closeoutBid":"1.09","closeoutAsk":"1.21"}`), stream, usd)
	require.NoError(t, err)
	q, ok := a.ExecutionQuote.Get()
	require.True(t, ok)
	assert.Empty(t, q.Bids)
	assert.Empty(t, q.Asks)
	assert.Equal(t, "1.09", price(t, q.CloseoutBid))
	a.ImportedAt = time.Now()
	assert.NoError(t, a.Validate())
}
