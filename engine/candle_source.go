package engine

import (
	"context"

	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
)

// CandleSource provides candle iterators for backtest and replay execution.
// datamanager.DataManager satisfies this interface.
type CandleSource interface {
	Candles(context.Context, datamanager.CandleRequest) (market.CandleIterator, error)
}
