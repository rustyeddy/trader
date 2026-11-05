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
	tr, err := getTrader()
	if err != nil {
		return err
	}
	prices, err := getPrices()
	if err != nil {
		return err
	}

	slog.Info("backtest starting",
		"strategy", tr.strategy.name(),
		"instrument", prices.instrument,
		"interval", prices.interval,
		"bars", len(prices.data))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := (&backtest{}).run(ctx, prices.bars(ctx), tr); err != nil {
		return err
	}

	slog.Info("backtest complete", "strategy", tr.strategy.name())
	// write the reports
	return nil
}

func getTrader() (*trader, error) {
	strat, err := getStrategy()
	if err != nil {
		return nil, err
	}
	br := newBroker("sim")
	return &trader{
		broker:   br,
		account:  br.addAccount("sim123", usd(10000)),
		strategy: strat,
	}, nil
}

func getPrices() (*marketData, error) {
	tstart, err := time.Parse(time.DateOnly, "2010-01-01")
	if err != nil {
		return nil, err
	}
	tend, err := time.Parse(time.DateOnly, "2011-12-31")
	if err != nil {
		return nil, err
	}
	prices := &marketData{
		instrument: "EURUSD",
		timerange:  timeRange{start: tstart, end: tend},
		interval:   "d1",
	}
	prices.synthesize(1, 110000) // start at 1.10000
	return prices, nil
}

func getStrategy() (strategy, error) {
	emax := &emax{
		id:      "emax",
		short:   20,
		long:    50,
		stopPct: rateOnePercent,
	}
	return emax, emax.init(nil)
}
