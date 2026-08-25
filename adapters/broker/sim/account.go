package sim

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/rustyeddy/trader/account"
	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// positionKey identifies one (instrument, provider, venue) listing for
// accountState.positions, matching the uniqueness rule
// account.NewSnapshot itself enforces on Snapshot.Positions.
type positionKey struct {
	instrumentID instrument.ID
	provider     string
	venue        string
}

func keyForListing(l instrument.Listing) positionKey {
	return positionKey{instrumentID: l.InstrumentID(), provider: l.Provider(), venue: l.Venue()}
}

// accountState is one simulated account's mutable state, independently
// guarded by its own mutex so operations against different accounts
// never contend. asOf tracks the last time this state actually changed
// (construction, or a Submit); Snapshot reports it directly rather than
// re-reading the clock on every query, so two Snapshot calls with no
// intervening state change report identical AsOf values.
//
// closed and changed together let eventReader.Next block for a future
// event instead of returning io.EOF merely because it has caught up
// (ADR-024: io.EOF from a live-style stream must mean the producer
// itself has ended, not "nothing new yet"). changed is closed and
// replaced with a fresh channel every time events grows; a blocked
// reader observes the close, wakes, and rechecks. closed is set true
// exactly once, by Broker.Close, at which point changed is closed one
// final time (and never replaced) to wake every blocked reader for
// good. Both fields are read and written only while holding mu, which
// is what lets Submit and Broker.Close safely race against each other
// without ever double-closing changed — see commitOrderEvent and
// Broker.Close.
type accountState struct {
	mu sync.Mutex

	ref      account.Reference
	currency num.Currency
	cash     num.Money
	zero     num.Money
	asOf     time.Time

	// orders holds every order ever accepted by Submit, in every
	// status including terminal ones — this is what Submit's OrderID
	// idempotency check consults (see Submit), distinct from which of
	// them are still "open" (see snapshotLocked). A market order that
	// fills immediately (issue #149) is stored here as StatusFilled
	// from the moment it commits; it never passes through this map as
	// StatusWorking.
	orders map[id.OrderID]order.Order
	// positions holds this account's current position per listing.
	// Only Position values with a non-Flat Side are ever stored — a
	// position that returns to flat is deleted rather than kept as a
	// zero-quantity entry, matching account.Snapshot's own "Positions
	// is simply empty" convention for a flat account.
	positions map[positionKey]order.Position

	events       []brokerpkg.Event
	nextSequence uint64

	closed  bool
	changed chan struct{}
}

// zeroMoney returns zero money denominated in currency.
func zeroMoney(currency num.Currency) (num.Money, error) {
	return num.ParseMoney("0", currency)
}

// snapshotLocked builds this account's current account.Snapshot. The
// caller must already hold s.mu. OpenOrders and Positions are each
// sorted deterministically before account.NewSnapshot sees them: both
// are backed by maps, so ranging either directly would expose Go's
// randomized map iteration order through the resulting Snapshot,
// breaking the reproducibility this package otherwise guarantees — two
// calls against identical state must return both in the same order,
// not just the same set. Equity, BuyingPower, and MarginAvailable
// track s.cash directly; this package models no leverage, margin, or
// mark-to-market unrealized PnL of its own (that is risk's concern,
// M4, and — for unrealized PnL specifically — issue #152's, M3-09).
func (s *accountState) snapshotLocked() (account.Snapshot, error) {
	openOrders := make([]order.Order, 0, len(s.orders))
	for _, o := range s.orders {
		if !o.Status.Terminal() {
			openOrders = append(openOrders, o)
		}
	}
	sort.Slice(openOrders, func(i, j int) bool {
		return openOrders[i].Request.OrderID.String() < openOrders[j].Request.OrderID.String()
	})

	positions := make([]order.Position, 0, len(s.positions))
	for _, p := range s.positions {
		positions = append(positions, p)
	}
	sort.Slice(positions, func(i, j int) bool {
		a, b := positions[i].Listing, positions[j].Listing
		if a.InstrumentID() != b.InstrumentID() {
			return a.InstrumentID().String() < b.InstrumentID().String()
		}
		if a.Provider() != b.Provider() {
			return a.Provider() < b.Provider()
		}
		return a.Venue() < b.Venue()
	})

	return account.NewSnapshot(account.SnapshotParams{
		AccountID:       s.ref.AccountID,
		Broker:          s.ref.Broker,
		Currency:        s.currency,
		AsOf:            s.asOf,
		CashBalances:    []num.Money{s.cash},
		Equity:          s.cash,
		BuyingPower:     s.cash,
		MarginUsed:      s.zero,
		MarginAvailable: s.cash,
		RealizedPnL:     s.zero,
		UnrealizedPnL:   s.zero,
		Fees:            s.zero,
		Financing:       s.zero,
		Positions:       positions,
		OpenOrders:      openOrders,
	})
}

