package numericstudy

// Asset is one representative instrument in the validation matrix.
type Asset struct {
	Class    string // asset class, e.g. "FX", "Futures"
	Symbol   string // representative symbol
	Price    string // representative price as decimal text
	TickSize string // instrument tick size as decimal text
	Decimals int    // fraction digits the quotation actually requires
	Note     string // non-obvious representation concerns
}

// Assets is the representative matrix from issue #33.  It deliberately
// includes the extremes — the highest-priced and smallest-priced values we
// intend to support — because those, not the typical values, decide the scale.
var Assets = []Asset{
	{
		Class: "FX", Symbol: "EUR/USD",
		Price: "1.08473", TickSize: "0.00001", Decimals: 5,
	},
	{
		Class: "FX", Symbol: "USD/JPY",
		Price: "147.325", TickSize: "0.001", Decimals: 3,
		Note: "JPY pairs quote to 3 decimals; the pip is 0.01, not 0.0001",
	},
	{
		Class: "Equity", Symbol: "SPY",
		Price: "700.12", TickSize: "0.01", Decimals: 2,
	},
	{
		Class: "Equity", Symbol: "BRK.A",
		Price: "750000.00", TickSize: "0.01", Decimals: 2,
		Note: "highest-priced listed equity; sets the practical range floor",
	},
	{
		Class: "Equity", Symbol: "SUB-PENNY",
		Price: "0.0001", TickSize: "0.0001", Decimals: 4,
		Note: "sub-penny quotes are permitted below $1.00",
	},
	{
		Class: "Futures", Symbol: "ES",
		Price: "6250.25", TickSize: "0.25", Decimals: 2,
		Note: "tick is 0.25 index points, a multiple of the 0.01 increment",
	},
	{
		Class: "Futures", Symbol: "ZN",
		Price: "110.515625", TickSize: "0.015625", Decimals: 6,
		Note: "10-yr note quotes in 32nds and halves of 32nds; " +
			"1/64 = 0.015625 exactly, so decimal form needs 6 decimals",
	},
	{
		Class: "Crypto", Symbol: "BTC/USD",
		Price: "150000.12345678", TickSize: "0.00000001", Decimals: 8,
		Note: "satoshi precision is the widest decimal requirement we accept",
	},
	{
		Class: "Crypto", Symbol: "SHIB/USD",
		Price: "0.00000001", TickSize: "0.00000001", Decimals: 8,
		Note: "smallest representable non-zero value at 8 decimals",
	},
	{
		Class: "Rate", Symbol: "FINANCING",
		Price: "0.000125", TickSize: "0.000001", Decimals: 6,
		Note: "small financing/fee rates share the Price parsing path",
	},
}

// Notional is an intermediate-arithmetic case: a realistic largest position
// whose Price x Quantity product must not overflow int64 before the descale.
type Notional struct {
	Name     string
	Price    string // decimal text
	Quantity int64  // whole units; fractional quantity is #36's problem
	Why      string
}

// Notionals are the multiplication cases exercised by the headroom study.
var Notionals = []Notional{
	{
		Name: "BRK.A block", Price: "750000.00", Quantity: 10_000,
		Why: "highest price x an institutional block",
	},
	{
		Name: "FX 10M notional", Price: "1.08473", Quantity: 10_000_000,
		Why: "typical institutional FX ticket size",
	},
	{
		Name: "FX 1B notional", Price: "1.08473", Quantity: 1_000_000_000,
		Why: "extreme FX notional; upper bound on realistic size",
	},
	{
		Name: "BTC 1k coins", Price: "150000.12345678", Quantity: 1_000,
		Why: "widest precision x a large position",
	},
	{
		Name: "SHIB 1e12 units", Price: "0.00000001", Quantity: 1_000_000_000_000,
		Why: "smallest price x the huge unit counts such tokens trade in",
	},
}

// Rates are multiplier cases where BOTH operands are scaled, so the product
// carries the scale twice and must be descaled once.  This double-scaled
// intermediate, not the result, is the binding constraint on scale choice.
var Rates = []struct {
	Name string
	Rate string // decimal text
	Why  string
}{
	{Name: "commission 0.02%", Rate: "0.0002", Why: "per-fill fee applied to notional"},
	{Name: "financing 3.75%", Rate: "0.0375", Why: "overnight carry on position value"},
	{Name: "FX cross 1.08473", Rate: "1.08473", Why: "currency conversion of a money value"},
	{Name: "JPY cross 147.325", Rate: "147.325", Why: "large-magnitude conversion rate"},
}

// UnsupportedQuotations records provider quotation formats that this
// representation cannot ingest directly.  Each must be normalized to plain
// decimal text in a provider adapter before reaching Price.
var UnsupportedQuotations = []struct {
	Format  string
	Example string
	Decimal string
	Reason  string
}{
	{
		Format:  "Treasury 32nds (dash)",
		Example: "110-16",
		Decimal: "110.5",
		Reason:  "not decimal text; '-' is a fraction separator, not a sign",
	},
	{
		Format:  "Treasury 32nds plus halves/quarters",
		Example: "110-16.5",
		Decimal: "110.515625",
		Reason:  "fraction of a 32nd; expands to 64ths, needing 6 decimals",
	},
	{
		Format:  "Grain fractions in eighths",
		Example: "575'6",
		Decimal: "575.75",
		Reason:  "apostrophe-delimited eighths of a cent",
	},
	{
		Format:  "Scientific notation",
		Example: "1.5e-8",
		Decimal: "0.000000015",
		Reason:  "exponent form is rejected; expand before parsing",
	},
	{
		Format:  "Grouped thousands",
		Example: "750,000.00",
		Decimal: "750000.00",
		Reason:  "locale grouping is a display concern, not an exact input",
	},
}
