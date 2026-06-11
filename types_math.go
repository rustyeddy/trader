package trader

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

// mulDivFloor64 computes floor((a*b)/den) with 128-bit intermediate precision.
// Inputs must be non-negative and den > 0.
func mulDivFloor64(a, b, den int64) (int64, error) {
	q, _, err := mulDivNonNegative64(a, b, den, "mulDivFloor64")
	return q, err
}

// mulDivCeil64 computes ceil((a*b)/den) with 128-bit intermediate precision.
// Inputs must be non-negative and den > 0.
func mulDivCeil64(a, b, den int64) (int64, error) {
	q, r, err := mulDivNonNegative64(a, b, den, "mulDivCeil64")
	if err != nil {
		return 0, err
	}
	if r != 0 {
		q++
	}
	if q > math.MaxInt64 {
		return 0, fmt.Errorf("mulDivCeil64: overflow result")
	}
	return q, nil
}

// mulChecked64 multiplies two non-negative int64 values and reports overflow.
func mulChecked64(a, b int64) (int64, error) {
	if a < 0 || b < 0 {
		return 0, fmt.Errorf("mulChecked64: invalid args a=%d b=%d", a, b)
	}

	hi, lo := bits.Mul64(uint64(a), uint64(b))
	if hi != 0 || lo > uint64(math.MaxInt64) {
		return 0, fmt.Errorf("mulChecked64: overflow a=%d b=%d", a, b)
	}

	return int64(lo), nil
}

// roundHalfAwayFromZero is an internal helper for trader type processing.
func roundHalfAwayFromZero(num, den int64) (int64, error) {
	if num < 0 || den <= 0 {
		return 0, fmt.Errorf("roundHalfAwayFromZero: invalid args num=%d den=%d", num, den)
	}

	q := num / den
	r := num % den
	if r >= (den+1)/2 {
		if q == math.MaxInt64 {
			return 0, fmt.Errorf("roundHalfAwayFromZero: overflow")
		}
		q++
	}

	return q, nil
}

// absInt64Checked is an internal helper for trader type processing.
func absInt64Checked(v int64) (int64, error) {
	if v == math.MinInt64 {
		return 0, fmt.Errorf("absInt64Checked: overflow")
	}
	if v < 0 {
		return -v, nil
	}
	return v, nil
}

// signedMulDivRound is an internal helper for trader type processing.
func signedMulDivRound(a, b, den int64) (int64, error) {
	if b < 0 || den <= 0 {
		return 0, fmt.Errorf("signedMulDivRound: invalid args a=%d b=%d den=%d", a, b, den)
	}

	absA, err := absInt64Checked(a)
	if err != nil {
		return 0, err
	}

	prod, err := mulChecked64(absA, b)
	if err != nil {
		return 0, err
	}

	q, err := roundHalfAwayFromZero(prod, den)
	if err != nil {
		return 0, err
	}

	if a < 0 {
		return -q, nil
	}

	return q, nil
}
