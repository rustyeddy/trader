// Package num implements Trader's exact foundational numeric value types, as
// decided by ADR-004 (docs/arch/adr-004-exact-numeric-representation.org):
//
//	Price
//	Quantity
//	Rate
//	Currency
//	Money
//
// # Architectural boundary
//
// num expresses financial semantics: what a Price is, that a Quantity cannot
// be negative, that Money requires a currency.  The private package
// num/internal/fixed implements exact scaled-integer representation
// mechanics: the common scale, checked arithmetic, widened multiplication and
// division, rounding, and decimal parsing and formatting.
//
// Packages outside num must never import num/internal/fixed.  Code outside
// num expresses financial intent using the semantic types exported here; it
// does not know the raw scale, does not manipulate raw scaled integers, and
// does not call generic fixed-point primitives directly.
//
// # Context-free versus context-dependent arithmetic
//
// Only universally valid operations are exported here, for example
// Price.MulRate, Quantity.MulRate, Money.Add, and Money.DivRate.
// Context-dependent calculations that need instrument metadata, contract
// multipliers, or lot conventions — such as deriving Money from Price and
// Quantity — belong in their eventual domain packages, not in num.
//
// # Percent and Ratio
//
// Rate is the foundational exact dimensionless type.  Percent and Ratio are
// intentionally deferred: introducing them now, before a concrete domain
// operation needs direction, bounds, or interpretation that Rate does not
// already provide, would just be a naming exercise.  When a real constraint
// materially differs from Rate's, add a semantic wrapper around Rate rather
// than a competing base type.
//
// # Floating point
//
// float32 and float64 are prohibited from this package's implementation and
// from its public API: no field, parameter, return value, or internal
// arithmetic may use them.  An architecture test enforces this by parsing
// package source.
//
// # Construction
//
// Ordinary callers construct values from exact decimal text (ParsePrice,
// ParseQuantity, ...) or from the text/JSON unmarshalers.  Raw scaled int64
// construction (PriceFromScaled, ...) is reserved for trusted internal code,
// persistence reconstruction, and tests that exercise representation
// boundaries directly; it is not the ordinary public API.
package num
