package broker

type CloseCause int

const (
	CloseUnknown CloseCause = iota
	CloseManual
	CloseStopLoss
	CloseTakeProfit
	CloseBrokerLiquidation
)

func (c CloseCause) String() string {
	switch c {
	case CloseManual:
		return "Manual"
	case CloseStopLoss:
		return "StopLoss"
	case CloseTakeProfit:
		return "TakeProfit"
	case CloseBrokerLiquidation:
		return "BrokerLiquidation"
	case CloseUnknown:
		fallthrough
	default:
		return "Unknown"
	}
}
