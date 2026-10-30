package main

import (
	"context"
	"log/slog"
	"os"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("backtest failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	br, ok := getBroker("sim")
	if !ok {
		return errUnknownBroker
	}

	act, ok := br.getAccount("sim123")
	if !ok {
		return errUnknownAccount
	}

	tr := &trader{
		broker:  br,
		account: act,
	}

	// determine the price data
	tstart, err := time.Parse("2006-01-02", "2010-01-01")
	if err != nil {
		return err
	}

	tend, err := time.Parse("2006-01-02", "2011-12-31")
	if err != nil {
		return err
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
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	slog.Info("backtest starting",
		"strategy", emax.name,
		"instrument", prices.instrument,
		"interval", prices.interval,
		"bars", len(prices.data))

	bt := &backtest{}
	if err := bt.run(ctx, prices.bars(ctx), emax, tr); err != nil {
		return err
	}

	slog.Info("backtest complete", "strategy", emax.name)

	// write the reports
	return nil
}