// buildOrderEvent constructs the deterministic EventKindOrder Event
// recording o, at the given sequence. It performs no mutation of s and
// returns an error, with s left completely untouched, if event ID
// generation or validation fails — the caller commits the returned
// Event (via commitEvents) only once every other part of the state
// transition it belongs to has also succeeded, so a failure here can
// never leave an order accepted with no matching event. sequence must
// be the exact value this event will occupy once committed — see
// Submit, the only intended caller, for how a multi-event commit
// assigns increasing sequences to each event before building any of
// them. The caller must already hold s.mu.
func (s *accountState) buildOrderEvent(deps Deps, o order.Order, causationID id.EventID, sequence uint64) (brokerpkg.Event, error) {
	orderForEvent := cloneOrder(o)
	return s.buildEvent(deps, brokerpkg.EventKindOrder, causationID, sequence, &orderForEvent, nil)
}

// buildFillEvent constructs the deterministic EventKindFill Event
// recording f, at the given sequence. See buildOrderEvent for the
// atomicity and sequencing contract this shares. Unlike buildOrderEvent,
// this also sets f.Metadata to the same EventID/CausationID/Timestamp
// as the wrapping Event itself: a Fill has no independent identity
// apart from the event that reports it, so the two stay in sync by
// construction rather than by convention.
func (s *accountState) buildFillEvent(deps Deps, f order.Fill, causationID id.EventID, sequence uint64) (brokerpkg.Event, error) {
	eventID, err := id.GenerateEventID(deps.IDs)
	if err != nil {
		return brokerpkg.Event{}, err
	}
	now := deps.Clock.Now()
	f.Metadata = id.Metadata{EventID: eventID, CausationID: causationID, Timestamp: now}
	fillForEvent := cloneFill(f)
	return brokerpkg.NewEvent(brokerpkg.Event{
		Metadata: id.Metadata{
			EventID:     eventID,
			CausationID: causationID,
			Timestamp:   now,
		},
		ObservedAt: now,
		Sequence:   sequence,
		Kind:       brokerpkg.EventKindFill,
		Fill:       &fillForEvent,
	})
}

// buildEvent is buildOrderEvent/buildFillEvent's shared construction
// path. Exactly one of orderPayload/fillPayload must be non-nil,
// matching kind; the caller already owns a private clone safe to hand
// to brokerpkg.NewEvent directly.
func (s *accountState) buildEvent(deps Deps, kind brokerpkg.EventKind, causationID id.EventID, sequence uint64, orderPayload *order.Order, fillPayload *order.Fill) (brokerpkg.Event, error) {
	eventID, err := id.GenerateEventID(deps.IDs)
	if err != nil {
		return brokerpkg.Event{}, err
	}
	now := deps.Clock.Now()
	return brokerpkg.NewEvent(brokerpkg.Event{
		Metadata: id.Metadata{
			EventID:     eventID,
			CausationID: causationID,
			Timestamp:   now,
		},
		ObservedAt: now,
		Sequence:   sequence,
		Kind:       kind,
		Order:      orderPayload,
		Fill:       fillPayload,
	})
}

