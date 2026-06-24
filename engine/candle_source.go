package engine

import (
	"context"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/marketdata"
)

// CandleSource provides candle iterators for backtest and replay execution.
// marketdata.DataManager satisfies this interface.
type CandleSource interface {
	Candles(context.Context, marketdata.CandleRequest) (market.CandleIterator, error)
}
