package execution

type EventType int

const (
	EventOrderFilled EventType = iota + 1
	EventPositionClosed
)

type Event struct {
	Type  EventType
	Trade *Trade
	Lot   *Lot
}

func (e EventType) String() string {
	switch e {
	case EventOrderFilled:
		return "OrderFilled"
	case EventPositionClosed:
		return "PositionClosed"
	default:
		return "UnknownEventType"
	}
}
