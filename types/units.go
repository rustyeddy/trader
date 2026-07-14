package types

import (
	"fmt"
)

// Units represents a trader domain type.
type Units int64

// UnitsScale is the fixed-point scale for Units values that represent
// fractional multipliers (e.g. 2.5 → Units(2_500_000)).
const UnitsScale int64 = 1_000_000

// UnitsFromFloat converts a float64 multiplier to a fixed-point Units value.
func UnitsFromFloat(f float64) Units {
	return Units(mustScaledInt64("UnitsFromFloat", f, UnitsScale))
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
	Flat  Side = 0
	Short Side = -1
	Long  Side = 1
)

// Valid is an internal helper for trader type processing.
func (s Side) Valid() bool {
	return s == Short || s == Long
}

// String is an internal helper for trader type processing.
func (s Side) String() string {
	switch s {
	case Flat:
		return "flat"
	case Short:
		return "short"
	case Long:
		return "long"
	default:
		return "unknown"
	}
}

// Pips stores tenths of a pip (deci-pips):
// 1 == 0.1 pip and 20 == 2.0 pips.
type Pips int32

const PipScale = 10 // deci-pips per pip

// PipsFromFloat converts a whole/decimal pip count into internal deci-pips.
func PipsFromFloat(v float64) Pips {
	return Pips(mustScaledInt32("PipsFromFloat", v, PipScale))
}

// Float64 is an internal helper for trader type processing.
func (p Pips) Float64() float64 {
	return float64(p) / PipScale
}
