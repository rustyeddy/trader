package service

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dailyAt builds a synthetic D1 candle opening at the OANDA-style
// 21:00 UTC day-roll for the given UTC calendar date, with the given close
// price used for open/high/low/close so week-aggregation math is easy to
// hand-verify.
func dailyAt(date time.Time, price market.Price) market.CandleTime {
	open := time.Date(date.Year(), date.Month(), date.Day()-1, 21, 0, 0, 0, time.UTC)
	return market.CandleTime{
		Candle:    market.Candle{Open: price, High: price, Low: price, Close: price, Ticks: 1},
		Timestamp: market.FromTime(open),
	}
}

// TestDeriveWeeklyCandles_AggregatesOneCompleteWeek verifies the OHLC
// rollup: open of the week's first daily bar, high/low across the week,
// close of the week's last daily bar — for a single Mon-Fri week, with an
// older, definitely-complete week so the "drop the in-progress week" guard
// doesn't also eat the week under test.
func TestDeriveWeeklyCandles_AggregatesOneCompleteWeek(t *testing.T) {
	// An arbitrary complete week far in the past: Mon 2024-01-08 .. Fri 2024-01-12.
	mon := time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC)
	daily := []market.CandleTime{
		dailyAt(mon, market.PriceFromFloat(1.10)),
		dailyAt(mon.AddDate(0, 0, 1), market.PriceFromFloat(1.12)),
		dailyAt(mon.AddDate(0, 0, 2), market.PriceFromFloat(1.08)), // week low
		dailyAt(mon.AddDate(0, 0, 3), market.PriceFromFloat(1.13)), // week high
		dailyAt(mon.AddDate(0, 0, 4), market.PriceFromFloat(1.11)), // week close
	}

	weeks := deriveWeeklyCandles(daily, 10)
	require.Len(t, weeks, 1)
	w := weeks[0]
	assert.Equal(t, market.PriceFromFloat(1.10), w.Open, "open must be the week's first daily open")
	assert.Equal(t, market.PriceFromFloat(1.13), w.High, "high must be the max across the week")
	assert.Equal(t, market.PriceFromFloat(1.08), w.Low, "low must be the min across the week")
	assert.Equal(t, market.PriceFromFloat(1.11), w.Close, "close must be the week's last daily close")
	assert.EqualValues(t, 5, w.Ticks, "ticks must sum across the week's daily bars")
}

// TestDeriveWeeklyCandles_DropsInProgressWeek verifies the current,
// still-forming week is excluded, mirroring the !Complete filter used for
// direct OANDA fetches.
func TestDeriveWeeklyCandles_DropsInProgressWeek(t *testing.T) {
	now := time.Now().UTC()
	daily := []market.CandleTime{
		dailyAt(now.AddDate(0, 0, -21), market.PriceFromFloat(1.10)), // complete week, 3 weeks ago
		dailyAt(now, market.PriceFromFloat(1.20)),                    // today: current in-progress week
	}

	weeks := deriveWeeklyCandles(daily, 10)
	require.Len(t, weeks, 1, "the in-progress week must be dropped")
	assert.Equal(t, market.PriceFromFloat(1.10), weeks[0].Close)
}

// TestDeriveWeeklyCandles_TrimsToCount verifies only the most recent count
// complete weeks are returned, oldest first.
func TestDeriveWeeklyCandles_TrimsToCount(t *testing.T) {
	mon := time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC)
	var daily []market.CandleTime
	for w := range 5 {
		daily = append(daily, dailyAt(mon.AddDate(0, 0, 7*w), market.PriceFromFloat(1.10+float64(w)*0.01)))
	}

	weeks := deriveWeeklyCandles(daily, 2)
	require.Len(t, weeks, 2)
	assert.Equal(t, market.PriceFromFloat(1.13), weeks[0].Open, "must keep the most recent weeks, oldest first")
	assert.Equal(t, market.PriceFromFloat(1.14), weeks[1].Open)
}

func TestDeriveWeeklyCandles_EmptyInput(t *testing.T) {
	assert.Nil(t, deriveWeeklyCandles(nil, 10))
	assert.Nil(t, deriveWeeklyCandles([]market.CandleTime{dailyAt(time.Now().UTC(), market.PriceFromFloat(1.1))}, 0))
}
