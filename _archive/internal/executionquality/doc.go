// Package executionquality measures how a fill compared with an explicit
// reference price (issue #513, ADR-083). It is a pure derivation over an
// accountactivity.Activity: it stores nothing, performs no I/O, reads no clock,
// and writes nothing back into the ledger. The ledger answers what the broker
// reported; this package answers how that execution compares with a reference
// the caller chose, and a different reference later is a different call, not a
// rewrite of history.
//
// # The benchmark is explicit
//
// Measure never selects a benchmark. The caller supplies one, with a kind
// (order reference, quote mid at submit, trigger price, historical mid, the
// broker's fullVWAP, the broker's quote touch), a source, and ideally the time
// it was taken. The kind, source and time survive into the Result, because the
// kinds answer different questions. No benchmark means unknown slippage, never
// zero.
//
// # Execution price and its provenance
//
// The execution price is, in order of preference:
//
//  1. the units-weighted average of the broker's per-trade effect prices, when
//     every effect has units and an exact price and the units add up to the
//     fill (one exact calculation, one half-even rounding);
//  2. the provider's fill-level aggregate price, labelled as a non-authoritative
//     fallback;
//  3. unknown.
//
// A partial average is never presented as complete. The Result says which
// method produced the price.
//
// # Signs
//
// Slippage follows the account's point of view, like every amount in the
// ledger: negative is worse for the trader, positive better, zero neutral.
// With d = execution - benchmark, a buy has slippage -d and a sell +d.
//
// # Cost and currency
//
// Slippage multiplied by units is money in the instrument's quote currency.
// It becomes the account currency only when the quote currency already is
// that currency, or when the caller supplies an explicit Conversion with its
// own provenance. A cross rate is never inferred; without a path the cost is
// unknown while the price-unit slippage stays known.
//
// # Separate from spread cost and from trade history
//
// A broker-reported SpreadCost is neither added to nor subtracted from
// slippage, and slippage is not a term of tradehistory.Net.
package executionquality
