package trader

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// minSummary returns a minimal BacktestReportSummary for report tests.
func minSummary() BacktestReportSummary {
	return BacktestReportSummary{
		Name:         "test-run",
		Strategy:     "TestStrategy",
		Instrument:   "EURUSD",
		Timeframe:    "h1",
		Start:        "2024-01-01T00:00:00Z",
		End:          "2024-12-31T00:00:00Z",
		Trades:       100,
		Wins:         60,
		Losses:       40,
		WinRate:      60.0,
		StartBalance: 10_000,
		EndBalance:   10_420,
		NetPL:        420.00,
		ReturnPct:    4.20,
		RiskPct:      0.50,
		Stop:         "ATR×3",
		RR:           1.5,
		MaxDrawdown:  -380.00,
		AvgWinner:    52.50,
		AvgLoser:     -35.00,
	}
}

// --- PrintSummary ---

func TestPrintSummary_PositivePL(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	PrintSummary(&buf, minSummary())
	out := buf.String()

	assert.Contains(t, out, "TestStrategy")
	assert.Contains(t, out, "EURUSD")
	assert.Contains(t, out, "H1")
	assert.Contains(t, out, "2024-01-01")
	assert.Contains(t, out, "2024-12-31")
	assert.Contains(t, out, "Trades : 100")
	assert.Contains(t, out, "Wins: 60")
	assert.Contains(t, out, "Losses: 40")
	assert.Contains(t, out, "+$420.00")
	assert.Contains(t, out, "+4.20%")
	assert.Contains(t, out, "-$380.00") // drawdown
	assert.Contains(t, out, "ATR×3")
	assert.Contains(t, out, "1.5")    // RR
	assert.Contains(t, out, "$52.50") // avg winner
}

func TestPrintSummary_NegativePL(t *testing.T) {
	t.Parallel()

	s := minSummary()
	s.NetPL = -300.00
	s.ReturnPct = -3.00

	var buf bytes.Buffer
	PrintSummary(&buf, s)
	out := buf.String()

	assert.Contains(t, out, "-$300.00")
	assert.Contains(t, out, "-3.00%")
}

func TestPrintSummary_NoDrawdown(t *testing.T) {
	t.Parallel()

	s := minSummary()
	s.MaxDrawdown = 0

	var buf bytes.Buffer
	PrintSummary(&buf, s)
	assert.Contains(t, buf.String(), "Drawdown: —")
}

func TestPrintSummary_NoRR(t *testing.T) {
	t.Parallel()

	s := minSummary()
	s.RR = 0

	var buf bytes.Buffer
	PrintSummary(&buf, s)
	assert.Contains(t, buf.String(), "RR: —")
}

func TestPrintSummary_NoStop(t *testing.T) {
	t.Parallel()

	s := minSummary()
	s.Stop = ""

	var buf bytes.Buffer
	PrintSummary(&buf, s)
	assert.Contains(t, buf.String(), "Stop: —")
}

func TestPrintSummary_WithRegimeAndMaxSpread(t *testing.T) {
	t.Parallel()

	s := minSummary()
	s.Regime = "weekly-ema"
	s.MaxSpread = "2.0p"

	var buf bytes.Buffer
	PrintSummary(&buf, s)
	out := buf.String()

	assert.Contains(t, out, "Regime: weekly-ema")
	assert.Contains(t, out, "MaxSpread: 2.0p")
}

func TestPrintSummary_WithSpreadStats(t *testing.T) {
	t.Parallel()

	s := minSummary()
	s.AvgSpreadPips = 1.23
	s.SpreadFiltered = 7
	s.Slippage = "0.5p"

	var buf bytes.Buffer
	PrintSummary(&buf, s)
	out := buf.String()

	assert.Contains(t, out, "AvgSpread: 1.23p")
	assert.Contains(t, out, "Slip: 0.5p")
	assert.Contains(t, out, "Filtered: 7")
}

func TestPrintSummary_DateTruncation(t *testing.T) {
	t.Parallel()

	s := minSummary()
	s.Start = "2024-01-01T00:00:00Z" // longer than 10 chars
	s.End = "2024-12-31T23:59:59Z"

	var buf bytes.Buffer
	PrintSummary(&buf, s)
	out := buf.String()

	assert.Contains(t, out, "2024-01-01")
	assert.Contains(t, out, "2024-12-31")
	assert.NotContains(t, out, "T00:00:00Z")
}

// --- WriteOrgReport ---

func TestWriteOrgReport_Structure(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	WriteOrgReport(&buf, minSummary())
	out := buf.String()

	assert.Contains(t, out, "#+TITLE: Backtest:")
	assert.Contains(t, out, "#+DATE:")
	assert.Contains(t, out, "TestStrategy")
	assert.Contains(t, out, "EURUSD")
	assert.Contains(t, out, ":PROPERTIES:")
	assert.Contains(t, out, ":END:")
	assert.Contains(t, out, "** Summary")
}

func TestWriteOrgReport_NoTradesNoMonthlySection(t *testing.T) {
	t.Parallel()

	s := minSummary()
	s.TradeDetails = nil

	var buf bytes.Buffer
	WriteOrgReport(&buf, s)
	out := buf.String()

	assert.NotContains(t, out, "** Monthly Breakdown")
	assert.NotContains(t, out, "** Trades")
}

