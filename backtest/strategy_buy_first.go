package backtest

import (
	"github.com/rustyeddy/trader/market"
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

func (s *BuyFirstBarStrategy) OnBar(ctx *CandleContext, c market.Candle) *OrderRequest {
	if s.done || ctx.Pos.Open {
		return nil
	}

	s.done = true

	return &OrderRequest{
		Side:   Long,
		Units:  types.Units(1000),
		Reason: "enter on first bar",
	}
}
