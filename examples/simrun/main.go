package main

import (
	"context"
	"fmt"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/broker/sim"
	"github.com/rustyeddy/trader/journal"
	"github.com/rustyeddy/trader/market"
)

func main() {
	ctx := context.Background()

	j, err := journal.NewCSV("./trades.csv", "./equity.csv")
	if err != nil {
		panic(err)
	}
	defer j.Close()

	engine := sim.NewEngine(broker.Account{
		ID:       "SIM-001",
		Currency: "USD",
		Balance:  100_000,
		Equity:   100_000,
	}, j)

	// Seed an initial price (include Time so snapshots are non-zero timestamps)
	engine.Prices().Set(market.Tick{
		Instrument: "EUR_USD",
		Bid:        1.0849,
		Ask:        1.0851,
		Time:       time.Now(),
	})

	// Open a 10k unit long (close will happen on BID)
	fill, err := engine.CreateMarketOrder(ctx, broker.MarketOrderRequest{
		Instrument: "EUR_USD",
		Units:      10_000,
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("OPEN  trade=%s units=%.0f entry=%.5f\n", fill.TradeID, fill.Units, fill.Price)

	// Move the market a bit and record an equity snapshot (UpdatePrice does snapshotting)
	t2 := time.Now().Add(10 * time.Second)
	if err := engine.UpdatePrice(market.Tick{
		Instrument: "EUR_USD",
		Bid:        1.0860,
		Ask:        1.0862,
		Time:       t2,
	}); err != nil {
		panic(err)
	}

	// Now manually close at current BID (since it's a long)
	if err := engine.CloseTrade(ctx, fill.TradeID, "ManualClose"); err != nil {
		panic(err)
	}
	acct, _ := engine.GetAccount(ctx)
	fmt.Printf("CLOSE balance=%.2f equity=%.2f\n", acct.Balance, acct.Equity)
}
