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
func deriveWeeklyCandles(daily []market.Candle, count int) []market.Candle {
	return deriveWeeklyCandlesAsOf(daily, count, time.Now())
}

// deriveWeeklyCandlesAsOf is deriveWeeklyCandles with an explicit reference
// time for "is the trailing bucket still in progress" instead of always
// comparing against the real wall-clock time.Now(). The historical sweep
// (review_sweep.go) must pass its step's asOf here: asOf is always in the
// past relative to time.Now(), so it would never match the real current
// ISO week, and the trailing-week check would never fire — silently
// treating a partial trailing week (asOf falling midweek) as if it were a
// complete week's OHLC.
func deriveWeeklyCandlesAsOf(daily []market.Candle, count int, asOf time.Time) []market.Candle {
	if len(daily) == 0 || count <= 0 {
		return nil
	}

	type bucket struct {
		year, week int
		candle     market.Candle
	}

	var weeks []bucket
	for _, ct := range daily {
		if ct.IsZero() {
			continue
		}
		t := time.Unix(int64(ct.Timestamp), 0).UTC().Add(dayRollShift)
		year, week := t.ISOWeek()

		if n := len(weeks); n > 0 && weeks[n-1].year == year && weeks[n-1].week == week {
			b := &weeks[n-1]
			if ct.High > b.candle.High {
				b.candle.High = ct.High
			}
			if ct.Low < b.candle.Low {
				b.candle.Low = ct.Low
			}
			b.candle.Close = ct.Close
			b.candle.Ticks += ct.Ticks
			continue
		}

		weeks = append(weeks, bucket{year: year, week: week, candle: ct})
	}
	if len(weeks) == 0 {
		return nil
	}

	nowYear, nowWeek := asOf.UTC().Add(dayRollShift).ISOWeek()
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
