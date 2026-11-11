// Package numericstudy is a design-investigation spike for
// https://github.com/rustyeddy/trader/issues/33 — "Validate Price scale
// across supported asset classes".
//
// It is NOT the future public Price implementation.  It lives under
// test/internal so the import path itself keeps it out of production code:
// nothing outside test/ can reference it.  Its purpose is to produce executable evidence for
// ADR-004 (docs/arch/adr-004-exact-numeric-representation.org):
//
//   - which fixed internal scale represents FX, equities, futures,
//     high-priced assets, crypto, and small-priced assets exactly;
//   - what maximum price and overflow headroom each candidate scale leaves;
//   - which quotation conventions cannot be represented as-is;
//   - where intermediate arithmetic (Price x Quantity, Price x Rate, rolling
//     sums) overflows int64 before the final descale.
//
// The parsing and formatting routines here deliberately avoid binary
// floating point entirely: decimal text is converted directly to and from
// scaled int64.
//
// This package should be deleted or superseded when #44 consolidates
// ADR-004 and the real numeric types land.
package numericstudy
