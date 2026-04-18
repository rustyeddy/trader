package trader

import (
	"context"
)

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
	ID   string
	evtQ chan *Event
	OpenOrders
}

func (b *Broker) OpenRequest(ctx context.Context, req *OpenRequest) (*OpenResult, error) {

	// Create an order and submit the order
	o := &Order{}
	o.TradeCommon = req.TradeCommon
	o.OrderType = OrderMarket
	o.OrderStatus = OrderPending

	// place the order in the orderbook
	b.OpenOrders.Add(o)

	// here is where the order get submitted to a real broker if
	// we are using one.
	res := &OpenResult{
		Order: o,
	}

	// Here we will just fake it and return a filled order.
	pos, err := b.SubmitOrder(ctx, o)
	if err != nil {
		return res, err
	}
	// TODO: Need to create a fill fills
	pos.FillPrice = req.Price
	pos.FillTime = req.Timestamp

	// This would be the time to emulate a delay between order and fill
	// we will ignore this for now
	pos.State = PositionOpen
	res.Position = pos
	// send position back on event queue
	evt := &Event{
		Type:          EventOrderFilled,
		Time:          pos.FillTime,
		ClientOrderID: req.ID,
		BrokerOrderID: NewULID(),

		// Redundant?
		PositionID: pos.ID,
		Instrument: req.Instrument,
		Open:       req,
		Position:   pos,
	}

	b.evtQ <- evt

	return res, nil
}

func (b *Broker) SubmitOrder(ctx context.Context, ord *Order) (*Position, error) {
	pos := &Position{
		TradeCommon: ord.TradeCommon,
	}
	return pos, nil
}

// OrderRequest will change
func (b *Broker) ReadOrderResponses(req *OpenRequest) {

	o := &Order{
		TradeCommon: req.TradeCommon,
		OrderType:   OrderMarket,
		OrderStatus: OrderPending,
	}
	b.OpenOrders.Add(o)
}

func (b *Broker) SubmitClose(ctx context.Context, req *CloseRequest) error {

	// place req.CloseRequest on an close queue Submit the order,
	// this is where the emulator would be injecting delays and stuff

	// When the order is filled, create a trade
	trade := &Trade{
		TradeCommon: req.Request.TradeCommon,
		FillPrice:   req.Price,
		FillTime:    req.Timestamp,
	}

	// send trade back on event queue
	evt := &Event{
		BrokerOrderID: NewULID(),
		Type:          EventPositionClosed,
		ClientOrderID: req.Request.ID,
		Reason:        "lowest low",
		Cause:         CloseManual,
		Trade:         trade,
		Position:      req.Position,
	}

	b.evtQ <- evt
	return nil
}

func (b *Broker) Events() <-chan *Event {
	if b.evtQ == nil {
		b.evtQ = make(chan *Event)
	}

	return b.evtQ
}
