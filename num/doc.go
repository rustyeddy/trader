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
// num/internal/fixed implements the representation mechanics: the common
// scale, checked arithmetic, widened multiplication and division, rounding,
// and decimal parsing and formatting.
//
// Packages outside num must never import num/internal/fixed.  Code outside
// num expresses financial intent with the semantic types exported here; it
// does not know the raw scale or manipulate raw scaled integers.
//
// # Context-free versus context-dependent arithmetic
//
// Only universally valid operations are exported, for example Price.MulRate,
// Quantity.MulRate, Money.Add, and Money.DivRate.  Calculations that need
// instrument metadata, contract multipliers, or lot conventions belong in
// their domain packages, not in num.
//
// Min and Max order Price, Quantity and Rate (Money is ordered with its
// error-returning Cmp), because the builtin min and max do not work on the
// struct value types.
//
// Price.MulQuantity (ADR-025) and Money.DivQuantity (ADR-027) are deliberate,
// narrow exceptions.  They derive Money from Price and Quantity and back, and
// the arithmetic is context-free: the currency is an explicit caller-supplied
// argument, never inferred, and duplicating the widened-intermediate
// arithmetic outside num would reintroduce the overflow risk ADR-004
// identified.  Contract multipliers and conversion to an account's home
// currency (Money.Convert) remain domain-package concerns.
//
// # Percent and Ratio
//
// Rate is the foundational exact dimensionless type.  Percent and Ratio are
// deferred until a domain operation needs direction, bounds, or an
// interpretation Rate does not provide; then add a semantic wrapper around
// Rate rather than a competing base type.
//
// # Floating point
//
// float32 and float64 are prohibited from this package's implementation and
// public API, with one exception (ADR-045): a method literally named Float64
// (for example Price.Float64) is the sanctioned exact-to-analytical conversion
// boundary, a direct numeric conversion and never a serialize/reparse
// round-trip through decimal text.  num/internal/fixed has no exception.  An
// architecture test enforces both rules by parsing package source.
//
// num adds no float64-to-exact constructor.  An analytical result may become
// authoritative again only through the normal checked, quantized,
// semantically validated construction path (ADR-004), never by silently
// propagating a raw float64 into an order or accounting value.
//
// # Construction
//
// Callers construct values from exact decimal text (ParsePrice,
// ParseQuantity, ...) or from the text and JSON unmarshalers.  There is no
// public raw-scaled-int64 constructor or accessor: that would leak the 1e8
// scale to every importer.  A persistence adapter that needs exact scaled
// reconstruction should introduce its own narrowly scoped boundary rather than
// reuse a general-purpose escape hatch here.
//
// This package is part of Trader's supported external strategy/research/value
// API (ADR-065); it does not expose runtime composition or internal types.
package num
