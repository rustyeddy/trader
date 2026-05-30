package trader

// orderType represents a trader domain type.
type orderType uint8

const (
	OrderNone orderType = iota
	OrderMarket
	OrderLimit
	OrderStop
	OrderStopLimit
	OrderTrailingStop
)

// String is an internal helper for trader type processing.
func (ot orderType) String() string {
	switch ot {
	case OrderNone:
		return "none"
	case OrderMarket:
		return "market"
	case OrderLimit:
		return "limit"
	case OrderStop:
		return "stop"
	case OrderStopLimit:
		return "stop-limit"
	case OrderTrailingStop:
		return "trailing-stop"
	default:
		return "<unknown>"
	}
}

// orderStatus represents a trader domain type.
type orderStatus uint8

const (
	OrderStatusNone orderStatus = iota
	OrderPending
	OrderAccepted
	OrderFilled
	OrderRejected
	OrderCanceled
)

// String is an internal helper for trader type processing.
func (os orderStatus) String() string {
	switch os {
	case OrderStatusNone:
		return "none"

	case OrderPending:
		return "pending"
	case OrderAccepted:
		return "accepted"
	case OrderFilled:
		return "filled"
	case OrderRejected:
		return "rejected"
	case OrderCanceled:
		return "canceled"
	default:
		return "<unknown>"
	}
}

// order represents a trader domain type.
type order struct {
	*TradeCommon
	orderType
	orderStatus
}
