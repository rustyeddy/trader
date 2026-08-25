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
// multipliers, or lot conventions belong in their eventual domain packages,
// not in num.
//
// Price.MulQuantity (ADR-025, docs/arch/adr-025-price-quantity-notional.org)
// is a deliberate, narrow exception: it derives Money from Price and
// Quantity, but the underlying widened-intermediate arithmetic is itself
// context-free — currency is required as an explicit caller-supplied
// argument, never inferred — and duplicating that arithmetic outside num
// would only reintroduce the overflow risk ADR-004 already identified.
// Composing that notional result with instrument-specific concerns —
// contract multipliers, or converting from the quote/settlement currency
// MulQuantity returns to an account's home currency via Money.Convert —
// remains a domain-package responsibility, not num's.
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
// Callers construct values from exact decimal text (ParsePrice,
// ParseQuantity, ...) or from the text/JSON unmarshalers.  There is no public
// raw-scaled-int64 constructor or accessor: ADR-004 reserves that
// representation detail for trusted internal code, persistence
// reconstruction, and representation-boundary tests, and a public
// FromScaled/Scaled pair on every semantic type would leak the 1e8 scale to
// every importer of num rather than keep it narrowly scoped. A future
// persistence adapter that needs exact scaled BIGINT reconstruction should
// introduce its own narrowly scoped boundary for that purpose rather than
// reuse a general-purpose escape hatch here.
package num
