package market

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/rustyeddy/trader/types"
)

// AssetClass identifies the broad market category of an instrument.
type AssetClass int

const (
	AssetForex   AssetClass = iota // currency pairs (default zero value)
	AssetEquity                    // stocks and ETFs
	AssetFutures                   // futures contracts
)

// Instrument represents a trader domain type.
type Instrument struct {
	Name                string
	AssetClass          AssetClass
	BaseCurrency        string
	QuoteCurrency       string
	PipLocation         int
	TradeUnitsPrecision int
	MinimumTradeSize    types.Units
	MarginRate          types.Rate
}

var majorInstrumentNames = []string{
	"EURUSD",
	"GBPUSD",
	"USDJPY",
	"USDCHF",
	"AUDUSD",
	"USDCAD",
	"NZDUSD",
}

func makeInstrument(name, base, quote string, pipLocation int, marginRate types.Rate) Instrument {
	return Instrument{
		Name:                name,
		AssetClass:          AssetForex,
		BaseCurrency:        base,
		QuoteCurrency:       quote,
		PipLocation:         pipLocation,
		TradeUnitsPrecision: 0,
		MinimumTradeSize:    1,
		MarginRate:          marginRate,
	}
}

var instrumentRegistry = map[string]Instrument{
	// USD majors
	"EURUSD": makeInstrument("EURUSD", "EUR", "USD", -4, types.Rate(20_000)),
	"GBPUSD": makeInstrument("GBPUSD", "GBP", "USD", -4, types.Rate(20_000)),
	"USDJPY": makeInstrument("USDJPY", "USD", "JPY", -2, types.Rate(20_000)),
	"USDCHF": makeInstrument("USDCHF", "USD", "CHF", -4, types.Rate(20_000)),
	"AUDUSD": makeInstrument("AUDUSD", "AUD", "USD", -4, types.Rate(20_000)),
	"USDCAD": makeInstrument("USDCAD", "USD", "CAD", -4, types.Rate(20_000)),
	"NZDUSD": makeInstrument("NZDUSD", "NZD", "USD", -4, types.Rate(20_000)),
	// Precious metals
	"XAUUSD": makeInstrument("XAUUSD", "XAU", "USD", -2, types.Rate(50_000)),
	// JPY crosses
	"EURJPY": makeInstrument("EURJPY", "EUR", "JPY", -2, types.Rate(20_000)),
	"GBPJPY": makeInstrument("GBPJPY", "GBP", "JPY", -2, types.Rate(20_000)),
	"AUDJPY": makeInstrument("AUDJPY", "AUD", "JPY", -2, types.Rate(20_000)),
	"CADJPY": makeInstrument("CADJPY", "CAD", "JPY", -2, types.Rate(20_000)),
	"CHFJPY": makeInstrument("CHFJPY", "CHF", "JPY", -2, types.Rate(20_000)),
	"NZDJPY": makeInstrument("NZDJPY", "NZD", "JPY", -2, types.Rate(20_000)),
	// EUR crosses
	"EURGBP": makeInstrument("EURGBP", "EUR", "GBP", -4, types.Rate(20_000)),
	"EURAUD": makeInstrument("EURAUD", "EUR", "AUD", -4, types.Rate(20_000)),
	"EURCAD": makeInstrument("EURCAD", "EUR", "CAD", -4, types.Rate(20_000)),
	"EURCHF": makeInstrument("EURCHF", "EUR", "CHF", -4, types.Rate(20_000)),
	"EURNZD": makeInstrument("EURNZD", "EUR", "NZD", -4, types.Rate(20_000)),
	// GBP crosses
	"GBPAUD": makeInstrument("GBPAUD", "GBP", "AUD", -4, types.Rate(20_000)),
	"GBPCAD": makeInstrument("GBPCAD", "GBP", "CAD", -4, types.Rate(20_000)),
	"GBPNZD": makeInstrument("GBPNZD", "GBP", "NZD", -4, types.Rate(20_000)),
	// AUD crosses
	"AUDCAD": makeInstrument("AUDCAD", "AUD", "CAD", -4, types.Rate(20_000)),
	"AUDCHF": makeInstrument("AUDCHF", "AUD", "CHF", -4, types.Rate(20_000)),
	"AUDNZD": makeInstrument("AUDNZD", "AUD", "NZD", -4, types.Rate(20_000)),
}

// approximateUSDPerUnit provides static approximate USD exchange rates for
// non-USD currencies. Used for cross-pair P/L conversion and position sizing
// when a live rate is not available. Accuracy ±30%; correct order of magnitude.
var approximateUSDPerUnit = map[string]types.Rate{
	"EUR": types.RateFromFloat(1.08),
	"GBP": types.RateFromFloat(1.26),
	"JPY": types.RateFromFloat(0.0067), // ~1/150
	"AUD": types.RateFromFloat(0.65),
	"CAD": types.RateFromFloat(0.74),
	"NZD": types.RateFromFloat(0.61),
	"CHF": types.RateFromFloat(1.10),
}

func init() {
	validateInstrumentRegistry()
}

