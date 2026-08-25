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
	// is simply empty" convention for a flat account. Use
	// commitPosition, never a direct map write, so this invariant
	// cannot be violated by accident.
	positions map[positionKey]order.Position
	// marks holds the last known price per listing (issue #152,
	// M3-09): set from a market order's fill price (Submit), a
	// triggered limit/stop fill's price, or — even when no order
	// triggers — a bar Observation's Close (Broker.Advance). Snapshot
	// computes UnrealizedPnL from these marks against each open
	// Position's AvgPrice; it is explicitly "as of the simulator's last
	// known market observation," not live/real-time mark-to-market —
	// see snapshotLocked's doc comment. Entries are never deleted, even
	// after a position closes, so the last traded price remains
	// available for history/display.
	marks map[positionKey]num.Price
	// realizedPnL is this account's cumulative realized profit and
	// loss, denominated in currency. It moves only when a fill reduces,
	// closes, or reverses a position (see position.go); opening or
	// increasing a position never changes it. Reported directly as
	// account.Snapshot.RealizedPnL.
	realizedPnL num.Money
	// fees is this account's cumulative commission paid, denominated in
	// currency. It moves only when a fill reports a non-nil
	// order.Fill.Commission (issue #152, M3-09) — this package builds
	// no commission model of its own, so it is zero unless a caller's
	// injected dependencies eventually produce one. Reported directly
	// as account.Snapshot.Fees.
	fees num.Money

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
// caller must already hold s.mu; this performs no I/O and consults no
// injected dependency — every value comes from s's own already-stored
// fields, so Snapshot remains a pure, synchronous read (issue #152,
// M3-09, design discussion on that issue).
//
// OpenOrders and Positions are each sorted deterministically before
// account.NewSnapshot sees them: both are backed by maps, so ranging
// either directly would expose Go's randomized map iteration order
// through the resulting Snapshot, breaking the reproducibility this
// package otherwise guarantees — two calls against identical state
// must return both in the same order, not just the same set.
//
// UnrealizedPnL is computed from s.marks against each open Position's
// AvgPrice (see unrealizedPnLForPosition) — explicitly "as of the
// simulator's last known market observation" (whatever last touched
// s.marks for that listing: a fill, or a Broker.Advance revaluation),
// not live/real-time mark-to-market; this package has no ongoing price
// feed to mark against between those events. Equity is s.cash plus
// that UnrealizedPnL. BuyingPower and MarginAvailable mirror s.cash
// directly and MarginUsed is always zero: this package still models an
// unleveraged, fully funded account with no margin policy of its own
// (that is M4's job) — these fields are a deliberate M3 placeholder,
// not a claim of real margin/leverage semantics.
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

	unrealizedPnL := s.zero
	for key, p := range s.positions {
		mark, ok := s.marks[key]
		if !ok {
			continue // a position always has a mark from its opening fill
		}
		delta, err := unrealizedPnLForPosition(p, mark, p.Listing.Spec().SettlementCurrency())
		if err != nil {
			return account.Snapshot{}, err
		}
		unrealizedPnL, err = unrealizedPnL.Add(delta)
		if err != nil {
			return account.Snapshot{}, err
		}
	}

	equity, err := s.cash.Add(unrealizedPnL)
	if err != nil {
		return account.Snapshot{}, err
	}

	return account.NewSnapshot(account.SnapshotParams{
		AccountID:       s.ref.AccountID,
		Broker:          s.ref.Broker,
		Currency:        s.currency,
		AsOf:            s.asOf,
		CashBalances:    []num.Money{s.cash},
		Equity:          equity,
		BuyingPower:     s.cash,
		MarginUsed:      s.zero,
		MarginAvailable: s.cash,
		RealizedPnL:     s.realizedPnL,
		UnrealizedPnL:   unrealizedPnL,
		Fees:            s.fees,
		Financing:       s.zero,
		Positions:       positions,
		OpenOrders:      openOrders,
	})
}

// commitPosition stores pos as the account's current position for
// key, or removes any stored entry when pos is Flat — the only way
// s.positions is ever written, so its "only non-Flat entries" invariant
// (see accountState's doc comment) cannot be violated by a direct map
// write at a call site. The caller must already hold s.mu.
func (s *accountState) commitPosition(key positionKey, pos order.Position) {
	if pos.Side == order.Flat {
		delete(s.positions, key)
		return
	}
	s.positions[key] = pos
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
// transitioning the order to StatusFilled and, if the listing was flat,
// opening a Position — see buildMarketFill and the package doc comment
// for what this does and does not yet cover. It deliberately does not
// touch cash/balance state; that is issue #152's (M3-09) scope. Limit
// and stop orders remain StatusWorking with no fill matching until
// issue #150 (M3-07).
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

	price, err := h.broker.deps.Prices.Price(req.Listing, req.Side)
	if err != nil {
		return order.Order{}, err
	}

	outcome, err := h.state.buildFill(h.broker.deps, o, price, acceptEvent.Metadata.EventID, h.state.nextSequence+2)
	if err != nil {
		return order.Order{}, err
	}

	h.state.commitFill(req.Listing, outcome)
	h.state.asOf = now
	h.state.commitEvents(acceptEvent, outcome.fillEvent, outcome.filledEvent)
	return outcome.order, nil
}

