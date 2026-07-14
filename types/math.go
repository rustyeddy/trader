package types

import (
	"fmt"
	"math"
	"math/bits"
)

func mulDivNonNegative64(a, b, den int64, fnName string) (q, r int64, err error) {
	if a < 0 || b < 0 || den <= 0 {
		return 0, 0, fmt.Errorf("%s: invalid args a=%d b=%d den=%d", fnName, a, b, den)
	}

	hi, lo := bits.Mul64(uint64(a), uint64(b))
	if hi >= uint64(den) {
		return 0, 0, fmt.Errorf("%s: overflow result", fnName)
	}
	q64, r64 := bits.Div64(hi, lo, uint64(den))
	if q64 > uint64(math.MaxInt64) {
		return 0, 0, fmt.Errorf("%s: overflow result", fnName)
	}
	return int64(q64), int64(r64), nil
}

// MulDivFloor64 computes floor((a*b)/den) with 128-bit intermediate precision.
// Inputs must be non-negative and den > 0.
func MulDivFloor64(a, b, den int64) (int64, error) {
	q, _, err := mulDivNonNegative64(a, b, den, "MulDivFloor64")
	return q, err
}

// MulDivCeil64 computes ceil((a*b)/den) with 128-bit intermediate precision.
// Inputs must be non-negative and den > 0.
func MulDivCeil64(a, b, den int64) (int64, error) {
	q, r, err := mulDivNonNegative64(a, b, den, "MulDivCeil64")
	if err != nil {
		return 0, err
	}
	if r != 0 {
		if q == math.MaxInt64 {
			return 0, fmt.Errorf("MulDivCeil64: overflow result")
		}
		q++
	}
	return q, nil
}

// MulChecked64 multiplies two non-negative int64 values and reports overflow.
func MulChecked64(a, b int64) (int64, error) {
	if a < 0 || b < 0 {
		return 0, fmt.Errorf("MulChecked64: invalid args a=%d b=%d", a, b)
	}

	hi, lo := bits.Mul64(uint64(a), uint64(b))
	if hi != 0 || lo > uint64(math.MaxInt64) {
		return 0, fmt.Errorf("MulChecked64: overflow a=%d b=%d", a, b)
	}

	return int64(lo), nil
}

// RoundHalfAwayFromZero divides num by den, rounding halves away from zero.
// Inputs must be non-negative and den > 0.
func RoundHalfAwayFromZero(num, den int64) (int64, error) {
	if num < 0 || den <= 0 {
		return 0, fmt.Errorf("RoundHalfAwayFromZero: invalid args num=%d den=%d", num, den)
	}

	q := num / den
	r := num % den
	if r >= (den+1)/2 {
		if q == math.MaxInt64 {
			return 0, fmt.Errorf("RoundHalfAwayFromZero: overflow")
		}
		q++
	}

	return q, nil
}

// SignedMulDivRound computes round((a*b)/den) half away from zero, where a
// may be negative; b must be non-negative and den > 0.
func SignedMulDivRound(a, b, den int64) (int64, error) {
	if b < 0 || den <= 0 {
		return 0, fmt.Errorf("SignedMulDivRound: invalid args a=%d b=%d den=%d", a, b, den)
	}

	absA, err := AbsInt64Checked(a)
	if err != nil {
		return 0, err
	}

	prod, err := MulChecked64(absA, b)
	if err != nil {
		return 0, err
	}

	q, err := RoundHalfAwayFromZero(prod, den)
	if err != nil {
		return 0, err
	}

	if a < 0 {
		return -q, nil
	}

	return q, nil
}

// AbsInt64Checked returns |v|, reporting overflow for math.MinInt64.
func AbsInt64Checked(v int64) (int64, error) {
	if v == math.MinInt64 {
		return 0, fmt.Errorf("AbsInt64Checked: overflow")
	}
	if v < 0 {
		return -v, nil
	}
	return v, nil
}
