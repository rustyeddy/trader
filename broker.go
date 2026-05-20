package trader

import (
	"context"
	"fmt"
)

const brokerEventQueueSize = 1024

type BrokerInterface interface {
	SubmitOpen(ctx context.Context, req *OpenRequest) error
	SubmitClose(ctx context.Context, req *CloseRequest) error
	Events() <-chan *Event
}

type OrderRequest struct {
	Instrument string
	Units      Units
}

type Broker struct {
	ID string
	*Account
	OpenOrders // should Account own OpenOrders?

	evtQ chan *Event
}

func NewBroker(name string) *Broker {
	return &Broker{
		ID: name,
		OpenOrders: OpenOrders{
			Orders: make(map[string]*order),
		},
	}
}

func (b *Broker) SubmitOpen(ctx context.Context, req *OpenRequest) (*openResult, error) {
	if b == nil {
		return nil, fmt.Errorf("nil broker")
	}
	if b.Account == nil {
		return nil, fmt.Errorf("broker account is nil")
	}
	if req == nil {
		return nil, fmt.Errorf("nil open request")
	}
	if req.TradeCommon == nil {
		return nil, fmt.Errorf("open request missing trade common")
	}
	if b.OpenOrders.Orders == nil {
		b.OpenOrders.Orders = make(map[string]*order)
	}

	// Create an order and submit the order
	o := &order{}
	o.TradeCommon = req.TradeCommon
	o.orderType = OrderMarket
	o.orderStatus = OrderPending

	// place the order in the orderbook
	b.OpenOrders.Add(o)

	// here is where the order get submitted to a real broker if
	// we are using one.
	res := &openResult{
		order: o,
	}

	// Here we will just fake it and return a filled order.
	lot, err := b.SubmitOrder(ctx, o)
	if err != nil {
		return res, err
	}
	// TODO: Need to create a fill fills
	lot.EntryPrice = req.Price // add some spread & slippage
	lot.EntryTime = req.Timestamp

	// This would be the time to emulate a delay between order and fill
	// we will ignore this for now
	lot.State = LotOpen
	res.Lot = lot

	if err := b.Account.AddLot(ctx, lot); err != nil {
		return res, err
	}

	// send lot back on event queue
	evt := &Event{
		Type:          EventOrderFilled,
		Time:          lot.EntryTime,
		ClientOrderID: req.ID,
		BrokerOrderID: NewULID(),

		// Redundant?
		PositionID: lot.ID,
		Instrument: req.Instrument,
		Open:       req,
		Lot:        lot,
	}

	if err := b.emitEvent(ctx, evt); err != nil {
		return res, err
	}

	return res, nil
}

func (b *Broker) SubmitOrder(ctx context.Context, ord *order) (*Lot, error) {
	units := ord.TradeCommon.Units
	lot := &Lot{
		TradeCommon:    ord.TradeCommon,
		OriginalUnits:  units,
		RemainingUnits: units,
	}
	return lot, nil
}

func (b *Broker) SubmitClose(ctx context.Context, req *CloseRequest) error {
	if b == nil {
		return fmt.Errorf("nil broker")
	}
	if b.Account == nil {
		return fmt.Errorf("broker account is nil")
	}
	if req == nil {
		return fmt.Errorf("nil close request")
	}
	if req.Lot == nil {
		return fmt.Errorf("close request missing position")
	}

	// place req.CloseRequest on a close queue; submit the order.
	// This is where the emulator would inject delays and such.

	// When the order is filled, create a trade
	trade := &Trade{
		TradeCommon: req.Request.TradeCommon,
		EntryPrice:  req.Lot.EntryPrice,
		EntryTime:   req.Lot.EntryTime,
		ExitPrice:   req.Price,
		ExitTime:    req.Timestamp,
		CloseCause:  req.CloseCause,
	}

	if err := b.Account.CloseLot(req.Lot, trade); err != nil {
		return err
	}

	// send trade back on event queue
	evt := &Event{
		BrokerOrderID: NewULID(),
		Type:          EventPositionClosed,
		ClientOrderID: req.Request.ID,
		Reason:        "lowest low",
		Cause:         CloseManual,
		Trade:         trade,
		Lot:           req.Lot,
	}

	return b.emitEvent(ctx, evt)
}

func (b *Broker) Events() <-chan *Event {
	if b.evtQ == nil {
		b.evtQ = make(chan *Event, brokerEventQueueSize)
	}

	return b.evtQ
}

func (b *Broker) emitEvent(ctx context.Context, evt *Event) error {
	if b.evtQ == nil {
		b.evtQ = make(chan *Event, brokerEventQueueSize)
	}

	if ctx == nil {
		select {
		case b.evtQ <- evt:
			return nil
		default:
			return fmt.Errorf("broker event queue is full")
		}
	}

	select {
	case b.evtQ <- evt:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return fmt.Errorf("broker event queue is full")
	}
}
