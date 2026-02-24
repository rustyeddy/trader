package main

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/broker/sim"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

type noopJournal struct{}

func (noopJournal) RecordTrade(journal.TradeRecord) error   { return nil }
func (noopJournal) RecordEquity(journal.EquitySnapshot) error { return nil }
func (noopJournal) Close() error                            { return nil }

func tick(instr string, bid, ask float64) market.Tick {
	return market.Tick{
		Instrument: instr,
		Timestamp:  types.FromTime(time.Now()),
		BA: market.BA{Bid: types.PriceFromFloat(bid), Ask: types.PriceFromFloat(ask)},
	}
}

func main() {
	engine := sim.NewEngine(broker.Account{
		ID:       "EXAMPLE-MULTI",
		Currency: "USD",
		Balance:  types.MoneyFromFloat(20000),
		Equity:   types.MoneyFromFloat(20000),
	}, &noopJournal{})

	_ = engine.UpdatePrice(tick("EUR_USD", 1.1000, 1.1002))
	_ = engine.UpdatePrice(tick("USD_JPY", 150.00, 150.02))

	_, _ = engine.CreateMarketOrder(context.Background(), broker.MarketOrderRequest{Instrument: "EUR_USD", Units: types.Units(1000)})
	_, _ = engine.CreateMarketOrder(context.Background(), broker.MarketOrderRequest{Instrument: "USD_JPY", Units: types.Units(2000)})

	acct, _ := engine.GetAccount(context.Background())
	fmt.Println("open positions across instruments", acct.Equity.Float64())
}
