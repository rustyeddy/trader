package trader

// RequestType defines the RequestType type.
type RequestType uint8

const (
	RequestNone RequestType = iota
	RequestMarketOpen
	RequestLimitOpen
	RequestClose
)

// Request defines the Request type.
type Request struct {
	*TradeCommon
	RequestType
	Price
	Timestamp
	Reason string
	Candle Candle
}

// OpenRequest defines the OpenRequest type.
type OpenRequest struct {
	Request
}

// CloseRequest defines the CloseRequest type.
type CloseRequest struct {
	Request
	*Lot
	CloseCause closeCause
}

// NewOpenRequest performs NewOpenRequest.
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

// closeCause defines the closeCause type.
type closeCause int

const (
	CloseUnknown closeCause = iota
	CloseManual
	CloseStopLoss
	CloseTakeProfit
	CloseBrokerLiquidation
)

// String performs String.
func (c closeCause) String() string {
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
