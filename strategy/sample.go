// pkg/strategy/sample.go
package strategy

import (
	"context"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/risk"
)

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

