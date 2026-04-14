package portfolio

import (
	"log"
)

type OrderType uint8

const (
	OrderNone OrderType = iota
	OrderMarket
	OrderLimit
	OrderStop
	OrderStopLimit
	OrderTrailingStop
)

func (ot OrderType) String() string {
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
		log.Fatal("Uknown Order Type")
		return "unknown"
	}
}

type OrderStatus uint8

const (
	OrderStatusNone OrderStatus = iota
	OrderPending
	OrderAccepted
	OrderFilled
	OrderRejected
	OrderCanceled
)

func (os OrderStatus) String() string {
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
		return "cancled"
	default:
		log.Fatal("unknown order", os)
	}
	return ""
}

type Order struct {
	*TradeCommon
	OrderType
	OrderStatus
}
