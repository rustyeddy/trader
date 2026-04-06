package sim

import (
	"context"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/portfolio"
	"github.com/rustyeddy/trader/types"
)

// Sim Broker
type Sim struct {
	evtQ chan *broker.Event
}

func (s *Sim) SubmitOpen(ctx context.Context, req *portfolio.OpenRequest) error {
	// we will fill immediately, could simulate
	// Submit the order
	pos := &portfolio.Position{
		Common: req.Common,
	}
	pos.ID = types.NewULID()
	pos.FillPrice = req.Price
	pos.FillTime = req.ReqTimestamp
	pos.State = portfolio.PositionOpenRequested

	// This would be the time to emulate a delay between order and fill
	// we will ignore this for now
	pos.State = portfolio.PositionOpen

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
		Open:       req,
		Position:   pos,
	}

	s.evtQ <- evt

	return nil
}

func (s *Sim) SubmitClose(ctx context.Context, req *portfolio.CloseRequest) error {
	if req.Position == nil {
		panic("position is nil")
	}

	// place req.CloseRequest on an close queue Submit the order,
	// this is where the emulator would be injecting delays and stuff

	// When the order is filled, create a trade
	pos := req.Position
	trade := &portfolio.Trade{
		ID:         types.NewULID(),
		Common:     req.Common,
		Price:      req.Price,
		PositionID: pos.ID,
		OpenPrice:  pos.OpenPrice,
		FillPrice:  pos.FillPrice,
		ExitPRice:  req.ExitPrice,
	}

	// send trade back on event queue
	evt := &broker.Event{
		BrokerOrderID: types.NewULID(),
		ClientOrderID: req.ID,
		Type:          broker.EventPositionClosed,
		PositionID:    req.Position.ID,
		Instrument:    req.Instrument.Name,
		Reason:        "lowest low",
		Cause:         broker.CloseManual,
	}

	s.evtQ <- evt
	return nil
}

func (s *Sim) Events() <-chan *broker.Event {
	if s.evtQ == nil {
		s.evtQ = make(chan *broker.Event)
	}

	return s.evtQ
}
