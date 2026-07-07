package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/datamanager"
	"github.com/rustyeddy/trader/market"
	_ "github.com/rustyeddy/trader/strategies/fake"
	_ "github.com/rustyeddy/trader/strategies/noop"
	"github.com/rustyeddy/trader/strategy"
)

// seedReplayStore writes two months of synthetic H1 EURUSD candles into a
// temp store.
func seedReplayStore(t *testing.T) {
	t.Helper()
	datamanager.UseTempDataDir(t)

	base := market.Price(110000) // 1.10000
	makeMonth := func(_ int, _ time.Month, rows int) []market.Candle {
		candles := make([]market.Candle, rows)
		for i := range candles {
			p := base + market.Price(i*10)
			candles[i] = market.Candle{
				Open: p, High: p + 500, Low: p - 500, Close: p + 100,
			}
		}
		return candles
	}

	// Write Jan + Feb 2024 (744 + 696 H1 slots).
	datamanager.WriteCandles(t, "oanda", "EURUSD", market.H1,
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), makeMonth(2024, 1, 744))
	datamanager.WriteCandles(t, "oanda", "EURUSD", market.H1,
		time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), makeMonth(2024, 2, 696))
}

func TestRunReplay_ReturnsBarsAndSignals(t *testing.T) {
	seedReplayStore(t)

	svc := &Service{}
	result, err := svc.RunReplay(context.Background(), ReplayRequest{
		Instrument: "EURUSD",
		Timeframe:  "H1",
		From:       "2024-02-01",
		To:         "2024-02-29",
		WarmupBars: 50,
		Strategy:   strategy.StrategyConfig{Kind: "fake"},
		Exit:       strategy.ExitConfig{},
		Regime:     strategy.RegimeConfig{},
	})
	require.NoError(t, err)
	assert.Equal(t, "EURUSD", result.Instrument)
	assert.Equal(t, "H1", result.Timeframe)
	assert.NotEmpty(t, result.Bars, "should return bars for the requested range")
}

func TestRunReplay_MissingInstrumentErrors(t *testing.T) {
	svc := &Service{}
	_, err := svc.RunReplay(context.Background(), ReplayRequest{
		From: "2024-01-01", To: "2024-01-31",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "instrument")
}

func TestRunReplay_BadDateErrors(t *testing.T) {
	svc := &Service{}
	_, err := svc.RunReplay(context.Background(), ReplayRequest{
		Instrument: "EURUSD",
		Timeframe:  "H1",
		From:       "not-a-date",
		To:         "2024-01-31",
	})
	require.Error(t, err)
}

func TestRunReplay_EmptyStoreReturnsNoBars(t *testing.T) {
	datamanager.UseTempDataDir(t)

	svc := &Service{}
	result, err := svc.RunReplay(context.Background(), ReplayRequest{
		Instrument: "EURUSD",
		Timeframe:  "H1",
		From:       "2024-01-01",
		To:         "2024-01-31",
		WarmupBars: 10,
		Strategy:   strategy.StrategyConfig{Kind: "noop"},
	})
	require.NoError(t, err)
	assert.Empty(t, result.Bars)
	assert.Empty(t, result.Signals)
}

func TestSignalKindConstants(t *testing.T) {
	// Ensure the constants haven't drifted — the UI depends on these strings.
	assert.Equal(t, SignalKind("open"), SignalOpen)
	assert.Equal(t, SignalKind("close"), SignalClose)
	assert.Equal(t, SignalKind("blocked"), SignalBlocked)
	assert.Equal(t, SignalKind("no_stop"), SignalNoStop)
	assert.Equal(t, SignalKind("stop_update"), SignalStopUpdate)
}
