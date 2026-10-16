package executionquality

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/internal/accountactivity"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

var (
	usd   = num.MustParseCurrency("USD")
	eur   = num.MustParseCurrency("EUR")
	t0    = time.Date(2024, 3, 5, 9, 0, 0, 0, time.UTC)
	eurus = instrument.CurrencyPairID(eur, usd)
	usdjp = instrument.CurrencyPairID(usd, num.MustParseCurrency("JPY"))
)

func qty(s string) num.Quantity { return num.MustParseQuantity(s) }
func px(s string) num.Price     { return num.MustParsePrice(s) }

func effect(id, units, price string) accountactivity.TradeEffect {
	return accountactivity.TradeEffect{
		BrokerTradeID: id, Role: accountactivity.TradeRoleOpened,
		Units: accountactivity.Reported(qty(units)), Price: accountactivity.Reported(px(price)),
	}
}

// fill is a one-trade fill at price; fillPrice "" leaves the fill-level price
// unknown.
func fill(side order.Side, units, price string) accountactivity.Activity {
	a := accountactivity.Activity{
		Provider: "oanda", Environment: accountactivity.EnvironmentPractice, ProviderAccountID: "acct",
		ProviderActivityID: "1", ProviderType: "ORDER_FILL", Kind: accountactivity.KindFill,
		OccurredAt: t0, ImportedAt: t0.Add(time.Hour),
		Instrument: eurus, Side: side,
		Quantity:     accountactivity.Reported(qty(units)),
		TradeEffects: []accountactivity.TradeEffect{effect("t1", units, price)},
	}
	return a
}

func bench(price string) *Benchmark {
	return &Benchmark{Kind: OrderReference, Price: px(price), At: t0.Add(-time.Second), Source: "trader order request"}
}

var usdOpts = Options{AccountCurrency: usd}

func measure(t *testing.T, a accountactivity.Activity, b *Benchmark, o Options) Result {
	t.Helper()
	r, err := Measure(a, b, o)
	require.NoError(t, err)
	return r
}

func slip(t *testing.T, r Result) string {
	t.Helper()
	v, ok := r.Slippage.Get()
	require.True(t, ok, "slippage unknown: %v", r.Unresolved)
	return v.String()
}

func cost(t *testing.T, r Result) string {
	t.Helper()
	v, ok := r.SlippageCost.Get()
	require.True(t, ok, "cost unknown: %v", r.Unresolved)
	return v.String()
}

// Favorable is positive, adverse negative, for both sides: a buy filled above
// the benchmark and a sell filled below it are both worse for the trader.
func TestSignConvention(t *testing.T) {
	for _, c := range []struct {
		name       string
		side       order.Side
		price      string
		slip, cost string
	}{
		{"buy adverse", order.Buy, "1.1010", "-0.001", "-1 USD"},
		{"buy favorable", order.Buy, "1.0990", "0.001", "1 USD"},
		{"sell adverse", order.Sell, "1.0990", "-0.001", "-1 USD"},
		{"sell favorable", order.Sell, "1.1010", "0.001", "1 USD"},
		{"buy at benchmark", order.Buy, "1.1000", "0", "0 USD"},
		{"sell at benchmark", order.Sell, "1.1000", "0", "0 USD"},
	} {
		r := measure(t, fill(c.side, "1000", c.price), bench("1.1000"), usdOpts)
		assert.Equal(t, c.slip, slip(t, r), c.name)
		assert.Equal(t, c.cost, cost(t, r), c.name)
		assert.Equal(t, accountactivity.BasisDerived, r.Slippage.Basis(), c.name)
		assert.NotEmpty(t, r.Slippage.Source(), c.name)
	}
}

// A measured zero is a known zero.
func TestKnownZeroIsKnown(t *testing.T) {
	r := measure(t, fill(order.Buy, "1000", "1.1000"), bench("1.1000"), usdOpts)
	_, ok := r.Slippage.Get()
	assert.True(t, ok)
	_, ok = r.SlippageCost.Get()
	assert.True(t, ok)
	assert.Empty(t, r.Unresolved)
}