// commitEvents appends every Event in evs, in order, and advances
// s.nextSequence to the last one's Sequence. The caller must already
// hold s.mu and must supply events built by buildOrderEvent/
// buildFillEvent against s's current nextSequence, in strictly
// increasing Sequence order with no gaps — see Submit, the only
// intended caller.
func (s *accountState) commitEvents(evs ...brokerpkg.Event) {
	if len(evs) == 0 {
		return
	}
	s.nextSequence = evs[len(evs)-1].Sequence
	s.events = append(s.events, evs...)
	close(s.changed)
	s.changed = make(chan struct{})
}

// cloneOrder returns a copy of o that shares no pointer or slice state
// with it, so storing or emitting the clone is safe from later mutation
// through the original — the same discipline account.Snapshot's own
// internal cloning applies, duplicated here because it is unexported
// there.
func cloneOrder(o order.Order) order.Order {
	cloned := o
	if o.Request.LimitPrice != nil {
		v := *o.Request.LimitPrice
		cloned.Request.LimitPrice = &v
	}
	if o.Request.StopPrice != nil {
		v := *o.Request.StopPrice
		cloned.Request.StopPrice = &v
	}
	if o.AcceptedQuantity != nil {
		v := *o.AcceptedQuantity
		cloned.AcceptedQuantity = &v
	}
	if o.AcceptedLimitPrice != nil {
		v := *o.AcceptedLimitPrice
		cloned.AcceptedLimitPrice = &v
	}
	if o.AcceptedStopPrice != nil {
		v := *o.AcceptedStopPrice
		cloned.AcceptedStopPrice = &v
	}
	if o.AvgFillPrice != nil {
		v := *o.AvgFillPrice
		cloned.AvgFillPrice = &v
	}
	if o.Rejection != nil {
		v := *o.Rejection
		cloned.Rejection = &v
	}
	if o.AppliedFillIDs != nil {
		cloned.AppliedFillIDs = append([]id.FillID(nil), o.AppliedFillIDs...)
	}
	if o.AppliedBrokerFillIDs != nil {
		cloned.AppliedBrokerFillIDs = append([]string(nil), o.AppliedBrokerFillIDs...)
	}
	return cloned
}

// accountHandle is broker.Account bound to one account of a Broker.
// Obtain one from Broker.OpenAccount.
type accountHandle struct {
	broker *Broker
	state  *accountState
}

var _ brokerpkg.Account = (*accountHandle)(nil)

// Reference implements broker.Account.
func (h *accountHandle) Reference() account.Reference {
	return h.state.ref
}

// Snapshot implements broker.Account.
func (h *accountHandle) Snapshot(ctx context.Context) (account.Snapshot, error) {
	if h.broker.isClosed() {
		return account.Snapshot{}, brokerpkg.ErrClosed
	}

	h.state.mu.Lock()
	defer h.state.mu.Unlock()
	return h.state.snapshotLocked()
}

