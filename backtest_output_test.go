package trader

// ─── PrintBacktest ─────────────────────────────────────────────────────────

// func TestPrintBacktest_Basic(t *testing.T) {
// 	r := Backtest{
// 		RunID:        "run-001",
// 		Name:         "test",
// 		Kind:         "candle",
// 		Created:      FromTime(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)),
// 		Strategy:     "buy-first",
// 		Instrument:   "EURUSD",
// 		Timeframe:    "H1",
// 		Dataset:      "dukascopy",
// 		Start:        FromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
// 		End:          FromTime(time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)),
// 		Trades:       10,
// 		Wins:         6,
// 		Losses:       4,
// 		StartBalance: MoneyFromFloat(10000.0),
// 		EndBalance:   MoneyFromFloat(10500.0),
// 		NetPL:        MoneyFromFloat(500.0),
// 		ReturnPct:    RateFromFloat(0.05),
// 		WinRate:      RateFromFloat(0.6),
// 		ProfitFactor: RateFromFloat(1.5),
// 		MaxDDPct:     RateFromFloat(0.03),
// 		RiskPct:      RateFromFloat(0.005),
// 		StopPips:     20,
// 		RR:           RateFromFloat(2.0),
// 	}

// 	var buf bytes.Buffer
// 	PrintBacktest(&buf, r)
// 	out := buf.String()

// 	assert.Contains(t, out, "run-001")
// 	assert.Contains(t, out, "EURUSD")
// 	assert.Contains(t, out, "H1")
// 	assert.Contains(t, out, "Trades:")
// 	assert.Contains(t, out, "10")
// 	assert.Contains(t, out, "Wins:")
// 	assert.Contains(t, out, "6")
// 	assert.Contains(t, out, "Losses:")
// 	assert.Contains(t, out, "4")
// }

// func TestPrintBacktest_WithGitCommit(t *testing.T) {
// 	r := Backtest{
// 		RunID:     "run-002",
// 		GitCommit: "abc1234",
// 	}
// 	var buf bytes.Buffer
// 	PrintBacktest(&buf, r)
// 	assert.Contains(t, buf.String(), "abc1234")
// }

// func TestPrintBacktest_WithEquityPNG(t *testing.T) {
// 	r := Backtest{
// 		RunID:     "run-003",
// 		EquityPNG: "/tmp/equity.png",
// 	}
// 	var buf bytes.Buffer
// 	PrintBacktest(&buf, r)
// 	assert.Contains(t, buf.String(), "/tmp/equity.png")
// }

// func TestPrintBacktest_WithOrgPath(t *testing.T) {
// 	r := Backtest{
// 		RunID:   "run-004",
// 		OrgPath: "/tmp/result.org",
// 	}
// 	var buf bytes.Buffer
// 	PrintBacktest(&buf, r)
// 	assert.Contains(t, buf.String(), "/tmp/result.org")
// }

// func TestPrintBacktest_WithNotes(t *testing.T) {
// 	r := Backtest{
// 		Notes: []string{"note one", "note two"},
// 	}
// 	var buf bytes.Buffer
// 	PrintBacktest(&buf, r)
// 	out := buf.String()
// 	assert.Contains(t, out, "note one")
// 	assert.Contains(t, out, "note two")
// }

// func TestPrintBacktest_WithNextActions(t *testing.T) {
// 	r := Backtest{
// 		NextActions: []string{"action one", "action two"},
// 	}
// 	var buf bytes.Buffer
// 	PrintBacktest(&buf, r)
// 	out := buf.String()
// 	assert.Contains(t, out, "action one")
// 	assert.Contains(t, out, "action two")
// }

// func TestPrintBacktest_ZeroProfitFactorAndDD(t *testing.T) {
// 	r := Backtest{
// 		ProfitFactor: 0,
// 		MaxDDPct:     0,
// 	}
// 	var buf bytes.Buffer
// 	PrintBacktest(&buf, r)
// 	// zero profit factor and max dd should not be printed
// 	out := buf.String()
// 	assert.NotContains(t, out, "Profit Factor")
// 	assert.NotContains(t, out, "Max Drawdown")
// }
