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

// ── bot tools ─────────────────────────────────────────────────────────────

func newBotSrv() *Server {
	return New(&service.Service{Log: slog.Default()}, true)
}

func TestToolListBots_Empty(t *testing.T) {
	srv := newBotSrv()
	got, rpcErr := srv.toolListBots()
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	assert.Contains(t, content[0]["text"].(string), `"count": 0`)
}

func TestToolGetBot_MissingID(t *testing.T) {
	srv := newBotSrv()
	raw, _ := json.Marshal(map[string]any{})
	_, rpcErr := srv.toolGetBot(raw)
	require.NotNil(t, rpcErr)
	assert.Contains(t, rpcErr.Message, "id is required")
}

func TestToolGetBot_NotFound(t *testing.T) {
	srv := newBotSrv()
	raw, _ := json.Marshal(map[string]any{"id": "bot-missing"})
	got, rpcErr := srv.toolGetBot(raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	assert.Equal(t, true, payload["isError"])
}

func TestToolStartBot_MissingFields(t *testing.T) {
	srv := newBotSrv()
	raw, _ := json.Marshal(map[string]any{"instrument": "EUR_USD"}) // missing strategy
	_, rpcErr := srv.toolStartBot(context.Background(), raw)
	require.NotNil(t, rpcErr)
	assert.Contains(t, rpcErr.Message, "strategy")
}

func TestToolStopBot_MissingID(t *testing.T) {
	srv := newBotSrv()
	raw, _ := json.Marshal(map[string]any{})
	_, rpcErr := srv.toolStopBot(raw)
	require.NotNil(t, rpcErr)
	assert.Contains(t, rpcErr.Message, "id is required")
}

func TestToolStopBot_NotFound(t *testing.T) {
	srv := newBotSrv()
	raw, _ := json.Marshal(map[string]any{"id": "bot-missing"})
	got, rpcErr := srv.toolStopBot(raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	assert.Equal(t, true, payload["isError"])
}

func TestListBotsAllowedWithoutOANDA(t *testing.T) {
	srv := New(&service.Service{Log: slog.Default()}, false) // no OANDA, no write
	raw, _ := json.Marshal(map[string]any{"name": "list_bots", "arguments": map[string]any{}})
	got, rpcErr := srv.handleToolsCall(context.Background(), raw)
	require.Nil(t, rpcErr)
	assert.NotNil(t, got)
}

func TestStartBotRequiresWriteEnable(t *testing.T) {
	srv := New(&service.Service{Log: slog.Default()}, false) // write disabled
	raw, _ := json.Marshal(map[string]any{
		"name":      "start_bot",
		"arguments": map[string]any{"instrument": "EUR_USD", "strategy": "noop"},
	})
	got, rpcErr := srv.handleToolsCall(context.Background(), raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	assert.Equal(t, true, payload["isError"])
}

func TestStartBotRequiresOANDA(t *testing.T) {
	srv := New(&service.Service{Log: slog.Default()}, true) // write enabled, no OANDA
	raw, _ := json.Marshal(map[string]any{
		"name":      "start_bot",
		"arguments": map[string]any{"instrument": "EUR_USD", "strategy": "noop"},
	})
	got, rpcErr := srv.handleToolsCall(context.Background(), raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	assert.Equal(t, true, payload["isError"])
	text := payload["content"].([]map[string]any)[0]["text"].(string)
	assert.Contains(t, text, "OANDA not configured")
}

// ── infra tools ───────────────────────────────────────────────────────────

func TestToolGetVersion(t *testing.T) {
	srv := New(&service.Service{Log: slog.Default()}, false)
	got, rpcErr := srv.toolGetVersion()
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	assert.Contains(t, content[0]["text"].(string), `"version"`)
}

func TestToolGetHealth(t *testing.T) {
	srv := New(&service.Service{Log: slog.Default()}, false)
	got, rpcErr := srv.toolGetHealth()
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	assert.Contains(t, content[0]["text"].(string), `"status": "ok"`)
}

func TestGetVersionAllowedWithoutOANDA(t *testing.T) {
	srv := New(&service.Service{Log: slog.Default()}, false)
	raw, _ := json.Marshal(map[string]any{"name": "get_version", "arguments": map[string]any{}})
	got, rpcErr := srv.handleToolsCall(context.Background(), raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	assert.NotEqual(t, true, payload["isError"])
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
