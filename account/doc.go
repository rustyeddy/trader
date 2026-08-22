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
//
// # M3 audit (issue #146, M3-03)
//
// Snapshot's field set — cash balances, equity, buying power, margin
// used/available, realized/unrealized PnL, fees, financing, positions,
// and open orders, all denominated in one exact-value home Currency
// (num.Money/num.Quantity/num.Price, never float64) — was reviewed
// against what the M3 deterministic simulator (issues #148-#153) needs
// to represent authoritative account state and found sufficient with no
// changes: it is a plain, immutable value the simulator reconstructs
// via NewSnapshot after every state change, exactly as ADR-019 already
// specifies, rather than something requiring in-place mutation or a
// broker-scoped handle (that remains broker.Account's job; see
// ADR-007). See Snapshot's own doc comment for its zero-value/unknown-
// state semantics, added as part of this audit. account continues to
// import only id, instrument, num, and order — no broker or transport
// dependency — which broker/arch_test.go's
// TestAccountAndOrderDoNotImportBroker enforces mechanically.
//
// One related gap was identified but is explicitly out of this
// package's scope: computing realized PnL from a fill's Price and
// Quantity needs Price-by-Quantity multiplication, which num does not
// yet provide (see ADR-018's discussion of the same gap for
// order.Order.AvgFillPrice). That is a num/order-package concern for
// M3-09 (issue #152) to resolve when it implements PnL accounting, not
// something Snapshot's own fields need to change to accommodate.
package account
