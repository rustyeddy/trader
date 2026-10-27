package main

import (
	"context"
	"time"
)

func main() {
	br, ok := getBroker("sim")
	if !ok {
		panic("unknown broker")
		return
	}

	act, ok := br.getAccount("sim123")
	if !ok {
		panic("unknown account")
		return
	}

	tr := &trader{
		broker:  br,
		account: act,
	}

	// determine the price data
	tstart, _ := time.Parse(time.RFC3339, "1/1/2010")
	tend, _ := time.Parse(time.RFC3339, "12/31/2011")
	prices := &marketData{
		instrument: "EUR/USD",
		timerange: timeRange{
			start: tstart,
			end:   tend,
		},
		interval: "d1",
	}

	emax := &emax{
		name:  "emax",
		short: 20,
		long:  50,
	}

	bt := &backtest{}
	err := bt.run(context.TODO(), prices.getPriceQ(), emax, tr)
	if err != nil {
		panic(err)
	}

	// write the reports
}
