package backtest

import (
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type CandleFeed interface {
	Next() bool
	Candle() market.Candle
	NextCandle() (market.Candle, bool)
	Timestamp() types.Timestamp
	Err() error
	Close() error
}
