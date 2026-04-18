package main

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/broker/sim"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/types"
)

type noopJournal struct{}

func (noopJournal) RecordTrade(journal.TradeRecord) error     { return nil }
func (noopJournal) RecordEquity(journal.EquitySnapshot) error { return nil }
func (noopJournal) Close() error                              { return nil }

func main() {
	j := &noopJournal{}
	engine := sim.NewEngine(trader.Account{
		ID:       "EXAMPLE-BASIC",
		Currency: "USD",
		Balance:  types.MoneyFromFloat(10000),
		Equity:   types.MoneyFromFloat(10000),
	}, j)

	tick := trader.Tick{
		Instrument: "EUR_USD",
		Timestamp:  types.FromTime(time.Now()),
		BA: trader.BA{
			Bid: types.PriceFromFloat(1.1000),
			Ask: types.PriceFromFloat(1.1002),
		},
	}
	_ = engine.UpdatePrice(tick)

	_, _ = engine.CreateMarketOrder(context.Background(), trader.OrderRequest{
		Instrument: "EUR_USD",
		Units:      types.Units(1000),
	})

	acct, _ := engine.GetAccount(context.Background())
	fmt.Printf("balance=%0.2f equity=%0.2f\n", acct.Balance.Float64(), acct.Equity.Float64())
}
