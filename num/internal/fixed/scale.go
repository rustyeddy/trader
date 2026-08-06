// Package fixed implements the exact scaled-integer representation mechanics
// used by the num package.
//
// This package is deliberately unexported outside num.  It knows nothing about
// prices, quantities, money, or currencies; it knows only how to represent an
// exact decimal value as a signed int64 scaled by the scale constant below,
// how to do checked arithmetic on such values, and how to convert between
// that representation and canonical decimal text.
//
// The division of responsibility is:
//
//	code outside num          expresses financial intent
//	code inside num           attaches semantic meaning to exact values
//	code inside num/internal/fixed  implements exact representation mechanics
//
// Binary floating point is prohibited here.  Nothing in this package may
// declare a float32 or float64, and no arithmetic may route through one.  A
// test enforces this by parsing the package source.
//
// See docs/arch/adr-004-exact-numeric-representation.org.
package fixed

import "math"

// places is the number of decimal places retained by the common scale.
const places = 8

// scale is the common scaling factor applied to every authoritative Trader
// numeric value.  A decimal value v is represented as the integer v*scale.
//
// ADR-004 fixes this at 1e8.  The scale is an internal representation detail:
// it is not an instrument tick size, a display precision, a wire-format scale,
// or a provider quotation convention. Nothing outside this package needs the
// raw scale factor itself — num's semantic types never expose it, so there is
// no cross-package caller for this constant to serve.
const scale int64 = 100_000_000

// Representable bounds of the scaled representation, expressed as decimal
// values rather than raw integers:
//
//	minimum: -92,233,720,368.54775808
//	maximum:  92,233,720,368.54775807
const (
	// minRaw is the smallest representable raw scaled value.
	minRaw int64 = math.MinInt64

	// maxRaw is the largest representable raw scaled value.
	maxRaw int64 = math.MaxInt64
)
