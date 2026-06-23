package market

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Money represents a trader domain type.
type Money int64

// Price represents a trader domain type.
type Price int32

// PriceSum represents an accumulated sum of Price values.
type PriceSum int64

// Scale6 represents a trader domain type.
type Scale6 int32

// Scale7 represents a trader domain type.
type Scale7 int64

const (
	PriceScale Scale6 = 100_000
	MoneyScale Scale7 = 1_000_000
	RateScale  Scale7 = MoneyScale
)

func mustScaledInt64(name string, f float64, scale int64) int64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		panic(fmt.Sprintf("%s: non-finite value %v", name, f))
	}

	scaled := math.Round(f * float64(scale))
	if math.IsNaN(scaled) || math.IsInf(scaled, 0) || scaled < math.MinInt64 || scaled > math.MaxInt64 {
		panic(fmt.Sprintf("%s: value %v out of int64 range at scale %d", name, f, scale))
	}

	return int64(scaled)
}

func mustScaledInt32(name string, f float64, scale int32) int32 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		panic(fmt.Sprintf("%s: non-finite value %v", name, f))
	}

	scaled := math.Round(f * float64(scale))
	if math.IsNaN(scaled) || math.IsInf(scaled, 0) || scaled < math.MinInt32 || scaled > math.MaxInt32 {
		panic(fmt.Sprintf("%s: value %v out of int32 range at scale %d", name, f, scale))
	}

	return int32(scaled)
}

// MoneyFromFloat is an internal helper for trader type processing.
func MoneyFromFloat(f float64) Money {
	return Money(mustScaledInt64("MoneyFromFloat", f, int64(MoneyScale)))
}

// String is an internal helper for trader type processing.
func (m Money) String() string {
	return strconv.FormatFloat(m.Float64(), 'f', 6, 64)
}

// Float64 is an internal helper for trader type processing.
func (m Money) Float64() float64 {
	return float64(m) / float64(MoneyScale)
}

// Price represents scaled int price ticks

// PriceFromFloat is an internal helper for trader type processing.
func PriceFromFloat(f float64) Price {
	return Price(mustScaledInt32("PriceFromFloat", f, int32(PriceScale)))
}

// Float64 is an internal helper for trader type processing.
func (p Price) Float64() float64 {
	return float64(p) / float64(PriceScale)
}

//	func PriceToFloat(price int32, scale int32) float64 {
//		return float64(price) / math.Pow10(int(scale))
//	}
func formatScaledPrice(price Price, scale int32) string {
	decimals := 0
	for s := scale; s > 1; s /= 10 {
		decimals++
	}
	return strconv.FormatFloat(float64(price)/float64(scale), 'f', decimals, 64)
}

// parseRawPrice parses a CSV field as a raw scaled Price (int32) value.
func parseRawPrice(s string) (Price, error) {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 32)
	if err != nil {
		return 0, err
	}
	return Price(v), nil
}

// String is an internal helper for trader type processing.
func (p Price) String() string {
	return formatScaledPrice(p, int32(PriceScale))
}

// Rate represents a trader domain type.
type Rate int64

// RateFromFloat is an internal helper for trader type processing.
func RateFromFloat(f float64) Rate {
	return Rate(mustScaledInt64("RateFromFloat", f, int64(RateScale)))
}

// Float64 is an internal helper for trader type processing.
func (r Rate) Float64() float64 {
	return float64(r) / float64(RateScale)
}

// String is an internal helper for trader type processing.
func (r Rate) String() string {
	return fmt.Sprintf("%0.6f", r.Float64())
}
