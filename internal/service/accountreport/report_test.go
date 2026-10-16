package accountreport_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/internal/adapters/accountactivity/filestore"
	svc "github.com/rustyeddy/trader/internal/service/accountreport"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

var (
	usd    = num.MustParseCurrency("USD")
	t0     = time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)
	eurusd = instrument.CurrencyPairID(num.MustParseCurrency("EUR"), usd)
	stream = accountactivity.SyncKey{Provider: "oanda", Environment: accountactivity.EnvironmentPractice, ProviderAccountID: "acct"}
)

func day(n int) time.Duration { return time.Duration(n) * 24 * time.Hour }

type fillSpec struct {
	id    string
	at    time.Duration
	side  order.Side
	qty   string
	price string
	vwap  string // broker fullVWAP; "" for none
	pl    string // "" leaves realized P/L unknown
}

func fill(s fillSpec) accountactivity.Activity {
	a := accountactivity.Activity{
		Provider: stream.Provider, Environment: stream.Environment, ProviderAccountID: stream.ProviderAccountID,
		ProviderActivityID: s.id, ProviderType: "ORDER_FILL", Kind: accountactivity.KindFill,
		OccurredAt: t0.Add(s.at), Instrument: eurusd, Side: s.side,
		Quantity: accountactivity.Reported(num.MustParseQuantity(s.qty)),
		Price:    accountactivity.Reported(num.MustParsePrice(s.price)),
	}
	if s.vwap != "" {
		a.FullVWAP = accountactivity.Reported(num.MustParsePrice(s.vwap))
	}
	if s.pl != "" {
		a.RealizedPL = accountactivity.Reported(num.MustParseMoney(s.pl, usd))
		a.Commission = accountactivity.Reported(num.MustParseMoney("-1", usd))
		a.Financing = accountactivity.Reported(num.MustParseMoney("0", usd))
	}
	return a
}

func ledger(t *testing.T, as ...accountactivity.Activity) accountactivity.Store {
	t.Helper()
	store, err := filestore.New(t.TempDir())
	require.NoError(t, err)
	for i := range as {
		as[i].ImportedAt = t0.Add(1000 * time.Hour)
	}
	_, err = store.Apply(context.Background(), accountactivity.Batch{Sync: stream, Activities: as, Cursor: "c", At: t0.Add(1000 * time.Hour)})
	require.NoError(t, err)
	return store
}

// A, entered on day 0 and closed on day 2; B, entered on day 5 and closed on
// day 6; C, entered on day 8 and still open.
func standard(t *testing.T) accountactivity.Store {
	return ledger(t,
		fill(fillSpec{id: "1", at: 0, side: order.Buy, qty: "1000", price: "1.1010", vwap: "1.1000"}),
		fill(fillSpec{id: "2", at: day(2), side: order.Sell, qty: "1000", price: "1.1090", vwap: "1.1100", pl: "8"}),
		fill(fillSpec{id: "3", at: day(5), side: order.Buy, qty: "500", price: "1.1200", vwap: "1.1200"}),
		fill(fillSpec{id: "4", at: day(6), side: order.Sell, qty: "500", price: "1.1300", vwap: "1.1300", pl: "5"}),
		fill(fillSpec{id: "5", at: day(8), side: order.Buy, qty: "200", price: "1.1400"}),
	)
}

func params(store accountactivity.Store) svc.Params {
	return svc.Params{Store: store, Stream: stream}
}

func tradeIDs(r svc.TradesResponse) []string {
	out := []string{}
	for _, row := range r.Trades {
		out = append(out, row.Trade.ID)
	}
	return out
}

