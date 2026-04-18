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

func tick(instr string, bid, ask float64) trader.Tick {
	return trader.Tick{
		Instrument: instr,
		Timestamp:  trader.FromTime(time.Now()),
		BA:         trader.BA{Bid: trader.PriceFromFloat(bid), Ask: trader.PriceFromFloat(ask)},
	}
}

func main() {
	engine := sim.NewEngine(trader.Account{
		ID:       "EXAMPLE-MULTI",
		Currency: "USD",
		Balance:  trader.MoneyFromFloat(20000),
		Equity:   trader.MoneyFromFloat(20000),
	}, &noopJournal{})

	_ = engine.UpdatePrice(tick("EURUSD", 1.1000, 1.1002))
	_ = engine.UpdatePrice(tick("USDJPY", 150.00, 150.02))

	_, _ = engine.CreateMarketOrder(context.Background(), trader.OrderRequest{Instrument: "EURUSD", Units: trader.Units(1000)})
	_, _ = engine.CreateMarketOrder(context.Background(), trader.OrderRequest{Instrument: "USDJPY", Units: trader.Units(2000)})

	acct, _ := engine.GetAccount(context.Background())
	fmt.Println("open positions across instruments", acct.Equity.Float64())
}
