package sim

import (
	"context"
	"errors"
	"fmt"
	"sort"

	brokerpkg "github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

// Advance is the simulator-specific entry point (issue #150/M3-07,
// ADR-026) that evaluates obs against every account this Broker owns,
// filling at most one triggered pending Limit/Stop order per account
// per call (see accountState.advance and IntrabarPolicy). It is not
// part of the public broker.Broker port: a real adapter has no
// simulation to drive.
//
// Every account is evaluated independently, in deterministic
// (AccountID-sorted) order; a failure for one account (for example
// ErrAmbiguousIntrabarOrder, or a triggered fill hitting
// ErrPositionUpdateUnsupported) does not prevent any other account
// from advancing. Advance returns every such error joined with
// errors.Join, or nil if every account advanced without error.
func (b *Broker) Advance(ctx context.Context, obs Observation) error {
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
		if err := s.advance(b.deps, obs); err != nil {
			errs = append(errs, fmt.Errorf("account %s: %w", s.ref.AccountID, err))
		}
	}
	return errors.Join(errs...)
}

// triggeredOrder pairs a pending order with the price it would fill at
// if obs is the only observation resolved against it.
type triggeredOrder struct {
	order order.Order
	price num.Price
}

// advance evaluates obs against s's pending StatusWorking Limit/Stop
// orders for obs.Listing (Market orders already fill at Submit time,
// issue #149; StopLimit is not evaluated here — see the package doc
// comment), filling exactly one triggered order, or reporting why it
// filled none. The caller must not already hold s.mu.
func (s *accountState) advance(deps Deps, obs Observation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return brokerpkg.ErrClosed
	}

	var triggered []triggeredOrder
	for _, o := range s.orders {
		if o.Status != order.StatusWorking {
			continue
		}
		if o.Request.Listing != obs.Listing {
			continue
		}
		switch o.Request.Type {
		case order.Limit:
			if price, ok := limitTriggerPrice(o.Request.Side, *o.AcceptedLimitPrice, obs); ok {
				triggered = append(triggered, triggeredOrder{order: o, price: price})
			}
		case order.Stop:
			if price, ok := stopTriggerPrice(o.Request.Side, *o.AcceptedStopPrice, obs); ok {
				triggered = append(triggered, triggeredOrder{order: o, price: price})
			}
		}
	}
	if len(triggered) == 0 {
		return nil
	}

	// Deterministic before any policy decision, regardless of outcome:
	// s.orders is a map, so without sorting, which OrderIDs an
	// ambiguity error names (or, if this package later implements
	// IntrabarPessimistic, which order it resolves first) would depend
	// on Go's randomized map iteration.
	sort.Slice(triggered, func(i, j int) bool {
		return triggered[i].order.Request.OrderID.String() < triggered[j].order.Request.OrderID.String()
	})

	if len(triggered) > 1 {
		if deps.IntrabarPolicy == IntrabarPessimistic {
			return fmt.Errorf("%w: pessimistic intrabar resolution", brokerpkg.ErrUnsupported)
		}
		ids := make([]string, len(triggered))
		for i, t := range triggered {
			ids[i] = t.order.Request.OrderID.String()
		}
		return fmt.Errorf("%w: orders %v for listing %s", ErrAmbiguousIntrabarOrder, ids, obs.Listing.Symbol())
	}

	t := triggered[0]
	filled, fillEvent, filledEvent, positionAfter, err := s.buildFill(deps, t.order, t.price, id.EventID{}, s.nextSequence+1)
	if err != nil {
		return err
	}

	s.orders[t.order.Request.OrderID] = cloneOrder(filled)
	s.positions[keyForListing(t.order.Request.Listing)] = positionAfter
	s.asOf = deps.Clock.Now()
	s.commitEvents(fillEvent, filledEvent)
	return nil
}

// limitTriggerPrice reports the price a Limit order for side would
// fill at against obs, and whether it triggers at all (ADR-026):
//
//	Buy:  Open <= limit  => fill at Open
//	      Low  <= limit  => fill at limit
//	Sell: Open >= limit  => fill at Open
//	      High >= limit  => fill at limit
//
// A limit order is a maximum-pay (Buy) or minimum-receive (Sell)
// constraint, so a favorable gap must fill at the better open price
// rather than the originally requested limit.
func limitTriggerPrice(side order.Side, limit num.Price, obs Observation) (num.Price, bool) {
	switch side {
	case order.Buy:
		if obs.Open.Cmp(limit) <= 0 {
			return obs.Open, true
		}
		if obs.Low.Cmp(limit) <= 0 {
			return limit, true
		}
	case order.Sell:
		if obs.Open.Cmp(limit) >= 0 {
			return obs.Open, true
		}
		if obs.High.Cmp(limit) >= 0 {
			return limit, true
		}
	}
	return num.Price{}, false
}

// stopTriggerPrice reports the price a Stop order for side would fill
// at against obs, and whether it triggers at all (ADR-026):
//
//	Buy:  Open >= stop  => fill at Open
//	      High >= stop  => fill at stop
//	Sell: Open <= stop  => fill at Open
//	      Low  <= stop  => fill at stop
//
// A stop order becomes a market order once triggered, so an adverse
// gap must fill at the worse open price rather than pretending
// execution occurred at a price the market never actually offered.
func stopTriggerPrice(side order.Side, stop num.Price, obs Observation) (num.Price, bool) {
	switch side {
	case order.Buy:
		if obs.Open.Cmp(stop) >= 0 {
			return obs.Open, true
		}
		if obs.High.Cmp(stop) >= 0 {
			return stop, true
		}
	case order.Sell:
		if obs.Open.Cmp(stop) <= 0 {
			return obs.Open, true
		}
		if obs.Low.Cmp(stop) <= 0 {
			return stop, true
		}
	}
	return num.Price{}, false
}
