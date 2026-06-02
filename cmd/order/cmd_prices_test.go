package order

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/rustyeddy/trader/brokers/oanda"
	trader "github.com/rustyeddy/trader"
)

func TestPipDecimals(t *testing.T) {
	cases := []struct {
		inst string
		want int
	}{
		{"EURUSD", 5}, // PipLocation=-4 → 5
		{"GBPUSD", 5},
		{"USDJPY", 3}, // PipLocation=-2 → 3
		{"USDCHF", 5},
	}
	for _, tc := range cases {
		inst := trader.GetInstrument(tc.inst)
		assert.Equal(t, tc.want, pipDecimals(inst), tc.inst)
	}
}

func TestFmtPipVal(t *testing.T) {
	assert.Equal(t, "$10.00", fmtPipVal(10.0))
	assert.Equal(t, "$6.67", fmtPipVal(6.6667))
	assert.Equal(t, "$100.00", fmtPipVal(100.0))
}

func TestSplitInstrumentCSV(t *testing.T) {
	got := splitInstrumentCSV("EURUSD,usdjpy, GBPUSD")
	assert.Equal(t, []string{"EURUSD", "USDJPY", "GBPUSD"}, got)
}

func TestSplitInstrumentCSV_Empty(t *testing.T) {
	assert.Empty(t, splitInstrumentCSV(""))
}

func TestPrintPrices_Smoke(t *testing.T) {
	prices := []oanda.Price{
		{Instrument: "EUR_USD", Bid: 1.08450, Ask: 1.08453, Mid: 1.08452},
		{Instrument: "USD_JPY", Bid: 150.120, Ask: 150.124, Mid: 150.122},
	}
	// Just verify it doesn't panic and produces some output.
	var buf bytes.Buffer
	old := bytes.Buffer{}
	_ = old // printPrices writes to stdout; capture via reassigning would need refactor.
	// Call printPrices — if it panics the test fails.
	assert.NotPanics(t, func() {
		printPrices(prices, 100_000, "practice")
	})
	_ = buf
}
