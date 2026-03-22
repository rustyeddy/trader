package strategies

import (
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func mkClose(scale int32, close float64) market.Candle {
	toP := func(x float64) types.Price { return types.Price(x*float64(scale) + 0.5) }
	return market.Candle{Close: toP(close)}
}
