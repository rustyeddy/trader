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
	engine := sim.NewEngine(trader.Account{
		ID:       "EXAMPLE-SIMRUN",
		Currency: "USD",
		Balance:  trader.MoneyFromFloat(5000),
		Equity:   trader.MoneyFromFloat(5000),
	}, &noopJournal{})

	for i := 0; i < 3; i++ {
		_ = engine.UpdatePrice(trader.Tick{
			Instrument: "EUR_USD",
			Timestamp:  trader.FromTime(time.Now().Add(time.Duration(i) * time.Second)),
			BA: trader.BA{
				Bid: trader.PriceFromFloat(1.1000 + 0.0001*float64(i)),
				Ask: trader.PriceFromFloat(1.1002 + 0.0001*float64(i)),
			},
		})
	}

	acct, _ := engine.GetAccount(context.Background())
	fmt.Printf("equity=%0.2f\n", acct.Equity.Float64())
}