func validateInstrumentRegistry() {
	for key, inst := range instrumentRegistry {
		if NormalizeInstrument(key) != key {
			panic(fmt.Sprintf("instrument key must be normalized: %q", key))
		}
		if inst.Name != key {
			panic(fmt.Sprintf("instrument name mismatch for %q: %q", key, inst.Name))
		}
		if inst.BaseCurrency == "" || inst.QuoteCurrency == "" {
			panic(fmt.Sprintf("instrument %q has blank currencies", key))
		}
		if inst.MinimumTradeSize <= 0 {
			panic(fmt.Sprintf("instrument %q has non-positive minimum trade size", key))
		}
		if inst.MarginRate <= 0 {
			panic(fmt.Sprintf("instrument %q has non-positive margin rate", key))
		}
		if inst.PriceUnitsPerPip() <= 0 {
			panic(fmt.Sprintf("instrument %q has invalid pip configuration", key))
		}
	}

	for _, name := range majorInstrumentNames {
		if _, ok := instrumentRegistry[name]; !ok {
			panic(fmt.Sprintf("major instrument %q missing from registry", name))
		}
	}
}

// MajorInstruments returns the ordered list of seven major FX pairs tracked by this engine.
func MajorInstruments() []string {
	return append([]string(nil), majorInstrumentNames...)
}

// AllInstruments returns every instrument name in the registry, sorted
// alphabetically for deterministic output.
func AllInstruments() []string {
	names := make([]string, 0, len(instrumentRegistry))
	for name := range instrumentRegistry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ApproximateUSDPerUnit reports a rough USD-per-unit types.Rate for a non-USD currency.
func ApproximateUSDPerUnit(currency string) (types.Rate, bool) {
	rate, ok := approximateUSDPerUnit[strings.ToUpper(strings.TrimSpace(currency))]
	return rate, ok
}

// LookupInstrument returns a copy of the instrument metadata and whether it exists.
func LookupInstrument(symbol string) (Instrument, bool) {
	inst, ok := instrumentRegistry[NormalizeInstrument(symbol)]
	return inst, ok
}

// GetInstrument is an internal helper for trader type processing.
func GetInstrument(symbol string) *Instrument {
	inst, ok := LookupInstrument(symbol)
	if !ok {
		return nil
	}
	return &inst
}

// PriceUnitsPerPip is an internal helper for trader type processing.
func (inst *Instrument) PriceUnitsPerPip() types.Price {
	if inst == nil {
		return 0
	}
	units := int64(types.PriceScale)
	for i := 0; i < -inst.PipLocation; i++ {
		units /= 10
	}
	return types.Price(units)
}

// PriceDeltaFromPips is an internal helper for trader type processing.
func (inst *Instrument) PriceDeltaFromPips(pips types.Pips) types.Price {
	perPip := inst.PriceUnitsPerPip()
	return types.Price((int64(perPip) * int64(pips)) / int64(types.PipScale))
}

// AddPips is an internal helper for trader type processing.
func (inst *Instrument) AddPips(px types.Price, pips types.Pips) types.Price {
	delta := inst.PriceDeltaFromPips(pips)
	return px + delta
}

// SubPips is an internal helper for trader type processing.
func (inst *Instrument) SubPips(px types.Price, pips types.Pips) types.Price {
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
	if inst == nil {
		return 0
	}
	return math.Pow10(inst.PipLocation)
}

// PipValueUSD returns the USD value of pips pips for a position of units units.
//
// For USD-quoted pairs (EURUSD, GBPUSD, AUDUSD, NZDUSD) the result is exact
// and rate is ignored.  For USD-base pairs (USDJPY, USDCHF, USDCAD) the pip
// value is denominated in the quote currency, so rate (the current pair price)
// is required to convert back to USD.  Returns 0 if rate ≤ 0.
func (inst *Instrument) PipValueUSD(rate float64, units int64, pips float64) float64 {
	if inst == nil {
		return 0
	}
	pip := inst.PipSize()
	if inst.QuoteCurrency == "USD" {
		return pip * float64(units) * pips
	}
	if rate <= 0 {
		return 0
	}
	return pip * float64(units) * pips / rate
}

// DukascopyPriceMultiplier returns the factor needed to convert a raw
// Dukascopy bi5 price integer into a types.Price value at the current types.PriceScale.
//
// Dukascopy stores prices with (−PipLocation + 1) decimal places:
//   - 5-decimal pairs (EURUSD, PipLocation=−4): native scale 100,000  → multiplier = 1
//   - 3-decimal pairs (USDJPY, PipLocation=−2): native scale   1,000  → multiplier = 100
func (inst *Instrument) DukascopyPriceMultiplier() uint32 {
	if inst == nil {
		return 0
	}
	nativeDecimals := -inst.PipLocation + 1
	nativeScale := int64(math.Pow10(nativeDecimals))
	return uint32(int64(types.PriceScale) / nativeScale)
}

// AvgSpreadPips converts an accumulated types.Price spread into average pips.
// Lives in market (not types) because it depends on *Instrument.
func AvgSpreadPips(spreadSum types.Price, spreadOpened int, inst *Instrument) float64 {
	if spreadOpened <= 0 || inst == nil {
		return 0
	}
	unitsPerPip := inst.PriceUnitsPerPip()
	if unitsPerPip <= 0 {
		return 0
	}
	return float64(spreadSum) / float64(spreadOpened) / float64(unitsPerPip)
}
