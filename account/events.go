package account

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/log"
)

const brokerEventQueueSize = 1024

// SubmitOpen validates req, opens a Lot on acct, and publishes an
// EventOrderFilled notification. The Lot's EntryPrice is exactly req.Price
// — trusted as-is, not yet resolved against a real Broker fill (see
// docs/Manual/architecture-broker-account-order.org, phase 4 chunk 4).
func (acct *Account) SubmitOpen(ctx context.Context, req *OpenRequest) (*Lot, error) {
	if acct == nil {
		return nil, fmt.Errorf("account is nil")
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}

	units := req.TradeCommon.Units
	lot := &Lot{
		TradeCommon:    req.TradeCommon,
		EntryPrice:     req.Price,
		EntryTime:      req.Timestamp,
		OriginalUnits:  units,
		RemainingUnits: units,
		State:          LotOpen,
	}

	if err := acct.AddLot(lot); err != nil {
		return nil, err
	}

	evt := &Event{
		Type: EventOrderFilled,
		Lot:  lot,
	}
	acct.publishEvent(ctx, evt)

	return lot, nil
}

// SubmitClose validates req, closes the referenced Lot on acct, and
// publishes an EventPositionClosed notification. The exit price is
// exactly req.Price — trusted as-is, same caveat as SubmitOpen.
func (acct *Account) SubmitClose(ctx context.Context, req *CloseRequest) error {
	if acct == nil {
		return fmt.Errorf("account is nil")
	}
	if err := req.Validate(); err != nil {
		return err
	}

	trade := &Trade{
		TradeCommon: req.Lot.TradeCommon.Clone(),
		EntryPrice:  req.Lot.EntryPrice,
		EntryTime:   req.Lot.EntryTime,
		ExitPrice:   req.Price,
		ExitTime:    req.Timestamp,
		CloseCause:  req.CloseCause,
	}

	if err := acct.CloseLot(req.Lot, trade); err != nil {
		return err
	}

	evt := &Event{
		Type:  EventPositionClosed,
		Trade: trade,
		Lot:   req.Lot,
	}

	acct.publishEvent(ctx, evt)
	return nil
}

// Events returns the order-filled/position-closed notification channel,
// initializing it on first use.
func (acct *Account) Events() <-chan *Event {
	return acct.ensureEventQueue()
}

func (acct *Account) emitEvent(ctx context.Context, evt *Event) error {
	evtQ := acct.ensureEventQueue()

	if ctx == nil {
		select {
		case evtQ <- evt:
			return nil
		default:
			return fmt.Errorf("account event queue is full")
		}
	}

	select {
	case evtQ <- evt:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("account event queue is full")
	}
}

func (acct *Account) publishEvent(ctx context.Context, evt *Event) {
	if err := acct.emitEvent(ctx, evt); err != nil {
		log.L.Warn("dropping account event", "type", evt.Type.String(), "err", err)
	}
}

func (acct *Account) ensureEventQueue() chan *Event {
	if acct.evtQ == nil {
		acct.evtQ = make(chan *Event, brokerEventQueueSize)
	}
	return acct.evtQ
}

// EventQueueLen returns the number of pending events, or 0 if the queue
// has not been initialized. Used by the engine to detect idleness.
func (acct *Account) EventQueueLen() int {
	if acct == nil || acct.evtQ == nil {
		return 0
	}
	return len(acct.evtQ)
}

// EventQueueCap returns the capacity of the event queue, or 0 if it has
// not been initialized.
func (acct *Account) EventQueueCap() int {
	if acct == nil || acct.evtQ == nil {
		return 0
	}
	return cap(acct.evtQ)
}

// EnqueueEvent places evt on the event queue without blocking, returning
// true if it was accepted. The queue is initialized on first use. Useful
// for injecting events from outside the normal Submit path (e.g. tests,
// replay).
func (acct *Account) EnqueueEvent(evt *Event) bool {
	q := acct.ensureEventQueue()
	select {
	case q <- evt:
		return true
	default:
		return false
	}
}
