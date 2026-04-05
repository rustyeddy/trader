package broker

type CloseCause int

const (
	CloseUnknown CloseCause = iota
	CloseManual
	CloseStopLoss
	CloseTakeProfit
	CloseBrokerLiquidation
)
