package execution

import (
	"fmt"

	"github.com/rustyeddy/trader/idgen"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// RequestType represents a trader domain type.
type RequestType uint8

const (
	RequestNone RequestType = iota
	RequestMarketOpen
	RequestLimitOpen
	RequestClose
)

// String is an internal helper for trader type processing.
func (t RequestType) String() string {
	switch t {
	case RequestNone:
		return "none"
	case RequestMarketOpen:
		return "market-open"
	case RequestLimitOpen:
		return "limit-open"
	case RequestClose:
		return "close"
	default:
		return "unknown"
	}
}

// Request represents a trader domain type.
type Request struct {
	*TradeCommon
	RequestType
	types.Price
	types.Timestamp
	Reason string
	Candle market.Candle
}

// OpenRequest represents a trader domain type.
type OpenRequest struct {
	Request
}

// CloseRequest represents a trader domain type.
type CloseRequest struct {
	Request
	*Lot
	CloseCause CloseCause
}

// Validate is an internal helper for trader type processing.
func (r *OpenRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("open request is nil")
	}
	if r.TradeCommon == nil {
		return fmt.Errorf("open request missing trade common")
	}
	if r.RequestType != RequestNone && r.RequestType != RequestMarketOpen && r.RequestType != RequestLimitOpen {
		return fmt.Errorf("open request type must be market-open or limit-open, got %s", r.RequestType)
	}
	if r.Instrument == "" {
		return fmt.Errorf("open request instrument must not be empty")
	}
	if r.Side != types.Long && r.Side != types.Short {
		return fmt.Errorf("open request side must be long or short")
	}
	if r.Units <= 0 {
		return fmt.Errorf("open request units must be > 0")
	}
	if r.Price <= 0 {
		return fmt.Errorf("open request price must be > 0")
	}
	return nil
}

// Validate is an internal helper for trader type processing.
func (r *CloseRequest) Validate() error {
	if r == nil {
		return fmt.Errorf("close request is nil")
	}
	if r.Lot == nil {
		return fmt.Errorf("close request missing position")
	}
	if r.Lot.TradeCommon == nil {
		return fmt.Errorf("close request position missing trade common")
	}
	if r.RequestType != RequestNone && r.RequestType != RequestClose {
		return fmt.Errorf("close request type must be close, got %s", r.RequestType)
	}
	if r.Price <= 0 {
		return fmt.Errorf("close request price must be > 0")
	}
	if r.Request.TradeCommon != nil {
		if r.Request.ID != "" && r.Request.ID != r.Lot.ID {
			return fmt.Errorf("close request id %q does not match position id %q", r.Request.ID, r.Lot.ID)
		}
		if r.Request.Instrument != "" && r.Request.Instrument != r.Lot.Instrument {
			return fmt.Errorf("close request instrument %q does not match position instrument %q", r.Request.Instrument, r.Lot.Instrument)
		}
	}
	return nil
}

// NewOpenRequest is an internal helper for trader type processing.
func NewOpenRequest(
	instr string,
	c *market.Candle,
	side types.Side,
	stop types.Price,
	take types.Price,
	reason string) *OpenRequest {
	if c == nil {
		panic("NewOpenRequest: candle time is nil")
	}
	op := &OpenRequest{
		Request: Request{
			TradeCommon: &TradeCommon{
				ID:         idgen.NewULID(),
				Instrument: instr,
				Side:       side,
				Stop:       stop,
				Take:       take,
				Reason:     reason,
			},
			RequestType: RequestMarketOpen,
			Price:       c.Close,
			Reason:      reason,
			Timestamp:   c.Timestamp,
			Candle:      *c,
		},
	}
	return op
}

// CloseCause represents a trader domain type.
type CloseCause int

const (
	CloseUnknown CloseCause = iota
	CloseManual
	CloseStopLoss
	CloseTakeProfit
	CloseBrokerLiquidation
)

// String is an internal helper for trader type processing.
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