func TestNoBenchmarkIsUnknownNotZero(t *testing.T) {
	r := measure(t, fill(order.Buy, "1000", "1.1010"), nil, usdOpts)
	assert.False(t, r.Slippage.Known())
	assert.False(t, r.SlippageCost.Known())
	assert.Nil(t, r.Benchmark)
	assert.Contains(t, r.Unresolved[0], "no benchmark")
	p, ok := r.ExecutionPrice.Get()
	require.True(t, ok, "the execution price is still reported")
	assert.Equal(t, "1.101", p.String())
}

// The result keeps what the benchmark was, where it came from and how old it
// was.
func TestBenchmarkProvenanceAndAgeSurvive(t *testing.T) {
	b := &Benchmark{Kind: SubmitQuoteMid, Price: px("1.1000"), At: t0.Add(-250 * time.Millisecond), Source: "oanda pricing at submit"}
	r := measure(t, fill(order.Buy, "1000", "1.1005"), b, usdOpts)
	require.NotNil(t, r.Benchmark)
	assert.Equal(t, SubmitQuoteMid, r.Benchmark.Kind)
	assert.Equal(t, "oanda pricing at submit", r.Benchmark.Source)
	assert.True(t, r.AgeKnown)
	assert.Equal(t, 250*time.Millisecond, r.BenchmarkAge)
	assert.Contains(t, r.Slippage.Source(), "submit_quote_mid")

	// A benchmark taken after the fill has a negative age; it is reported,
	// not hidden.
	after := &Benchmark{Kind: TriggerPrice, Price: px("1.1000"), At: t0.Add(time.Second), Source: "stop"}
	r = measure(t, fill(order.Buy, "1000", "1.1005"), after, usdOpts)
	assert.Equal(t, -time.Second, r.BenchmarkAge)

	// No benchmark time: no age.
	untimed := &Benchmark{Kind: OrderReference, Price: px("1.1000"), Source: "request"}
	r = measure(t, fill(order.Buy, "1000", "1.1005"), untimed, usdOpts)
	assert.False(t, r.AgeKnown)
	assert.Zero(t, r.BenchmarkAge)
}

func TestBenchmarkValidation(t *testing.T) {
	good := Benchmark{Kind: OrderReference, Price: px("1.1"), Source: "x"}
	for name, mod := range map[string]func(*Benchmark){
		"unknown kind":          func(b *Benchmark) { b.Kind = "guess" },
		"empty kind":            func(b *Benchmark) { b.Kind = "" },
		"zero price":            func(b *Benchmark) { b.Price = num.Price{} },
		"blank source":          func(b *Benchmark) { b.Source = "  " },
		"historical no bar":     func(b *Benchmark) { b.Kind = HistoricalMid },
		"interval on non-hist.": func(b *Benchmark) { b.Interval = time.Minute },
	} {
		b := good
		mod(&b)
		_, err := Measure(fill(order.Buy, "1000", "1.1"), &b, usdOpts)
		assert.ErrorIs(t, err, ErrInvalidBenchmark, name)
	}
	h := good
	h.Kind, h.Interval = HistoricalMid, time.Hour
	_, err := Measure(fill(order.Buy, "1000", "1.1"), &h, usdOpts)
	assert.NoError(t, err)
}

// A bar-derived mid carries its interval into the result.
func TestHistoricalMidKeepsItsBarInterval(t *testing.T) {
	b := &Benchmark{Kind: HistoricalMid, Price: px("1.1000"), Source: "oanda H1 mid", Interval: time.Hour}
	r := measure(t, fill(order.Sell, "1000", "1.0995"), b, usdOpts)
	require.NotNil(t, r.Benchmark)
	assert.Equal(t, time.Hour, r.Benchmark.Interval)
}