func TestTrades_ReconstructsTheWholeStreamThenFilters(t *testing.T) {
	store := standard(t)
	ctx := context.Background()

	all, err := svc.Trades(ctx, params(store))
	require.NoError(t, err)
	require.Len(t, all.Trades, 2)
	require.Len(t, all.Open, 1)
	assert.Empty(t, all.Orphans)

	// A range that holds A's close but not its entry still shows A as a whole
	// completed trade. Filtering activities first would have left an orphan
	// exit and no trade.
	p := params(store)
	p.From, p.To = t0.Add(day(1)), t0.Add(day(3))
	r, err := svc.Trades(ctx, p)
	require.NoError(t, err)
	require.Len(t, r.Trades, 1)
	assert.Equal(t, all.Trades[0].Trade.ID, r.Trades[0].Trade.ID)
	assert.Equal(t, t0, r.Trades[0].Trade.OpenedAt, "the entry is before the range")
	assert.Empty(t, r.Orphans)

	// A trade is selected by its close time, half-open: closing exactly at To
	// is outside.
	p.From, p.To = t0.Add(day(2)), t0.Add(day(6))
	r, err = svc.Trades(ctx, p)
	require.NoError(t, err)
	assert.Equal(t, []string{all.Trades[0].Trade.ID}, tradeIDs(r))
	p.From, p.To = t0.Add(day(6)), time.Time{}
	r, err = svc.Trades(ctx, p)
	require.NoError(t, err)
	assert.Equal(t, []string{all.Trades[1].Trade.ID}, tradeIDs(r))

	// Open positions are not subject to the range.
	assert.Len(t, r.Open, 1)

	// A range that selects nothing is an empty list, not nil.
	p.From, p.To = t0.Add(day(100)), time.Time{}
	r, err = svc.Trades(ctx, p)
	require.NoError(t, err)
	assert.NotNil(t, r.Trades)
	assert.Empty(t, r.Trades)

	p.From, p.To = t0.Add(day(3)), t0.Add(day(1))
	_, err = svc.Trades(ctx, p)
	assert.ErrorIs(t, err, svc.ErrBadRange)
}

func TestTrades_ReportsWhatItCouldNotPlace(t *testing.T) {
	// A close of a broker trade this window never saw open, and a standalone
	// financing charge: neither may be dropped.
	orphan := fill(fillSpec{id: "1", at: 0, side: order.Sell, qty: "300", price: "1.1", pl: "1"})
	orphan.TradeEffects = []accountactivity.TradeEffect{{BrokerTradeID: "t0", Role: accountactivity.TradeRoleClosed}}
	fin := accountactivity.Activity{
		Provider: stream.Provider, Environment: stream.Environment, ProviderAccountID: stream.ProviderAccountID,
		ProviderActivityID: "2", ProviderType: "DAILY_FINANCING", Kind: accountactivity.KindFinancing,
		OccurredAt: t0.Add(day(1)), Financing: accountactivity.Reported(num.MustParseMoney("-0.5", usd)),
	}
	r, err := svc.Trades(context.Background(), params(ledger(t, orphan, fin)))
	require.NoError(t, err)
	assert.Empty(t, r.Trades)
	assert.Empty(t, r.Open)
	require.Len(t, r.Orphans, 1)
	assert.Equal(t, "300", r.Orphans[0].Quantity.String())
	require.Len(t, r.Unattributed, 1)
	assert.Equal(t, "2", r.Unattributed[0].ProviderActivityID)
}

func TestTrades_NoBenchmarkMeansNoExecutionQuality(t *testing.T) {
	r, err := svc.Trades(context.Background(), params(standard(t)))
	require.NoError(t, err)
	for _, row := range r.Trades {
		assert.Nil(t, row.Quality, "not requested is not the same as unknown")
	}
	assert.Equal(t, svc.BenchmarkNone, r.Benchmark)
}

