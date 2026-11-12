package trader

import (
	"context"
	"testing"
	"time"

	instr "github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRate_Constants(t *testing.T) {
	assert.Equal(t, rt(10_000), rateOnePercent, "1%")
	assert.Equal(t, rt(20_000), rateTwoPercent, "2%")
	assert.Equal(t, rt(50_000), rateFivePercent, "5%")
	assert.Equal(t, rt(1_000_000), rateOne, "100%")
	assert.Equal(t, rt(500_000), rateHalf, "50%")
}

// rawMarket builds one catalog entry with every part adjustable, so a test can
// break exactly one rule.
type rawMarket struct {
	key, symbol  string
	base, quote  string
	settle       string
	multiplier   num.Rate
	tick         string
	step         num.Quantity
	tradable     bool
	rules        tradingRules
	equityTicker string // when set, the instrument is this equity instead of a pair
}

func goodRaw(key string) rawMarket {
	return rawMarket{key: key, symbol: key, base: "EUR", quote: "USD", settle: "USD", multiplier: rateOne,
		tick: "0.00001", step: oneUnit, tradable: true, rules: tradingRules{marginRate: rateTwoPercent, minUnits: oneUnit}}
}

func (r rawMarket) config(t *testing.T) marketConfig {
	t.Helper()
	var ins instr.Instrument
	var err error
	if r.equityTicker != "" {
		ins, err = instr.NewEquity("NASDAQ", r.equityTicker)
	} else {
		ins, err = instr.NewCurrencyPair(num.MustParseCurrency(r.base), num.MustParseCurrency(r.quote))
	}
	require.NoError(t, err)
	spec, err := instr.NewSpec(num.MustParsePrice(r.tick), r.step, r.multiplier, num.MustParseCurrency(r.settle))
	require.NoError(t, err)
	listing, err := instr.NewListing(instr.ListingParams{Instrument: ins, Provider: "TEST", Symbol: r.symbol, Spec: spec, Tradable: r.tradable})
	require.NoError(t, err)
	return marketConfig{key: r.key, instrument: ins, listing: listing, rules: r.rules}
}

func TestDefaultCatalog_HoldsTheSimulatedMarkets(t *testing.T) {
	c := testMarkets()

	assert.Equal(t, []string{"EURUSD", "GBPUSD"}, c.keys())
	eur, ok := c.lookup("EURUSD")
	require.True(t, ok)
	assert.Equal(t, rateTwoPercent, eur.rules.marginRate)
	assert.Equal(t, px(1), eur.tick)
	assert.Equal(t, oneUnit, eur.unitStep)
	assert.Equal(t, usdCurrency, eur.quote)
	assert.True(t, eur.listing.Tradable())
	assert.Equal(t, instr.CurrencyPairID(num.MustParseCurrency("EUR"), usdCurrency), eur.instrument.ID())
	gbp, ok := c.lookup("GBPUSD")
	require.True(t, ok)
	assert.Equal(t, rateFivePercent, gbp.rules.marginRate)
}

func TestCatalogs_AreIndependent(t *testing.T) {
	a, b := testMarkets(), testMarkets()
	require.NotSame(t, a, b)

	extended := withMarket(t, a, testInstrument("EXTRA"))

	_, inExtended := extended.lookup("EXTRA")
	_, inA := a.lookup("EXTRA")
	_, inB := b.lookup("EXTRA")
	assert.True(t, inExtended)
	assert.False(t, inA, "building a catalog from another never changes it")
	assert.False(t, inB)
}

