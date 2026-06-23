package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildCandleCSVStore(t *testing.T) func() {
	t.Helper()
	store := marketdata.NewStoreAt(t.TempDir())
	candles := make([]trader.Candle, 744)
	candles[0] = trader.Candle{Open: 110000, High: 110100, Low: 109900, Close: 110050, AvgSpread: 10, MaxSpread: 15, Ticks: 60}
	candles[1] = trader.Candle{Open: 110050, High: 110200, Low: 110000, Close: 110150, AvgSpread: 11, MaxSpread: 16, Ticks: 55}
	require.NoError(t, store.WriteMonthlyCandles("oanda", "EURUSD", trader.H1,
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), candles))
	return marketdata.SwapStore(store)
}

func TestCandlesCSV_ReturnsCanonicalCSV(t *testing.T) {
	restore := buildCandleCSVStore(t)
	defer restore()

	result, err := (&Service{}).CandlesCSV(context.Background(), CandlesCSVRequest{
		Instrument: "EUR_USD",
		Timeframe:  "H1",
		From:       "2024-01-01",
		To:         "2024-01-01",
	})
	require.NoError(t, err)

	assert.Equal(t, "EURUSD", result.Instrument)
	assert.Equal(t, "h1", result.Timeframe)
	assert.Equal(t, "oanda", result.Source)
	assert.Equal(t, 2, result.Count)
	assert.Contains(t, result.CSV, "# schema=v1 source=oanda instrument=EURUSD tf=h1 scale=100000\n")
	assert.Contains(t, result.CSV, "Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n")
	assert.Contains(t, result.CSV, "1704067200,110100,110000,109900,110050,10,15,60,0x0001\n")
	assert.True(t, strings.HasSuffix(result.CSV, "\n"))
}

func TestCandlesCSV_DefaultsToNowWhenToOmitted(t *testing.T) {
	restore := buildCandleCSVStore(t)
	defer restore()

	result, err := (&Service{}).CandlesCSV(context.Background(), CandlesCSVRequest{
		Instrument: "EURUSD",
		Timeframe:  "H1",
		From:       "2024-01-01",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result.To)
	assert.Equal(t, 2, result.Count)
}
