package broker

import (
	"github.com/rustyeddy/trader/portfolio"
	"github.com/rustyeddy/trader/types"
)

type EventType int

const (
	EventOrderAccepted EventType = iota + 1
	EventOrderRejected
	EventOrderFilled
	EventOrderPartiallyFilled
	EventOrderCanceled
	EventPositionClosed
	EventAccountUpdated
)

type Event struct {
	Type          EventType
	Time          types.Timestamp
	ClientOrderID string
	BrokerOrderID string
	PositionID    string
	Instrument    string
	Reason        string
	Cause         CloseCause

	Open     *portfolio.OpenRequest
	Close    *portfolio.CloseRequest
	Trade    *portfolio.Trade
	Position *portfolio.Position
}