// Submit implements broker.Account. It validates req and accepts it
// into StatusWorking, emitting the resulting EventKindOrder event.
// Resubmitting the same req.OrderID is idempotent: Submit returns the
// already-stored Order unchanged and emits no additional event,
// matching Request.OrderID's role as the initial-submission idempotency
// key (ADR-017).
//
// A market order (req.Type == order.Market) additionally fills
// immediately and completely at the price h.broker.deps.Prices reports,
// updating cash and, if the listing was flat, opening a Position — see
// buildMarketFill and the package doc comment for what this does and
// does not yet cover. Limit and stop orders remain StatusWorking with
// no fill matching until issue #150 (M3-07).
func (h *accountHandle) Submit(ctx context.Context, req order.Request) (order.Order, error) {
	if h.broker.isClosed() {
		return order.Order{}, brokerpkg.ErrClosed
	}

	h.state.mu.Lock()
	defer h.state.mu.Unlock()

	// Re-check under state.mu: h.broker.isClosed() above is only a fast
	// pre-check under a different mutex (Broker.mu), so Close could run
	// between it and this point. h.state.closed is set only while
	// holding state.mu (see Broker.Close), so this check is the
	// authoritative one — without it, a Submit racing a concurrent
	// Close could call commitEvents after Close already closed
	// h.state.changed, double-closing it and panicking.
	if h.state.closed {
		return order.Order{}, brokerpkg.ErrClosed
	}

	if existing, ok := h.state.orders[req.OrderID]; ok {
		return cloneOrder(existing), nil
	}

	accepted := req.Quantity
	now := h.broker.deps.Clock.Now()

	// AcceptedLimitPrice/AcceptedStopPrice mirror the requested values
	// exactly: this package performs no price improvement or matching
	// (see the package doc comment), so whatever the request asked for
	// is what gets accepted.
	var acceptedLimit, acceptedStop *num.Price
	if req.LimitPrice != nil {
		v := *req.LimitPrice
		acceptedLimit = &v
	}
	if req.StopPrice != nil {
		v := *req.StopPrice
		acceptedStop = &v
	}

	o, err := order.NewOrder(order.Order{
		Request:            req,
		BrokerOrderID:      "sim-" + req.OrderID.String(),
		AcceptedQuantity:   &accepted,
		AcceptedLimitPrice: acceptedLimit,
		AcceptedStopPrice:  acceptedStop,
		Status:             order.StatusWorking,
		UpdatedAt:          now,
	})
	if err != nil {
		return order.Order{}, err
	}

	// Build every event before mutating any state: if event ID
	// generation or validation fails at any step below, Submit must
	// return an error with h.state left exactly as it was — never an
	// order accepted with no matching event, which would also break
	// idempotency (a retry would see the OrderID already present and
	// report success without ever emitting the event). This is the same
	// build-then-commit discipline for a single event extended to a
	// market order's three (accept, fill, filled).
	acceptEvent, err := h.state.buildOrderEvent(h.broker.deps, o, req.Metadata.EventID, h.state.nextSequence+1)
	if err != nil {
		return order.Order{}, err
	}

	if req.Type != order.Market {
		h.state.orders[req.OrderID] = cloneOrder(o)
		h.state.asOf = now
		h.state.commitEvents(acceptEvent)
		return o, nil
	}

	filled, fillEvent, filledEvent, cashAfter, positionAfter, err := h.state.buildMarketFill(h.broker.deps, o, acceptEvent.Metadata.EventID, h.state.nextSequence+2)
	if err != nil {
		return order.Order{}, err
	}

	h.state.orders[req.OrderID] = cloneOrder(filled)
	h.state.cash = cashAfter
	h.state.positions[keyForListing(req.Listing)] = positionAfter
	h.state.asOf = now
	h.state.commitEvents(acceptEvent, fillEvent, filledEvent)
	return filled, nil
}

