package design

import (
	"context"
	"os"
	"time"
)

type trader struct {
	broker  *broker
	account *account
}

func (tr *trader) intentLoop(ctx context.Context) (intentQ chan intent) {
	intentQ = make(chan intent)
	go func() {

		select {
		case i := <-intentQ:
			tr.handleIntent(i)

		case <-ctx.Done():
			os.Exit(0)
		}

	}()
	return intentQ
}

func (tr *trader) backtest(ctx context.Context) {

	// Determine the broker and account the backtest
	// will run with
	tr.broker = getBroker("sim")
	if tr.broker == nil {
		panic("broker is nil")
	}

	var ok bool
	tr.account, ok = tr.broker.getAccount("test123")
	if !ok {
		panic("not ok")
	}

	// select a strategy and set the parameters
	cfg := map[string]any{
		"short": 20,
		"long":  50,
	}
	strat := &emax{}
	strat.init(cfg)

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

	bt := backtest{
		strategy: strat,
		prices:   prices,
	}

	intentQ := tr.intentLoop(ctx)
	res, err := bt.Run(ctx, intentQ)
	if err != nil {
		panic(err)
	}

	err = res.writeJournal()
	if err != nil {
		panic(err)
	}
}

func (tr *trader) handleIntent(i intent) {

	switch i.kind {
	case "open":
		if tr.account.marginAvailable() <= 0 {
			return
		}
		tr.openTrade(i)

	case "close":
		tr.closeTrade(i)
	}

}

func (tr *trader) openTrade(i intent) error {

	return nil
}

func (tr *trader) closeTrade(i intent) error {

	return nil
}
