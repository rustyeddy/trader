package account

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/log"
)

const brokerEventQueueSize = 1024

type Ledger struct {
	Name    string
	Account *Account

	evtQ chan *Event
}

func NewLedger(name string) *Ledger {
	return &Ledger{
		Name: name,
	}
}

func (b *Ledger) SubmitOpen(ctx context.Context, req *OpenRequest) (*Lot, error) {
	if b == nil {
		return nil, fmt.Errorf("broker is nil")
	}
	if b.Account == nil {
		return nil, fmt.Errorf("broker account is nil")
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

	if err := b.Account.AddLot(lot); err != nil {
		return nil, err
	}

	evt := &Event{
		Type: EventOrderFilled,
		Lot:  lot,
	}
	b.publishEvent(ctx, evt)

	return lot, nil
}

func (b *Ledger) SubmitClose(ctx context.Context, req *CloseRequest) error {
	if b == nil {
		return fmt.Errorf("broker is nil")
	}
	if b.Account == nil {
		return fmt.Errorf("broker account is nil")
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

	if err := b.Account.CloseLot(req.Lot, trade); err != nil {
		return err
	}

	evt := &Event{
		Type:  EventPositionClosed,
		Trade: trade,
		Lot:   req.Lot,
	}

	b.publishEvent(ctx, evt)
	return nil
}

func (b *Ledger) Events() <-chan *Event {
	return b.ensureEventQueue()
}

func (b *Ledger) emitEvent(ctx context.Context, evt *Event) error {
	evtQ := b.ensureEventQueue()

	if ctx == nil {
		select {
		case evtQ <- evt:
			return nil
		default:
			return fmt.Errorf("broker event queue is full")
		}
	}

	select {
	case evtQ <- evt:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("broker event queue is full")
	}
}

func (b *Ledger) publishEvent(ctx context.Context, evt *Event) {
	if err := b.emitEvent(ctx, evt); err != nil {
		log.L.Warn("dropping broker event", "type", evt.Type.String(), "err", err)
	}
}

func (b *Ledger) ensureEventQueue() chan *Event {
	if b.evtQ == nil {
		b.evtQ = make(chan *Event, brokerEventQueueSize)
	}
	return b.evtQ
}

// EventQueueLen returns the number of pending broker events, or 0 if the queue
// has not been initialized. Used by the engine to detect broker idleness.
func (b *Ledger) EventQueueLen() int {
	if b == nil || b.evtQ == nil {
		return 0
	}
	return len(b.evtQ)
}

// EventQueueCap returns the capacity of the broker event queue, or 0 if it has
// not been initialized.
func (b *Ledger) EventQueueCap() int {
	if b == nil || b.evtQ == nil {
		return 0
	}
	return cap(b.evtQ)
}

// EnqueueEvent places evt on the broker event queue without blocking, returning
// true if it was accepted. The queue is initialized on first use. Useful for
// injecting events from outside the normal Submit path (e.g. tests, replay).
func (b *Ledger) EnqueueEvent(evt *Event) bool {
	q := b.ensureEventQueue()
	select {
	case q <- evt:
		return true
	default:
		return false
	}
}
