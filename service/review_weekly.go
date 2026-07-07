package service

import (
	"time"

	"github.com/rustyeddy/trader/market"
)

// dayRollShift nudges a daily candle's evening-UTC open timestamp (OANDA
// rolls the trading day at 21:00/22:00 UTC, before the calendar date it
// represents) across midnight so ISO-week bucketing groups it with the
// weekday candles that follow it rather than the prior ISO week.
const dayRollShift = 4 * time.Hour

// deriveWeeklyCandles aggregates a series of daily candles into weekly OHLC
// bars — open of the week's first daily candle, high/low across the week,
// close of the week's last daily candle — grouped by ISO week (Monday to
// Sunday). This is an exact rollup, not an approximation, as long as daily
// has no gaps within a week and the grouping boundary matches OANDA's own
// week boundary.
//
// The most recent bucket is dropped whenever it is still the current,
// in-progress week, mirroring the "!c.Complete" filter OANDA-native fetches
// apply so review never scores a still-forming week. Returns at most the
// most recent count complete weeks, oldest first.
func deriveWeeklyCandles(daily []market.CandleTime, count int) []market.Candle {
	if len(daily) == 0 || count <= 0 {
		return nil
	}

	type bucket struct {
		year, week int
		candle     market.Candle
	}

	var weeks []bucket
	for _, ct := range daily {
		if ct.Candle.IsZero() {
			continue
		}
		t := time.Unix(int64(ct.Timestamp), 0).UTC().Add(dayRollShift)
		year, week := t.ISOWeek()

		if n := len(weeks); n > 0 && weeks[n-1].year == year && weeks[n-1].week == week {
			b := &weeks[n-1]
			if ct.Candle.High > b.candle.High {
				b.candle.High = ct.Candle.High
			}
			if ct.Candle.Low < b.candle.Low {
				b.candle.Low = ct.Candle.Low
			}
			b.candle.Close = ct.Candle.Close
			b.candle.Ticks += ct.Candle.Ticks
			continue
		}

		weeks = append(weeks, bucket{year: year, week: week, candle: ct.Candle})
	}
	if len(weeks) == 0 {
		return nil
	}

	nowYear, nowWeek := time.Now().UTC().Add(dayRollShift).ISOWeek()
	if last := weeks[len(weeks)-1]; last.year == nowYear && last.week == nowWeek {
		weeks = weeks[:len(weeks)-1]
	}

	if len(weeks) > count {
		weeks = weeks[len(weeks)-count:]
	}

	out := make([]market.Candle, len(weeks))
	for i, b := range weeks {
		out[i] = b.candle
	}
	return out
}
