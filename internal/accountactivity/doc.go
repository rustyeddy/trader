// Package accountactivity defines Trader's provider-neutral model of
// authoritative historical broker account activity (issue #508, ADR-079):
// fills and their costs, financing, fees, deposits, withdrawals, dividends
// and other cash adjustments, exactly as a broker reported them.
//
// # Ledger, journal, trade history
//
// Three things are kept distinct:
//
//   - the broker activity ledger: facts the broker reported (this package's
//     Activity values, persisted elsewhere);
//   - the Trader journal: decisions and events Trader generated or observed
//     during a run (internal/journal). Imported history is never written
//     into it and Activity carries no RunID;
//   - trade history: a view derived from activities, optionally correlated
//     with journals later. It is not stored here.
//
// # Unknown is not zero; reported is not derived
//
// Every economic field is a Value: absent, broker-reported, or derived by
// Trader by a named method. An absent value stays absent. It is never
// estimated or defaulted, and it is distinct from a reported or derived
// zero. Spread cost, commission, financing and other fees are separate fields,
// not a lump sum. Slippage is never reported by a broker: it is derived, and
// only against an explicit benchmark price with a stated source.
//
// # Identity
//
// An Activity's durable identity is its Key: provider, environment
// (practice or live), provider account ID and provider activity ID. It is
// what makes import idempotent. ImportedAt is ingestion metadata and is not
// part of identity or of SameRecord.
//
// # Numerics and boundary
//
// Quantities, prices and money use Trader's exact num types. The package
// imports no provider SDK and no runtime, journal, broker, backtest,
// service, cmd or adapter package (enforced by boundary_test.go). It does
// not read the clock, files or the network; the importer supplies
// ImportedAt from an injected clock.
//
// Storage, import, trade reconstruction and slippage analysis are separate
// work (issues #509 through #517).
package accountactivity
