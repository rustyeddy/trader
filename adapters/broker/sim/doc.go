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
// always for its complete accepted quantity. A fill that would open a
// new Position from flat is supported; a fill against a listing where
// the account already holds a Position returns
// ErrPositionUpdateUnsupported — correctly adding to, reducing,
// closing, or reversing a position needs weighted-average cost basis
// and realized PnL accounting that issue #152 (M3-09) owns, and this
// package would rather report that plainly than compute a silently
// wrong result. A fill does not yet affect account cash: issue #152
// also explicitly owns "cash/balance effects of fills," and a naive
// full-notional debit/credit is not broker-neutral accounting (see
// buildMarketFill's doc comment in account.go). Limit and stop orders
// remain StatusWorking with no fill matching until issue #150 (M3-07)
// adds their trigger semantics. Cancel and Replace return
// broker.ErrUnsupported here; issue #151 (M3-08) implements their
// lifecycle semantics.
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
