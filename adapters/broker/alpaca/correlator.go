package alpaca

import (
	"sync"

	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
)

// correlator is one account's adapter-local, in-memory correlation
// state: it owns Sequence assignment for this account's Event stream
// (Alpaca reports none — this adapter, like adapters/broker/sim's
// accountState, is the sole authority for its own stream's Sequence),
// the Trader-OrderID-to-Alpaca-native-order-id mapping Cancel/Replace
// need, and the per-order (status, filled quantity) last observed by
// the polling EventReader, used to detect what has changed since the
// previous poll.
//
// Like accountState in adapters/broker/sim, correlator is scoped to one
// account and shared by every accountHandle/EventReader opened against
// it — Account.Events resumes the same append-only events log and
// Sequence counter across calls, not a fresh one per reader. None of
// this state survives a process restart: Alpaca's own order id lookup
// (GetOrderByClientOrderID) is the fallback Cancel/Replace use when a
// Trader OrderID is not found in brokerOrderIDs, exactly for that case.
// Sequence continuity across a restart is out of scope here, matching
// sim's own equivalent in-memory-only limitation and ADR-009's broader
// deferral of live-session recovery.
type correlator struct {
	mu sync.Mutex

	nextSequence uint64
	events       []brokerpkg.Event

	// brokerOrderIDs maps Trader's OrderID to Alpaca's native order id,
	// populated by Submit and by every wire order this adapter observes
	// (Snapshot, the polling EventReader, Replace).
	brokerOrderIDs map[id.OrderID]string
	// lastObserved is the polling EventReader's own de-duplication state:
	// the (status, filled quantity) last seen for each Alpaca order id,
	// so a poll only synthesizes events for what genuinely changed.
	lastObserved map[string]observedOrderState

	closed  bool
	changed chan struct{}
}

// observedOrderState is what the polling EventReader compares between
// polls to detect a status change or a filled-quantity increase.
type observedOrderState struct {
	status    string
	filledQty string
}

func newCorrelator() *correlator {
	return &correlator{
		brokerOrderIDs: make(map[id.OrderID]string),
		lastObserved:   make(map[string]observedOrderState),
		changed:        make(chan struct{}),
	}
}

// rememberOrderID records the Alpaca native id for a Trader OrderID.
func (c *correlator) rememberOrderID(orderID id.OrderID, alpacaOrderID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.brokerOrderIDs[orderID] = alpacaOrderID
}

// lookupOrderID returns the Alpaca native id previously recorded for
// orderID, if any.
func (c *correlator) lookupOrderID(orderID id.OrderID) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.brokerOrderIDs[orderID]
	return v, ok
}

// appendEvent assigns the next Sequence to ev, appends it to the
// account's event log, and wakes every blocked EventReader. The caller
// supplies ev with Sequence left at its zero value; appendEvent fills
// it in and returns the committed Event.
//
// It reports brokerpkg.ErrClosed, touching neither c.events nor
// c.changed, if the correlator was already closed by the time this
// call acquired c.mu — Broker.Close (see close, below) can race an
// in-flight Submit/poll that is about to append its own event; without
// this check, appendEvent would close(c.changed) a second time after
// close's own close(c.changed) already ran, panicking (PR #313 review).
func (c *correlator) appendEvent(build func(sequence uint64) (brokerpkg.Event, error)) (brokerpkg.Event, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return brokerpkg.Event{}, brokerpkg.ErrClosed
	}
	ev, err := build(c.nextSequence + 1)
	if err != nil {
		return brokerpkg.Event{}, err
	}
	c.nextSequence = ev.Sequence
	c.events = append(c.events, ev)
	close(c.changed)
	c.changed = make(chan struct{})
	return ev, nil
}

// eventsAfter returns every event with Sequence > after, plus the
// channel to wait on for more, and whether the correlator is closed.
func (c *correlator) eventsAfter(after uint64) ([]brokerpkg.Event, <-chan struct{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []brokerpkg.Event
	for _, ev := range c.events {
		if ev.Sequence > after {
			out = append(out, ev)
		}
	}
	return out, c.changed, c.closed
}

// observe returns the previously observed state for alpacaOrderID (zero
// value if none) and records newState as current.
func (c *correlator) observe(alpacaOrderID string, newState observedOrderState) observedOrderState {
	c.mu.Lock()
	defer c.mu.Unlock()
	prev := c.lastObserved[alpacaOrderID]
	c.lastObserved[alpacaOrderID] = newState
	return prev
}

func (c *correlator) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.changed)
}
