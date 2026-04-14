package portfolio

import (
	"github.com/rustyeddy/trader/portfolio"
	"github.com/rustyeddy/trader/types"
)

type TradeHistory struct {
	*TradeCommon
	*portfolio.Request
}

func NewTradeHistory(inst string) *TradeHistory {
	return &TradeHistory{
		TradeCommon: &TradeCommon{
			ID:         types.NewULID(),
			Instrument: inst,
		},
	}
}
