package review

import "github.com/rustyeddy/trader/market"

// CandleCounts is the per-timeframe candle window from docs/Review.org's
// "Data requirements" table. "W" is the number of weekly bars
// DeriveWeeklyCandles produces from the D1 series, not a separate OANDA
// fetch (see WeeklyLookbackDays). "H1" is only ever fetched for pairs
// already classified "tradeable", not for the full watchlist.
//
// D/H4/H1 are 200, not the ADX(14) readiness minimum of 2*14=28 or the old
// value of 60: GitHub issue #175 found that Wilder's recursive smoothing
// only reaches Ready() at 2*N periods, not convergence — a cold-started
// ADX at 60 candles measured 0.6-4.7 points off a long-run reference in
// synthetic testing, while ~100-150 candles closed that gap to <0.1 points.
// 200 leaves headroom beyond the empirically-observed convergence point.
var CandleCounts = map[string]int{"W": 30, "D": 200, "H4": 200, "H1": 200}

// WeeklyLookbackDays sizes the D1 window fetched/cached so there is enough
// daily history to cover both CandleCounts["D"]'s 200-candle
// ADX-convergence window and CandleCounts["W"]'s 30 complete weekly bars
// via DeriveWeeklyCandles. 200 weekday candles needs roughly 280 calendar
// days at ~5/7 weekdays-per-week; 340 leaves headroom for holidays and the
// current partial week that gets dropped.
const WeeklyLookbackDays = 340

// CandlesOnly strips timestamps, keeping candle order.
func CandlesOnly(cts []market.Candle) []market.Candle {
	out := make([]market.Candle, len(cts))
	copy(out, cts)
	return out
}
