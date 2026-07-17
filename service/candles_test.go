package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedCandleCSVStore(t *testing.T) {
	t.Helper()
	candles := make([]market.Candle, 744)
	candles[0] = market.Candle{Open: 110000, High: 110100, Low: 109900, Close: 110050, AvgSpread: 10, MaxSpread: 15, Ticks: 60}
	candles[1] = market.Candle{Open: 110050, High: 110200, Low: 110000, Close: 110150, AvgSpread: 11, MaxSpread: 16, Ticks: 55}
	datamanager.SeedCandles(t, "oanda", "EURUSD", types.H1, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), candles)
}

func TestCandlesCSV_ReturnsCanonicalCSV(t *testing.T) {
	seedCandleCSVStore(t)

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
	assert.Contains(t, result.CSV, "# schema=candle-v2 source=oanda instrument=EURUSD tf=h1 scale=100000\n")
	assert.Contains(t, result.CSV, "Timestamp,Open,High,Low,Close,avgspread,maxspread,ticks,flags\n")
	assert.Contains(t, result.CSV, "1704067200,110000,110100,109900,110050,10,15,60,0x0001\n")
	assert.True(t, strings.HasSuffix(result.CSV, "\n"))
}

func TestCandlesCSV_DefaultsToNowWhenToOmitted(t *testing.T) {
	seedCandleCSVStore(t)

	result, err := (&Service{}).CandlesCSV(context.Background(), CandlesCSVRequest{
		Instrument: "EURUSD",
		Timeframe:  "H1",
		From:       "2024-01-01",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result.To)
	assert.Equal(t, 2, result.Count)
}
