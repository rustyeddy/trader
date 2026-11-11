// Package tradertest provides private deterministic builders and assertions
// for Trader's runtime tests: instruments, runtime orders, accounts, portfolios,
// and exact-value assertions. Builders preserve domain validation.
//
// ADR-065 internalizes this package with the runtime objects it constructs.
// External SDK testing support, if added later, must use only public contracts.
package tradertest
