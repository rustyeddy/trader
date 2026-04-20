package trader

import (
	"math"
	"strings"
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
	MinimumTradeSize    Units
	MarginRate          Rate
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

var Instruments = map[string]*Instrument{
	"EURUSD": {
		Name:                "EURUSD",
		BaseCurrency:        "EUR",
		QuoteCurrency:       "USD",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          Rate(20_000), // 2% (50:1)
	},
	"GBPUSD": {
		Name:                "GBPUSD",
		BaseCurrency:        "GBP",
		QuoteCurrency:       "USD",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          Rate(20000),
	},
	"USDJPY": {
		Name:                "USDJPY",
		BaseCurrency:        "USD",
		QuoteCurrency:       "JPY",
		PipLocation:         -2,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          Rate(20_000),
	},
	"USDCHF": {
		Name:                "USDCHF",
		BaseCurrency:        "USD",
		QuoteCurrency:       "CHF",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          Rate(20_000),
	},
	"AUDUSD": {
		Name:                "AUDUSD",
		BaseCurrency:        "AUD",
		QuoteCurrency:       "USD",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          Rate(20_000),
	},
	"USDCAD": {
		Name:                "USDCAD",
		BaseCurrency:        "USD",
		QuoteCurrency:       "CAD",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          Rate(20_000),
	},
	"NZDUSD": {
		Name:                "NZDUSD",
		BaseCurrency:        "NZD",
		QuoteCurrency:       "USD",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          Rate(20_000),
	},
	"XAUUSD": {
		Name:                "XAUUSD",
		BaseCurrency:        "XAU",
		QuoteCurrency:       "USD",
		PipLocation:         -2, // Gold pip = 0.01
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          Rate(50_000), // 5% (20:1 typical retail gold)
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

func (inst *Instrument) PriceUnitsPerPip() Price {
	units := int64(PriceScale)
	for i := 0; i < -inst.PipLocation; i++ {
		units /= 10
	}
	return Price(units)
}

func (inst *Instrument) PriceDeltaFromPips(pips Pips) Price {
	perPip := inst.PriceUnitsPerPip()
	return Price((int64(perPip) * int64(pips)) / int64(pipScale))
}

func (inst *Instrument) AddPips(px Price, pips Pips) Price {
	delta := inst.PriceDeltaFromPips(pips)
	return px + delta
}

func (inst *Instrument) SubPips(px Price, pips Pips) Price {
	delta := inst.PriceDeltaFromPips(pips)
	return px - delta
}

func NormalizeInstrument(sym string) string {
	sym = strings.TrimSpace(sym)
	sym = strings.ReplaceAll(sym, "_", "")
	sym = strings.ReplaceAll(sym, "/", "")
	return strings.ToUpper(sym)
}

func (inst *Instrument) PipSize() float64 {
	return math.Pow10(inst.PipLocation)
}
