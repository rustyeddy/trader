package market

import "github.com/rustyeddy/trader/types"

type Instrument struct {
	Name                string
	BaseCurrency        string
	QuoteCurrency       string
	PipLocation         int
	TradeUnitsPrecision int
	MinimumTradeSize    types.Units
	MarginRate          types.Rate
}

var InstrumentList = []string{
	"EUR_USD",
	"GBP_USD",
	"USD_JPY",
	"USD_CHF",
	"AUD_USD",
	"USD_CAD",
	"NZD_USD",
	"XAU_USD",
}

var Instruments = map[string]*Instrument{
	"EUR_USD": {
		Name:                "EUR_USD",
		BaseCurrency:        "EUR",
		QuoteCurrency:       "USD",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          types.Rate(20_000), // 2% (50:1)
	},
	"GBP_USD": {
		Name:                "GBP_USD",
		BaseCurrency:        "GBP",
		QuoteCurrency:       "USD",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          types.Rate(20_000),
	},
	"USD_JPY": {
		Name:                "USD_JPY",
		BaseCurrency:        "USD",
		QuoteCurrency:       "JPY",
		PipLocation:         -2,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          types.Rate(20_000),
	},
	"USD_CHF": {
		Name:                "USD_CHF",
		BaseCurrency:        "USD",
		QuoteCurrency:       "CHF",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          types.Rate(20_000),
	},
	"AUD_USD": {
		Name:                "AUD_USD",
		BaseCurrency:        "AUD",
		QuoteCurrency:       "USD",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          types.Rate(20_000),
	},
	"USD_CAD": {
		Name:                "USD_CAD",
		BaseCurrency:        "USD",
		QuoteCurrency:       "CAD",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          types.Rate(20_000),
	},
	"NZD_USD": {
		Name:                "NZD_USD",
		BaseCurrency:        "NZD",
		QuoteCurrency:       "USD",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          types.Rate(20_000),
	},
	"XAU_USD": {
		Name:                "XAU_USD",
		BaseCurrency:        "XAU",
		QuoteCurrency:       "USD",
		PipLocation:         -2, // Gold pip = 0.01
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          types.Rate(50_000), // 5% (20:1 typical retail gold)
	},
}
