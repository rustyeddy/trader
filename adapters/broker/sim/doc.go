// Package sim implements Trader's first concrete broker adapter: a
// deterministic, in-memory simulated broker (issue #148, M3-05,
// ADR-008). It implements the public broker.Broker and broker.Account
// ports directly — not a bespoke backtest-only path — so backtests and
// paper runs compose it exactly as live runs compose a real broker
// adapter.
//
// # Scope of this package
//
// This package owns broker/account construction, clean shutdown,
// account discovery, deterministic starting balances, and account
// state isolation. Submit validates and accepts an order.Request into
// StatusWorking and emits the corresponding EventKindOrder event.
//
// A market order additionally fills immediately and completely at the
// price Deps.Prices reports (issue #149, M3-06): Submit emits the
// order-accepted EventKindOrder event (StatusWorking), then an
// EventKindFill event, then a second EventKindOrder event recording
// the resulting StatusFilled transition — in that deterministic order,
// each causally chained to the one before it. This package has no
// partial-fill or order-book volume model, so a market order's fill is
// always for its complete accepted quantity.
//
// Every fill — market (Submit) or triggered limit/stop (Advance) —
// updates authoritative position and PnL state (issue #152, M3-09):
// opening a new Position from flat, increasing an existing same-side
// Position (recomputing its quantity-weighted AvgPrice via
// Money.DivQuantity, ADR-027), reducing or exactly closing an opposite-
// side Position, and reversing one (closing it, then opening a new
// Position in the new direction for the remainder) are all supported —
// see order.ApplyFillToPosition for the full transition table (moved
// out of this package into broker-neutral position accounting by
// issue #217, M5-09, so backtest trade derivation can share the exact
// same math).
// Cash moves only by realized PnL (on a reduce/close/reverse) and, when
// a fill reports a non-nil order.Fill.Commission, by that commission —
// never by a universal full-notional debit/credit on open/increase,
// which is not broker-neutral accounting (a cash purchase should leave
// equity roughly unchanged, not book the full notional as an immediate
// loss). See buildFill's doc comment in account.go for the full
// reasoning and the design discussion this followed on issue #152.
//
// Every fill is rejected outright — before any position/PnL state is
// touched — if the listing's settlement currency does not match the
// account's own currency (ErrUnsupportedSettlementCurrency). Cash,
// realized/unrealized PnL, and fees all accumulate in the listing's
// settlement currency, and this package has no FX conversion-rate
// source to reconcile a mismatch; rather than let that surface as a
// num.ErrCurrencyMismatch deep inside PnL arithmetic (or, worse,
// succeed silently on the zero-PnL open case and fail unpredictably
// later), this package rejects the whole trade explicitly and
// consistently across every transition.
//
// Execution assumptions beyond the base fill price are explicit,
// optional, and deterministic (issue #153, M3-10, ADR-028):
// Deps.Slippage adjusts a Market or Stop fill's price (never Limit,
// which is a price guarantee) and Deps.Commission computes the fee
// owed, both consulted from buildFill, in that order — slippage first,
// then commission from the resulting final price. Both default to nil
// (no slippage, no fee), matching Deps.IntrabarPolicy's existing
// zero-value-is-a-real-default pattern — unlike Deps.Prices, which
// remains required and validated non-nil — rather than requiring every
// caller to pass a no-op implementation. See models.go for the full
// SlippageModel/CommissionModel/ModelInfo contract.
//
// Cancel and Replace (issue #151, M3-08) resolve synchronously within
// one call — this simulator has no real broker latency, so
// StatusPendingCancel/StatusPendingReplace exist only transiently
// inside one Cancel/Replace call's own event sequence, never as
// durable state a later call observes. Both emit two causally chained
// EventKindOrder events (pending, then resolved) when the target order
// is StatusWorking/StatusPartiallyFilled; an order in any other
// terminal status declines the command (CancelResult/ReplaceResult
// reports the order's actual status and a Rejection) without any
// transition or event, since nothing about the order changed. Replace
// decides accept-versus-decline by revalidating the order with the
// requested new values applied through order.NewOrder — the same
// tick-size/quantity-increment/filled-quantity rules every other order
// construction obeys — rather than inventing separate replace-specific
// validation. Both require req.Metadata.EventID to be non-zero
// uniformly — before any status-dependent branch — so a malformed
// request is rejected the same way regardless of whether the target
// order happens to be live or already terminal; a returned Cancel/
// Replace result always correlates to its own request via
// Metadata.CausationID, on every outcome including a decline or an
// idempotent no-op. Both honor ctx cancellation the same way Advance
// does (checked before validating req or acquiring the account lock,
// and again immediately after). See cancel_replace.go for the full
// contract, including each method's idempotency story.
//
// Limit and stop orders remain StatusWorking with no fill matching at
// Submit time; Broker.Advance (issue #150, M3-07, ADR-026) is how they
// fill. Advance is not part of the public broker.Broker/broker.Account
// ports — a real adapter has no simulation to drive — and is called
// with a simulator-owned Observation (listing, OHLC, time), not
// marketdata.Bar, once per market observation per listing. Advance
// evaluates every account independently: for one account's pending
// Limit/Stop orders on the observed listing, every order that triggers
// at the observation's Open fills first (in deterministic order — the
// open is the bar's single, known first instant, so these are never
// ambiguous relative to each other), using the trigger/gap rules
// documented on limitTriggerPrice/stopTriggerPrice in advance.go. At
// most one order that triggers elsewhere within the bar fills after
// them; more than one such within-bar trigger is genuinely ambiguous —
// OHLC alone cannot say which the market reached first — and, under
// the default IntrabarRejectAmbiguous policy, reports
// ErrAmbiguousIntrabarOrder and fills none of that group (any at-open
// orders still fill). IntrabarPessimistic is declared but not
// implemented for that ambiguous case — selecting it reports
// broker.ErrUnsupported. StopLimit orders are not evaluated by Advance
// at all in this package yet. A triggered fill shares Submit's
// market-order fill and position/PnL accounting mechanics exactly,
// differing only in how its price and event causation are derived.
//
// Advance also revalues every open Position's mark from the
// observation's Close whenever obs.Listing matches one, even if no
// pending order triggers — unrealized PnL (issue #152, M3-09) must not
// go stale merely because there was nothing to fill. Advance honors
// context cancellation between accounts and before committing each
// individual fill, but never rolls back a fill that already committed
// before cancellation was observed.
//
// # Determinism
//
// Broker never calls time.Now, a global random source, or global
// configuration. Deps injects a clock.Clock, an *id.Generator, and a
// FillPriceSource at construction (ADR-015); every timestamp,
// generated identifier, and market-order fill price a Broker produces
// derives from them. The same Deps and AccountConfig values reproduce
// byte-identical starting Snapshots and event streams across separate
// runs, provided the injected FillPriceSource is itself deterministic.
// Advance derives every timestamp it produces from Deps.Clock as well,
// not from an Observation's own Time; a caller driving a backtest is
// expected to keep Clock synchronized with each Observation it
// advances.
//
// # Account isolation
//
// Each account's mutable state (cash, open orders, event log) lives in
// its own accountState, guarded by its own mutex. Broker's own mutex
// protects only the account map and its closed flag — looking up or
// opening one account never blocks concurrent operations against a
// different account. See Broker and accountHandle's doc comments for
// the full concurrency-ownership contract.
package sim
