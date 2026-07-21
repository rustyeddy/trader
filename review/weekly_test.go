package review

import (
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dailyAt builds a synthetic D1 candle opening at the OANDA-style
// 21:00 UTC day-roll for the given UTC calendar date, with the given close
// price used for open/high/low/close so week-aggregation math is easy to
// hand-verify.
func dailyAt(date time.Time, price types.Price) market.Candle {
	open := time.Date(date.Year(), date.Month(), date.Day()-1, 21, 0, 0, 0, time.UTC)
	return market.Candle{Open: price, High: price, Low: price, Close: price, Ticks: 1, Timestamp: types.FromTime(open)}
}

// TestDeriveWeeklyCandles_AggregatesOneCompleteWeek verifies the OHLC
// rollup: open of the week's first daily bar, high/low across the week,
// close of the week's last daily bar — for a single Mon-Fri week, with an
// older, definitely-complete week so the "drop the in-progress week" guard
// doesn't also eat the week under test.
func TestDeriveWeeklyCandles_AggregatesOneCompleteWeek(t *testing.T) {
	// An arbitrary complete week far in the past: Mon 2024-01-08 .. Fri 2024-01-12.
	mon := time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC)
	daily := []market.Candle{
		dailyAt(mon, types.PriceFromFloat(1.10)),
		dailyAt(mon.AddDate(0, 0, 1), types.PriceFromFloat(1.12)),
		dailyAt(mon.AddDate(0, 0, 2), types.PriceFromFloat(1.08)), // week low
		dailyAt(mon.AddDate(0, 0, 3), types.PriceFromFloat(1.13)), // week high
		dailyAt(mon.AddDate(0, 0, 4), types.PriceFromFloat(1.11)), // week close
	}

	weeks := DeriveWeeklyCandles(daily, 10)
	require.Len(t, weeks, 1)
	w := weeks[0]
	assert.Equal(t, types.PriceFromFloat(1.10), w.Open, "open must be the week's first daily open")
	assert.Equal(t, types.PriceFromFloat(1.13), w.High, "high must be the max across the week")
	assert.Equal(t, types.PriceFromFloat(1.08), w.Low, "low must be the min across the week")
	assert.Equal(t, types.PriceFromFloat(1.11), w.Close, "close must be the week's last daily close")
	assert.EqualValues(t, 5, w.Ticks, "ticks must sum across the week's daily bars")
}

// TestDeriveWeeklyCandles_DropsInProgressWeek verifies the current,
// still-forming week is excluded, mirroring the !Complete filter used for
// direct OANDA fetches.
func TestDeriveWeeklyCandles_DropsInProgressWeek(t *testing.T) {
	now := time.Now().UTC()
	daily := []market.Candle{
		dailyAt(now.AddDate(0, 0, -21), types.PriceFromFloat(1.10)), // complete week, 3 weeks ago
		dailyAt(now, types.PriceFromFloat(1.20)),                    // today: current in-progress week
	}

	weeks := DeriveWeeklyCandles(daily, 10)
	require.Len(t, weeks, 1, "the in-progress week must be dropped")
	assert.Equal(t, types.PriceFromFloat(1.10), weeks[0].Close)
}

// TestDeriveWeeklyCandles_TrimsToCount verifies only the most recent count
// complete weeks are returned, oldest first.
func TestDeriveWeeklyCandles_TrimsToCount(t *testing.T) {
	mon := time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC)
	var daily []market.Candle
	for w := range 5 {
		daily = append(daily, dailyAt(mon.AddDate(0, 0, 7*w), types.PriceFromFloat(1.10+float64(w)*0.01)))
	}

	weeks := DeriveWeeklyCandles(daily, 2)
	require.Len(t, weeks, 2)
	assert.Equal(t, types.PriceFromFloat(1.13), weeks[0].Open, "must keep the most recent weeks, oldest first")
	assert.Equal(t, types.PriceFromFloat(1.14), weeks[1].Open)
}

func TestDeriveWeeklyCandles_EmptyInput(t *testing.T) {
	assert.Nil(t, DeriveWeeklyCandles(nil, 10))
	assert.Nil(t, DeriveWeeklyCandles([]market.Candle{dailyAt(time.Now().UTC(), types.PriceFromFloat(1.1))}, 0))
}

// TestDeriveWeeklyCandlesAsOf_DropsPartialTrailingWeekForHistoricalAsOf is a
// regression test: DeriveWeeklyCandles compares its trailing bucket against
// the real time.Now(), which never matches a past asOf's ISO week, so it
// would never drop a historical sweep's partial trailing week (asOf
// falling midweek) — silently reporting 3 days of a 5-day week as if it
// were the complete week's OHLC. DeriveWeeklyCandlesAsOf must use asOf
// instead, so this partial week is dropped exactly like a live in-progress
// week would be.
func TestDeriveWeeklyCandlesAsOf_DropsPartialTrailingWeekForHistoricalAsOf(t *testing.T) {
	mon := time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC) // Mon 2024-01-08 .. Fri 2024-01-12
	daily := []market.Candle{
		dailyAt(mon.AddDate(0, 0, -21), types.PriceFromFloat(1.10)), // complete week, 3 weeks earlier
		dailyAt(mon, types.PriceFromFloat(1.20)),                    // Mon: only 1 of 5 days of this week
		dailyAt(mon.AddDate(0, 0, 1), types.PriceFromFloat(1.21)),   // Tue
		dailyAt(mon.AddDate(0, 0, 2), types.PriceFromFloat(1.22)),   // Wed: asOf falls here, midweek
	}
	asOf := mon.AddDate(0, 0, 2).Add(12 * time.Hour) // Wed midday: Mon-Wed exist, Thu/Fri don't yet

	weeks := DeriveWeeklyCandlesAsOf(daily, 10, asOf)
	require.Len(t, weeks, 1, "the partial Mon-Wed trailing week must be dropped, not reported as a complete week")
	assert.Equal(t, types.PriceFromFloat(1.10), weeks[0].Close)
}
