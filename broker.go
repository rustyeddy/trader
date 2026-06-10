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
	if req == nil {
		return nil, fmt.Errorf("open request is nil")
	}
	if req.TradeCommon == nil {
		return nil, fmt.Errorf("open request missing trade common")
	}
	if req.Instrument == "" {
		return nil, fmt.Errorf("open request instrument must not be empty")
	}
	if req.Price <= 0 {
		return nil, fmt.Errorf("open request price must be > 0")
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
	if req == nil {
		return fmt.Errorf("close request is nil")
	}
	if req.Lot == nil {
		return fmt.Errorf("close request missing position")
	}
	if req.Lot.TradeCommon == nil {
		return fmt.Errorf("close request position missing trade common")
	}
	if req.Price <= 0 {
		return fmt.Errorf("close request price must be > 0")
	}
	if req.Request.TradeCommon != nil {
		if req.Request.ID != "" && req.Request.ID != req.Lot.ID {
			return fmt.Errorf("close request id %q does not match position id %q", req.Request.ID, req.Lot.ID)
		}
		if req.Request.Instrument != "" && req.Request.Instrument != req.Lot.Instrument {
			return fmt.Errorf("close request instrument %q does not match position instrument %q", req.Request.Instrument, req.Lot.Instrument)
		}
	}

	trade := &Trade{
		TradeCommon: req.Lot.TradeCommon,
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
