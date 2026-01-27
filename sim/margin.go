package sim

import "github.com/rustyeddy/trader/market"

func TradeMargin(units float64, price float64, instrument string, quoteToAccount float64) float64 {
	meta := market.Instruments[instrument]
	notionalQuote := abs(units) * price
	notionalAccount := notionalQuote * quoteToAccount
	return notionalAccount * meta.MarginRate
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
