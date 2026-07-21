package sim

import "github.com/rustyeddy/trader/market"

// TickFromCandle synthesizes a market.Tick from a candle for feeding Sim
// in backtest's candle-only world, which has no real bid/ask — bid/ask are
// split evenly around the candle's close, half of AvgSpread on each side,
// so Sim's existing ask/bid fill logic (SubmitMarketOrder, CloseTrade)
// gets a realistic spread without needing a separate candle-based code
// path. Not wired into backtest execution yet — see
// docs/Manual/architecture-broker-account-order.org, phase 4 chunk 2.
func TickFromCandle(instrument string, candle market.Candle) market.Tick {
	half := candle.AvgSpread / 2
	return market.Tick{
		Instrument: instrument,
		Timestamp:  candle.Timestamp,
		BA: market.BA{
			Bid: candle.Close - half,
			Ask: candle.Close + half,
		},
	}
}
