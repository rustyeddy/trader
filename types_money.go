package trader

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// dollars represents a trader domain type.
type dollars float64

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
)

// MoneyFromFloat is an internal helper for trader type processing.
func MoneyFromFloat(f float64) Money {
	return Money(math.Round(f * float64(MoneyScale)))
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
	return Price(math.Round(f * float64(PriceScale)))
}

// Float64 is an internal helper for trader type processing.
func (p Price) Float64() float64 {
	return float64(p) / float64(PriceScale)
}

//	func PriceToFloat(price int32, scale int32) float64 {
//		return float64(price) / math.Pow10(int(scale))
//	}
func formatNumber(price Price, scale int32) string {
	decimals := 0
	for s := scale; s > 1; s /= 10 {
		decimals++
	}
	return strconv.FormatFloat(float64(price)/float64(scale), 'f', decimals, 64)
}

// parsePrice parses a CSV field as a raw Price (int32) value.
// TODO MOVE TO Type
func parsePrice(s string) (Price, error) {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 32)
	if err != nil {
		return 0, err
	}
	return Price(v), nil
}

// String is an internal helper for trader type processing.
func (p Price) String() string {
	return formatNumber(p, int32(PriceScale))
}

// Rate represents a trader domain type.
type Rate int64

const rateScale = MoneyScale

// RateFromFloat is an internal helper for trader type processing.
func RateFromFloat(f float64) Rate {
	return Rate(math.Round(f * float64(rateScale)))
}

// Float64 is an internal helper for trader type processing.
func (r Rate) Float64() float64 {
	return float64(r) / float64(rateScale)
}

// String is an internal helper for trader type processing.
func (r Rate) String() string {
	return fmt.Sprintf("%0.6f", r.Float64())
}