func TestOnlyFillsAreMeasured(t *testing.T) {
	d := fill(order.Buy, "1000", "1.1")
	d.Kind = accountactivity.KindDeposit
	_, err := Measure(d, bench("1.1"), usdOpts)
	assert.ErrorIs(t, err, ErrNotAFill)

	noSide := fill(order.Buy, "1000", "1.1")
	noSide.Side = 0
	_, err = Measure(noSide, bench("1.1"), usdOpts)
	assert.ErrorIs(t, err, ErrNotAFill)

	noQty := fill(order.Buy, "1000", "1.1")
	noQty.Quantity = accountactivity.Value[num.Quantity]{}
	_, err = Measure(noQty, bench("1.1"), usdOpts)
	assert.ErrorIs(t, err, ErrNotAFill)

	_, err = Measure(fill(order.Buy, "1000", "1.1"), bench("1.1"), Options{})
	assert.Error(t, err, "an account currency is required")
}

// Per-trade prices give the units-weighted fill price, preferred over the
// provider's aggregate.
func TestExecutionPricePrefersCompletePerTradeVWAP(t *testing.T) {
	a := fill(order.Buy, "300", "1")
	a.TradeEffects = []accountactivity.TradeEffect{effect("t1", "100", "1.1000"), effect("t2", "200", "1.1030")}
	// A deliberately different, deprecated aggregate must be ignored.
	a.Price = accountactivity.Reported(px("1.5"))

	r := measure(t, a, bench("1.1000"), usdOpts)
	p, ok := r.ExecutionPrice.Get()
	require.True(t, ok)
	assert.Equal(t, "1.102", p.String(), "(100*1.1000 + 200*1.1030)/300")
	assert.Equal(t, methodVWAP, r.ExecutionPrice.Source())
	assert.Equal(t, accountactivity.BasisDerived, r.ExecutionPrice.Basis())
	assert.Equal(t, "-0.002", slip(t, r))
	assert.Equal(t, "-0.6 USD", cost(t, r))
}

func TestVWAPRoundsOnceHalfEven(t *testing.T) {
	// (1 * 1.00000000 + 2 * 1.00000001) / 3 = 1.0000000066...: rounds to
	// 1.00000001, once.
	a := fill(order.Buy, "3", "1")
	a.TradeEffects = []accountactivity.TradeEffect{effect("t1", "1", "1.00000000"), effect("t2", "2", "1.00000001")}
	p, ok := measure(t, a, bench("1"), usdOpts).ExecutionPrice.Get()
	require.True(t, ok)
	assert.Equal(t, "1.00000001", p.String())

	// An exact tie goes to even: (1.00000000 + 1.00000001)/2 = 1.000000005.
	a = fill(order.Buy, "2", "1")
	a.TradeEffects = []accountactivity.TradeEffect{effect("t1", "1", "1.00000000"), effect("t2", "1", "1.00000001")}
	p, _ = measure(t, a, bench("1"), usdOpts).ExecutionPrice.Get()
	assert.Equal(t, "1", p.String(), "tie rounds to the even 1.00000000")

	// ... and 1.00000001 + 1.00000002 ties to the even 1.00000002.
	a.TradeEffects = []accountactivity.TradeEffect{effect("t1", "1", "1.00000001"), effect("t2", "1", "1.00000002")}
	p, _ = measure(t, a, bench("1"), usdOpts).ExecutionPrice.Get()
	assert.Equal(t, "1.00000002", p.String())
}

// An incomplete set of per-trade facts is never partly averaged.
func TestIncompleteEffectsFallBackOrAreUnknown(t *testing.T) {
	for name, mod := range map[string]func(*accountactivity.Activity){
		"units do not sum to the fill": func(a *accountactivity.Activity) {
			a.TradeEffects = []accountactivity.TradeEffect{effect("t1", "100", "1.1000"), effect("t2", "100", "1.1030")}
		},
		"an effect has no price": func(a *accountactivity.Activity) {
			a.TradeEffects[1].Price = accountactivity.Value[num.Price]{}
		},
		"an effect has no units": func(a *accountactivity.Activity) {
			a.TradeEffects[1].Units = accountactivity.Value[num.Quantity]{}
		},
		"no effects": func(a *accountactivity.Activity) { a.TradeEffects = nil },
	} {
		a := fill(order.Buy, "300", "1")
		a.TradeEffects = []accountactivity.TradeEffect{effect("t1", "100", "1.1000"), effect("t2", "200", "1.1030")}
		mod(&a)

		// With a provider aggregate: used, and labelled as the fallback.
		a.Price = accountactivity.Reported(px("1.1050"))
		r := measure(t, a, bench("1.1000"), usdOpts)
		p, ok := r.ExecutionPrice.Get()
		require.True(t, ok, name)
		assert.Equal(t, "1.105", p.String(), name)
		assert.Equal(t, methodFallback, r.ExecutionPrice.Source(), name)

		// Without one: unknown, not a partial average.
		a.Price = accountactivity.Value[num.Price]{}
		r = measure(t, a, bench("1.1000"), usdOpts)
		assert.False(t, r.ExecutionPrice.Known(), name)
		assert.False(t, r.Slippage.Known(), name)
		assert.NotEmpty(t, r.Unresolved, name)
	}
}

