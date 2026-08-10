// Package portfolio models a Trader-level view spanning one or more
// account.Snapshot values (issue #30, M1-12, ADR-019): Portfolio, an
// immutable aggregate that preserves each contributing account's
// provenance rather than collapsing accounts into an opaque total.
//
// # Currency-safe aggregation
//
// num deliberately implements no implicit currency conversion (see the
// num.Money doc comment): Money.Convert performs only the exact
// arithmetic for an explicit target currency and rate, with no policy
// about when conversion is appropriate or where a rate comes from.
// portfolio is the layer that needs that policy, so it is the layer
// that owns it. NewPortfolio takes
// a BaseCurrency and a set of ConversionRate values supplied by the
// caller — portfolio never fetches rates itself. An account already
// denominated in BaseCurrency needs no rate. An account in any other
// currency needs a matching ConversionRate or its currency is recorded
// as missing.
//
// If every account resolved, ConversionStatus is ConversionComplete and
// Equity returns the summed, converted total. If any account's currency
// had no matching rate, ConversionStatus is ConversionIncomplete and
// Equity's second return value is false rather than a partial or
// misleading sum — see MissingCurrencies for exactly which currencies
// blocked it. ConversionRates returns only the rates actually used,
// preserving the provenance (source, observation time) the caller
// supplied, so a report can show what produced the total.
//
// # Exposure is a grouping, not a valuation
//
// Exposures groups positions across every account by instrument.ID —
// the cross-venue, cross-broker economic instrument, which is why this
// aggregation belongs in portfolio rather than account. It deliberately
// does not sum position quantities into a single number: two Listings
// for the same instrument are not guaranteed to express one Quantity
// unit identically (Spec.Multiplier can differ), and no live or
// historical price is available here to convert to a common notional
// value. Exposure instead preserves every contributing account's own
// Position unmodified, so a caller with the additional data required
// (multipliers, marks, conversion rates) can compute a defensible
// aggregate itself, rather than being handed a number whose units this
// package cannot vouch for.
//
// # Portfolio.AsOf is an observation time, not a claim of simultaneity
//
// Portfolio.AsOf is when the caller assembled this view — it does not
// imply every contributing account.Snapshot was observed at the same
// instant. Each account's own AsOf is preserved through Accounts;
// AccountAsOfRange reports the oldest and newest of them for a caller
// that wants to see how stale the least current account is.
package portfolio
