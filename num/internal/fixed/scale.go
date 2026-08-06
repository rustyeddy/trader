// Package fixed implements the exact scaled-integer representation mechanics
// used by the num package.
//
// This package is deliberately unexported outside num.  It knows nothing about
// prices, quantities, money, or currencies; it knows only how to represent an
// exact decimal value as a signed int64 scaled by [Scale], how to do checked
// arithmetic on such values, and how to convert between that representation
// and canonical decimal text.
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

// Places is the number of decimal places retained by the common scale.
const Places = 8

// Scale is the common scaling factor applied to every authoritative Trader
// numeric value.  A decimal value v is represented as the integer v*Scale.
//
// ADR-004 fixes this at 1e8.  The scale is an internal representation detail:
// it is not an instrument tick size, a display precision, a wire-format scale,
// or a provider quotation convention.
const Scale int64 = 100_000_000

// Representable bounds of the scaled representation, expressed as decimal
// values rather than raw integers:
//
//	minimum: -92,233,720,368.54775808
//	maximum:  92,233,720,368.54775807
const (
	// MinRaw is the smallest representable raw scaled value.
	MinRaw int64 = math.MinInt64

	// MaxRaw is the largest representable raw scaled value.
	MaxRaw int64 = math.MaxInt64
)
