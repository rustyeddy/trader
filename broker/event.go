package broker

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/order"
)

// EventKind identifies which payload field of an Event is populated.
// Unlike order.Status or order.RejectReason, EventKind is Trader's own
// closed vocabulary, not broker-reported state: Trader itself decides
// which category an event belongs to when constructing it, so — like
// order.Side and order.Type (ADR-017) — the zero value is reserved as
// invalid/unset rather than a broker-facing "unknown" sentinel.
type EventKind uint8

const (
	// EventKindUnknown is EventKind's zero value. It is never a valid
	// Event.Kind.
	EventKindUnknown EventKind = iota
	// EventKindOrder reports an order's acceptance, rejection, or any
	// other status change. Event.Order is populated; order.Order.Status
	// (see order/transition.go) says which. Trader represents every
	// order-lifecycle change as one kind rather than one kind per
	// status, since order.Order already carries the full picture of
	// what changed and a consumer needs the whole Order regardless.
	EventKindOrder
	// EventKindFill reports one execution against an order.
	// Event.Fill is populated.
	EventKindFill
	// EventKindAccount reports a change to authoritative account state
	// — balances, margin, or PnL. Event.Account is populated with the
	// account's new, complete Snapshot rather than a partial delta:
	// account.Snapshot (ADR-019) is already an immutable, internally
	// consistent whole-state value, and inventing a second, partial
	// "balance changed" payload type would let the two disagree.
	EventKindAccount
	// EventKindStatus reports a change in the broker session's or
	// account's operational status (for example, connected, degraded,
	// or disconnected). Event.Status is populated.
	EventKindStatus
)

// String returns a human-readable EventKind name.
func (k EventKind) String() string {
	switch k {
	case EventKindOrder:
		return "order"
	case EventKindFill:
		return "fill"
	case EventKindAccount:
		return "account"
	case EventKindStatus:
		return "status"
	default:
		return fmt.Sprintf("EventKind(%d)", uint8(k))
	}
}

func (k EventKind) valid() bool {
	switch k {
	case EventKindOrder, EventKindFill, EventKindAccount, EventKindStatus:
		return true
	default:
		return false
	}
}

// AccountStatus is a broker session's or account's operational status
// as reported through an EventKindStatus Event. Unlike EventKind, this
// value is broker-reported, not Trader-decided, so — matching
// order.Status and order.RejectReason (ADR-017) — its zero value is
// reserved for status an adapter cannot classify, rather than treating
// an unrecognized value as fatal.
type AccountStatus uint8

const (
	// AccountStatusUnknown reports broker/session status an adapter
	// could not classify. It is a legal Event.Status value, not an
	// error: an adapter that cannot classify a status still reports the
	// event rather than dropping it.
	AccountStatusUnknown AccountStatus = iota
	// AccountStatusActive means the broker session and account are
	// connected and able to trade.
	AccountStatusActive
	// AccountStatusDegraded means the broker session is connected but
	// operating in a reduced-confidence state — for example, stale
	// market data or a slow connection. Whether trading is still
	// permitted while degraded is a live-session guard policy (the
	// future live package), not something this status decides on its
	// own.
	AccountStatusDegraded
	// AccountStatusDisconnected means the broker session is not
	// currently connected. A deterministic simulator, which has no
	// network to lose, is not expected to ever report this value.
	AccountStatusDisconnected
)

// String returns a human-readable AccountStatus name.
func (s AccountStatus) String() string {
	switch s {
	case AccountStatusUnknown:
		return "unknown"
	case AccountStatusActive:
		return "active"
	case AccountStatusDegraded:
		return "degraded"
	case AccountStatusDisconnected:
		return "disconnected"
	default:
		return fmt.Sprintf("AccountStatus(%d)", uint8(s))
	}
}

func (s AccountStatus) valid() bool {
	switch s {
	case AccountStatusUnknown, AccountStatusActive, AccountStatusDegraded, AccountStatusDisconnected:
		return true
	default:
		return false
	}
}

