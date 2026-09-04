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
// Money.DivQuantity (ADR-027,
// docs/arch/adr-027-money-quantity-price-division.org) is MulQuantity's
// inverse and the same kind of exception, for the same reason: recovering
// a weighted-average price from a cost basis and a quantity is
// context-free exact arithmetic, not a decision about what that price
// means to any particular instrument or account.
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
// from its public API, with exactly one narrow, explicit exception (ADR-045,
// docs/arch/adr-045-analytical-float64-conversion-boundary.org): a method
// literally named Float64 (for example Price.Float64) is Trader's one
// sanctioned exact-to-analytical conversion boundary, a direct numeric
// conversion from a semantic type's exact representation to float64 — never
// a serialize/reparse round-trip through decimal text (Price.String()
// followed by strconv.ParseFloat). Every other declaration in num remains as
// float-free as before ADR-045, and num/internal/fixed remains absolutely
// float-free without exception: the representation-mechanics package itself
// must never perform float64 arithmetic. An architecture test enforces both
// rules by parsing package source.
//
// A value that crosses this boundary into float64 for an analytical
// calculation (indicators, statistics, and similar; see the architecture
// document's "Exact Values Versus Analytical Values" section) must never
// re-enter Trader's exact domain by parsing the float64 back into a
// semantic type. Construct a fresh exact value from a validated decimal or
// order-derived source instead.
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
