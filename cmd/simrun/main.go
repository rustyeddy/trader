package main

import (
	"context"
	"fmt"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/sim"
	"github.com/rustyeddy/trader/strategy"
)

func main() {
	j, err := journal.NewCSV("./trades.csv", "./equity.csv")
	if err != nil {
		panic(err)
	}

	engine := sim.NewEngine(broker.Account{
		ID:       "SIM-001",
		Currency: "USD",
		Balance:  100_000,
		Equity:   100_000,
	}, j)

	engine.Prices().Set(broker.Price{
		Instrument: "EUR_USD",
		Bid:        1.0849,
		Ask:        1.0851,
	})

	ctx := context.Background()
	err = strategy.TradeEURUSD(ctx, engine)
	if err != nil {
		panic(err)
	}

	fmt.Println("Trade executed")
}
