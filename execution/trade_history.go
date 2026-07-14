package execution

import "github.com/rustyeddy/trader/idgen"

// TradeHistory represents a trader domain type.
type TradeHistory struct {
	*TradeCommon
	*OpenRequest
}

// NewTradeHistory is an internal helper for trader type processing.
func NewTradeHistory(inst string) *TradeHistory {
	return &TradeHistory{
		TradeCommon: &TradeCommon{
			ID:         idgen.NewULID(),
			Instrument: inst,
		},
	}
}
