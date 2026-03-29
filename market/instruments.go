package market

import (
	"math"
	"strings"

	"github.com/rustyeddy/trader/types"
)

type Symbol string

const (
	EUR_USD Symbol = "EUR_USD"
	GBP_USD Symbol = "GBP_USD"
	USD_JPY Symbol = "USD_JPY"
	USD_CHF Symbol = "USD_CHF"
	AUD_USD Symbol = "AUD_USD"
	USD_CAD Symbol = "USD_CAD"
	NZD_USD Symbol = "NZD_USD"
)

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
	"EURUSD",
	"GBPUSD",
	"USDJPY",
	"USDCHF",
	"AUDUSD",
	"USDCAD",
	"NZDUSD",
}

var InstrumentList_ = []string{
	"EUR_USD",
	"GBP_USD",
	"USD_JPY",
	"USD_CHF",
	"AUD_USD",
	"USD_CAD",
	"NZD_USD",
}

var Instruments = map[string]*Instrument{
	"EURUSD": {
		Name:                "EURUSD",
		BaseCurrency:        "EUR",
		QuoteCurrency:       "USD",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          types.Rate(20_000), // 2% (50:1)
	},
	"GBPUSD": {
		Name:                "GBPUSD",
		BaseCurrency:        "GBP",
		QuoteCurrency:       "USD",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          types.Rate(20000),
	},
	"USDJPY": {
		Name:                "USDJPY",
		BaseCurrency:        "USD",
		QuoteCurrency:       "JPY",
		PipLocation:         -2,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          types.Rate(20_000),
	},
	"USDCHF": {
		Name:                "USDCHF",
		BaseCurrency:        "USD",
		QuoteCurrency:       "CHF",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          types.Rate(20_000),
	},
	"AUDUSD": {
		Name:                "AUDUSD",
		BaseCurrency:        "AUD",
		QuoteCurrency:       "USD",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          types.Rate(20_000),
	},
	"USDCAD": {
		Name:                "USDCAD",
		BaseCurrency:        "USD",
		QuoteCurrency:       "CAD",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          types.Rate(20_000),
	},
	"NZDUSD": {
		Name:                "NZDUSD",
		BaseCurrency:        "NZD",
		QuoteCurrency:       "USD",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          types.Rate(20_000),
	},
	"XAUUSD": {
		Name:                "XAUUSD",
		BaseCurrency:        "XAU",
		QuoteCurrency:       "USD",
		PipLocation:         -2, // Gold pip = 0.01
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          types.Rate(50_000), // 5% (20:1 typical retail gold)
	},
}

var Symmap = map[string]string{
	"EUR_USD": "EURUSD",
	"GBP_USD": "GBPUSD",
	"USD_JPY": "USDJPY",
	"USD_CHF": "USDCHF",
	"AUD_USD": "AUDUSD",
	"USD_CAD": "USDCAD",
	"NZD_USD": "NZDUSD",
	"EURUSD":  "EUR_USD",
	"GBPUSD":  "GBP_USD",
	"USDJPY":  "USD_JPY",
	"USDCHF":  "USD_CHF",
	"AUDUSD":  "AUD_USD",
	"USDCAD":  "USD_CAD",
	"NZDUSD":  "NZD_USD",
}

func GetInstrument(symbol string) *Instrument {
	if inst, ok := Instruments[symbol]; ok {
		return inst
	} else {
		if symbol, ok = Symmap[symbol]; ok {
			if inst, ok = Instruments[symbol]; ok {
				return inst
			}
		}
	}
	return nil
}

// Move to Market.
func NormalizeInstrument(sym string) string {
	sym = strings.TrimSpace(sym)
	sym = strings.ReplaceAll(sym, "_", "")
	sym = strings.ReplaceAll(sym, "/", "")
	return strings.ToUpper(sym)
}

func (inst *Instrument) PipSize() float64 {
	return math.Pow10(inst.PipLocation)
}
