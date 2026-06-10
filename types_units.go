package trader

import (
	"fmt"
	"math"
)

// Units represents a trader domain type.
type Units int64

// UnitsScale is the fixed-point scale for Units values that represent
// fractional multipliers (e.g. 2.5 → Units(2_500_000)).
const UnitsScale int64 = 1_000_000

// UnitsFromFloat converts a float64 multiplier to a fixed-point Units value.
func UnitsFromFloat(f float64) Units {
	return Units(math.Round(f * float64(UnitsScale)))
}

// Float64 converts a fixed-point Units multiplier back to float64.
// Use only at output boundaries (display, broker API).
func (u Units) Float64() float64 {
	return float64(u) / float64(UnitsScale)
}

// Int64 is an internal helper for trader type processing.
func (u Units) Int64() int64 {
	return int64(u)
}

// String is an internal helper for trader type processing.
func (u Units) String() string {
	return fmt.Sprintf("%d", u)
}

// Side represents a trader domain type.
type Side int

const (
	Short Side = -1
	Long  Side = 1
)

// String is an internal helper for trader type processing.
func (s Side) String() string {
	if s == Short {
		return "short"
	}
	return "long"
}

// Pips is scaled such that 1 == .1 pip
// and 20 == 2 pips
type Pips int32

const pipScale = 10 // tenths of a pip

// PipsFromFloat converts a pip count expressed as float64 to the Pips type.
func PipsFromFloat(v float64) Pips {
	return Pips(math.Round(v * pipScale))
}

// Float64 is an internal helper for trader type processing.
func (p Pips) Float64() float64 {
	return float64(p) / pipScale
}

// AvgSpreadPips converts an accumulated Price spread into average pips.
func AvgSpreadPips(spreadSum Price, spreadOpened int, inst *Instrument) float64 {
	if spreadOpened <= 0 || inst == nil {
		return 0
	}
	unitsPerPip := inst.PriceUnitsPerPip()
	if unitsPerPip <= 0 {
		return 0
	}
	return float64(spreadSum) / float64(spreadOpened) / float64(unitsPerPip)
}
