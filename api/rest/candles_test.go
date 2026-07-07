package rest

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleGetCandlesCSV(t *testing.T) {
	candles := make([]market.Candle, 744)
	candles[0] = market.Candle{Open: 110000, High: 110100, Low: 109900, Close: 110050, AvgSpread: 10, MaxSpread: 15, Ticks: 60}
	datamanager.SeedCandles(t, "oanda", "EURUSD", market.H1, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), candles)

	srv := New(&service.Service{}, "")
	rr := do(t, srv.Handler(), "GET", "/api/v1/candles/EUR_USD?from=2024-01-01&to=2024-01-01&timeframe=H1")

	require.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, strings.HasPrefix(rr.Header().Get("Content-Type"), "text/csv"))
	assert.Equal(t, "1", rr.Header().Get("X-Candle-Count"))
	assert.Contains(t, rr.Body.String(), "Timestamp,High,Open,Low,Close,avgspread,maxspread,ticks,flags\n")
	assert.Contains(t, rr.Body.String(), "1704067200,110100,110000,109900,110050,10,15,60,0x0001\n")
}
