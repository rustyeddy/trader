package main

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/broker/sim"
)

type noopJournal struct{}

func (noopJournal) RecordTrade(trader.TradeRecord) error     { return nil }
func (noopJournal) RecordEquity(trader.EquitySnapshot) error { return nil }
func (noopJournal) Close() error                             { return nil }

func main() {
	j := &noopJournal{}
	engine := sim.NewEngine(trader.Account{
		ID:       "EXAMPLE-BASIC",
		Currency: "USD",
		Balance:  trader.MoneyFromFloat(10000),
		Equity:   trader.MoneyFromFloat(10000),
	}, j)

	tick := trader.Tick{
		Instrument: "EUR_USD",
		Timestamp:  trader.FromTime(time.Now()),
		BA: trader.BA{
			Bid: trader.PriceFromFloat(1.1000),
			Ask: trader.PriceFromFloat(1.1002),
		},
	}
	_ = engine.UpdatePrice(tick)

	_, _ = engine.CreateMarketOrder(context.Background(), trader.OrderRequest{
		Instrument: "EUR_USD",
		Units:      trader.Units(1000),
	})

	acct, _ := engine.GetAccount(context.Background())
	fmt.Printf("balance=%0.2f equity=%0.2f\n", acct.Balance.Float64(), acct.Equity.Float64())
}
