package trader

import (
	"github.com/rustyeddy/trader/types"
)

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func mkClose(close float64) types.Candle {
	toP := func(x float64) types.Price { return types.Price(x*float64(types.PriceScale) + 0.5) }
	return types.Candle{Close: toP(close)}
}
