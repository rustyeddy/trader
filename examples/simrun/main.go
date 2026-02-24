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

func main() {
	engine := sim.NewEngine(broker.Account{
		ID:       "EXAMPLE-SIMRUN",
		Currency: "USD",
		Balance:  types.MoneyFromFloat(5000),
		Equity:   types.MoneyFromFloat(5000),
	}, &noopJournal{})

	for i := 0; i < 3; i++ {
		_ = engine.UpdatePrice(market.Tick{
			Instrument: "EUR_USD",
			Timestamp:  types.FromTime(time.Now().Add(time.Duration(i) * time.Second)),
			BA: market.BA{
				Bid: types.PriceFromFloat(1.1000 + 0.0001*float64(i)),
				Ask: types.PriceFromFloat(1.1002 + 0.0001*float64(i)),
			},
		})
	}

	acct, _ := engine.GetAccount(context.Background())
	fmt.Printf("equity=%0.2f\n", acct.Equity.Float64())
}