// The cost is the quote-currency amount only when it already is the account
// currency, or an explicit conversion says how to change it.
func TestCostCurrency(t *testing.T) {
	// EUR/USD in a EUR account: the quote currency is USD, so a cost in EUR
	// needs a conversion, and without one only the price-unit slippage is
	// known.
	eurAcct := Options{AccountCurrency: eur}
	r := measure(t, fill(order.Buy, "1000", "1.1010"), bench("1.1000"), eurAcct)
	assert.Equal(t, "-0.001", slip(t, r))
	assert.False(t, r.SlippageCost.Known())
	assert.Contains(t, r.Unresolved[0], "no conversion from USD to EUR")

	conv := &Conversion{Rate: num.MustParseRate("0.9"), From: usd, To: eur, At: t0, Source: "ecb reference"}
	eurAcct.Conversion = conv
	r = measure(t, fill(order.Buy, "1000", "1.1010"), bench("1.1000"), eurAcct)
	assert.Equal(t, "-0.9 EUR", cost(t, r), "-1 USD x 0.9, converted once")
	require.NotNil(t, r.Conversion)
	assert.Equal(t, "ecb reference", r.Conversion.Source)
	assert.Contains(t, r.SlippageCost.Source(), "ecb reference")

	// A conversion that does not run quote -> account is refused, not guessed.
	wrong := Options{AccountCurrency: eur, Conversion: &Conversion{Rate: num.MustParseRate("1.1"), From: eur, To: usd, Source: "x"}}
	r = measure(t, fill(order.Buy, "1000", "1.1010"), bench("1.1000"), wrong)
	assert.False(t, r.SlippageCost.Known())
	assert.Contains(t, r.Unresolved[0], "runs EUR to USD")

	// When the quote currency is the account currency a conversion is unused.
	same := Options{AccountCurrency: usd, Conversion: conv}
	r = measure(t, fill(order.Buy, "1000", "1.1010"), bench("1.1000"), same)
	assert.Equal(t, "-1 USD", cost(t, r))
	assert.Nil(t, r.Conversion)

	// USD/JPY in a USD account: quote is JPY, nothing to convert with.
	j := fill(order.Sell, "1000", "150.00")
	j.Instrument = usdjp
	r = measure(t, j, bench("150.10"), usdOpts)
	assert.Equal(t, "-0.1", slip(t, r))
	assert.False(t, r.SlippageCost.Known())

	// An instrument whose quote currency is not known: cost unknown.
	cfd := fill(order.Buy, "10", "5000")
	cfd.Instrument = instrument.ID{}
	r = measure(t, cfd, bench("4999"), usdOpts)
	assert.Equal(t, "-1", slip(t, r))
	assert.False(t, r.SlippageCost.Known())
}

func TestInvalidConversionIsAnError(t *testing.T) {
	for name, c := range map[string]*Conversion{
		"no source":     {Rate: num.MustParseRate("0.9"), From: usd, To: eur},
		"zero rate":     {From: usd, To: eur, Source: "x"},
		"negative rate": {Rate: num.MustParseRate("-1"), From: usd, To: eur, Source: "x"},
		"no currency":   {Rate: num.MustParseRate("1"), Source: "x"},
	} {
		_, err := Measure(fill(order.Buy, "1000", "1.1"), bench("1.1"), Options{AccountCurrency: eur, Conversion: c})
		assert.ErrorIs(t, err, ErrInvalidConversion, name)
	}
}