// Event is one broker-neutral occurrence delivered through an
// Account's EventReader (ADR-024). Kind says which of Order, Fill,
// Account, or Status is populated; the other three are nil.
//
// # Identity, correlation, and time
//
// Metadata.EventID identifies this event. Metadata.CorrelationID
// should carry the correlation id of the workflow this event belongs to
// (for example, the same id an originating order.Request shared), and
// Metadata.CausationID should name the EventID of whatever directly
// caused this event when Trader can determine one — an order.Request's
// EventID for the OrderEvent it produced, or a preceding Event's
// EventID for one event that triggers another. Metadata.Timestamp is
// this event's occurred time: when the broker or simulator considers
// the event to have happened. ObservedAt is separate: when Trader's own
// process observed or received it. For a deterministic simulator the
// two are typically equal; for a real broker adapter they usually are
// not, since network and processing delay separate them. Neither
// Timestamp nor ObservedAt is required to be monotonic across events —
// see Sequence for Trader's actual ordering guarantee.
//
// # Deterministic ordering
//
// Sequence is the sole ordering guarantee this package makes: within
// one Broker session's Account event stream, Sequence is assigned in
// the exact order the producer generated the events, strictly
// increasing with no gaps, starting at 1. A consumer that sorts
// delivered events by Sequence recovers the producer's exact original
// order, regardless of Timestamp/ObservedAt (which may disagree with
// it, especially under simulated or real network latency). A
// deterministic simulator (ADR-008) must assign Sequence values that
// are reproducible from the same seed/inputs across separate runs, the
// same way every other simulator output must be (see the architecture
// document's determinism principles). Sequence is scoped to one
// Account's stream; it carries no meaning compared across different
// accounts or different Broker sessions.
//
// # Duplicates, delayed, and out-of-order delivery
//
// Event delivery is at-least-once, matching the architecture document's
// general delivery-semantics rule: a consumer resuming from an
// EventCursor after a disconnect may receive one or more events it
// already saw (Sequence <= the cursor it resumed from). Consumers must
// dedupe by Metadata.EventID — the same idempotency discipline
// order.Order's Apply* functions already require for fills and
// cancel/replace results (ADR-018). Within one uninterrupted stream, an
// implementation must not reorder or skip events; EventReader.Next
// never returns a later Sequence before an earlier one. Reconciling a
// stream that appears to have skipped Sequence values entirely (for
// example, after an adapter restart with a shorter retention window
// than the gap) is a live-session concern (the future live package and
// ADR-009), not something this package's contract resolves.
//
// Critical events — every EventKindOrder, EventKindFill, and
// EventKindAccount event — are never silently dropped or coalesced by a
// conforming implementation, matching the architecture document's
// concurrency-ownership rule that only high-frequency, quote-like data
// may ever use a documented coalescing policy. EventKindStatus events
// describing session health are likewise never dropped, though a
// producer may coalesce a rapid run of identical consecutive Status
// values into one event if it documents that choice.
type Event struct {
	Metadata   id.Metadata
	ObservedAt time.Time
	Sequence   uint64
	Kind       EventKind

	Order   *order.Order
	Fill    *order.Fill
	Account *account.Snapshot
	Status  *AccountStatus
}

// NewEvent validates and returns an Event. Metadata.EventID must be
// non-zero, Metadata.Timestamp must be non-zero, ObservedAt must be
// non-zero, Sequence must be greater than zero, and Kind must be one of
// its defined values. Exactly one of Order, Fill, Account, or Status
// must be non-nil, matching Kind. Order and Fill are fully revalidated
// through order.NewOrder/order.NewFill — not merely trusted by
// provenance, for the same reason every other stage in the order
// package revalidates a nested value (ADR-017) — Account is not, since
// account.Snapshot's unexported fields already make it impossible to
// construct outside the account package as an unvalidated bare literal.
func NewEvent(e Event) (Event, error) {
	if e.Metadata.EventID.IsZero() {
		return Event{}, fmt.Errorf("%w: event id must be set", ErrInvalidEvent)
	}
	if e.Metadata.Timestamp.IsZero() {
		return Event{}, fmt.Errorf("%w: metadata timestamp must be set", ErrInvalidEvent)
	}
	if e.ObservedAt.IsZero() {
		return Event{}, fmt.Errorf("%w: observed-at time must be set", ErrInvalidEvent)
	}
	if e.Sequence == 0 {
		return Event{}, fmt.Errorf("%w: sequence must be greater than zero", ErrInvalidEvent)
	}
	if !e.Kind.valid() {
		return Event{}, fmt.Errorf("%w: invalid kind %v", ErrInvalidEvent, e.Kind)
	}

	populated := 0
	if e.Order != nil {
		populated++
	}
	if e.Fill != nil {
		populated++
	}
	if e.Account != nil {
		populated++
	}
	if e.Status != nil {
		populated++
	}
	if populated != 1 {
		return Event{}, fmt.Errorf("%w: exactly one of Order, Fill, Account, or Status must be set, found %d", ErrInvalidEvent, populated)
	}

	switch e.Kind {
	case EventKindOrder:
		if e.Order == nil {
			return Event{}, fmt.Errorf("%w: kind order requires Order", ErrInvalidEvent)
		}
		validated, err := order.NewOrder(*e.Order)
		if err != nil {
			return Event{}, fmt.Errorf("%w: order: %v", ErrInvalidEvent, err)
		}
		e.Order = &validated
	case EventKindFill:
		if e.Fill == nil {
			return Event{}, fmt.Errorf("%w: kind fill requires Fill", ErrInvalidEvent)
		}
		validated, err := order.NewFill(*e.Fill)
		if err != nil {
			return Event{}, fmt.Errorf("%w: fill: %v", ErrInvalidEvent, err)
		}
		e.Fill = &validated
	case EventKindAccount:
		if e.Account == nil {
			return Event{}, fmt.Errorf("%w: kind account requires Account", ErrInvalidEvent)
		}
	case EventKindStatus:
		if e.Status == nil {
			return Event{}, fmt.Errorf("%w: kind status requires Status", ErrInvalidEvent)
		}
		if !e.Status.valid() {
			return Event{}, fmt.Errorf("%w: invalid account status %v", ErrInvalidEvent, *e.Status)
		}
	}

	return e, nil
}
