package trader

// orderType defines the orderType type.
type orderType uint8

const (
	OrderNone orderType = iota
	OrderMarket
	OrderLimit
	OrderStop
	OrderStopLimit
	OrderTrailingStop
)

// String performs String.
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

// orderStatus defines the orderStatus type.
type orderStatus uint8

const (
	OrderStatusNone orderStatus = iota
	OrderPending
	OrderAccepted
	OrderFilled
	OrderRejected
	OrderCanceled
)

// String performs String.
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

// order defines the order type.
type order struct {
	*TradeCommon
	orderType
	orderStatus
}
