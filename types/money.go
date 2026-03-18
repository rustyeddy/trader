package types

import (
	"fmt"
	"math"
)

type Dollars float64
type Money int64
type Price int32

type Scale6 int32
type Scale7 int32

const (
	PriceScale Scale6 = 100_000
	MoneyScale Scale7 = 1_000_000
)

func (s Scale6) Int32() int32 { return int32(s) }
func (s Scale6) Int64() int64 { return int64(s) }
func (s Scale7) Int32() int32 { return int32(s) }
func (s Scale7) Int64() int64 { return int64(s) }

func MoneyFromFloat(f float64) Money {
	return Money(math.Round(f * float64(MoneyScale)))
}

func (m Money) String() string {
	return fmt.Sprintf("%f", float64(m))
}

func (m Money) Float64() float64 {
	return float64(m) / float64(MoneyScale)
}

// Price represents scaled int price ticks

func PriceFromFloat(f float64) Price {
	return Price(math.Round(f * float64(PriceScale)))
}

func (p Price) String() string {
	return fmt.Sprintf("%f", float64(p))
}

type Rate int64

const RateScale = 1_000_000

func RateFromFloat(f float64) Rate {
	return Rate(math.Round(f * float64(RateScale)))
}

func (r Rate) Float64() float64 {
	return float64(r) / float64(RateScale)
}

func (r Rate) String() string {
	return fmt.Sprintf("%0.6f", r.Float64())
}
