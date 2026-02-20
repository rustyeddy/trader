package strategies

import "github.com/rustyeddy/trader/market"

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func mkClose(scale int32, close float64) market.OHLC {
	toP := func(x float64) market.Price { return market.Price(x*float64(scale) + 0.5) }
	return market.OHLC{C: toP(close)}
}
