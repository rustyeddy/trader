package execution

import "github.com/rustyeddy/trader/market"

// FillAdjust returns the price adjustment for spread and slippage when turning
// a bid-side OHLC price into an executed fill. Dukascopy OHLC prices are
// bid-side: when buying (long open, short close) we pay the ask, so the
// adjustment is +spread+slippage; when selling we only lose slippage.
func FillAdjust(isBuy bool, spread, slippage market.Price) market.Price {
	if isBuy {
		return spread + slippage
	}
	return -slippage
}
