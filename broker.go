package trader

import (
	"context"
	"fmt"
)

const brokerEventQueueSize = 1024

type Broker struct {
	Name    string
	Account *Account

	evtQ chan *Event
}

func NewBroker(name string) *Broker {
	return &Broker{
		Name: name,
	}
}

func (b *Broker) SubmitOpen(ctx context.Context, req *OpenRequest) (*Lot, error) {
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

func (b *Broker) SubmitClose(ctx context.Context, req *CloseRequest) error {
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

func (b *Broker) Events() <-chan *Event {
	return b.ensureEventQueue()
}

func (b *Broker) emitEvent(ctx context.Context, evt *Event) error {
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

func (b *Broker) publishEvent(ctx context.Context, evt *Event) {
	if err := b.emitEvent(ctx, evt); err != nil {
		L.Warn("dropping broker event", "type", evt.Type.String(), "err", err)
	}
}

func (b *Broker) ensureEventQueue() chan *Event {
	if b.evtQ == nil {
		b.evtQ = make(chan *Event, brokerEventQueueSize)
	}
	return b.evtQ
}
