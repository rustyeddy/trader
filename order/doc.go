// Package order defines Trader's broker-neutral order and execution
// vocabulary, as decided by issue #28 (M1-10) and ADR-017. It defines
// what these lifecycle concepts are and what makes one well-formed; it
// does not implement order-state transition rules (Working -> Filled,
// terminal-state enforcement, duplicate/out-of-order event handling —
// see issue #29/M1-11), broker I/O, or an execution manager.
//
// # The proposal-to-fill chain
//
// Proposal, Request, Order, and Fill are deliberately distinct types,
// not variations on one struct:
//
//	Proposal: a candidate order before risk approval
//	Request:  a Proposal assigned a Trader OrderID, ready for submission
//	Order:    the broker's accepted order and its lifecycle state
//	Fill:     one execution against an Order
//
// Request.OrderID (from the id package, M1-06) doubles as the
// idempotency key for creating that order: a retried submission reuses
// the same OrderID, and a broker adapter dedupes on it as the client
// order ID. That covers idempotency for initial submission only — a
// cancel or replace against an existing order is a separate command with
// its own idempotency needs; see CancelRequest and ReplaceRequest.
//
// # Requested versus accepted values
//
// Order embeds its originating Request, preserving the requested
// quantity and prices, alongside separately tracked AcceptedQuantity,
// AcceptedLimitPrice, and AcceptedStopPrice: a broker may accept an
// order but normalize its values (tick rounding, quantity increment,
// venue rules), and Trader represents both sides of that boundary
// explicitly rather than inferring one from the other. RemainingQuantity
// derives from AcceptedQuantity minus FilledQuantity — never from the
// originally requested quantity, and never stored as an independent
// field that could drift out of sync with the two values it depends on.
//
// # Broker-native identifiers are preserved, not replaced
//
// Order carries BrokerOrderID, and Fill carries both BrokerOrderID and
// BrokerFillID, alongside Trader's own OrderID and FillID: a broker
// execution normally has its own native identity independent of its
// parent order, which matters for reconciliation and deduplicating
// broker execution events. Fill also carries AccountID directly, since a
// fill is a broker/account event in its own right and should not require
// a join through its parent Order to know which account it belongs to.
//
// # Unknown external state is representable, not fatal
//
// Status and RejectReason both reserve their zero value — StatusUnknown,
// ReasonUnknown — for broker-reported state Trader does not yet
// recognize, the same pattern marketdata.Status established: an adapter
// parsing an unfamiliar broker status or rejection code produces the
// Unknown value instead of crashing or guessing, and an uninitialized
// value can never be silently mistaken for something specific.
// Rejection.BrokerCode additionally preserves the broker's own original
// text even when Reason can only be classified as ReasonUnknown, so
// information is never silently dropped.
//
// Side, Type, and TimeInForce are different: they are closed,
// Trader-controlled vocabulary rather than broker-reported state, so
// there is no "unknown" case for them — construction sites reject
// anything outside their defined values. Their zero value is still
// reserved as invalid/unset rather than defaulting to one of the real
// values, so a forgotten field is never silently mistaken for Buy or
// Market.
//
// # Position and Trade
//
// Position represents one net position per account/listing pair — the
// architecture's default assumption for the initial account model — with
// PositionSide (Flat, Long, Short) kept as a distinct type from Side: an
// order Side is a transaction direction, a PositionSide is a holding
// direction, and Flat has no order-side equivalent. Trade is the derived
// reporting concept grouping entry and exit fills into a round-trip; it
// deliberately has no first-class identity in this package, since it can
// be recalculated or revised as fills continue to arrive — a dedicated
// TradeID is additive future work if the journal/report milestones later
// need one.
//
// # Deferred
//
// OCO/bracket order grouping and parent/child order relationships are
// capability-driven features, not part of this initial vocabulary — see
// the architecture document's discussion of capability discovery over a
// lowest-common-denominator API. GTD (good-til-date) is deferred from
// TimeInForce for the same reason: it needs an expiration field this
// issue's scope does not call for. Both are additive when a real
// consumer needs them.
package order
