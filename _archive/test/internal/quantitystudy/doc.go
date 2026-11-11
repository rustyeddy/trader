// Package quantitystudy is a design-investigation spike for
// https://github.com/rustyeddy/trader/issues/36 — "Decide Quantity precision
// and asset constraints".
//
// It is NOT the future public Quantity type.  Like its sibling
// test/internal/numericstudy, it lives under test/internal so the import path
// keeps it out of production code, and it should be deleted or superseded when
// #44 consolidates ADR-004.
//
// It reuses numericstudy for exact decimal parsing and formatting, table
// rendering, and the generated-documentation machinery, so both studies share
// one implementation of "no binary floating point" and one implementation of
// the guarantee that checked-in tables match the code.
//
// The central question is whether one fixed Trader-wide Quantity scale can
// serve every intended asset class.  The evidence says no: satoshi precision
// and trillion-unit token positions cannot coexist in a scaled int64.  See
// Frontier and the report for the range arithmetic behind that.
package quantitystudy
