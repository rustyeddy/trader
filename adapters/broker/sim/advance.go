package sim

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// Advance is the simulator-specific entry point (issue #150/M3-07,
// ADR-026) that evaluates obs against every account this Broker owns
// (see accountState.advance and IntrabarPolicy). It is not part of the
// public broker.Broker port: a real adapter has no simulation to
// drive.
//
// Every account is evaluated independently, in deterministic
// (AccountID-sorted) order; a failure for one account (for example
// ErrAmbiguousIntrabarOrder) does not prevent any other account from
// advancing. Advance returns every such error joined with
// errors.Join, or nil if every account advanced without error.
//
// Advance honors ctx cancellation: it is checked before any account is
// evaluated, again before each account, and again immediately before
// committing each triggered fill, so a canceled or expired ctx stops
// further mutation rather than walking every remaining account.
// Whatever fills already committed before cancellation was observed
// remain committed — Advance does not roll back completed, independent
// per-order work merely because a later step was canceled.
func (b *Broker) Advance(ctx context.Context, obs Observation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.isClosed() {
		return brokerpkg.ErrClosed
	}
	if err := obs.validate(); err != nil {
		return err
	}

	b.mu.RLock()
	states := make([]*accountState, 0, len(b.accounts))
	for _, s := range b.accounts {
		states = append(states, s)
	}
	b.mu.RUnlock()

	// Deterministic account processing order: b.accounts is a map, the
	// same map-iteration hazard already guarded against for
	// OpenOrders/Positions ordering (see snapshotLocked).
	sort.Slice(states, func(i, j int) bool {
		return states[i].ref.AccountID.String() < states[j].ref.AccountID.String()
	})

	var errs []error
	for _, s := range states {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		if err := s.advance(ctx, b.deps, obs); err != nil {
			errs = append(errs, fmt.Errorf("account %s: %w", s.ref.AccountID, err))
		}
	}
	return errors.Join(errs...)
}

// ObserveMark implements backtest.MarketObserver (issue #219, M5-11) —
// satisfied structurally, without this package importing backtest: see
// that interface's own doc comment for why its signature deliberately
// uses only already-shared primitive types. This is a deliberately
// narrow extraction from accountState.advance's own mark-revaluation
// step (see its doc comment below): it revalues every open position
// for instrumentID with close, exactly as advance already does
// unconditionally at the top of its own body, but it never evaluates
// resting Limit/Stop order triggers — that remains Advance's own,
// separate, still-deferred responsibility (ADR-026).
func (h *accountHandle) ObserveMark(ctx context.Context, instrumentID instrument.ID, close num.Price, at time.Time) error {
	if h.broker.isClosed() {
		return brokerpkg.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	h.state.observeMark(instrumentID, close, h.broker.deps)
	return nil
}

// observeMark revalues every position s holds for instrumentID
// (regardless of provider/venue — s is already scoped to one account,
// and this package's own convention, matching InputBuilder's default
// resolution elsewhere, is one provider per account) to close. A
// listing with no open position is a no-op: there is nothing to
// revalue, matching advance's own "even if no pending order triggers
// below" guard.
func (s *accountState) observeMark(instrumentID instrument.ID, close num.Price, deps Deps) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}

	changed := false
	for key := range s.positions {
		if key.instrumentID != instrumentID {
			continue
		}
		s.marks[key] = close
		changed = true
	}
	if changed {
		s.asOf = deps.Clock.Now()
	}
}

// triggeredOrder pairs a pending order with the price it would fill at
// against one Observation, and whether that trigger resolved at the
// observation's Open (see limitTriggerPrice/stopTriggerPrice) — the
// single, known instant the bar begins — versus somewhere within the
// bar, where OHLC alone cannot say exactly when.
type triggeredOrder struct {
	order  order.Order
	price  num.Price
	atOpen bool
}

