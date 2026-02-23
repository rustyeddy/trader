package types

import (
	"fmt"
	"math"
)

type Dollars float64

type Money int64

const MoneyScale = 1_000_000 // or 100000

func MoneyFromFloat(f float64) Money {
	return Money(math.Round(f * MoneyScale))
}

func (m Money) String() string {
	return fmt.Sprintf("%f", float64(m))
}

func (m Money) Float64() float64 {
	return float64(m) / MoneyScale
}

// Price represents scaled int price ticks
type Price int32

const PriceScale = 1_000_000

func PriceFromFloat(f float64) Price {
	return Price(math.Round(f * PriceScale))
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
