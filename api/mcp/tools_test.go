package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolGetCandlesCSV(t *testing.T) {
	store := trader.NewStoreAt(t.TempDir())
	candles := make([]trader.Candle, 744)
	candles[0] = trader.Candle{Open: 110000, High: 110100, Low: 109900, Close: 110050, AvgSpread: 10, MaxSpread: 15, Ticks: 60}
	require.NoError(t, store.WriteMonthlyCandles("oanda", "EURUSD", trader.H1,
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), candles))
	restore := trader.SwapStore(store)
	defer restore()

	srv := New(&service.Service{Log: slog.Default()}, false)
	raw, err := json.Marshal(map[string]any{
		"instrument": "EUR_USD",
		"timeframe":  "H1",
		"from":       "2024-01-01",
		"to":         "2024-01-01",
	})
	require.NoError(t, err)

	got, rpcErr := srv.toolGetCandlesCSV(context.Background(), raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	text := content[0]["text"].(string)
	assert.Contains(t, text, "Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n")
	assert.Contains(t, text, "1704067200,110100,110000,109900,110050,10,15,60,0x0001\n")
	metadata := payload["metadata"].(map[string]any)
	assert.Equal(t, 1, metadata["count"])
	assert.Equal(t, "text/csv", metadata["mime_type"])
}

func TestHandleToolsCall_AllowsGetCandlesCSVWithoutOANDA(t *testing.T) {
	store := trader.NewStoreAt(t.TempDir())
	candles := make([]trader.Candle, 744)
	candles[0] = trader.Candle{Open: 110000, High: 110100, Low: 109900, Close: 110050, AvgSpread: 10, MaxSpread: 15, Ticks: 60}
	require.NoError(t, store.WriteMonthlyCandles("oanda", "EURUSD", trader.H1,
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), candles))
	restore := trader.SwapStore(store)
	defer restore()

	srv := New(&service.Service{Log: slog.Default()}, false)
	raw, err := json.Marshal(map[string]any{
		"name": "get_candles_csv",
		"arguments": map[string]any{
			"instrument": "EUR_USD",
			"timeframe":  "H1",
			"from":       "2024-01-01",
			"to":         "2024-01-01",
		},
	})
	require.NoError(t, err)

	got, rpcErr := srv.handleToolsCall(context.Background(), raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	assert.Contains(t, content[0]["text"].(string), "1704067200,110100,110000,109900,110050,10,15,60,0x0001\n")
}