func TestNewCatalog_RejectsWhatIsInconsistent(t *testing.T) {
	good := goodRaw("EURUSD")
	other := goodRaw("GBPUSD")
	other.base = "GBP"

	mismatched := good.config(t)
	mismatched.listing = other.config(t).listing // lists GBP/USD, not its EUR/USD instrument

	sameIDOtherKey := good
	sameIDOtherKey.key, sameIDOtherKey.symbol = "EUR_USD", "EUR_USD"

	symbolClash := other
	symbolClash.symbol = "EURUSD" // another entry's key

	equity := goodRaw("AAPL")
	equity.equityTicker = "AAPL"

	settlesElsewhere := good
	settlesElsewhere.settle = "EUR"

	timesTwo := good
	timesTwo.multiplier = rt(2_000_000)

	noMargin := good
	noMargin.rules.marginRate = num.Rate{}

	negativeMargin := good
	negativeMargin.rules.marginRate = rt(-1)

	minAboveMax := good
	minAboveMax.rules.minUnits, minAboveMax.rules.maxUnits = qty(10), qty(5)

	tests := []struct {
		name    string
		entries []marketConfig
		wantErr error
	}{
		{"no engine key", []marketConfig{func() marketConfig { c := good.config(t); c.key = ""; return c }()}, errCatalog},
		{"no instrument", []marketConfig{{key: "X"}}, errCatalog},
		{"a listing for another instrument", []marketConfig{mismatched}, errCatalog},
		{"one instrument under two keys", []marketConfig{good.config(t), sameIDOtherKey.config(t)}, errCatalog},
		{"one key twice", []marketConfig{good.config(t), good.config(t)}, errCatalog},
		{"a symbol claimed by two entries", []marketConfig{good.config(t), symbolClash.config(t)}, errCatalog},
		{"an equity", []marketConfig{equity.config(t)}, errUnsupportedInstrument},
		{"settlement is not the quote currency", []marketConfig{settlesElsewhere.config(t)}, errUnsupportedInstrument},
		{"a contract multiplier other than 1", []marketConfig{timesTwo.config(t)}, errUnsupportedInstrument},
		{"no margin rate", []marketConfig{noMargin.config(t)}, errCatalog},
		{"a negative margin rate", []marketConfig{negativeMargin.config(t)}, errCatalog},
		{"a minimum above the maximum", []marketConfig{minAboveMax.config(t)}, errCatalog},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := newCatalog(tt.entries...)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, c)
		})
	}

	_, err := newCatalog(good.config(t), other.config(t))
	require.NoError(t, err, "two different instruments are fine")
}

func TestNewCatalog_AMinimumOrMaximumNeedNotBeAMultipleOfTheStep(t *testing.T) {
	r := goodRaw("EURUSD")
	r.step = qty(300)
	r.rules.minUnits, r.rules.maxUnits = qty(100), qty(5000) // sizing rounds down to the step

	_, err := newCatalog(r.config(t))

	require.NoError(t, err)
}

func TestCatalog_LookupIsExactAndKeyForTranslatesProviderSymbols(t *testing.T) {
	// The engine key is EURUSD; this provider spells the pair EUR_USD.
	r := goodRaw("EURUSD")
	r.symbol = "EUR_USD"
	c, err := newCatalog(r.config(t))
	require.NoError(t, err)

	_, ok := c.lookup("EURUSD")
	assert.True(t, ok)
	_, ok = c.lookup("EUR_USD")
	assert.False(t, ok, "a provider spelling is not an engine key: no fallback")
	_, ok = c.lookup("eurusd")
	assert.False(t, ok)

	for _, symbol := range []string{"EURUSD", "EUR_USD"} {
		key, err := c.keyFor(symbol)
		require.NoError(t, err)
		assert.Equal(t, "EURUSD", key, symbol)
	}
	_, err = c.keyFor("EUR/USD")
	require.ErrorIs(t, err, errUnknownInstrument)
}

func barsOf(instrument string, closes ...int64) *marketData {
	md := &marketData{instrument: instrument, interval: "d1"}
	for i, p := range closes {
		md.data = append(md.data, bar{instrument: instrument, Time: testStart.AddDate(0, 0, i),
			ohlc: ohlc{o: px(p), h: px(p), l: px(p), c: px(p)}, availableAt: testStart.AddDate(0, 0, i+1)})
	}
	return md
}

func TestCatalog_NormalizeGivesTheEngineOneKeyPerInstrument(t *testing.T) {
	r := goodRaw("EURUSD")
	r.symbol = "EUR_USD"
	c, err := newCatalog(r.config(t))
	require.NoError(t, err)

	md := barsOf("EUR_USD", 110000, 110100)
	require.NoError(t, c.normalize(md))

	assert.Equal(t, "EURUSD", md.instrument)
	for _, b := range md.data {
		assert.Equal(t, "EURUSD", b.instrument)
	}
}

