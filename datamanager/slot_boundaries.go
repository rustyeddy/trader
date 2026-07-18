package datamanager

import (
	"time"

	"github.com/rustyeddy/trader/types"
)

// dailyAligned reports whether tf's grid boundaries are anchored to
// OANDA's daily-alignment boundary (17:00 America/New_York, DST-aware)
// rather than to a fixed stride from the Unix epoch. Only granularities
// that subdivide the broker's trading day are affected — M1/H1 bars land
// on true UTC-epoch boundaries regardless of where the trading day begins.
func dailyAligned(tf types.Timeframe) bool {
	return tf == types.D1 || tf == types.H4
}

// SlotBoundaries returns the open time of every slot in a dense CandleSet
// of n slots at timeframe tf, starting at the first tf-aligned boundary at
// or after start. For D1/H4, OANDA's true day boundary is 17:00
// America/New_York, DST-aware, so the day spanning a DST transition is 23
// or 25 wall-clock hours — this walks real day boundaries instead of
// assuming a fixed 86400-second stride. D1 always emits exactly one slot
// per real day, regardless of its length; H4 emits however many whole
// 4-hour periods fit within that day (5 or 6 on a transition day, 6
// otherwise). For every other timeframe this is a uniform tf-second
// stride, which is exactly correct since their grid boundaries don't
// depend on where the broker's trading day begins.
func SlotBoundaries(start time.Time, tf types.Timeframe, n int) []time.Time {
	out := make([]time.Time, n)
	if n == 0 {
		return out
	}

	if !dailyAligned(tf) {
		step := time.Duration(tf) * time.Second
		for i := 0; i < n; i++ {
			out[i] = start.Add(time.Duration(i) * step)
		}
		return out
	}

	day := firstDailyBoundaryAtOrAfter(start)

	if tf == types.D1 {
		for i := 0; i < n; i++ {
			out[i] = day
			day = nextDailyBoundary(day)
		}
		return out
	}

	// H4: however many whole 4-hour periods fit within each real day.
	step := time.Duration(tf) * time.Second
	i := 0
	for i < n {
		next := nextDailyBoundary(day)
		for slot := day; slot.Before(next) && i < n; slot = slot.Add(step) {
			out[i] = slot
			i++
		}
		day = next
	}
	return out
}

// SlotIndexForTime returns the slot index that t falls in in a dense
// CandleSet beginning at start with timeframe tf — the inverse of
// SlotBoundaries. For D1/H4 this walks real daily-alignment boundaries so
// the index is correct even across a DST transition, where a fixed
// division by the timeframe's seconds can misplace or collide rows.
func SlotIndexForTime(start time.Time, tf types.Timeframe, t time.Time) int {
	if !dailyAligned(tf) {
		return int(t.Sub(start) / (time.Duration(tf) * time.Second))
	}

	day := firstDailyBoundaryAtOrAfter(start)
	if t.Before(day) {
		// t precedes the first slot; return a negative index so callers'
		// existing "idx < 0" bounds checks reject it, same as the
		// non-daily-aligned case above.
		return -1
	}

	if tf == types.D1 {
		idx := 0
		for {
			next := nextDailyBoundary(day)
			if t.Before(next) {
				return idx
			}
			day = next
			idx++
		}
	}

	// H4: however many whole 4-hour periods fit within each real day. Must
	// count days the same way SlotBoundaries' emission loop does (a slot
	// counts if it starts before the day ends, even if it doesn't finish
	// before the day ends) — i.e. ceil(width/step), not floor(width/step).
	// A 23-hour transition day is 5.75 steps of 4h; SlotBoundaries emits 6
	// slots for it (0,4,8,12,16,20h all start before the 23h mark), so
	// floor(23h/4h)=5 undercounts by one and permanently shifts every
	// later index in the file by -1 relative to SlotBoundaries.
	step := time.Duration(tf) * time.Second
	idx := 0
	for {
		next := nextDailyBoundary(day)
		if t.Before(next) {
			return idx + int(t.Sub(day)/step)
		}
		width := next.Sub(day)
		idx += int((width + step - 1) / step)
		day = next
	}
}

// firstDailyBoundaryAtOrAfter returns the earliest daily-alignment
// boundary that is not before start.
func firstDailyBoundaryAtOrAfter(start time.Time) time.Time {
	b := types.DailyAlignmentBoundary(start)
	if b.Before(start) {
		b = nextDailyBoundary(b)
	}
	return b
}

// nextDailyBoundary returns the daily-alignment boundary immediately
// after b. Adding 25 hours always lands within the next day's
// boundary-to-boundary window (a broker day is 23-25 wall-clock hours),
// so flooring it gives exactly the next true boundary.
func nextDailyBoundary(b time.Time) time.Time {
	return types.DailyAlignmentBoundary(b.Add(25 * time.Hour))
}

// MonthSlotBoundaries returns every slot open-time in [monthStart, monthEnd)
// for tf — the exact set of timestamps a canonical file for that calendar
// month owns. This is the single source of truth for month-scoped slot
// construction (derive-from-raw, direct OANDA candle fetch, and reading a
// canonical file back) and replaces the SlotBoundaries(monthStart, tf, n)
// + SlotIndexForTime(monthStart, tf, t) pair for that specific use, closing
// off the class of bug where two independently-computed index functions can
// drift out of sync (see TestSlotIndexForTime_MatchesSlotBoundaries).
//
// For D1/H4, types.DailyAlignmentBoundary(monthStart) is the open of
// whichever trading session is already in progress at monthStart — a
// session almost always straddles UTC midnight on the 1st, since it opens
// at 17:00 America/New_York. D1 attributes that whole session to whichever
// calendar month its open falls in (so it's correctly excluded here when
// its open precedes monthStart — the session's own file already owns it).
// H4 subdivides that session into six 4-hour sub-slots, and unlike D1's
// atomic whole-day granularity, an in-progress session's early sub-slots
// (each with its own open time >= monthStart) are real, independently
// timestamped data that ONLY this month's own raw file has (OANDA's
// date-scoped fetch means the prior month's raw fetch never reaches a
// timestamp in this month) — anchoring to the boundary strictly AFTER
// monthStart, as SlotBoundaries(monthStart, tf, n) does, skips those
// sub-slots entirely: not written, not even reported as missing, because
// the reporting loop iterates the same incomplete boundary set. This walk
// starts from the in-progress session instead and keeps every sub-slot
// whose own open is >= monthStart, fixing that silent data loss.
func MonthSlotBoundaries(monthStart, monthEnd time.Time, tf types.Timeframe) []time.Time {
	if !dailyAligned(tf) {
		step := time.Duration(tf) * time.Second
		var out []time.Time
		for t := monthStart; t.Before(monthEnd); t = t.Add(step) {
			out = append(out, t)
		}
		return out
	}

	day := types.DailyAlignmentBoundary(monthStart)

	if tf == types.D1 {
		var out []time.Time
		for day.Before(monthEnd) {
			if !day.Before(monthStart) {
				out = append(out, day)
			}
			day = nextDailyBoundary(day)
		}
		return out
	}

	// H4: subdivide each real day into 4-hour blocks, keeping only the
	// ones whose own open falls within [monthStart, monthEnd).
	step := time.Duration(tf) * time.Second
	var out []time.Time
	for day.Before(monthEnd) {
		next := nextDailyBoundary(day)
		for slot := day; slot.Before(next); slot = slot.Add(step) {
			if !slot.Before(monthStart) && slot.Before(monthEnd) {
				out = append(out, slot)
			}
		}
		day = next
	}
	return out
}
