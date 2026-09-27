// Package margin is the one shared, pure calculation of an account's
// gross notional exposure and required initial margin (ADR-066, issue
// #411). Both the account-level initial-margin risk rule (#413) and the
// simulated broker's fill-time check (#415) use it, so Trader has
// exactly one definition of gross exposure and required margin.
//
// # What it computes
//
// Gross notional is the sum of the absolute notional of every open
// position — longs and shorts both add, and nothing is netted across
// instruments:
//
//	gross = Σ |quantityᵢ| × priceᵢ × multiplierᵢ
//
// Required margin is a sum of per-position required-margin amounts,
// each computed by a Policy:
//
//	required = Σ policy.RequiredMargin(positionᵢ)
//
// v1 has one Policy, Ratio, which requires |notional| × ratio for every
// position, so v1's required margin is exactly gross × ratio. The
// per-position Policy is the extension point ADR-066 reserves for
// per-listing or per-instrument margin (a percentage such as OANDA's
// marginRate, or a fixed amount per futures contract). No such policy
// exists yet.
//
// # Valuation basis
//
// Every position is valued at exactly one price — never a blend of a
// current price and a position's historical AvgPrice (#183). Account
// values each open position at its current mark, supplied by the
// caller in Marks. Assess values the changed instrument at the
// Change's own price for both the current and the prospective state,
// so comparing the two is a pure comparison of that instrument's
// quantity; every other position is valued at its mark in both.
//
// Deciding whether a proposal is de-risking, and whether a price is
// needed at all, is the caller's job (ADR-066): a de-risking proposal
// can be admitted from quantities alone, without calling this package.
//
// # Constraints
//
// All arithmetic uses Trader's exact num types (ADR-004); there is no
// floating point. v1 supports one account currency: every listing must
// settle in the currency the caller passes, and any mismatch is
// ErrCurrencyMismatch rather than a conversion. The package performs
// no I/O, holds no state, and is safe for concurrent use.
package margin
