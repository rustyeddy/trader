package trader

type RequestType uint8

const (
	RequestNone RequestType = iota
	RequestMarketOpen
	RequestLimitOpen
	RequestClose
)

type Request struct {
	*TradeCommon
	RequestType
	Price
	Timestamp
	Reason string
	Candle Candle
}

type OpenRequest struct {
	Request
}

type CloseRequest struct {
	Request
	*Position
	CloseCause CloseCause
}

func NewOpenRequest(
	instr string,
	c *CandleTime,
	side Side,
	stop Price,
	take Price,
	reason string) *OpenRequest {

	th := NewTradeHistory(instr)
	th.Side = side
	th.Stop = stop
	th.Take = take
	op := &OpenRequest{
		Request: Request{
			TradeCommon: th.TradeCommon,
			RequestType: RequestMarketOpen,
			Price:       c.Close,
			Reason:      reason,
			Timestamp:   c.Timestamp,
			Candle:      c.Candle,
		},
	}
	th.OpenRequest = op
	return op
}

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