func TestWriteOrgReport_WithTradesIncludesMonthlyAndTradeTable(t *testing.T) {
	t.Parallel()

	s := minSummary()
	s.TradeDetails = []BacktestReportTrade{
		{
			ID: "1", Instrument: "EURUSD", Side: "long",
			Units: 10_000, OpenPrice: 1.1000, ClosePrice: 1.1050,
			OpenTime: "2024-03-15T10:00:00Z", CloseTime: "2024-03-15T14:00:00Z",
			PNL: 50.00,
		},
		{
			ID: "2", Instrument: "EURUSD", Side: "short",
			Units: 10_000, OpenPrice: 1.1050, ClosePrice: 1.1080,
			OpenTime: "2024-04-02T09:00:00Z", CloseTime: "2024-04-02T11:00:00Z",
			PNL: -30.00,
		},
	}

	var buf bytes.Buffer
	WriteOrgReport(&buf, s)
	out := buf.String()

	assert.Contains(t, out, "** Monthly Breakdown")
	assert.Contains(t, out, "** Trades")
	assert.Contains(t, out, "2024-03")
	assert.Contains(t, out, "2024-04")
	assert.Contains(t, out, "Long")
	assert.Contains(t, out, "Short")
}

func TestWriteOrgReport_PropertiesContainKeyFields(t *testing.T) {
	t.Parallel()

	s := minSummary()
	s.Regime = "atr-percentile"

	var buf bytes.Buffer
	WriteOrgReport(&buf, s)
	out := buf.String()

	assert.Contains(t, out, ":strategy:")
	assert.Contains(t, out, ":instrument:")
	assert.Contains(t, out, ":net_pl:")
	assert.Contains(t, out, ":regime:         atr-percentile")
}

// --- WriteOrgIndex ---

func TestWriteOrgIndex_Header(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	WriteOrgIndex(&buf, nil)
	out := buf.String()

	assert.Contains(t, out, "#+TITLE: Backtest Index")
	assert.Contains(t, out, "#+DATE:")
	assert.Contains(t, out, "* Results")
}

func TestWriteOrgIndex_MultipleRows(t *testing.T) {
	t.Parallel()

	summaries := []BacktestReportSummary{
		{
			Name: "run-a", Strategy: "EMA", Instrument: "EURUSD", Timeframe: "h1",
			Start: "2024-01-01", End: "2024-12-31",
			Trades: 50, WinRate: 55.0, NetPL: 200.0, ReturnPct: 2.0,
			MaxDrawdown: -150.0, RR: 1.8, Stop: "ATR×2",
		},
		{
			Name: "run-b", Strategy: "Donchian", Instrument: "GBPUSD", Timeframe: "h4",
			Start: "2023-01-01", End: "2023-12-31",
			Trades: 30, WinRate: 40.0, NetPL: -100.0, ReturnPct: -1.0,
			MaxDrawdown: 0, RR: 0, Stop: "fixed",
		},
	}

	var buf bytes.Buffer
	WriteOrgIndex(&buf, summaries)
	out := buf.String()

	assert.Contains(t, out, "run-a")
	assert.Contains(t, out, "EMA")
	assert.Contains(t, out, "EURUSD")
	assert.Contains(t, out, "H1")
	assert.Contains(t, out, "+200.00")
	assert.Contains(t, out, "-$150") // drawdown formatted
	assert.Contains(t, out, "1.80")  // RR formatted
	assert.Contains(t, out, "run-b")
	assert.Contains(t, out, "Donchian")
	assert.Contains(t, out, "GBPUSD")
	assert.Contains(t, out, "—") // RR and drawdown both "—" for run-b
}

// --- internal helpers ---

func TestShortDate(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "2024-01-15", shortDate("2024-01-15T10:00:00Z"))
	assert.Equal(t, "2024-01-15", shortDate("2024-01-15"))
	assert.Equal(t, "short", shortDate("short")) // under 10 chars, unchanged
	assert.Equal(t, "", shortDate(""))
}

func TestShortMonth(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "2024-03", shortMonth("2024-03-15T10:00:00Z"))
	assert.Equal(t, "2024-03", shortMonth("2024-03"))
	assert.Equal(t, "abc", shortMonth("abc")) // under 7 chars, unchanged
}

func TestShortDateTime(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "2024-03-15 10:30", shortDateTime("2024-03-15T10:30:00Z"))
	assert.Equal(t, "not-a-date", shortDateTime("not-a-date")) // invalid falls back to input
	assert.Equal(t, "", shortDateTime(""))
}

func TestTitleASCII(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Long", titleASCII("long"))
	assert.Equal(t, "Short", titleASCII("short"))
	assert.Equal(t, "A", titleASCII("a"))
	assert.Equal(t, "", titleASCII(""))
}

func TestOrgTable_Write(t *testing.T) {
	t.Parallel()

	tbl := newOrgTable("Name", "Value")
	tbl.setRight(1)
	tbl.addRow("alpha", "100")
	tbl.addRow("beta", "2000")

	var buf bytes.Buffer
	tbl.write(&buf, "  ")
	out := buf.String()

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	assert.Len(t, lines, 4) // header, separator, 2 data rows

	// Header row
	assert.Contains(t, lines[0], "| Name")
	assert.Contains(t, lines[0], "| Value")

	// Separator row
	assert.Contains(t, lines[1], "---")

	// Right-alignment: value column padded on left
	assert.Contains(t, lines[2], " 100 |")
	assert.Contains(t, lines[3], "2000 |")
}

func TestOrgTable_EmptyRows(t *testing.T) {
	t.Parallel()

	tbl := newOrgTable("Col1", "Col2")
	var buf bytes.Buffer
	tbl.write(&buf, "")
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	assert.Len(t, lines, 2) // header + separator only
}
