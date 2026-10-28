package main

import (
	"context"
	"time"
)

func init() {
}

func main() {
	br, ok := getBroker("sim")
	if !ok {
		panic("unknown broker")
	}

	act, ok := br.getAccount("sim123")
	if !ok {
		panic("unknown account")
	}

	tr := &trader{
		broker:  br,
		account: act,
	}

	// determine the price data
	tstart, err := time.Parse("2006-01-02", "2010-01-01")
	if err != nil {
		panic(err)
	}

	tend, err := time.Parse("2006-01-02", "2011-12-31")
	if err != nil {
		panic(err)
	}
	prices := &marketData{
		instrument: "EUR/USD",
		timerange: timeRange{
			start: tstart,
			end:   tend,
		},
		interval: "d1",
	}
	prices.synthesize(1, 110000) // start at 1.10000

	emax := &emax{
		name:  "emax",
		short: 20,
		long:  50,
	}

	if err := emax.init(nil); err != nil {
		panic(err)
	}

	bt := &backtest{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := bt.run(ctx, prices.bars(ctx), emax, tr); err != nil {
		panic(err)
	}

	// write the reports
}
