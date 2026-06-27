package indicator

import (
	"math/bits"

	"github.com/rustyeddy/trader/market"
)

const (
	indicatorValueScale int64 = 1_000_000
	log2TenQ32          int64 = 14_267_572_527

	// ValueScale is the scale factor used for all indicator output values
	// (ADX, DI, DX, etc.). An ADX of 25.0 is stored as 25_000_000.
	ValueScale = indicatorValueScale
)

func roundDivPositive(num, den int64) int64 {
	if den <= 0 {
		return 0
	}
	return (num + den/2) / den
}

func roundDivSigned(num, den int64) int64 {
	if den <= 0 {
		return 0
	}
	if num < 0 {
		return -roundDivPositive(-num, den)
	}
	return roundDivPositive(num, den)
}

func max3Int64(a, b, c int64) int64 {
	if a >= b && a >= c {
		return a
	}
	if b >= a && b >= c {
		return b
	}
	return c
}

func absPriceDiff(a, b market.Price) int64 {
	if a >= b {
		return int64(a - b)
	}
	return int64(b - a)
}

func percentScaled(num, den int64) int64 {
	if num <= 0 || den <= 0 {
		return 0
	}
	return roundDivPositive(num*indicatorValueScale, den) * 100
}

func fixedScaledToFloat64(v int64) float64 {
	return float64(v) / float64(indicatorValueScale)
}

func priceToFloat64(v int64, scale market.Scale6) float64 {
	return float64(v) / float64(scale)
}

func sqrtRoundRatio(num, den int64) int64 {
	if num <= 0 || den <= 0 {
		return 0
	}
	root := isqrtRounded(uint64(num))
	return roundDivPositive(int64(root), den)
}

func isqrtRounded(n uint64) uint64 {
	if n == 0 {
		return 0
	}
	x := isqrtFloor(n)
	lo := x * x
	next := x + 1
	hi := next * next
	if n-lo >= hi-n {
		return next
	}
	return x
}

func isqrtFloor(n uint64) uint64 {
	var x uint64
	var bit uint64 = 1 << 62
	for bit > n {
		bit >>= 2
	}
	for bit != 0 {
		if n >= x+bit {
			n -= x + bit
			x = (x >> 1) + bit
		} else {
			x >>= 1
		}
		bit >>= 2
	}
	return x
}

func fixedLog10Scaled(xScaled, scale int64) int64 {
	if xScaled <= 0 || scale <= 0 {
		return 0
	}
	xQ32 := uint64(roundDivPositive(xScaled*(1<<32), scale))
	log2Q32 := fixedLog2Q32(xQ32)
	return roundDivSigned(log2Q32*indicatorValueScale, log2TenQ32)
}

func fixedLog2Q32(x uint64) int64 {
	const fracBits = 32
	const one = uint64(1) << fracBits
	const two = one << 1

	var exp int64
	for x < one {
		x <<= 1
		exp--
	}
	for x >= two {
		x >>= 1
		exp++
	}

	result := exp << fracBits
	for bit := fracBits; bit > 0; bit-- {
		x = squareShiftRightQ32(x)
		if x >= two {
			x >>= 1
			result += 1 << (bit - 1)
		}
	}

	return result
}

func squareShiftRightQ32(x uint64) uint64 {
	hi, lo := bits.Mul64(x, x)
	return (hi << 32) | (lo >> 32)
}
