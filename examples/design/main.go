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
	panicOnError(err)

	prices, err := getPrices()
	panicOnError(err)

	strat, err := getStrategy()
	panicOnError(err)

	slog.Info("backtest starting",
		"strategy", strat.name(),
		"instrument", prices.instrument,
		"interval", prices.interval,
		"bars", len(prices.data))

	bt := &backtest{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := bt.run(ctx, prices.bars(ctx), strat, tr); err != nil {
		return err
	}

	slog.Info("backtest complete", "strategy", strat.name())

	// write the reports
	return nil
}

func panicOnError(err error) {
	if err != nil {
		slog.Error("Goodbye cruel world!", "error", err)
		panic(err)
	}
}

func getTrader() (*trader, error) {
	br, ok := getBroker("sim")
	if !ok {
		return nil, errUnknownBroker
	}

	act, ok := br.getAccount("sim123")
	if !ok {
		return nil, errUnknownAccount
	}

	tr := &trader{
		broker:  br,
		account: act,
	}
	return tr, nil
}

func getPrices() (*marketData, error) {

	// determine the price data
	tstart, err := time.Parse("2006-01-02", "2010-01-01")
	if err != nil {
		return nil, err
	}

	tend, err := time.Parse("2006-01-02", "2011-12-31")
	if err != nil {
		return nil, err
	}
	prices := &marketData{
		instrument: "EURUSD",
		timerange: timeRange{
			start: tstart,
			end:   tend,
		},
		interval: "d1",
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

	err := emax.init(nil)
	return emax, err
}