func TestTrades_BenchmarkMeasuresEachFill(t *testing.T) {
	p := params(standard(t))
	p.Benchmark = svc.BenchmarkBrokerVWAP
	r, err := svc.Trades(context.Background(), p)
	require.NoError(t, err)
	require.Len(t, r.Trades, 2)

	// Trade A: buy 1000 at 1.1010 vs 1.1000 is -0.001 x 1000 = -1 USD, and
	// sell 1000 at 1.1090 vs 1.1100 is -0.001 x 1000 = -1 USD.
	q := r.Trades[0].Quality
	require.NotNil(t, q)
	assert.Equal(t, svc.BenchmarkBrokerVWAP, q.Benchmark)
	require.Len(t, q.Fills, 2)
	cost, ok := q.SlippageCost.Get()
	require.True(t, ok, q.Reason)
	assert.Equal(t, "-2 USD", cost.String())
	assert.Equal(t, accountactivity.BasisDerived, q.SlippageCost.Basis())
	assert.True(t, r.AccountCurrency.Known)
	assert.Equal(t, "USD", r.AccountCurrency.Code.String())

	// Trade B filled exactly at the benchmark: a known zero.
	zero, ok := r.Trades[1].Quality.SlippageCost.Get()
	require.True(t, ok)
	assert.True(t, zero.IsZero())
}

func TestTrades_RequestedButUnmeasurableIsUnknownWithAReason(t *testing.T) {
	// The fills report no fullVWAP, so the requested benchmark cannot be built.
	store := ledger(t,
		fill(fillSpec{id: "1", at: 0, side: order.Buy, qty: "100", price: "1.1"}),
		fill(fillSpec{id: "2", at: day(1), side: order.Sell, qty: "100", price: "1.2", pl: "10"}),
	)
	p := params(store)
	p.Benchmark = svc.BenchmarkBrokerVWAP
	r, err := svc.Trades(context.Background(), p)
	require.NoError(t, err)
	require.Len(t, r.Trades, 1)
	q := r.Trades[0].Quality
	require.NotNil(t, q, "requested, so the quality object exists")
	assert.False(t, q.SlippageCost.Known())
	assert.Contains(t, q.Reason, "fullVWAP")
	require.Len(t, q.Fills, 2)
	assert.Nil(t, q.Fills[0].Result)
	assert.NotEmpty(t, q.Fills[0].Reason)
}

func TestTrades_ASharedFillIsNotChargedToBothTrades(t *testing.T) {
	// The second sell closes the long and opens a short in one fill; its
	// slippage belongs to the fill, not to either trade alone.
	store := ledger(t,
		fill(fillSpec{id: "1", at: 0, side: order.Buy, qty: "100", price: "1.1", vwap: "1.1"}),
		fill(fillSpec{id: "2", at: day(1), side: order.Sell, qty: "300", price: "1.2", vwap: "1.21", pl: "10"}),
		fill(fillSpec{id: "3", at: day(2), side: order.Buy, qty: "200", price: "1.1", vwap: "1.1", pl: "20"}),
	)
	p := params(store)
	p.Benchmark = svc.BenchmarkBrokerVWAP
	r, err := svc.Trades(context.Background(), p)
	require.NoError(t, err)
	require.Len(t, r.Trades, 2)
	for _, row := range r.Trades {
		assert.False(t, row.Quality.SlippageCost.Known(), row.Trade.ID)
		assert.Contains(t, row.Quality.Reason, "shared with another trade", row.Trade.ID)
	}
}

func TestTrades_UnknownAccountCurrencyLeavesCostUnknown(t *testing.T) {
	// No monetary figure anywhere, so the currency cannot be found.
	a := fill(fillSpec{id: "1", at: 0, side: order.Buy, qty: "100", price: "1.1010", vwap: "1.1"})
	b := fill(fillSpec{id: "2", at: day(1), side: order.Sell, qty: "100", price: "1.1", vwap: "1.1"})
	p := params(ledger(t, a, b))
	p.Benchmark = svc.BenchmarkBrokerVWAP
	r, err := svc.Trades(context.Background(), p)
	require.NoError(t, err)
	assert.False(t, r.AccountCurrency.Known)
	require.Len(t, r.Trades, 1)
	q := r.Trades[0].Quality
	assert.False(t, q.SlippageCost.Known())
	require.NotNil(t, q.Fills[0].Result)
	_, priceKnown := q.Fills[0].Result.Slippage.Get()
	assert.True(t, priceKnown, "the price-unit slippage is still measured")

	// An explicit currency makes the cost known.
	p.AccountCurrency = usd
	r, err = svc.Trades(context.Background(), p)
	require.NoError(t, err)
	assert.True(t, r.AccountCurrency.Known)
	assert.Equal(t, "configured", r.AccountCurrency.Source)
}

