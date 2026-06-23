package strategy

import "github.com/rustyeddy/trader/market"

// dailyCandleAccumulator rolls intraday CandleTime values into completed UTC
// daily candles while keeping the current partial day available for inspection.
type dailyCandleAccumulator struct {
	dayNum   int64
	dayOpen  market.Price
	dayHigh  market.Price
	dayLow   market.Price
	dayClose market.Price
	hasDay   bool
}

func (a *dailyCandleAccumulator) Tick(ct market.CandleTime) (market.Candle, bool) {
	dayNum := int64(ct.Timestamp) / 86400
	if !a.hasDay {
		a.start(dayNum, ct.Candle)
		return market.Candle{}, false
	}

	if dayNum != a.dayNum {
		completed := market.Candle{
			Open:  a.dayOpen,
			High:  a.dayHigh,
			Low:   a.dayLow,
			Close: a.dayClose,
		}
		a.start(dayNum, ct.Candle)
		return completed, true
	}

	if ct.High > a.dayHigh {
		a.dayHigh = ct.High
	}
	if ct.Low < a.dayLow {
		a.dayLow = ct.Low
	}
	a.dayClose = ct.Close
	return market.Candle{}, false
}

func (a *dailyCandleAccumulator) start(dayNum int64, c market.Candle) {
	a.dayNum = dayNum
	a.dayOpen = c.Open
	a.dayHigh = c.High
	a.dayLow = c.Low
	a.dayClose = c.Close
	a.hasDay = true
}
