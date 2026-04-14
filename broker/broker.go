package broker

import (
	"context"

	"github.com/rustyeddy/trader/types"
)

type BrokerInterface interface {
	SubmitOpen(ctx context.Context, req *types.OpenRequest) error
	SubmitClose(ctx context.Context, req *types.CloseRequest) error
	Events() <-chan *Event
}

type Broker struct {
	ID   string
	evtQ chan *Event
	OpenOrders
}

func (b *Broker) OpenRequest(ctx context.Context, req *types.OpenRequest) (*types.OrderResult, error) {

	// Create an order and submit the order
	o := &types.Order{}
	o.TradeCommon = req.TradeCommon
	o.OrderType = types.OrderMarket
	o.OrderStatus = types.OrderPending

	// place the order in the orderbook
	b.OpenOrders.Add(o)

	// here is where the order get submitted to a real broker if
	// we are using one.

	// Here we will just fake it and return a filled order.
	pos, err := b.SubmitOrder(ctx, o)
	if err != nil {
		return err // probably need to correct here and now
	}

	pos.FillPrice = req.Price
	pos.FillTime = req.Timestamp
	pos.State = types.PositionOpenRequested

	// This would be the time to emulate a delay between order and fill
	// we will ignore this for now
	pos.State = types.PositionOpen

	// send position back on event queue
	evt := &Event{
		Type:          EventOrderFilled,
		Time:          pos.FillTime,
		ClientOrderID: req.ID,
		BrokerOrderID: types.NewULID(),

		// Redundant?
		PositionID: pos.Common.ID,
		Instrument: req.Instrument,
		Open:       req,
		Position:   pos,
	}

	b.evtQ <- evt

	return nil
}

func (b *Broker) SubmitOrder(ctx context.Context, ord *types.Order) (*types.Position, error) {
	// fill := &types.Fill{
	// 	TradeCommon: ord.TradeCommon,
	// }
	pos := &types.Position{
		TradeCommon: ord.TradeCommon,
	}
	return pos, nil
}

// types.OrderRequest will change
func (b *Broker) ReadOrderResponses(req *types.OpenRequest) {

	o := &types.Order{
		TradeCommon: req.TradeCommon,
		OrderType:   types.OrderMarket,
		OrderStatus: types.OrderPending,
	}
	b.OpenOrders.Add(o)
	return
}

func (b *Broker) SubmitClose(ctx context.Context, req *types.CloseRequest) error {

	// place req.CloseRequest on an close queue Submit the order,
	// this is where the emulator would be injecting delays and stuff

	// When the order is filled, create a trade
	// pos := req.Position
	trade := &types.Trade{}

	// send trade back on event queue
	evt := &Event{
		BrokerOrderID: types.NewULID(),
		Type:          EventPositionClosed,
		ClientOrderID: req.ID,
		Reason:        "lowest low",
		Cause:         types.CloseManual,
		Trade:         trade,
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
