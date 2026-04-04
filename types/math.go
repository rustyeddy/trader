package types

import (
	"fmt"
	"math"
	"math/bits"
)

// MulDiv64Ceil computes ceil((a*b)/den) using 128-bit intermediate precision.
// Inputs must be non-negative. den must be > 0.
func MulDiv64(a, b, den int64) (int64, error) {
	if a < 0 || b < 0 || den <= 0 {
		return 0, fmt.Errorf("MulDiv64Ceil: invalid args a=%d b=%d den=%d", a, b, den)
	}

	hi, lo := bits.Mul64(uint64(a), uint64(b))
	// bits.Div64 panics if hi >= den (quotient overflows uint64); catch it early.
	if hi >= uint64(den) {
		return 0, fmt.Errorf("MulDiv64Ceil: overflow result")
	}
	q, r := bits.Div64(hi, lo, uint64(den))

	// If remainder != 0, bump q by 1 (ceiling)
	if r != 0 {
		q++
	}

	if q > uint64(math.MaxInt64) {
		return 0, fmt.Errorf("MulDiv64Ceil: overflow result")
	}
	return int64(q), nil
}

// mulDivFloor64 computes floor((a*b)/den) with 128-bit intermediate precision.
// Inputs must be non-negative and den > 0.
func MulDivFloor64(a, b, den int64) (int64, error) {
	if a < 0 || b < 0 || den <= 0 {
		return 0, fmt.Errorf("mulDivFloor64: invalid args a=%d b=%d den=%d", a, b, den)
	}

	hi, lo := bits.Mul64(uint64(a), uint64(b))
	if hi >= uint64(den) {
		return 0, fmt.Errorf("mulDivFloor64: overflow result")
	}
	q, _ := bits.Div64(hi, lo, uint64(den))
	if q > uint64(^uint64(0)>>1) {
		return 0, fmt.Errorf("mulDivFloor64: overflow result")
	}
	return int64(q), nil
}

// mulDivCeil64 computes ceil((a*b)/den) with 128-bit intermediate precision.
// Inputs must be non-negative and den > 0.
func MulDivCeil64(a, b, den int64) (int64, error) {
	if a < 0 || b < 0 || den <= 0 {
		return 0, fmt.Errorf("mulDivCeil64: invalid args a=%d b=%d den=%d", a, b, den)
	}

	hi, lo := bits.Mul64(uint64(a), uint64(b))
	if hi >= uint64(den) {
		return 0, fmt.Errorf("mulDivCeil64: overflow result")
	}
	q, r := bits.Div64(hi, lo, uint64(den))
	if r != 0 {
		q++
	}
	if q > uint64(^uint64(0)>>1) {
		return 0, fmt.Errorf("mulDivCeil64: overflow result")
	}
	return int64(q), nil
}

func Abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// Signed represents any signed integer or float type.
// The `~` allows named types whose underlying type matches.
type Signed interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~float32 | ~float64
}

// Abs returns the absolute value of any signed numeric type.
func Abs[T Signed](v T) T {
	if v < 0 {
		return -v
	}
	return v
}