// fillOutcome bundles everything one complete fill produces — built by
// buildFill without mutating accountState, committed by the caller
// (Submit or accountState.advance) only once every part of it has
// succeeded, matching the atomicity discipline established in #149.
type fillOutcome struct {
	order       order.Order
	fillEvent   brokerpkg.Event
	filledEvent brokerpkg.Event
	position    order.Position
	mark        num.Price
	cash        num.Money
	realizedPnL num.Money
	fees        num.Money
}

// commitFill applies outcome to s: stores the filled order, commits
// the resulting Position (opening/adjusting/closing it — see
// commitPosition), records the new mark, and updates cash/realizedPnL/
// fees. The caller must already hold s.mu and must call commitEvents
// separately (see Submit and accountState.advance).
func (s *accountState) commitFill(listing instrument.Listing, outcome fillOutcome) {
	s.orders[outcome.order.Request.OrderID] = cloneOrder(outcome.order)
	key := keyForListing(listing)
	s.commitPosition(key, outcome.position)
	s.marks[key] = outcome.mark
	s.cash = outcome.cash
	s.realizedPnL = outcome.realizedPnL
	s.fees = outcome.fees
}

// buildFill constructs everything one complete fill of o, at price,
// needs — the resulting filled Order, the EventKindFill and second
// EventKindOrder (status-change) events, this account's post-fill
// Position, its new mark for o.Request.Listing, and its post-fill
// cash/realizedPnL/fees — without mutating s (see fillOutcome and
// commitFill). causationID and sequence are the EventID and Sequence
// the fill event is assigned; the filled-status order event is
// assigned sequence+1, caused by the fill event's own EventID. Two
// callers build price and causationID differently: Submit (issue
// #149/M3-06) uses Deps.Prices and the just-built order-accepted
// event's EventID; accountState.advance (issue #150/M3-07, ADR-026)
// uses a trigger/gap-derived price and a zero causationID, since a
// market-observation-triggered fill is not caused by any preceding
// Trader-internal event.
//
// The fill is always for o's complete AcceptedQuantity — this package
// has no partial-fill/volume model. Position accounting (issue #152,
// M3-09) covers all five transitions — open, increase, reduce, close,
// reverse — via applyFillToPosition; see position.go. Cash moves only
// by realized PnL and, when o's resulting Fill reports a non-nil
// Commission, by that commission (applyCommission) — never by a
// universal full-notional debit/credit, which is not broker-neutral
// accounting (a cash purchase should leave equity roughly unchanged,
// not book the full notional as an immediate loss; see the design
// discussion on issue #152).
func (s *accountState) buildFill(deps Deps, o order.Order, price num.Price, causationID id.EventID, sequence uint64) (fillOutcome, error) {
	req := o.Request
	key := keyForListing(req.Listing)
	currency := req.Listing.Spec().SettlementCurrency()

	// Checked first, before any other part of the fill is built:
	// realized/unrealized PnL, cash, and fees are all computed and
	// accumulated in currency, and this package has no FX conversion-
	// rate source to reconcile it with a different account currency
	// (see ErrUnsupportedSettlementCurrency's doc comment). Every
	// transition below — open, increase, reduce, close, reverse —
	// needs this to hold, not only the ones that realize PnL, so it is
	// enforced uniformly up front rather than left to surface only
	// when arithmetic happens to combine mismatched currencies.
	if !currency.Equal(s.currency) {
		return fillOutcome{}, fmt.Errorf("%w: listing %s settles in %s, account is %s", ErrUnsupportedSettlementCurrency, req.Listing.Symbol(), currency, s.currency)
	}

	fillID, err := id.GenerateFillID(deps.IDs)
	if err != nil {
		return fillOutcome{}, err
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
		return fillOutcome{}, err
	}

	fillEvent, err := s.buildFillEvent(deps, fill, causationID, sequence)
	if err != nil {
		return fillOutcome{}, err
	}

	filled, err := order.ApplyFill(o, fill)
	if err != nil {
		return fillOutcome{}, err
	}

	filledEvent, err := s.buildOrderEvent(deps, filled, fillEvent.Metadata.EventID, sequence+1)
	if err != nil {
		return fillOutcome{}, err
	}

	existing, hasExisting := s.positions[key]
	positionAfter, realizedPnLDelta, err := applyFillToPosition(existing, hasExisting, req.AccountID, req.Listing, currency, req.Side, price, fillQty)
	if err != nil {
		return fillOutcome{}, err
	}

	cashAfter, err := s.cash.Add(realizedPnLDelta)
	if err != nil {
		return fillOutcome{}, err
	}
	realizedPnLAfter, err := s.realizedPnL.Add(realizedPnLDelta)
	if err != nil {
		return fillOutcome{}, err
	}
	feesAfter := s.fees

	if fill.Commission != nil {
		cashAfter, feesAfter, err = applyCommission(cashAfter, feesAfter, *fill.Commission)
		if err != nil {
			return fillOutcome{}, err
		}
	}

	return fillOutcome{
		order:       filled,
		fillEvent:   fillEvent,
		filledEvent: filledEvent,
		position:    positionAfter,
		mark:        price,
		cash:        cashAfter,
		realizedPnL: realizedPnLAfter,
		fees:        feesAfter,
	}, nil
}

// Cancel and Replace implement broker.Account; see cancel_replace.go
// (issue #151, M3-08).

// Events implements broker.Account.
func (h *accountHandle) Events(ctx context.Context, cursor brokerpkg.EventCursor) (brokerpkg.EventReader, error) {
	if h.broker.isClosed() {
		return nil, brokerpkg.ErrClosed
	}
	return &eventReader{state: h.state, after: decodeCursor(cursor)}, nil
}
