package trader

type orderType uint8

const (
	OrderNone orderType = iota
	OrderMarket
	OrderLimit
	OrderStop
	OrderStopLimit
	OrderTrailingStop
)

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

type orderStatus uint8

const (
	OrderStatusNone orderStatus = iota
	OrderPending
	OrderAccepted
	OrderFilled
	OrderRejected
	OrderCanceled
)

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

type order struct {
	*TradeCommon
	orderType
	orderStatus
}
