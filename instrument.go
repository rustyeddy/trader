package trader

import "github.com/rustyeddy/trader/num"

var instruments = map[string]*instrument{
	"EURUSD": {
		symbol:     "EURUSD",
		quote:      usdCurrency,
		marginRate: rateTwoPercent, // 50:1 leverage
		minUnits:   oneUnit,
		unitStep:   oneUnit,
	},
	"GBPUSD": {
		symbol:     "GBPUSD",
		quote:      usdCurrency,
		marginRate: rateFivePercent, // 20:1 leverage
		minUnits:   oneUnit,
		unitStep:   oneUnit,
	},
}

type instrument struct {
	symbol     string
	quote      num.Currency // what its prices and profit are denominated in
	marginRate num.Rate

	minUnits num.Quantity // smallest tradable size; zero means one unit
	maxUnits num.Quantity // largest order size; zero means no limit
	unitStep num.Quantity // sizes are whole multiples of this; zero means one unit
}
