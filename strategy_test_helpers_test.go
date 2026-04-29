package trader

func mkClose(close float64) Candle {
	toP := func(x float64) Price { return Price(x*float64(PriceScale) + 0.5) }
	return Candle{Close: toP(close)}
}
