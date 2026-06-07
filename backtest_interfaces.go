package trader

import "context"

// CandleSource provides candle iterators for backtest and replay execution.
// DataManager satisfies this interface.
type CandleSource interface {
	Candles(context.Context, CandleRequest) (candleIterator, error)
}
