package accountactivity

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

var (
	usd = num.MustParseCurrency("USD")
	at  = time.Date(2024, 3, 5, 14, 30, 0, 0, time.UTC)
)

func money(s string) num.Money { return num.MustParseMoney(s, usd) }

func must[T any](v Value[T], err error) Value[T] {
	if err != nil {
		panic(err)
	}
	return v
}

func eurusd() instrument.ID {
	return instrument.CurrencyPairID(num.MustParseCurrency("EUR"), usd)
}

// oandaFill is an OANDA ORDER_FILL: price, quantity, realized P/L and the
// broker-reported half-spread cost, financing and commission.
func oandaFill() Activity {
	return Activity{
		Provider: "oanda", Environment: EnvironmentPractice,
		ProviderAccountID: "101-001-1234567-001", ProviderActivityID: "6360",
		ProviderType: "ORDER_FILL",
		Kind:         KindFill,
		OccurredAt:   at,
		ImportedAt:   at.Add(time.Hour),
		Instrument:   eurusd(), Side: order.Buy,
		Quantity:      Reported(num.MustParseQuantity("1000")),
		Price:         Reported(num.MustParsePrice("1.08532")),
		BrokerOrderID: "6359",
		TradeLinks:    []TradeLink{{"6360", TradeRoleOpened}},
		RealizedPL:    Reported(money("0")),
		SpreadCost:    must(ReportedFrom(money("-0.25"), "halfSpreadCost")),
		Financing:     Reported(money("0")),
		Commission:    Reported(money("0")),
	}
}

func alpacaFill() Activity {
	return Activity{
		Provider: "alpaca", Environment: EnvironmentLive,
		ProviderAccountID: "acct-9", ProviderActivityID: "20240305143000000::abc",
		ProviderType: "FILL", Kind: KindFill, OccurredAt: at, ImportedAt: at,
		Instrument: instrument.EquityID("NASDAQ", "AAPL"), Side: order.Sell,
		Quantity:   Reported(num.MustParseQuantity("10")),
		Price:      Reported(num.MustParsePrice("182.50")),
		Commission: Reported(money("-0.02")),
	}
}

func TestValidate_RepresentativeActivitiesAreValid(t *testing.T) {
	cases := map[string]Activity{
		"oanda fill":   oandaFill(),
		"alpaca fill":  alpacaFill(),
		"financing":    {Provider: "oanda", Environment: EnvironmentLive, ProviderAccountID: "a", ProviderActivityID: "1", ProviderType: "DAILY_FINANCING", Kind: KindFinancing, OccurredAt: at, ImportedAt: at, Financing: Reported(money("-1.37"))},
		"fee":          {Provider: "alpaca", Environment: EnvironmentLive, ProviderAccountID: "a", ProviderActivityID: "2", ProviderType: "FEE", Kind: KindFee, OccurredAt: at, ImportedAt: at, OtherFees: Reported(money("-3"))},
		"deposit":      {Provider: "oanda", Environment: EnvironmentLive, ProviderAccountID: "a", ProviderActivityID: "3", ProviderType: "TRANSFER_FUNDS", Kind: KindDeposit, OccurredAt: at, ImportedAt: at, Cash: Reported(money("500"))},
		"withdrawal":   {Provider: "oanda", Environment: EnvironmentLive, ProviderAccountID: "a", ProviderActivityID: "4", ProviderType: "TRANSFER_FUNDS", Kind: KindWithdrawal, OccurredAt: at, ImportedAt: at, Cash: Reported(money("-200"))},
		"dividend":     {Provider: "alpaca", Environment: EnvironmentLive, ProviderAccountID: "a", ProviderActivityID: "5", ProviderType: "DIV", Kind: KindDividend, OccurredAt: at, ImportedAt: at, Instrument: instrument.EquityID("NASDAQ", "AAPL"), Cash: Reported(money("2.30"))},
		"other":        {Provider: "oanda", Environment: EnvironmentLive, ProviderAccountID: "a", ProviderActivityID: "9", ProviderType: "MARKET_ORDER", Kind: KindOther, OccurredAt: at, ImportedAt: at, BrokerOrderID: "9", Instrument: eurusd()},
		"adjustment":   {Provider: "oanda", Environment: EnvironmentLive, ProviderAccountID: "a", ProviderActivityID: "6", ProviderType: "ADJUSTMENT", Kind: KindAdjustment, OccurredAt: at, ImportedAt: at, Cash: Reported(money("-0.01"))},
		"reported 0":   {Provider: "oanda", Environment: EnvironmentLive, ProviderAccountID: "a", ProviderActivityID: "7", ProviderType: "DAILY_FINANCING", Kind: KindFinancing, OccurredAt: at, ImportedAt: at, Financing: Reported(money("0"))},
		"derived cost": {Provider: "oanda", Environment: EnvironmentLive, ProviderAccountID: "a", ProviderActivityID: "8", ProviderType: "ORDER_FILL", Kind: KindFill, OccurredAt: at, ImportedAt: at, Instrument: eurusd(), Side: order.Buy, Quantity: Reported(num.MustParseQuantity("1")), Price: Reported(num.MustParsePrice("1.1")), SpreadCost: must(Derived(money("-0.1"), "ask-bid at fill"))},
	}
	for name, a := range cases {
		t.Run(name, func(t *testing.T) { assert.NoError(t, a.Validate()) })
	}
}