// buildMarketFill constructs everything a market order's immediate,
// complete fill needs — the resulting filled Order, the EventKindFill
// and second EventKindOrder (status-change) events, and this account's
// post-fill cash and Position — without mutating s. acceptEventID and
// nextSequence are the EventID and Sequence of the just-built order-
// accepted event (see Submit): the fill event is assigned nextSequence,
// caused by acceptEventID, and the filled-status order event is
// assigned nextSequence+1, caused by the fill event's own EventID.
//
// Scope (issue #149, M3-06): the fill is always for o's complete
// AcceptedQuantity — this package has no partial-fill/volume model —
// and only opens a new Position when the listing was previously flat.
// A fill against a listing where the account already holds a Position
// returns ErrPositionUpdateUnsupported: correctly adding to, reducing,
// closing, or reversing a position requires weighted-average cost
// basis and realized PnL accounting that issue #152 (M3-09) owns, and
// this package would rather report that plainly than compute a
// silently wrong average price or PnL.
func (s *accountState) buildMarketFill(deps Deps, o order.Order, acceptEventID id.EventID, nextSequence uint64) (filled order.Order, fillEvent, filledEvent brokerpkg.Event, cashAfter num.Money, positionAfter order.Position, err error) {
	req := o.Request
	key := keyForListing(req.Listing)
	if existing, ok := s.positions[key]; ok && existing.Side != order.Flat {
		err = fmt.Errorf("%w: listing %s already holds a %s position", ErrPositionUpdateUnsupported, req.Listing.Symbol(), existing.Side)
		return
	}

	price, err := deps.Prices.Price(req.Listing, req.Side)
	if err != nil {
		return
	}

	fillID, err := id.GenerateFillID(deps.IDs)
	if err != nil {
		return
	}

	fillQty := *o.AcceptedQuantity
	fill, err := order.NewFill(order.Fill{
		FillID:        fillID,
		OrderID:       req.OrderID,
		BrokerOrderID: o.BrokerOrderID,
		AccountID:     req.AccountID,
		Listing:       req.Listing,
		Side:          req.Side,
		Price:         price,
		Quantity:      fillQty,
		Timestamp:     deps.Clock.Now(),
	})
	if err != nil {
		return
	}

	fillEvent, err = s.buildFillEvent(deps, fill, acceptEventID, nextSequence)
	if err != nil {
		return
	}

	filled, err = order.ApplyFill(o, fill)
	if err != nil {
		return
	}

	filledEvent, err = s.buildOrderEvent(deps, filled, fillEvent.Metadata.EventID, nextSequence+1)
	if err != nil {
		return
	}

	notional, err := price.MulQuantity(fillQty, req.Listing.Spec().SettlementCurrency())
	if err != nil {
		return
	}
	notional, err = notional.MulRate(req.Listing.Spec().Multiplier())
	if err != nil {
		return
	}

	// req.Side is already one of Buy/Sell: order.NewOrder (via o's own
	// construction in Submit) already validated it through
	// checkProposal, so there is no third case to handle here.
	if req.Side == order.Buy {
		cashAfter, err = s.cash.Sub(notional)
	} else {
		cashAfter, err = s.cash.Add(notional)
	}
	if err != nil {
		return
	}

	positionSide := order.Long
	if req.Side == order.Sell {
		positionSide = order.Short
	}
	avgPrice := price
	positionAfter, err = order.NewPosition(order.Position{
		AccountID: req.AccountID,
		Listing:   req.Listing,
		Side:      positionSide,
		Quantity:  fillQty,
		AvgPrice:  &avgPrice,
	})
	return
}

// Cancel implements broker.Account. Cancel/replace lifecycle behavior
// is issue #151 (M3-08)'s scope, not this package's yet.
func (h *accountHandle) Cancel(ctx context.Context, req order.CancelRequest) (order.CancelResult, error) {
	if h.broker.isClosed() {
		return order.CancelResult{}, brokerpkg.ErrClosed
	}
	return order.CancelResult{}, brokerpkg.ErrUnsupported
}

// Replace implements broker.Account. Cancel/replace lifecycle behavior
// is issue #151 (M3-08)'s scope, not this package's yet.
func (h *accountHandle) Replace(ctx context.Context, req order.ReplaceRequest) (order.ReplaceResult, error) {
	if h.broker.isClosed() {
		return order.ReplaceResult{}, brokerpkg.ErrClosed
	}
	return order.ReplaceResult{}, brokerpkg.ErrUnsupported
}

// Events implements broker.Account.
func (h *accountHandle) Events(ctx context.Context, cursor brokerpkg.EventCursor) (brokerpkg.EventReader, error) {
	if h.broker.isClosed() {
		return nil, brokerpkg.ErrClosed
	}
	return &eventReader{state: h.state, after: decodeCursor(cursor)}, nil
}
