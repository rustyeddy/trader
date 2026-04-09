package portfolio

import (
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

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
	types.Price
	types.Timestamp
	Reason string
	Candle market.Candle
}

type OpenRequest struct {
	Request
}

type CloseRequest struct {
	Request
	CloseCause CloseCause
}

func NewOpenRequest(
	instr string,
	c *market.CandleTime,
	side types.Side,
	stop types.Price,
	take types.Price,
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
