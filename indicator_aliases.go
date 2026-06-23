package trader

// Transition shim re-exporting the indicator package (extracted from the root
// trader package) so existing callers compile unchanged during the migration.
// Remove entries as callers move to github.com/rustyeddy/trader/indicator
// directly. See docs/pkg-migration.org.

import "github.com/rustyeddy/trader/indicator"

// Indicator types and interfaces.
type (
	EMA              = indicator.EMA
	ATR              = indicator.ATR
	ADX              = indicator.ADX
	BollingerBands   = indicator.BollingerBands
	ChoppinessIndex  = indicator.ChoppinessIndex
	CandleIndicator  = indicator.CandleIndicator
	Float64Indicator = indicator.Float64Indicator
	PriceIndicator   = indicator.PriceIndicator
)

// Indicator constructors.
var (
	NewEMA             = indicator.NewEMA
	NewATR             = indicator.NewATR
	NewADX             = indicator.NewADX
	NewBollingerBands  = indicator.NewBollingerBands
	NewChoppinessIndex = indicator.NewChoppinessIndex
)

// Internal helper still referenced by root (ChandelierExit) across the boundary.
var roundDivPositive = indicator.RoundDivPositive