func TestCatalog_NormalizeRefusesWhatItCannotPlace(t *testing.T) {
	r := goodRaw("EURUSD")
	r.symbol = "EUR_USD"
	r.tick = "0.00005"
	c, err := newCatalog(r.config(t))
	require.NoError(t, err)

	require.ErrorIs(t, c.normalize(barsOf("AUDUSD", 110000)), errUnknownInstrument, "no permissive fallback")

	mixed := barsOf("EURUSD", 110000)
	mixed.data[0].instrument = "AUDUSD"
	require.Error(t, c.normalize(mixed), "a bar of another instrument in the set")

	require.Error(t, c.normalize(barsOf("EUR_USD", 110003)), "110003 is not a whole number of 0.00005 ticks")
	require.NoError(t, c.normalize(barsOf("EUR_USD", 110005)))
}

func TestCatalog_MarginDiffersByMarketAndRate(t *testing.T) {
	c := testMarkets()

	eur, err := c.marginFor("EURUSD", qty(10000), px(110000), usdCurrency)
	require.NoError(t, err)
	gbp, err := c.marginFor("GBPUSD", qty(10000), px(110000), usdCurrency)
	require.NoError(t, err)

	assert.Equal(t, usd(220), eur, "2%")
	assert.Equal(t, usd(550), gbp, "5%")
	assert.Panics(t, func() { _, _ = c.marginFor("NOPE", qty(1), px(1), usdCurrency) }, "an unknown market is a bug, not a zero")
}

// Two markets with their own tick, unit step, margin rate and limits are sized
// by their own rules, in one account.
func TestAccount_Size_UsesEachMarketsOwnRules(t *testing.T) {
	captureLogs(t)
	wide := testInstrument("WIDE")
	wide.marginRate = rateFivePercent
	wide.unitStep = qty(100)
	wide.minUnits = qty(100)
	cat := withMarket(t, testMarkets(), wide)
	a := newAccount("a", usd(10000), cat)

	eur := openOrderAt("EURUSD", long, eurusd1_10)
	require.NoError(t, a.size(eur, cents(0)))
	assert.Equal(t, qty(9090), eur.units)

	w := openOrderAt("WIDE", long, eurusd1_10)
	require.NoError(t, a.size(w, cents(0)))
	assert.Equal(t, qty(9000), w.units, "WIDE's step of 100")
}

func TestAccount_Size_FractionalUnitStep(t *testing.T) {
	captureLogs(t)
	half := testInstrument("HALF")
	half.unitStep = num.MustParseQuantity("0.5")
	half.minUnits = num.MustParseQuantity("1.5")
	half.maxUnits = num.MustParseQuantity("9000.25")
	cat := withMarket(t, testMarkets(), half)

	o := openOrderAt("HALF", long, eurusd1_10)
	require.NoError(t, newAccount("a", usd(10000), cat).size(o, cents(0)))
	assert.Equal(t, num.MustParseQuantity("9000"), o.units, "the maximum 9000.25 is rounded down to the 0.5 step")

	small := openOrderAt("HALF", long, eurusd1_10)
	err := newAccount("a", usd(1), cat).size(small, cents(0)) // 0.9 units: below the 1.5 minimum
	require.ErrorIs(t, err, errOrderTooSmall)
	assert.Contains(t, err.Error(), "minimum 1.5")
}

func TestAccount_Size_ANonTradableListingIsRefusedForOrders(t *testing.T) {
	data := testInstrument("DATAONLY")
	data.untradable = true
	cat := withMarket(t, testMarkets(), data)
	o := openOrderAt("DATAONLY", long, eurusd1_10)

	err := newAccount("a", usd(10000), cat).size(o, cents(0))

	require.ErrorIs(t, err, errNotTradable)
	assert.False(t, isRejection(err), "a configuration error, not a trading outcome")
	assert.True(t, o.units.IsZero())
}

