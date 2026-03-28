package backtest

import (
"bytes"
"testing"
"time"

"github.com/rustyeddy/trader/types"
"github.com/stretchr/testify/assert"
)

// ─── PrintBacktestRun ─────────────────────────────────────────────────────────

func TestPrintBacktestRun_Basic(t *testing.T) {
r := BacktestRun{
RunID:        "run-001",
Name:         "test",
Kind:         "candle",
Created:      types.FromTime(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)),
Strategy:     "buy-first",
Instrument:   "EURUSD",
Timeframe:    "H1",
Dataset:      "dukascopy",
Start:        types.FromTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
End:          types.FromTime(time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC)),
Trades:       10,
Wins:         6,
Losses:       4,
StartBalance: types.MoneyFromFloat(10000.0),
EndBalance:   types.MoneyFromFloat(10500.0),
NetPL:        types.MoneyFromFloat(500.0),
ReturnPct:    types.RateFromFloat(0.05),
WinRate:      types.RateFromFloat(0.6),
ProfitFactor: types.RateFromFloat(1.5),
MaxDDPct:     types.RateFromFloat(0.03),
RiskPct:      types.RateFromFloat(0.005),
StopPips:     20,
RR:           types.RateFromFloat(2.0),
}

var buf bytes.Buffer
PrintBacktestRun(&buf, r)
out := buf.String()

assert.Contains(t, out, "run-001")
assert.Contains(t, out, "EURUSD")
assert.Contains(t, out, "H1")
assert.Contains(t, out, "Trades:")
assert.Contains(t, out, "10")
assert.Contains(t, out, "Wins:")
assert.Contains(t, out, "6")
assert.Contains(t, out, "Losses:")
assert.Contains(t, out, "4")
}

func TestPrintBacktestRun_WithGitCommit(t *testing.T) {
r := BacktestRun{
RunID:     "run-002",
GitCommit: "abc1234",
}
var buf bytes.Buffer
PrintBacktestRun(&buf, r)
assert.Contains(t, buf.String(), "abc1234")
}

func TestPrintBacktestRun_WithEquityPNG(t *testing.T) {
r := BacktestRun{
RunID:     "run-003",
EquityPNG: "/tmp/equity.png",
}
var buf bytes.Buffer
PrintBacktestRun(&buf, r)
assert.Contains(t, buf.String(), "/tmp/equity.png")
}

func TestPrintBacktestRun_WithOrgPath(t *testing.T) {
r := BacktestRun{
RunID:   "run-004",
OrgPath: "/tmp/result.org",
}
var buf bytes.Buffer
PrintBacktestRun(&buf, r)
assert.Contains(t, buf.String(), "/tmp/result.org")
}

func TestPrintBacktestRun_WithNotes(t *testing.T) {
r := BacktestRun{
Notes: []string{"note one", "note two"},
}
var buf bytes.Buffer
PrintBacktestRun(&buf, r)
out := buf.String()
assert.Contains(t, out, "note one")
assert.Contains(t, out, "note two")
}

func TestPrintBacktestRun_WithNextActions(t *testing.T) {
r := BacktestRun{
NextActions: []string{"action one", "action two"},
}
var buf bytes.Buffer
PrintBacktestRun(&buf, r)
out := buf.String()
assert.Contains(t, out, "action one")
assert.Contains(t, out, "action two")
}

func TestPrintBacktestRun_ZeroProfitFactorAndDD(t *testing.T) {
r := BacktestRun{
ProfitFactor: 0,
MaxDDPct:     0,
}
var buf bytes.Buffer
PrintBacktestRun(&buf, r)
// zero profit factor and max dd should not be printed
out := buf.String()
assert.NotContains(t, out, "Profit Factor")
assert.NotContains(t, out, "Max Drawdown")
}
