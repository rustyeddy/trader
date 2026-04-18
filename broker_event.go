package trader

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
	Time          Timestamp
	ClientOrderID string
	BrokerOrderID string
	PositionID    string
	Instrument    string
	Reason        string
	Cause         CloseCause

	Open     *OpenRequest
	Close    *CloseRequest
	Trade    *Trade
	Position *Position
}

func (e EventType) String() string {
	switch e {
	case EventOrderAccepted:
		return "OrderAccepted"
	case EventOrderRejected:
		return "OrderRejected"
	case EventOrderFilled:
		return "OrderFilled"
	case EventOrderPartiallyFilled:
		return "OrderPartiallyFilled"
	case EventOrderCanceled:
		return "OrderCanceled"
	case EventPositionClosed:
		return "PositionClosed"
	case EventAccountUpdated:
		return "AccountUpdated"
	default:
		return "UknownEventType"
	}
}

type EventQ struct {
	evtQ chan *Event
}