func TestAccount_Size_ChecksTheMarketFromItsOwnCatalog(t *testing.T) {
	// Another run's markets never leak in: a market only the other catalog has
	// is unknown here.
	other := withMarket(t, testMarkets(), testInstrument("OTHERONLY"))
	o := openOrderAt("OTHERONLY", long, eurusd1_10)

	err := newAccount("a", usd(10000), testMarkets()).size(o, cents(0))

	require.ErrorIs(t, err, errUnknownInstrument)
	require.NoError(t, newAccount("a", usd(10000), other).size(openOrderAt("OTHERONLY", long, eurusd1_10), cents(0)))
}

// The spellings EURUSD and EUR_USD name one instrument. Normalized at the
// boundary they share one position queue, so a long and a short cannot be open
// at once under two spellings (the netting guarantee).
func TestBroker_OneInstrumentCannotHoldTwoPositionsUnderTwoSpellings(t *testing.T) {
	captureLogs(t)
	r := goodRaw("EURUSD")
	r.symbol = "EUR_USD"
	cat, err := newCatalog(r.config(t))
	require.NoError(t, err)
	br := newBroker("t", cat)
	act := br.addAccount("a", usd(10000))

	feed := barsOf("EUR_USD", 110000, 110000, 110000)
	require.NoError(t, cat.normalize(feed))
	long1 := openOrderAt("EURUSD", long, px(110000))
	long1.accountID = "a"
	require.NoError(t, br.submit(context.Background(), long1))

	// A second open, this time naming the provider's spelling, is not a second
	// instrument: it is unknown to the engine, never a second queue.
	short1 := openOrderAt("EUR_USD", short, px(110000))
	short1.accountID = "a"
	err = br.submit(context.Background(), short1)
	require.ErrorIs(t, err, errUnknownInstrument)

	require.NoError(t, br.onBar(context.Background(), feed.data[1]))
	assert.Len(t, act.positions, 1)
	assert.Contains(t, act.positions, "EURUSD")
	assert.NotContains(t, act.positions, "EUR_USD")
	assert.Equal(t, time.Time{}, act.closedOutAt)
}

func TestEmax_Init_NeedsThePriceTickOfItsInstrument(t *testing.T) {
	err := (&emax{short: 2, long: 4, stopPct: rateOnePercent}).init()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no price tick")
}

func TestEmaxEach_TakesEachInstrumentsTickFromTheCatalog(t *testing.T) {
	coarse := testInstrument("COARSE")
	coarse.tick = "0.0005"
	cat := withMarket(t, testMarkets(), coarse)
	s := &emaxEach{id: "pair", instruments: []string{"EURUSD", "COARSE"}, short: 2, long: 3, stopPct: rateOnePercent, markets: cat}
	require.NoError(t, s.init())

	assert.Equal(t, px(1), s.byInstrument["EURUSD"].tick)
	assert.Equal(t, px(50), s.byInstrument["COARSE"].tick)

	// 1% of 1.10337 is 0.0110337: floored to the market's own tick.
	entry := px(110337)
	fine, err := s.byInstrument["EURUSD"].stopFor(entry, long)
	require.NoError(t, err)
	assert.Equal(t, px(110337-1103), fine, "1103 points on a 1-point tick")
	wide, err := s.byInstrument["COARSE"].stopFor(entry, long)
	require.NoError(t, err)
	assert.Equal(t, px(110337-1100), wide, "1100 points: 22 ticks of 50")

	unknown := &emaxEach{id: "pair", instruments: []string{"NOPE"}, short: 2, long: 3, stopPct: rt(400), markets: cat}
	require.ErrorIs(t, unknown.init(), errUnknownInstrument)
}