// advance evaluates obs against s's pending StatusWorking Limit/Stop
// orders for obs.Listing (Market orders already fill at Submit time,
// issue #149; StopLimit is not evaluated here — see the package doc
// comment). The caller must not already hold s.mu.
//
// Orders that trigger at obs.Open are not ambiguous relative to each
// other or to any order that triggers later within the bar: the open
// is definitionally the bar's first instant, so every at-open order is
// processed first, in deterministic (OrderID-sorted) order, followed
// by at most one order that triggered elsewhere within the bar. Only
// when *more than one* order's trigger genuinely depends on an unknown
// path through the bar's High/Low — i.e. more than one triggers
// somewhere other than the open — does IntrabarPolicy apply; that
// group is reported as ErrAmbiguousIntrabarOrder (or, under
// IntrabarPessimistic, broker.ErrUnsupported) without being filled,
// while any at-open orders still fill normally. A later order in the
// fill sequence that cannot be filled for some other reason (a
// currency mismatch surfaced from position/PnL accounting, issue
// #152) is reported as that specific error, not folded into
// ErrAmbiguousIntrabarOrder: OHLC ordering was never the uncertainty
// for an at-open group.
func (s *accountState) advance(ctx context.Context, deps Deps, obs Observation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return brokerpkg.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Revalue an existing position's mark from this observation's
	// Close, even if no pending order triggers below — unrealized PnL
	// (issue #152, M3-09) must not go stale merely because there was
	// nothing to fill this bar. Any fill processed below overwrites
	// this with its own, more specific fill price.
	key := keyForListing(obs.Listing)
	if _, hasPosition := s.positions[key]; hasPosition {
		s.marks[key] = obs.Close
		s.asOf = deps.Clock.Now()
	}

	var atOpen, withinBar []triggeredOrder
	for _, o := range s.orders {
		if o.Status != order.StatusWorking {
			continue
		}
		if o.Request.Listing != obs.Listing {
			continue
		}

		var price num.Price
		var isAtOpen, triggers bool
		switch o.Request.Type {
		case order.Limit:
			price, isAtOpen, triggers = limitTriggerPrice(o.Request.Side, *o.AcceptedLimitPrice, obs)
		case order.Stop:
			price, isAtOpen, triggers = stopTriggerPrice(o.Request.Side, *o.AcceptedStopPrice, obs)
		default:
			continue
		}
		if !triggers {
			continue
		}

		t := triggeredOrder{order: o, price: price, atOpen: isAtOpen}
		if isAtOpen {
			atOpen = append(atOpen, t)
		} else {
			withinBar = append(withinBar, t)
		}
	}
	if len(atOpen) == 0 && len(withinBar) == 0 {
		return nil
	}

	// Deterministic before any policy decision or fill, regardless of
	// outcome: s.orders is a map, so without sorting, which OrderIDs an
	// ambiguity error names, or the order multiple at-open fills are
	// attempted in, would depend on Go's randomized map iteration.
	sortTriggered(atOpen)
	sortTriggered(withinBar)

	var errs []error
	if len(withinBar) > 1 {
		if deps.IntrabarPolicy == IntrabarPessimistic {
			errs = append(errs, fmt.Errorf("%w: pessimistic intrabar resolution", brokerpkg.ErrUnsupported))
		} else {
			errs = append(errs, fmt.Errorf("%w: orders %v for listing %s", ErrAmbiguousIntrabarOrder, orderIDStrings(withinBar), obs.Listing.Symbol()))
		}
		withinBar = nil // ambiguous: fill none of this group
	}

	toFill := make([]triggeredOrder, 0, len(atOpen)+len(withinBar))
	toFill = append(toFill, atOpen...)
	toFill = append(toFill, withinBar...)

	for _, t := range toFill {
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}

		outcome, err := s.buildFill(deps, t.order, t.price, id.EventID{}, s.nextSequence+1)
		if err != nil {
			errs = append(errs, fmt.Errorf("order %s: %w", t.order.Request.OrderID, err))
			continue
		}

		s.commitFill(t.order.Request.Listing, outcome)
		s.asOf = deps.Clock.Now()
		s.commitEvents(outcome.fillEvent, outcome.filledEvent)
	}

	return errors.Join(errs...)
}

func sortTriggered(ts []triggeredOrder) {
	sort.Slice(ts, func(i, j int) bool {
		return ts[i].order.Request.OrderID.String() < ts[j].order.Request.OrderID.String()
	})
}

func orderIDStrings(ts []triggeredOrder) []string {
	ids := make([]string, len(ts))
	for i, t := range ts {
		ids[i] = t.order.Request.OrderID.String()
	}
	return ids
}

// limitTriggerPrice reports the price a Limit order for side would
// fill at against obs, whether that trigger resolved at obs.Open, and
// whether it triggers at all (ADR-026):
//
//	Buy:  Open <= limit  => fill at Open (at-open)
//	      Low  <= limit  => fill at limit (within bar)
//	Sell: Open >= limit  => fill at Open (at-open)
//	      High >= limit  => fill at limit (within bar)
//
// A limit order is a maximum-pay (Buy) or minimum-receive (Sell)
// constraint, so a favorable gap must fill at the better open price
// rather than the originally requested limit.
func limitTriggerPrice(side order.Side, limit num.Price, obs Observation) (price num.Price, atOpen bool, triggers bool) {
	switch side {
	case order.Buy:
		if obs.Open.Cmp(limit) <= 0 {
			return obs.Open, true, true
		}
		if obs.Low.Cmp(limit) <= 0 {
			return limit, false, true
		}
	case order.Sell:
		if obs.Open.Cmp(limit) >= 0 {
			return obs.Open, true, true
		}
		if obs.High.Cmp(limit) >= 0 {
			return limit, false, true
		}
	}
	return num.Price{}, false, false
}

// stopTriggerPrice reports the price a Stop order for side would fill
// at against obs, whether that trigger resolved at obs.Open, and
// whether it triggers at all (ADR-026):
//
//	Buy:  Open >= stop  => fill at Open (at-open)
//	      High >= stop  => fill at stop (within bar)
//	Sell: Open <= stop  => fill at Open (at-open)
//	      Low  <= stop  => fill at stop (within bar)
//
// A stop order becomes a market order once triggered, so an adverse
// gap must fill at the worse open price rather than pretending
// execution occurred at a price the market never actually offered.
func stopTriggerPrice(side order.Side, stop num.Price, obs Observation) (price num.Price, atOpen bool, triggers bool) {
	switch side {
	case order.Buy:
		if obs.Open.Cmp(stop) >= 0 {
			return obs.Open, true, true
		}
		if obs.High.Cmp(stop) >= 0 {
			return stop, false, true
		}
	case order.Sell:
		if obs.Open.Cmp(stop) <= 0 {
			return obs.Open, true, true
		}
		if obs.Low.Cmp(stop) <= 0 {
			return stop, false, true
		}
	}
	return num.Price{}, false, false
}
