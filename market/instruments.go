package market

type Instrument struct {
	Name                string
	BaseCurrency        string
	QuoteCurrency       string
	PipLocation         int
	TradeUnitsPrecision int
	MinimumTradeSize    float64
	MarginRate          float64
}

var Instruments = map[string]Instrument{
	"EUR_USD": {
		Name:                "EUR_USD",
		BaseCurrency:        "EUR",
		QuoteCurrency:       "USD",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          0.02,
	},
	"USD_JPY": {
		Name:                "USD_JPY",
		BaseCurrency:        "USD",
		QuoteCurrency:       "JPY",
		PipLocation:         -2,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          0.02,
	},
}

func init() {
	Instruments["EURUSD"] = Instruments["EUR_USD"]
	Instruments["USDJPY"] = Instruments["USD_JPY"]
}
