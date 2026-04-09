package broker

import (
	"context"

	"github.com/rustyeddy/trader/portfolio"
	"github.com/rustyeddy/trader/types"
)

type BrokerInterface interface {
	SubmitOpen(ctx context.Context, req *portfolio.OpenRequest) error
	SubmitClose(ctx context.Context, req *portfolio.CloseRequest) error
	Events() <-chan *Event
}

type Broker struct {
	ID   string
	evtQ chan *Event
}

func (b *Broker) SubmitOpen(ctx context.Context, req *portfolio.OpenRequest) error {

	// Create an order and submit the order
	order := portfolio.Order{}
	order.TradeCommon = req.TradeCommon
	order.OrderType = portfolio.OrderMarket
	order.OrderStatus = portfolio.OrderPending

	// place the order in the orderbook

	return nil
}

// portfolio.OrderRequest will change
func (b *Broker) ReadOrderResponses(req *portfolio.OpenRequest) {

	pos := &portfolio.Position{
		Common: req.TradeCommon,
	}
	// pos.FillPrice = req.Price
	// pos.FillTime = req.Timestamp
	// pos.State = portfolio.PositionOpenRequested

	// // This would be the time to emulate a delay between order and fill
	// // we will ignore this for now
	// pos.State = portfolio.PositionOpen

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

	return
}

func (b *Broker) SubmitClose(ctx context.Context, req *portfolio.CloseRequest) error {

	// place req.CloseRequest on an close queue Submit the order,
	// this is where the emulator would be injecting delays and stuff

	// When the order is filled, create a trade
	// pos := req.Position
	trade := &portfolio.Trade{}

	// send trade back on event queue
	evt := &Event{
		BrokerOrderID: types.NewULID(),
		Type:          EventPositionClosed,
		ClientOrderID: req.ID,
		Reason:        "lowest low",
		Cause:         portfolio.CloseManual,
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
