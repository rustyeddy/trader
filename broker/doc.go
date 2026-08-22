// Package broker defines Trader's public broker/account contracts
// (issue #145, M3-02, ADR-007 and ADR-008): the smallest stable ports
// needed by both the deterministic simulated broker and future real
// broker adapters. It does not implement a broker; it defines what one
// is.
//
// # Broker versus Account
//
// Broker is a session-level port: identity, account discovery, opening
// an operational handle, and session lifecycle. Account is that
// operational handle — actions and queries scoped to one broker
// account. This mirrors ADR-007's split between a broker-scoped action
// handle and account.Snapshot, an immutable value describing observed
// state with no methods that perform I/O: Account.Snapshot returns one,
// but is not one.
//
// account.Reference is the lightweight identity Broker.Accounts returns
// for discovery, before any account is opened; it carries no balance or
// position data.
//
// # Canonical values only
//
// Every Broker/Account method takes and returns canonical account and
// order package values — never a provider-native or vendor SDK type.
// An adapter translates at its own boundary; that translation is never
// visible through this package's public contracts.
//
// # Deliberately deferred
//
// Two capabilities the architecture document describes for broker
// adapters are intentionally not part of this core contract:
//
//   - Event streaming (order/fill/position/balance events, and the
//     ordering guarantees around them) is issue #147 (M3-04)'s own
//     scope, not this one's. Submit's synchronous return reflects only
//     whatever the broker reports immediately (for example
//     StatusPendingSubmit or StatusWorking); later state changes are
//     expected to arrive as events once that contract exists.
//   - Capability discovery (bracket orders, native trailing stops,
//     and similar broker-specific features) is not needed by M3's
//     deterministic simulator, which supports only market, limit, and
//     stop orders; see the architecture document's discussion of
//     capability discovery over a lowest-common-denominator API.
//
// Both are additive: a future capability interface can extend this
// package without breaking Broker or Account, consistent with keeping
// public interfaces small (see the architecture document's
// architectural invariants).
//
// # Dependency direction
//
// broker may import account and order. Neither account nor order may
// import broker; TestAccountAndOrderDoNotImportBroker in arch_test.go
// enforces this mechanically.
package broker
