package trader

import (
	"fmt"
	"math"
)

// Units defines the Units type.
type Units int64

// Int64 performs Int64.
func (u Units) Int64() int64 {
	return int64(u)
}

// String performs String.
func (u Units) String() string {
	return fmt.Sprintf("%d", u)
}

// Side defines the Side type.
type Side int

const (
	Short Side = -1
	Long  Side = 1
)

// String performs String.
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

// pipsFromFloat performs pipsFromFloat.
func pipsFromFloat(v float64) Pips {
	return Pips(math.Round(v * pipScale))
}

// Float64 performs Float64.
func (p Pips) Float64() float64 {
	return float64(p) / pipScale
}
