// strategy/sample.go
package strategy

import (
	"context"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/risk"
)

/*
   Big picture:
   1. Determine when to make a trade on a given instrument
   2. Given Risk level, determine lot size, SL & TP and Journal
   3. Wait to liquidation trigger, Journal and update P&L

   Algorithm
   1. Read the 20 - 50 EMA
   2. Trend: market bearish (20 below 50) or bullish (20 above 50)
      - We always trade with the trend
   3. Determine Support (bullish) or Resistance (bearish)
   4. Wait for pull back to support (bullish) or resistance (bearish)
      - within x% of current support / resistance (limit order)
   5. Given risk level, determine buy price, SL, TP and lot size
   7. Create market order with Buy/Sell, SL & TP
   8. Journal market order   - Journal market order
   8. Each market tick update the market price
   9. Check current price against SL & TP if either are hit liquidate
  11. Optional: automatically liquidate at the end of day
  12. Journal trade (liquidation) along with reason
  13. Update P&L

*/

func TradeEURUSD(ctx context.Context, b broker.Broker) error {
	acct, _ := b.GetAccount(ctx)
	price, _ := b.GetPrice(ctx, "EUR_USD")

	meta := market.Instruments["EUR_USD"]

	size := risk.Calculate(risk.Inputs{
		Equity:         acct.Equity,
		RiskPct:        0.005,
		EntryPrice:     price.Ask,
		StopPrice:      price.Ask - 0.0020, // 20 pips
		PipLocation:    meta.PipLocation,
		QuoteToAccount: 1.0,
	})

	_, err := b.CreateMarketOrder(ctx, broker.MarketOrderRequest{
		Instrument: "EUR_USD",
		Units:      size.Units,
	})
	return err
}
