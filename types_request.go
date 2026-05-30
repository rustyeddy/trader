package trader

// RequestType represents a trader domain type.
type RequestType uint8

const (
	RequestNone RequestType = iota
	RequestMarketOpen
	RequestLimitOpen
	RequestClose
)

// Request represents a trader domain type.
type Request struct {
	*TradeCommon
	RequestType
	Price
	Timestamp
	Reason string
	Candle Candle
}

// OpenRequest represents a trader domain type.
type OpenRequest struct {
	Request
}

// CloseRequest represents a trader domain type.
type CloseRequest struct {
	Request
	*Lot
	CloseCause closeCause
}

// NewOpenRequest is an internal helper for trader type processing.
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

// closeCause represents a trader domain type.
type closeCause int

const (
	CloseUnknown closeCause = iota
	CloseManual
	CloseStopLoss
	CloseTakeProfit
	CloseBrokerLiquidation
)

// String is an internal helper for trader type processing.
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