// An open's prices must sit on its market's tick: an off-tick stop could
// execute at a price the market cannot trade, so it is refused, never rounded.
func TestAccount_Size_RefusesOrderPricesOffTheMarketsTick(t *testing.T) {
	captureLogs(t)
	coarse := testInstrument("COARSE")
	coarse.tick = "0.0005"
	cat := withMarket(t, testMarkets(), coarse)
	open := func(mutate func(*order)) *order {
		o := &order{Time: testStart, instrument: "COARSE", direction: long, action: open, orderType: marketOrder,
			price: px(110000), stopLoss: px(108900)} // 1.10000 and 1.08900 are whole 0.0005 ticks
		mutate(o)
		return o
	}

	require.NoError(t, newAccount("a", usd(10000), cat).size(open(func(*order) {}), cents(0)), "on the tick")
	require.NoError(t, newAccount("a", usd(10000), cat).size(open(func(o *order) { o.takeProfit = px(111000); o.trail = px(500) }), cents(0)))

	for name, mutate := range map[string]func(*order){
		"price":       func(o *order) { o.price = px(110001) },
		"stop loss":   func(o *order) { o.stopLoss = px(108999) },
		"take profit": func(o *order) { o.takeProfit = px(111001) },
		"trail":       func(o *order) { o.trail = px(501) },
	} {
		o := open(mutate)

		err := newAccount("a", usd(10000), cat).size(o, cents(0))

		require.ErrorIs(t, err, errInvalidOrder, name)
		assert.False(t, isRejection(err), "an off-tick price is a strategy bug, not a trading outcome")
		assert.True(t, o.units.IsZero(), name)
	}

	// The same prices are fine on a market with a one-point tick.
	fine := open(func(o *order) { o.instrument = "EURUSD"; o.stopLoss = px(108999) })
	require.NoError(t, newAccount("a", usd(10000), cat).size(fine, cents(0)))
}

// A bar the catalog has not normalized must not reach the engine: it would
// silently miss the queue and the position held under the engine key. The
// frame is refused before the clock or any state moves.
func TestBroker_OnFrame_RefusesABarThatIsNotUnderAnEngineKey(t *testing.T) {
	captureLogs(t)
	r := goodRaw("EURUSD")
	r.symbol = "EUR_USD"
	cat, err := newCatalog(r.config(t))
	require.NoError(t, err)
	br := newBroker("t", cat)
	act := br.addAccount("a", usd(10000))
	ctx := context.Background()

	pending := openOrderAt("EURUSD", long, px(110000))
	pending.accountID = "a"
	require.NoError(t, br.submit(ctx, pending))
	require.NoError(t, br.onBar(ctx, barsOf("EURUSD", 110000, 110000).data[1]))
	require.Contains(t, act.positions, "EURUSD")
	held := act.positions["EURUSD"].markPrice
	clock := br.clock
	queued := len(br.orders["EURUSD"])

	unnormalized := barsOf("EUR_USD", 111000, 111000).data[1]
	err = br.onBar(ctx, unnormalized)

	require.ErrorIs(t, err, errUnknownInstrument)
	assert.Contains(t, err.Error(), "normalize")
	assert.Equal(t, clock, br.clock, "the clock did not advance")
	assert.Equal(t, held, act.positions["EURUSD"].markPrice, "the position was not marked")
	assert.Len(t, br.orders["EURUSD"], queued, "the queues were not touched")

	// An unknown instrument is refused the same way, and so is a price off the tick.
	unknown := barsOf("AUDUSD", 100000, 100000).data[1]
	require.ErrorIs(t, br.onBar(ctx, unknown), errUnknownInstrument)
	off := barsOf("EURUSD", 100000, 100000).data[1]
	off.c = num.MustParsePrice("1.000001")
	require.Error(t, br.onBar(ctx, off))
	assert.Equal(t, clock, br.clock)

	// The same bar, normalized, is processed.
	md := barsOf("EUR_USD", 111000, 111000, 111000)
	require.NoError(t, cat.normalize(md))
	require.NoError(t, br.onBar(ctx, md.data[2]))
	assert.Equal(t, px(111000), act.positions["EURUSD"].markPrice)
}

func TestCatalog_NormalizeChangesNothingWhenAnyBarIsRefused(t *testing.T) {
	r := goodRaw("EURUSD")
	r.symbol = "EUR_USD"
	c, err := newCatalog(r.config(t))
	require.NoError(t, err)
	md := barsOf("EUR_USD", 110000, 110000, 110000)
	md.data[2].c = num.MustParsePrice("1.100001") // the last bar is off the tick

	require.Error(t, c.normalize(md))

	assert.Equal(t, "EUR_USD", md.instrument, "the set is left as it was")
	for _, b := range md.data {
		assert.Equal(t, "EUR_USD", b.instrument)
	}
}
