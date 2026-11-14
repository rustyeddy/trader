// Package marketdata is Trader's public market-data model: the Bar and OHLC
// every consumer (storage, backtests, studies, strategies, external Go code)
// shares, with the validation of what a possible bar is and the exact
// midpoint derivation (ADR-090, ADR-091).
//
// It depends only on the num package and the standard library, and never on
// storage, providers, services or the root trading engine, so anything may
// import it without a cycle. A test enforces that.
package marketdata