func TestValidate_RejectsBrokenIdentity(t *testing.T) {
	mutate := map[string]func(*Activity){
		"empty provider":     func(a *Activity) { a.Provider = "" },
		"uppercase provider": func(a *Activity) { a.Provider = "OANDA" },
		"no environment":     func(a *Activity) { a.Environment = EnvironmentUnknown },
		"no account id":      func(a *Activity) { a.ProviderAccountID = "" },
		"no activity id":     func(a *Activity) { a.ProviderActivityID = "" },
		"no provider type":   func(a *Activity) { a.ProviderType = "" },
		"no kind":            func(a *Activity) { a.Kind = KindUnknown },
		"no occurred_at":     func(a *Activity) { a.OccurredAt = time.Time{} },
		"half account ref":   func(a *Activity) { a.Account.Broker = "OANDA" },
	}
	for name, f := range mutate {
		t.Run(name, func(t *testing.T) {
			a := oandaFill()
			f(&a)
			assert.ErrorIs(t, a.Validate(), ErrInvalidActivity)
		})
	}
}

func TestValidate_PerKindRules(t *testing.T) {
	mutate := map[string]func(*Activity){
		"fill without instrument": func(a *Activity) { a.Instrument = instrument.ID{} },
		"fill without side":       func(a *Activity) { a.Side = 0 },
		"fill without quantity":   func(a *Activity) { a.Quantity = Value[num.Quantity]{} },
		"fill without price":      func(a *Activity) { a.Price = Value[num.Price]{} },
		"fill zero quantity":      func(a *Activity) { a.Quantity = Reported(num.MustParseQuantity("0")) },
		"fill with cash":          func(a *Activity) { a.Cash = Reported(money("1")) },
		"fill mixed currency":     func(a *Activity) { a.SpreadCost = Reported(num.MustParseMoney("-0.1", num.MustParseCurrency("EUR"))) },
		"zero execution price":    func(a *Activity) { a.Price = Reported(num.MustParsePrice("0")) },
		"trade link without id":   func(a *Activity) { a.TradeLinks = []TradeLink{{"", TradeRoleOpened}} },
		"trade link without role": func(a *Activity) { a.TradeLinks = []TradeLink{{"1", TradeRoleUnknown}} },
		"duplicate trade link":    func(a *Activity) { a.TradeLinks = []TradeLink{{"1", TradeRoleClosed}, {"1", TradeRoleClosed}} },
		"money without currency":  func(a *Activity) { a.Commission = Reported(num.Money{}) },
	}
	for name, f := range mutate {
		t.Run(name, func(t *testing.T) {
			a := oandaFill()
			f(&a)
			assert.ErrorIs(t, a.Validate(), ErrInvalidActivity)
		})
	}

	nonFill := map[string]Activity{
		"financing without amount": {Kind: KindFinancing},
		"financing with price":     {Kind: KindFinancing, Financing: Reported(money("1")), Price: Reported(num.MustParsePrice("1"))},
		"fee without any fee":      {Kind: KindFee},
		"fee with financing":       {Kind: KindFee, OtherFees: Reported(money("-1")), Financing: Reported(money("1"))},
		"deposit negative":         {Kind: KindDeposit, Cash: Reported(money("-5"))},
		"deposit zero":             {Kind: KindDeposit, Cash: Reported(money("0"))},
		"withdrawal positive":      {Kind: KindWithdrawal, Cash: Reported(money("5"))},
		"deposit without cash":     {Kind: KindDeposit},
		"deposit with trade links": {Kind: KindDeposit, Cash: Reported(money("1")), TradeLinks: []TradeLink{{"1", TradeRoleOpened}}},
		"other with economics":     {Kind: KindOther, Cash: Reported(money("1"))},
		"other with a fill price":  {Kind: KindOther, Price: Reported(num.MustParsePrice("1"))},
		"adjustment with side":     {Kind: KindAdjustment, Cash: Reported(money("1")), Side: order.Buy},
	}
	for name, a := range nonFill {
		t.Run(name, func(t *testing.T) {
			a.Provider, a.Environment, a.ProviderAccountID, a.ProviderActivityID = "oanda", EnvironmentLive, "a", "1"
			a.ProviderType, a.OccurredAt, a.ImportedAt = "X", at, at
			assert.ErrorIs(t, a.Validate(), ErrInvalidActivity)
		})
	}
}

