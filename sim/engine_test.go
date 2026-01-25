package sim

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/journal"
)

type testJournal struct {
	trades []journal.TradeRecord
	equity []journal.EquitySnapshot
	closed bool
}

func (j *testJournal) RecordTrade(rec journal.TradeRecord) error {
	j.trades = append(j.trades, rec)
	return nil
}

func (j *testJournal) RecordEquity(rec journal.EquitySnapshot) error {
	j.equity = append(j.equity, rec)
	return nil
}

func (j *testJournal) Close() error {
	j.closed = true
	return nil
}

func newEngine(t *testing.T, balance float64) (*Engine, *testJournal) {
	t.Helper()
	acct := broker.Account{
		ID:       "acct-1",
		Currency: "USD",
		Balance:  balance,
		Equity:   balance,
	}
	j := &testJournal{}
	return NewEngine(acct, j), j
}

func setPrice(t *testing.T, e *Engine, instr string, bid, ask float64, tm time.Time) {
	t.Helper()
	err := e.UpdatePrice(broker.Price{
		Instrument: instr,
		Bid:        bid,
		Ask:        ask,
		Time:       tm,
	})
	if err != nil {
		t.Fatalf("update price: %v", err)
	}
}

func openMarket(t *testing.T, e *Engine, instr string, units float64, sl, tp *float64) broker.OrderFill {
	t.Helper()
	fill, err := e.CreateMarketOrder(context.Background(), broker.MarketOrderRequest{
		Instrument: instr,
		Units:      units,
		StopLoss:   sl,
		TakeProfit: tp,
	})
	if err != nil {
		t.Fatalf("create market order: %v", err)
	}
	return fill
}

func approxEqual(a, b, tol float64) bool {
	return math.Abs(a-b) <= tol
}

func TestEngineRevalueEURUSDLong(t *testing.T) {
	e, _ := newEngine(t, 100000)

	t0 := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)

	setPrice(t, e, "EUR_USD", 1.1000, 1.1002, t0)
	openMarket(t, e, "EUR_USD", 100000, nil, nil)

	setPrice(t, e, "EUR_USD", 1.1010, 1.1012, t1)

	acct, err := e.GetAccount(context.Background())
	if err != nil {
		t.Fatalf("get account: %v", err)
	}

	expectedPL := 100000 * (1.1010 - 1.1002)
	expectedEquity := 100000 + expectedPL

	if !approxEqual(acct.Balance, 100000, 1e-6) {
		t.Fatalf("balance mismatch: got %.6f", acct.Balance)
	}
	if !approxEqual(acct.Equity, expectedEquity, 1e-6) {
		t.Fatalf("equity mismatch: got %.6f want %.6f", acct.Equity, expectedEquity)
	}
}

func TestEngineRevalueUSDJPYLongWithConversion(t *testing.T) {
	e, _ := newEngine(t, 100000)

	t0 := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)

	setPrice(t, e, "USD_JPY", 150.00, 150.02, t0)
	openMarket(t, e, "USD_JPY", 100000, nil, nil)

	setPrice(t, e, "USD_JPY", 150.22, 150.24, t1)

	acct, err := e.GetAccount(context.Background())
	if err != nil {
		t.Fatalf("get account: %v", err)
	}

	plJPY := 100000 * (150.22 - 150.02)
	mid := (150.22 + 150.24) / 2
	plUSD := plJPY / mid
	expectedEquity := 100000 + plUSD

	if !approxEqual(acct.Equity, expectedEquity, 1e-3) {
		t.Fatalf("equity mismatch: got %.6f want %.6f", acct.Equity, expectedEquity)
	}
}

