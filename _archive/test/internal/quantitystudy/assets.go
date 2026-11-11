package quantitystudy

// AssetQuantity is one representative quantity requirement from #36.
type AssetQuantity struct {
	Class    string
	Symbol   string
	Quantity string // decimal text
	Decimals int    // fraction digits the quantity actually requires
	Integral bool   // instrument accepts whole units only
	Note     string
}

// Quantities is the representative matrix.  It deliberately carries both
// extremes — satoshi precision and trillion-unit token positions — because the
// tension between them, not the ordinary cases, decides the scale.
var Quantities = []AssetQuantity{
	{
		Class: "FX", Symbol: "EUR/USD", Quantity: "1", Decimals: 0,
		Note: "single unit; the smallest sane FX ticket",
	},
	{
		Class: "FX", Symbol: "EUR/USD", Quantity: "10000", Decimals: 0,
		Note: "micro lot",
	},
	{
		Class: "FX", Symbol: "EUR/USD", Quantity: "10000000", Decimals: 0,
		Note: "institutional ticket",
	},
	{
		Class: "FX", Symbol: "EUR/USD", Quantity: "1000000000", Decimals: 0,
		Note: "extreme FX notional; upper bound on realistic size",
	},
	{
		Class: "Equity", Symbol: "SPY", Quantity: "1", Decimals: 0,
		Note: "whole share",
	},
	{
		Class: "Equity", Symbol: "SPY", Quantity: "0.5", Decimals: 1,
		Note: "fractional shares are widely supported",
	},
	{
		Class: "Equity", Symbol: "SPY", Quantity: "0.000001", Decimals: 6,
		Note: "finest fractional-share precision brokers quote",
	},
	{
		Class: "Futures", Symbol: "ES", Quantity: "1", Decimals: 0, Integral: true,
		Note: "contracts are integral only",
	},
	{
		Class: "Futures", Symbol: "ES", Quantity: "3", Decimals: 0, Integral: true,
	},
	{
		Class: "Futures", Symbol: "ES", Quantity: "10000", Decimals: 0, Integral: true,
		Note: "large but realistic contract count",
	},
	{
		Class: "Crypto", Symbol: "BTC", Quantity: "1", Decimals: 0,
	},
	{
		Class: "Crypto", Symbol: "BTC", Quantity: "0.00000001", Decimals: 8,
		Note: "one satoshi; the finest quantity Trader intends to support",
	},
	{
		Class: "Token", Symbol: "SHIB", Quantity: "1000000000000", Decimals: 0,
		Note: "trillion-unit position; upper-range pressure from #36",
	},
	{
		Class: "Boundary", Symbol: "ZERO", Quantity: "0", Decimals: 0,
		Note: "representable; only order construction rejects it",
	},
}

// The two requirements that cannot coexist in a scaled int64.  Naming them
// keeps the frontier analysis anchored to the matrix rather than to constants
// buried in a test.
const (
	FinestQuantity  = "0.00000001"    // one satoshi: 8 decimals
	LargestQuantity = "1000000000000" // one trillion whole units
)

// Increment is a representative instrument rule set, expressed in decimal text
// so it can be scaled into whichever candidate is under test.
type Increment struct {
	Symbol       string
	Increment    string // smallest tradable step
	Minimum      string
	Maximum      string // empty means unbounded
	IntegralOnly bool
	Note         string
}

// Increments exercise the separation between representation scale and
// instrument rules: every one of these is an instrument property, none is a
// property of how the number is stored.
var Increments = []Increment{
	{
		Symbol: "EUR/USD", Increment: "1", Minimum: "1", Maximum: "100000000",
		Note: "FX trades in whole units with a broker cap",
	},
	{
		Symbol: "SPY", Increment: "0.000001", Minimum: "0.000001",
		Note: "fractional shares down to six decimals",
	},
	{
		Symbol: "ES", Increment: "1", Minimum: "1", Maximum: "10000",
		IntegralOnly: true,
		Note:         "contracts: integral only, and the increment agrees",
	},
	{
		Symbol: "BTC", Increment: "0.00000001", Minimum: "0.0001",
		Note: "satoshi increment, but a larger exchange minimum",
	},
	{
		Symbol: "SHIB", Increment: "1", Minimum: "1000",
		Note: "whole tokens, large minimum",
	},
}

// NotionalCase pairs a price with a quantity to measure the double-scaled
// Price x Quantity intermediate.
type NotionalCase struct {
	Name     string
	Price    string // decimal text, at PriceScale
	Quantity string // decimal text, at the candidate Quantity scale
	Why      string
}

// NotionalCases are the multiplication cases for check 10 of #36.
var NotionalCases = []NotionalCase{
	{
		Name: "BRK.A block", Price: "750000.00", Quantity: "10000",
		Why: "highest price x an institutional block",
	},
	{
		Name: "FX 10M notional", Price: "1.08473", Quantity: "10000000",
		Why: "typical institutional FX ticket",
	},
	{
		Name: "FX 1B notional", Price: "1.08473", Quantity: "1000000000",
		Why: "extreme FX notional",
	},
	{
		Name: "BTC 1k coins", Price: "150000.12345678", Quantity: "1000",
		Why: "widest price precision x a large position",
	},
	{
		Name: "SPY fractional", Price: "700.12", Quantity: "0.5",
		Why: "fractional quantity, ordinary price",
	},
	{
		Name: "SHIB 1e12 units", Price: "0.00000001", Quantity: "1000000000000",
		Why: "smallest price x the largest unit count",
	},
}
