package trader

import (
	"math"
	"strings"
)

// symbol represents a trader domain type.
type symbol string

const (
	EUR_USD symbol = "EUR_USD"
	GBP_USD symbol = "GBP_USD"
	USD_JPY symbol = "USD_JPY"
	USD_CHF symbol = "USD_CHF"
	AUD_USD symbol = "AUD_USD"
	USD_CAD symbol = "USD_CAD"
	NZD_USD symbol = "NZD_USD"
	EUR_GBP symbol = "EUR_GBP"
	GBP_JPY symbol = "GBP_JPY"
	EUR_JPY symbol = "EUR_JPY"
	AUD_JPY symbol = "AUD_JPY"
)

// Instrument represents a trader domain type.
type Instrument struct {
	Name                string
	BaseCurrency        string
	QuoteCurrency       string
	PipLocation         int
	TradeUnitsPrecision int
	MinimumTradeSize    Units
	MarginRate          Rate
}

var instrumentList = []string{
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
	"EURGBP": {
		Name:                "EURGBP",
		BaseCurrency:        "EUR",
		QuoteCurrency:       "GBP",
		PipLocation:         -4,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          Rate(20_000),
	},
	"GBPJPY": {
		Name:                "GBPJPY",
		BaseCurrency:        "GBP",
		QuoteCurrency:       "JPY",
		PipLocation:         -2,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          Rate(20_000),
	},
	"EURJPY": {
		Name:                "EURJPY",
		BaseCurrency:        "EUR",
		QuoteCurrency:       "JPY",
		PipLocation:         -2,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          Rate(20_000),
	},
	"AUDJPY": {
		Name:                "AUDJPY",
		BaseCurrency:        "AUD",
		QuoteCurrency:       "JPY",
		PipLocation:         -2,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          Rate(20_000),
	},
}

var symmap = map[string]string{
	"EUR_USD": "EURUSD",
	"GBP_USD": "GBPUSD",
	"USD_JPY": "USDJPY",
	"USD_CHF": "USDCHF",
	"AUD_USD": "AUDUSD",
	"USD_CAD": "USDCAD",
	"NZD_USD": "NZDUSD",
	"EUR_GBP": "EURGBP",
	"GBP_JPY": "GBPJPY",
	"EUR_JPY": "EURJPY",
	"AUD_JPY": "AUDJPY",
}

// ApproxUSDPerUnit provides static approximate USD values for non-USD currencies.
// Used for cross-pair P/L conversion and position sizing when a live complementary
// rate is not available. Accuracy ±30% over long periods; correct in order of magnitude.
var ApproxUSDPerUnit = map[string]float64{
	"EUR": 1.08,
	"GBP": 1.26,
	"JPY": 0.0067, // ~1/150
	"AUD": 0.65,
	"CAD": 0.74,
	"NZD": 0.61,
	"CHF": 1.10,
}

// GetInstrument is an internal helper for trader type processing.
func GetInstrument(symbol string) *Instrument {
	if inst, ok := Instruments[symbol]; ok {
		return inst
	} else {
		if symbol, ok = symmap[symbol]; ok {
			if inst, ok = Instruments[symbol]; ok {
				return inst
			}
		}
	}
	return nil
}

// PriceUnitsPerPip is an internal helper for trader type processing.
func (inst *Instrument) PriceUnitsPerPip() Price {
	units := int64(PriceScale)
	for i := 0; i < -inst.PipLocation; i++ {
		units /= 10
	}
	return Price(units)
}

// PriceDeltaFromPips is an internal helper for trader type processing.
func (inst *Instrument) PriceDeltaFromPips(pips Pips) Price {
	perPip := inst.PriceUnitsPerPip()
	return Price((int64(perPip) * int64(pips)) / int64(pipScale))
}

// AddPips is an internal helper for trader type processing.
func (inst *Instrument) AddPips(px Price, pips Pips) Price {
	delta := inst.PriceDeltaFromPips(pips)
	return px + delta
}

// SubPips is an internal helper for trader type processing.
func (inst *Instrument) SubPips(px Price, pips Pips) Price {
	delta := inst.PriceDeltaFromPips(pips)
	return px - delta
}

// NormalizeInstrument is an internal helper for trader type processing.
func NormalizeInstrument(sym string) string {
	sym = strings.TrimSpace(sym)
	sym = strings.ReplaceAll(sym, "_", "")
	sym = strings.ReplaceAll(sym, "/", "")
	return strings.ToUpper(sym)
}

// PipSize is an internal helper for trader type processing.
func (inst *Instrument) PipSize() float64 {
	return math.Pow10(inst.PipLocation)
}

// DukascopyPriceMultiplier returns the factor needed to convert a raw
// Dukascopy bi5 price integer into a Price value at the current PriceScale.
//
// Dukascopy stores prices with (−PipLocation + 1) decimal places:
//   - 5-decimal pairs (EURUSD, PipLocation=−4): native scale 100,000  → multiplier = 1
//   - 3-decimal pairs (USDJPY, PipLocation=−2): native scale   1,000  → multiplier = 100
func (inst *Instrument) DukascopyPriceMultiplier() uint32 {
	nativeDecimals := -inst.PipLocation + 1
	nativeScale := int64(math.Pow10(nativeDecimals))
	return uint32(int64(PriceScale) / nativeScale)
}
