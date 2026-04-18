package trader

import (
	"fmt"
	"math"
)

type Units int64

func (u Units) Int64() int64 {
	return int64(u)
}

func (u Units) String() string {
	return fmt.Sprintf("%d", u)
}

type Side int

const (
	Short Side = -1
	Long  Side = 1
)

// Pips is scaled such that 1 == .1 pip
// and 20 == 2 pips
type Pips int32

const PipScale = 10 // tenths of a pip

func PipsFromFloat(v float64) Pips {
	return Pips(math.Round(v * PipScale))
}

func (p Pips) Float64() float64 {
	return float64(p) / PipScale
}
