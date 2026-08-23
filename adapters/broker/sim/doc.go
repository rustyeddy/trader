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
// StatusWorking and emits the corresponding EventKindOrder event, but
// performs no fill matching: every simulated order stays working until
// a later package (issue #149, M3-06) adds deterministic fill
// behavior. Cancel and Replace return broker.ErrUnsupported here;
// issue #151 (M3-08) implements their lifecycle semantics.
//
// # Determinism
//
// Broker never calls time.Now, a global random source, or global
// configuration. Deps injects a clock.Clock and an *id.Generator at
// construction (ADR-015); every timestamp and generated EventID a
// Broker produces derives from them. The same Deps and AccountConfig
// values reproduce byte-identical starting Snapshots and event streams
// across separate runs.
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
