package trader

import (
	"github.com/rustyeddy/trader/types"
)

type BuyFirstBarStrategy struct {
	done bool
}

func (s *BuyFirstBarStrategy) Name() string {
	return "buy-first-bar"
}

func (s *BuyFirstBarStrategy) Reset() {
	s.done = false
}

func (s *BuyFirstBarStrategy) OnBar(ctx *CandleContext, c Candle) *types.OpenRequest {
	if s.done || ctx.Pos != nil {
		return nil
	}

	s.done = true

	th := types.NewTradeHistory(ctx.Instrument)
	th.Side = types.Long
	th.Units = types.Units(1000)
	return &types.OpenRequest{
		Request: types.Request{TradeCommon: th.TradeCommon, Price: c.Close, Timestamp: ctx.Timestamp, Reason: "enter on first bar"},
	}
}
