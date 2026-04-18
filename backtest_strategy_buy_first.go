package trader

import ()

type BuyFirstBarStrategy struct {
	done bool
}

func (s *BuyFirstBarStrategy) Name() string {
	return "buy-first-bar"
}

func (s *BuyFirstBarStrategy) Reset() {
	s.done = false
}

func (s *BuyFirstBarStrategy) OnBar(ctx *CandleContext, c Candle) *OpenRequest {
	if s.done || ctx.Pos != nil {
		return nil
	}

	s.done = true

	th := NewTradeHistory(ctx.Instrument)
	th.Side = Long
	th.Units = Units(1000)
	return &OpenRequest{
		Request: Request{TradeCommon: th.TradeCommon, Price: c.Close, Timestamp: ctx.Timestamp, Reason: "enter on first bar"},
	}
}