func TestTrade_ResolvesFullIDsAndUniquePrefixes(t *testing.T) {
	store := standard(t)
	ctx := context.Background()
	all, err := svc.Trades(ctx, params(store))
	require.NoError(t, err)
	a, b := all.Trades[0].Trade.ID, all.Trades[1].Trade.ID

	d, err := svc.Trade(ctx, params(store), a)
	require.NoError(t, err)
	require.NotNil(t, d.Completed)
	assert.Nil(t, d.Open)
	assert.Equal(t, a, d.Completed.ID)
	assert.Len(t, d.Fills, 2, "the source fills are returned in full")
	assert.Nil(t, d.Quality, "no benchmark requested")

	// A unique prefix resolves; ranges do not matter for an ID.
	p := params(store)
	p.From = t0.Add(day(50))
	d, err = svc.Trade(ctx, p, a[:len(a)-3])
	require.NoError(t, err)
	assert.Equal(t, a, d.Completed.ID)

	// A prefix shared by several trades is ambiguous and lists them all; the
	// service never picks one.
	common := commonPrefix(a, b)
	require.NotEmpty(t, common)
	_, err = svc.Trade(ctx, params(store), common)
	var amb *svc.AmbiguousError
	require.ErrorAs(t, err, &amb)
	assert.Contains(t, amb.Candidates, a)
	assert.Contains(t, amb.Candidates, b)
	assert.GreaterOrEqual(t, len(amb.Candidates), 2)

	_, err = svc.Trade(ctx, params(store), "no-such-trade")
	assert.ErrorIs(t, err, svc.ErrTradeNotFound)
	_, err = svc.Trade(ctx, params(store), "  ")
	assert.ErrorIs(t, err, svc.ErrTradeNotFound)

	// The open position is addressable too.
	open := all.Open[0].ID
	d, err = svc.Trade(ctx, params(store), open)
	require.NoError(t, err)
	require.NotNil(t, d.Open)
	assert.Nil(t, d.Completed)
}

func TestTrade_BenchmarkGivesPerFillForensics(t *testing.T) {
	store := standard(t)
	all, err := svc.Trades(context.Background(), params(store))
	require.NoError(t, err)
	p := params(store)
	p.Benchmark = svc.BenchmarkBrokerVWAP
	d, err := svc.Trade(context.Background(), p, all.Trades[0].Trade.ID)
	require.NoError(t, err)
	require.NotNil(t, d.Quality)
	require.Len(t, d.Quality.Fills, 2)
	for _, f := range d.Quality.Fills {
		require.NotNil(t, f.Result)
		assert.Equal(t, "broker_full_vwap", string(f.Result.Benchmark.Kind))
		assert.True(t, f.Result.AgeKnown)
	}
}

func TestParseBenchmark(t *testing.T) {
	for in, want := range map[string]svc.Benchmark{"": svc.BenchmarkNone, "broker-vwap": svc.BenchmarkBrokerVWAP, " broker-quote ": svc.BenchmarkBrokerQuote} {
		got, err := svc.ParseBenchmark(in)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	_, err := svc.ParseBenchmark("mid")
	assert.Error(t, err)
	_, err = svc.Trades(context.Background(), svc.Params{Store: standard(t), Stream: stream, Benchmark: "mid"})
	assert.Error(t, err)
}

func commonPrefix(a, b string) string {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return a[:n]
}