// The broker's spread cost is a separate metric: it changes nothing here.
func TestSpreadCostIsSeparate(t *testing.T) {
	plain := measure(t, fill(order.Buy, "1000", "1.1010"), bench("1.1000"), usdOpts)

	a := fill(order.Buy, "1000", "1.1010")
	a.SpreadCost = accountactivity.Reported(num.MustParseMoney("-0.5", usd))
	a.RealizedPL = accountactivity.Reported(num.MustParseMoney("3", usd))
	withSpread := measure(t, a, bench("1.1000"), usdOpts)

	assert.Equal(t, slip(t, plain), slip(t, withSpread))
	assert.Equal(t, cost(t, plain), cost(t, withSpread))
}

// Nothing is written back to the activity.
func TestMeasureDoesNotMutateTheActivity(t *testing.T) {
	a := fill(order.Buy, "1000", "1.1010")
	before := a
	measure(t, a, bench("1.1000"), usdOpts)
	assert.False(t, a.Slippage.Known())
	assert.False(t, a.SlippageCost.Known())
	assert.False(t, a.Benchmark.Known())
	assert.True(t, before.SameRecord(a))
}

func TestBrokerBenchmarks(t *testing.T) {
	a := fill(order.Buy, "1000", "1.1010")
	a.FullVWAP = accountactivity.Reported(px("1.1004"))
	a.ExecutionQuote = accountactivity.Reported(accountactivity.ExecutionQuote{
		At:   t0.Add(-100 * time.Millisecond),
		Bids: []accountactivity.PriceLevel{{Price: px("1.1001"), Liquidity: qty("500")}},
		Asks: []accountactivity.PriceLevel{{Price: px("1.1003"), Liquidity: qty("500")}, {Price: px("1.1006"), Liquidity: qty("1000")}},
	})

	vw, err := BenchmarkFromFullVWAP(a)
	require.NoError(t, err)
	assert.Equal(t, BrokerFullVWAP, vw.Kind)
	assert.Equal(t, "oanda fullVWAP", vw.Source)
	r := measure(t, a, &vw, usdOpts)
	assert.Equal(t, "-0.0006", slip(t, r))

	// A buy is measured against the best ask, a sell against the best bid.
	touch, err := BenchmarkFromQuoteTouch(a)
	require.NoError(t, err)
	assert.Equal(t, BrokerQuote, touch.Kind)
	assert.Equal(t, "1.1003", touch.Price.String())
	r = measure(t, a, &touch, usdOpts)
	assert.Equal(t, "-0.0007", slip(t, r))
	assert.Equal(t, 100*time.Millisecond, r.BenchmarkAge)

	s := a
	s.Side = order.Sell
	touch, err = BenchmarkFromQuoteTouch(s)
	require.NoError(t, err)
	assert.Equal(t, "1.1001", touch.Price.String())

	// Absent broker data is an error, never a guessed benchmark.
	plain := fill(order.Buy, "1000", "1.1")
	_, err = BenchmarkFromFullVWAP(plain)
	assert.ErrorIs(t, err, ErrInvalidBenchmark)
	_, err = BenchmarkFromQuoteTouch(plain)
	assert.ErrorIs(t, err, ErrInvalidBenchmark)
	a.ExecutionQuote = accountactivity.Reported(accountactivity.ExecutionQuote{
		At: t0, Bids: []accountactivity.PriceLevel{{Price: px("1.1001"), Liquidity: qty("500")}},
	})
	_, err = BenchmarkFromQuoteTouch(a)
	assert.ErrorIs(t, err, ErrInvalidBenchmark, "no asks for a buy")
}

func TestMeasureIsDeterministic(t *testing.T) {
	a := fill(order.Sell, "750", "1.0987")
	b := bench("1.1000")
	assert.Equal(t, measure(t, a, b, usdOpts), measure(t, a, b, usdOpts))
}