func TestSlippage_RequiresABenchmarkWithASource(t *testing.T) {
	slip := must(Derived(num.MustParseRate("-0.00012"), "execution price - benchmark, account sign"))
	cost := must(Derived(money("-0.12"), "slippage * quantity"))
	bench := must(ReportedFrom(num.MustParsePrice("1.08520"), "fullPrice.ask"))

	t.Run("with a sourced benchmark", func(t *testing.T) {
		a := oandaFill()
		a.Benchmark, a.Slippage, a.SlippageCost = bench, slip, cost
		assert.NoError(t, a.Validate())
	})
	t.Run("slippage may be absent entirely", func(t *testing.T) {
		assert.NoError(t, oandaFill().Validate())
	})
	t.Run("slippage without a benchmark", func(t *testing.T) {
		a := oandaFill()
		a.Slippage = slip
		assert.ErrorContains(t, a.Validate(), "benchmark")
	})
	t.Run("slippage cost without a benchmark", func(t *testing.T) {
		a := oandaFill()
		a.SlippageCost = cost
		assert.ErrorContains(t, a.Validate(), "benchmark")
	})
	t.Run("benchmark reported without a source", func(t *testing.T) {
		a := oandaFill()
		a.Benchmark = Reported(num.MustParsePrice("1.0852"))
		a.Slippage = slip
		assert.ErrorContains(t, a.Validate(), "source")
	})
	t.Run("slippage can never be broker-reported", func(t *testing.T) {
		a := oandaFill()
		a.Benchmark, a.Slippage = bench, Reported(num.MustParseRate("0.0001"))
		assert.ErrorContains(t, a.Validate(), "derived by Trader")
	})
	t.Run("slippage on a non-fill is rejected", func(t *testing.T) {
		a := Activity{Provider: "oanda", Environment: EnvironmentLive, ProviderAccountID: "a", ProviderActivityID: "1", ProviderType: "X", Kind: KindDeposit, OccurredAt: at, ImportedAt: at, Cash: Reported(money("1")), Slippage: slip, Benchmark: bench}
		assert.Error(t, a.Validate())
	})
}

func TestValue_UnknownReportedAndDerivedZeroAreDistinct(t *testing.T) {
	var unknown Value[num.Money]
	reported := Reported(money("0"))
	derived := must(Derived(money("0"), "sum of components"))

	for _, v := range []Value[num.Money]{unknown, reported, derived} {
		_, _ = v.Get()
	}
	_, ok := unknown.Get()
	assert.False(t, ok)
	assert.False(t, unknown.Known())
	got, ok := reported.Get()
	assert.True(t, ok)
	assert.True(t, got.IsZero())
	assert.Equal(t, BasisReported, reported.Basis())
	assert.Equal(t, BasisDerived, derived.Basis())
	assert.Equal(t, "sum of components", derived.Source())
	assert.NotEqual(t, reported, derived)
	assert.NotEqual(t, unknown, reported)
}

func TestValue_ConstructorsRequireTheirSource(t *testing.T) {
	_, err := Derived(1, "")
	assert.ErrorIs(t, err, ErrInvalidValue)
	_, err = Derived(1, "  ")
	assert.ErrorIs(t, err, ErrInvalidValue)
	_, err = ReportedFrom(1, "")
	assert.ErrorIs(t, err, ErrInvalidValue)
}

func TestKey_IsProviderNativeAndEnvironmentSensitive(t *testing.T) {
	a := oandaFill()
	b := oandaFill()
	b.ImportedAt = at.Add(48 * time.Hour)
	assert.Equal(t, a.Key(), b.Key(), "ImportedAt is not part of identity")

	live := oandaFill()
	live.Environment = EnvironmentLive
	assert.NotEqual(t, a.Key(), live.Key(), "practice and live never collide")

	m := map[Key]int{a.Key(): 1}
	assert.Equal(t, 1, m[b.Key()])
	assert.Equal(t, "oanda/practice/101-001-1234567-001/6360", a.Key().String())
}

