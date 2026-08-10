// Package account models one broker account's authoritative observed
// snapshot (issue #30, M1-12, ADR-019): an immutable value describing
// what a broker reported about one account at one point in time. It
// does not query brokers, reconcile local state against broker truth,
// or mutate anything after construction — see ADR-007 and the live
// package (not yet built) for those responsibilities. Deposit and
// withdrawal history (account.CashLedgerEntry in the architecture
// document) is also out of scope here; it is additive future work.
//
// # Snapshot, not a live handle
//
// Snapshot is deliberately distinct from a broker-scoped action handle
// (ADR-007's broker.Account): Snapshot is a value describing observed
// state, with no methods that perform I/O. Constructing one does not
// imply the data is current; AsOf is the only claim this package makes
// about freshness.
//
// # Construction validates, ingestion clones
//
// NewSnapshot takes a SnapshotParams value rather than a Snapshot,
// because Snapshot's collection-typed fields are unexported so nothing
// outside this package can mutate them after construction. Every
// balance field denominated in the account's home Currency is checked
// against it; CashBalances may span several currencies (a real FX
// account routinely holds more than one) and must not repeat a
// currency. Every Position must belong to this account, must be listed
// through this account's own Broker (Listing.Provider must
// case-insensitively equal SnapshotParams.Broker — a snapshot is one
// broker account's observation, so a contained listing from a different
// provider would misrepresent whose position it is), and must name a
// distinct (instrument, provider, venue) listing; every OpenOrder must
// belong to this account and this Broker, must not already be in a
// terminal status (see order.Status.Terminal), and must not repeat an
// OrderID. Positions and orders are revalidated through
// order.NewPosition/order.NewOrder rather than trusted by provenance,
// for the same reason order's own constructors revalidate nested
// stages: exported struct fields let a caller build an invalid value as
// a bare literal.
//
// Positions, OpenOrders, and CashBalances are deep-copied on the way in
// and every time an accessor returns them: order.Order and
// order.Position both carry pointer and slice fields (AcceptedQuantity,
// Rejection, AppliedFillIDs, AvgPrice, and so on) that a shallow copy
// would still share with the caller, defeating immutability. See
// cloneOrder and clonePosition.
package account