func TestStopLossUsesCorrectSide(t *testing.T) {
	t0 := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)

	t.Run("long stop loss uses bid", func(t *testing.T) {
		e, _ := newEngine(t, 100000)
		setPrice(t, e, "EUR_USD", 1.1000, 1.1002, t0)
		sl := 1.0990
		fill := openMarket(t, e, "EUR_USD", 100000, &sl, nil)

		setPrice(t, e, "EUR_USD", 1.0990, 1.0992, t1)

		acct, err := e.GetAccount(context.Background())
		if err != nil {
			t.Fatalf("get account: %v", err)
		}

		if len(e.trades) != 1 || e.trades[fill.TradeID].Open {
			t.Fatalf("expected trade to be closed")
		}

		expectedPL := 100000 * (1.0990 - 1.1002)
		expectedBalance := 100000 + expectedPL

		if !approxEqual(acct.Balance, expectedBalance, 1e-6) {
			t.Fatalf("balance mismatch: got %.6f want %.6f", acct.Balance, expectedBalance)
		}
		if !approxEqual(acct.Equity, acct.Balance, 1e-6) {
			t.Fatalf("equity should equal balance: got %.6f", acct.Equity)
		}
	})

	t.Run("short stop loss uses ask", func(t *testing.T) {
		e, _ := newEngine(t, 100000)
		setPrice(t, e, "EUR_USD", 1.1000, 1.1002, t0)
		sl := 1.1012
		fill := openMarket(t, e, "EUR_USD", -100000, &sl, nil)

		setPrice(t, e, "EUR_USD", 1.1010, 1.1012, t1)

		acct, err := e.GetAccount(context.Background())
		if err != nil {
			t.Fatalf("get account: %v", err)
		}

		if len(e.trades) != 1 || e.trades[fill.TradeID].Open {
			t.Fatalf("expected trade to be closed")
		}

		expectedPL := -100000 * (1.1012 - 1.1000)
		expectedBalance := 100000 + expectedPL

		if !approxEqual(acct.Balance, expectedBalance, 1e-6) {
			t.Fatalf("balance mismatch: got %.6f want %.6f", acct.Balance, expectedBalance)
		}
		if !approxEqual(acct.Equity, acct.Balance, 1e-6) {
			t.Fatalf("equity should equal balance: got %.6f", acct.Equity)
		}
	})
}

func TestForcedLiquidationWorstTradeFirst(t *testing.T) {
	e, j := newEngine(t, 1000)

	t0 := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)

	setPrice(t, e, "EUR_USD", 1.1000, 1.1002, t0)
	setPrice(t, e, "USD_JPY", 150.00, 150.02, t0)

	openMarket(t, e, "EUR_USD", 100000, nil, nil)
	openMarket(t, e, "USD_JPY", 100000, nil, nil)

	setPrice(t, e, "EUR_USD", 1.0500, 1.0502, t1)
	setPrice(t, e, "USD_JPY", 149.98, 150.00, t1)

	acct, err := e.GetAccount(context.Background())
	if err != nil {
		t.Fatalf("get account: %v", err)
	}

	openEUR := false
	openUSD := false
	for _, tr := range e.trades {
		if !tr.Open {
			continue
		}
		switch tr.Instrument {
		case "EUR_USD":
			openEUR = true
		case "USD_JPY":
			openUSD = true
		}
	}

	if openEUR {
		t.Fatalf("expected worst trade (EUR_USD) to be closed first")
	}

	if acct.MarginUsed > 0 && acct.Equity < acct.MarginUsed {
		t.Fatalf("margin invariant violated: equity %.6f margin %.6f", acct.Equity, acct.MarginUsed)
	}
	if acct.Balance >= 1000 {
		t.Fatalf("expected liquidation to realize losses, balance %.6f", acct.Balance)
	}
	if !openUSD && acct.MarginUsed > 0 {
		t.Fatalf("expected margin used to be cleared when no trades open")
	}

	liquidated := false
	for _, rec := range j.trades {
		if rec.Reason == "LIQUIDATION" {
			liquidated = true
			break
		}
	}
	if !liquidated {
		t.Fatalf("expected liquidation trade record")
	}
}
