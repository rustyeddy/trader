// Package order defines Trader's broker-neutral order and execution
// vocabulary (issue #28, M1-10, ADR-017) and its lifecycle transition
// rules (issue #29, M1-11, ADR-018). It defines what these lifecycle
// concepts are, what makes one well-formed, and which transitions
// between them are legal; it does not implement broker I/O or an
// execution manager.
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
// # Lifecycle transitions
//
// Status.CanTransitionTo names the legal transition graph, and Terminal
// names StatusFilled, StatusCanceled, StatusRejected, and StatusExpired
// as states nothing follows. Named functions — ApplyAcceptance,
// ApplyRejection, ApplyFill, ApplyCancelRequest/ApplyCancelResult,
// ApplyReplaceRequest/ApplyReplaceResult, ApplyExpiration — apply one
// event to an Order, each checking the graph and its own identity
// requirements before mutating, then revalidating the result through
// NewOrder. There is no single generic ApplyTransition: each real
// transition carries its own associated data (a fill's quantity, a
// rejection reason, a replace's new terms), which a generic mutator
// would force callers to pre-populate correctly by convention instead of
// by the type system.
//
// Two rules make redelivered and out-of-order events safe rather than
// merely detected:
//
//   - Redelivering an unchanged Status (a broker resending "Filled" for
//     an already-Filled Order) is always legal — CanTransitionTo(s, s)
//     is true for every valid, known Status, including terminal ones.
//     StatusUnknown is the one exception: it is never a legal target,
//     not even from itself, since Unknown means "needs operator
//     attention," not "safe to no-op."
//   - Reaching StatusCanceled, or a replace's resulting status, requires
//     having passed through StatusPendingCancel/StatusPendingReplace
//     first. A terminal confirmation arriving without Trader ever having
//     locally recorded the pending step is treated as an out-of-order
//     event and rejected by the graph, not silently absorbed;
//     reconciling that gap is a live-package concern.
//
// Every Apply* function that takes an external event (Fill, CancelResult,
// ReplaceRequest, ReplaceResult) verifies the event's identifying fields
// match the Order before applying it — ErrOrderMismatch, distinct from
// ErrIllegalTransition, since an event that belongs to a different order
// entirely is a more serious failure than an otherwise-valid but
// unreachable state change.
//
// Matching OrderID is not enough to prove a CancelResult/ReplaceResult
// belongs to the *currently outstanding* cancel/replace cycle: a delayed
// result from an earlier cycle on the same order could otherwise be
// misapplied during a later one. ApplyCancelRequest/ApplyReplaceRequest
// record the request's Metadata.EventID as Order.PendingCommandID, and
// ApplyCancelResult/ApplyReplaceResult require a result's
// Metadata.CausationID to match it — ErrStaleResult otherwise — whenever
// PendingCommandID is set. Once no command is outstanding (for example,
// because ApplyFill already overrode a pending cancel/replace with a
// complete fill), there is nothing left to correlate against, and the
// result is validated purely against the transition graph instead: a
// result matching the Order's current status is accepted as an
// idempotent no-op, satisfying redelivery of the same confirmation,
// while a result naming anything else is rejected as unreachable.
//
// ApplyFill additionally detects duplicate delivery of the same broker
// execution: fill.BrokerFillID is checked first, against
// Order.AppliedBrokerFillIDs, since an adapter is not guaranteed to
// reuse the same Trader FillID across redelivery of one broker
// execution, while broker state is architecturally authoritative;
// fill.FillID against Order.AppliedFillIDs is the fallback for adapters
// that do not report a BrokerFillID. A detected duplicate returns the
// Order unchanged alongside ErrDuplicateFill — detectable, but not
// fatal.
//
// A partial fill arriving while a cancel or replace is pending preserves
// StatusPendingCancel/StatusPendingReplace rather than downgrading to
// StatusPartiallyFilled, since the broker's pending confirmation may
// still arrive; only a fill that exactly completes AcceptedQuantity
// overrides the pending status to StatusFilled, since there is nothing
// left to cancel or replace once an order is fully filled.
//
// A replace result amends Order in place — same Trader OrderID, updated
// Accepted* fields — rather than modeling "replaced/superseded" as a
// distinct persistent state or a new order identity; see ADR-018 for
// why. The amendment only applies when result.Rejection is nil AND
// result.Status is StatusWorking or StatusPartiallyFilled: those are
// the only outcomes that actually mean "the replacement took effect and
// the order remains live." A non-rejected StatusCanceled or
// StatusFilled result still transitions Order.Status, but leaves the
// accepted values untouched, since the order's life resolved a
// different way around the same time as the replace.
//
// ApplyFill clears AvgFillPrice to nil on every fill rather than
// maintain it: num has no Price-by-Quantity multiplication yet, and
// leaving a stale average computed against an earlier, smaller
// FilledQuantity would be a false snapshot.
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
