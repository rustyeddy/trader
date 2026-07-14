package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/service"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedMCPCandleStore seeds a temp store with January 2024 H1 EURUSD candles.
func seedMCPCandleStore(t *testing.T) {
	t.Helper()
	candles := make([]market.Candle, 744)
	candles[0] = market.Candle{Open: 110000, High: 110100, Low: 109900, Close: 110050, AvgSpread: 10, MaxSpread: 15, Ticks: 60}
	candles[1] = market.Candle{Open: 110050, High: 110200, Low: 110000, Close: 110150, AvgSpread: 11, MaxSpread: 16, Ticks: 55}
	datamanager.SeedCandles(t, "oanda", "EURUSD", types.H1, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), candles)
}

// ── get_candle_stats ──────────────────────────────────────────────────────

func TestToolGetDataStats_ReturnsAnalyzers(t *testing.T) {
	seedMCPCandleStore(t)

	srv := New(&service.Service{Log: slog.Default()}, false)
	raw, err := json.Marshal(map[string]any{
		"instrument": "EURUSD",
		"timeframe":  "H1",
		"from":       "2024-01-01",
		"to":         "2024-01-31",
	})
	require.NoError(t, err)

	got, rpcErr := srv.toolGetDataStats(context.Background(), raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	text := content[0]["text"].(string)
	assert.Contains(t, text, `"analyzers"`)
	assert.Contains(t, text, "EURUSD")
}

func TestToolGetDataStats_DefaultsTimeframeToH1(t *testing.T) {
	seedMCPCandleStore(t)

	srv := New(&service.Service{Log: slog.Default()}, false)
	raw, err := json.Marshal(map[string]any{
		"instrument": "EURUSD",
		"from":       "2024-01-01",
		"to":         "2024-01-31",
	})
	require.NoError(t, err)

	got, rpcErr := srv.toolGetDataStats(context.Background(), raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	assert.Contains(t, content[0]["text"].(string), `"H1"`)
}

func TestToolGetDataStats_RequiresInstrumentFromTo(t *testing.T) {
	srv := New(&service.Service{Log: slog.Default()}, false)
	raw, _ := json.Marshal(map[string]any{"instrument": "EURUSD"})

	_, rpcErr := srv.toolGetDataStats(context.Background(), raw)
	require.NotNil(t, rpcErr)
}

func TestHandleToolsCall_AllowsGetDataStatsWithoutOANDA(t *testing.T) {
	seedMCPCandleStore(t)

	srv := New(&service.Service{Log: slog.Default()}, false)
	raw, err := json.Marshal(map[string]any{
		"name": "get_candle_stats",
		"arguments": map[string]any{
			"instrument": "EURUSD",
			"from":       "2024-01-01",
			"to":         "2024-01-31",
		},
	})
	require.NoError(t, err)

	got, rpcErr := srv.handleToolsCall(context.Background(), raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	assert.Contains(t, content[0]["text"].(string), `"analyzers"`)
}

// ── get_pip_values ────────────────────────────────────────────────────────

func TestToolGetPipValues_ReturnsMajorPairs(t *testing.T) {
	srv := New(&service.Service{Log: slog.Default()}, false)
	raw, err := json.Marshal(map[string]any{})
	require.NoError(t, err)

	got, rpcErr := srv.toolGetPipValues(context.Background(), raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	text := content[0]["text"].(string)
	assert.Contains(t, text, `"rows"`)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(text), &result))
	rows := result["rows"].([]any)
	assert.Len(t, rows, 7)
}

func TestToolGetPipValues_ScopesToInstruments(t *testing.T) {
	srv := New(&service.Service{Log: slog.Default()}, false)
	raw, err := json.Marshal(map[string]any{
		"instruments": []string{"EURUSD", "USDJPY"},
	})
	require.NoError(t, err)

	got, rpcErr := srv.toolGetPipValues(context.Background(), raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(content[0]["text"].(string)), &result))
	rows := result["rows"].([]any)
	assert.Len(t, rows, 2)
}

func TestHandleToolsCall_AllowsGetPipValuesWithoutOANDA(t *testing.T) {
	srv := New(&service.Service{Log: slog.Default()}, false)
	raw, err := json.Marshal(map[string]any{
		"name":      "get_pip_values",
		"arguments": map[string]any{},
	})
	require.NoError(t, err)

	got, rpcErr := srv.handleToolsCall(context.Background(), raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	assert.Contains(t, content[0]["text"].(string), `"rows"`)
}

// ── get_position ──────────────────────────────────────────────────────────

func TestToolGetPosition_ReturnsTable(t *testing.T) {
	srv := New(&service.Service{Log: slog.Default()}, false)
	raw, err := json.Marshal(map[string]any{
		"instrument": "EURUSD",
		"price":      1.08,
	})
	require.NoError(t, err)

	got, rpcErr := srv.toolGetPosition(context.Background(), raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(content[0]["text"].(string)), &result))
	rows := result["rows"].([]any)
	assert.Len(t, rows, 3) // micro / mini / standard
}

func TestHandleToolsCall_AllowsGetPositionWithoutOANDA(t *testing.T) {
	srv := New(&service.Service{Log: slog.Default()}, false)
	raw, err := json.Marshal(map[string]any{
		"name": "get_position",
		"arguments": map[string]any{
			"instrument": "EURUSD",
			"price":      1.08,
		},
	})
	require.NoError(t, err)

	got, rpcErr := srv.handleToolsCall(context.Background(), raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	assert.Contains(t, content[0]["text"].(string), `"rows"`)
}

// ── validate_candles ──────────────────────────────────────────────────────

func TestToolValidateCandles_ReturnsReport(t *testing.T) {
	seedMCPCandleStore(t)

	srv := New(&service.Service{Log: slog.Default()}, false)
	raw, err := json.Marshal(map[string]any{
		"instruments": []string{"EURUSD"},
		"from":        "2024-01",
		"to":          "2024-01",
	})
	require.NoError(t, err)

	got, rpcErr := srv.toolValidateCandles(context.Background(), raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	text := content[0]["text"].(string)
	assert.Contains(t, text, `"source"`)
}

func TestToolValidateCandles_RequiresInstrumentsFromTo(t *testing.T) {
	srv := New(&service.Service{Log: slog.Default()}, false)
	raw, _ := json.Marshal(map[string]any{"instruments": []string{"EURUSD"}})

	_, rpcErr := srv.toolValidateCandles(context.Background(), raw)
	require.NotNil(t, rpcErr)
}

func TestHandleToolsCall_AllowsValidateCandlesWithoutOANDA(t *testing.T) {
	seedMCPCandleStore(t)

	srv := New(&service.Service{Log: slog.Default()}, false)
	raw, err := json.Marshal(map[string]any{
		"name": "validate_candles",
		"arguments": map[string]any{
			"instruments": []string{"EURUSD"},
			"from":        "2024-01",
			"to":          "2024-01",
		},
	})
	require.NoError(t, err)

	got, rpcErr := srv.handleToolsCall(context.Background(), raw)
	require.Nil(t, rpcErr)
	payload := got.(map[string]any)
	content := payload["content"].([]map[string]any)
	assert.Contains(t, content[0]["text"].(string), `"source"`)
}