func TestSameRecord_IgnoresImportTimeOnly(t *testing.T) {
	a := oandaFill()

	later := oandaFill()
	later.ImportedAt = at.Add(72 * time.Hour)
	assert.True(t, a.SameRecord(later), "re-importing later is the same record")

	tz := oandaFill()
	tz.OccurredAt = at.In(time.FixedZone("x", 3600))
	assert.True(t, a.SameRecord(tz), "the same instant in another zone is the same record")

	changed := oandaFill()
	changed.Price = Reported(num.MustParsePrice("1.08533"))
	assert.False(t, a.SameRecord(changed))

	reclassified := oandaFill()
	reclassified.SpreadCost = must(Derived(money("-0.25"), "estimate"))
	assert.False(t, a.SameRecord(reclassified), "reported vs derived is a different record")
}

func TestKindAndEnvironmentStrings(t *testing.T) {
	assert.False(t, KindUnknown.Valid())
	assert.False(t, Kind(200).Valid())
	assert.Equal(t, "withdrawal", KindWithdrawal.String())
	assert.Equal(t, "unknown", Kind(200).String())
	assert.True(t, EnvironmentPractice.Valid())
	assert.False(t, EnvironmentUnknown.Valid())
	assert.Equal(t, "live", EnvironmentLive.String())
	assert.Equal(t, "reported", BasisReported.String())
	assert.Equal(t, "unknown", BasisUnknown.String())
}

func TestValidate_ErrorsNeverEchoAmounts(t *testing.T) {
	a := oandaFill()
	a.SpreadCost = Reported(num.MustParseMoney("-123456.78", num.MustParseCurrency("EUR")))
	err := a.Validate()
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "123456")
}

// One OANDA ORDER_FILL can open a trade, reduce another and close several;
// every link survives with its role and the provider's order.
func TestTradeLinks_PreserveMultiTradeFills(t *testing.T) {
	a := oandaFill()
	a.TradeLinks = []TradeLink{
		{"6360", TradeRoleOpened},
		{"6301", TradeRoleReduced},
		{"6288", TradeRoleClosed},
		{"6290", TradeRoleClosed},
	}
	require.NoError(t, a.Validate())
	assert.Len(t, a.TradeLinks, 4)

	same := a
	same.TradeLinks = append([]TradeLink(nil), a.TradeLinks...)
	assert.True(t, a.SameRecord(same))

	dropped := a
	dropped.TradeLinks = a.TradeLinks[:3]
	assert.False(t, a.SameRecord(dropped), "a lost link is a different record")

	none := oandaFill()
	none.TradeLinks = nil
	assert.NoError(t, none.Validate(), "links are optional")
	empty := none
	empty.TradeLinks = []TradeLink{}
	assert.True(t, none.SameRecord(empty), "nil and empty are the same")
	assert.Equal(t, "closed", TradeRoleClosed.String())
	assert.False(t, TradeRoleUnknown.Valid())
}

// Money follows the account's point of view: an adverse cost is negative,
// and slippage in price units shares that sign.
func TestSlippage_SignConventionIsAccountCentric(t *testing.T) {
	bench := must(ReportedFrom(num.MustParsePrice("1.08520"), "fullPrice.ask"))
	build := func(slip, cost string) Activity {
		a := oandaFill() // a buy filled at 1.08532, above the 1.08520 benchmark
		a.Benchmark = bench
		a.Slippage = must(Derived(num.MustParseRate(slip), "execution price - benchmark, account sign"))
		a.SlippageCost = must(Derived(money(cost), "slippage * quantity"))
		return a
	}
	assert.NoError(t, build("-0.00012", "-0.12").Validate(), "adverse: both negative")
	assert.NoError(t, build("0.00012", "0.12").Validate(), "favourable: both positive")
	assert.NoError(t, build("0", "0").Validate(), "neutral")
	assert.NoError(t, build("-0.00012", "0").Validate(), "cost may round to zero")
	assert.ErrorContains(t, build("-0.00012", "0.12").Validate(), "opposite signs")
	assert.ErrorContains(t, build("0.00012", "-0.12").Validate(), "opposite signs")
}

func TestPrices_MustBePositive(t *testing.T) {
	a := oandaFill()
	a.Benchmark = must(ReportedFrom(num.MustParsePrice("0"), "fullPrice.ask"))
	a.Slippage = must(Derived(num.MustParseRate("-0.1"), "x"))
	assert.ErrorContains(t, a.Validate(), "benchmark price must be positive")
}
