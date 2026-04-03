package types

import (
	"fmt"
	"math"
	"strconv"
	"strings"
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

//	func PriceToFloat(price int32, scale int32) float64 {
//		return float64(price) / math.Pow10(int(scale))
//	}
func FormatNumber(price Price, scale int32) string {
	decimals := 0
	for s := scale; s > 1; s /= 10 {
		decimals++
	}
	return strconv.FormatFloat(float64(price)/float64(scale), 'f', decimals, 64)
}

// parsePrice parses a CSV field as a raw types.Price (int32) value.
// TODO MOVE TO Type
func ParsePrice(s string) (Price, error) {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 32)
	if err != nil {
		return 0, err
	}
	return Price(v), nil
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
