// Package external is the wire/domain conversion boundary between
// Strategy Protocol v1 (protocol/strategy/v1, issue #377, ADR-062) and
// Trader's existing domain types (issue #378, ADR-062's own "Wire DTOs
// are not aliases of Trader's domain types" section).
//
// Every exported function here converts in exactly one direction,
// matching the one direction that boundary actually needs from the
// host's own point of view — the perspective this package occupies
// per ADR-062 ("adapters/strategy/external ... implements the gRPC
// server side ... from the host runtime's own point of view"):
//
//   - Outgoing (domain -> wire), functions named ToWire*: what the
//     host process computes from its own real strategy.BarEvent,
//     strategy.FillEvent, account.Snapshot, and marketdata.Bar values
//     and writes down the wire.
//   - Incoming (wire -> domain), functions named FromWire*: what the
//     host process receives from a guest — a Handshake's
//     StrategyDescriptor, a RunOpen's session_id, an OnBarResponse's
//     described intents/signals/error, an OnFillResponse's error, or
//     a GetHistoryBarsRequest's query.
//
// No function here returns a *v1.* type to, or accepts one from, any
// package outside this one: the only packages that will ever import
// protocol/strategy/v1 are this one (host side, ADR-062) and the
// future strategysdk (guest side) — never strategy, order, account,
// marketdata, or any other domain package. strategy/boundary_test.go
// mechanically enforces the strategy half of that (strategy must
// never import protocol, the same way it must never import adapters);
// there is no analogous boundary_test.go in this package itself, since
// every other domain package's own existing boundary tests already
// keep this package's only inbound dependency direction (domain ->
// this package, never the reverse) intact.
//
// # Canonical identity is never minted here
//
// A guest process never gets to name a real order.IntentID,
// id.EventID, or id.CorrelationID directly (ADR-005, ADR-062):
// IntentsFromWire takes the run's own retained strategy.IntentFactory
// and calls its Enter/Exit/AdjustStop/EnterWithStop/TargetExposure/
// NewCorrelationID/WithCorrelation methods — the identical
// identity-generation path an in-process strategy already goes
// through — rather than constructing an order.Intent value by hand.
// This package owns no *id.Generator and never will.
//
// # Lossy by design, not by accident
//
// Several wire messages are a deliberately reduced slice of the
// domain value they represent (issue #377's own "no speculative
// surfaces beyond current concrete consumers"): PositionSnapshot
// omits Quantity, AccountSnapshot omits Equity/RealizedPnL/
// UnrealizedPnL/OpenOrders/CashBalances/BuyingPower/margin, and
// FillEvent omits FillID/BrokerOrderID/BrokerFillID/Timestamp/
// Commission. Conversion functions for these messages are therefore
// one-directional (ToWire only) by construction: there is no
// FromWireAccountSnapshot or FromWireFillEvent, because reconstructing
// a full account.Snapshot or order.Fill from what the wire actually
// carries would have to fabricate the missing fields, which is
// exactly the silent, lossy behavior this package's own doc comment
// (and ADR-004/ADR-045's broader "explicit over fabricated" rule)
// forbids.
package external
