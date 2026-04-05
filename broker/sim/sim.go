package sim

import (
	"context"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/portfolio"
	"github.com/rustyeddy/trader/types"
)

// Sim Broker
type Sim struct {
	evtQ   chan *broker.Event
	opens  map[string]*portfolio.OpenRequest
	closes map[string]*portfolio.CloseRequest
}

func (s *Sim) SubmitOpen(ctx context.Context, req *portfolio.OpenRequest) error {
	if s.opens == nil {
		s.opens = make(map[string]*portfolio.OpenRequest)
	}
	s.opens[req.ID] = req

	// we will fill immediately, could simulate
	// Submit the order
	pos := &portfolio.Position{
		Common: req.Common,
	}
	pos.ID = types.NewULID()
	pos.FillPrice = req.Price
	pos.FillTime = req.ReqTimestamp

	// This would be the time to emulate a delay between order and fill
	// we will ignore this for now

	// send position back on event queue
	evt := &broker.Event{
		Type:          broker.EventOrderFilled,
		Time:          pos.FillTime,
		ClientOrderID: req.ID,
		BrokerOrderID: types.NewULID(),

		// Redundant?
		PositionID: pos.ID,
		Instrument: req.Common.Instrument.Name,
		Reason:     req.Common.Reason,

		Open:     req,
		Position: pos,
	}

	s.evtQ <- evt

	return nil
}

func (s *Sim) SubmitClose(ctx context.Context, req *portfolio.CloseRequest) error {
	if s.closes == nil {
		s.closes = make(map[string]*portfolio.CloseRequest)
	}
	// place req.CloseRequest on an close queue
	s.closes[req.ID] = req

	// Submit the order

	// When the order is filled, create a trade

	// send trade back on event queue

	return nil
}

func (s *Sim) Events() <-chan *broker.Event {
	if s.evtQ == nil {
		s.evtQ = make(chan *broker.Event)
	}

	return s.evtQ
}
