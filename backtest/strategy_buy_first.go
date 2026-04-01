package backtest

import (
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/portfolio"
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

func (s *BuyFirstBarStrategy) OnBar(ctx *CandleContext, c market.Candle) *portfolio.OpenRequest {
	if s.done || ctx.Pos != nil {
		return nil
	}

	s.done = true

	return &portfolio.OpenRequest{
		Side:   types.Long,
		Units:  types.Units(1000),
		Reason: "enter on first bar",
	}
}
