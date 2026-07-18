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
// per real day, regardless of its length; H4 always emits six slots per
// day, opening at fixed NY-local wall-clock hours (see h4SlotsInDay), one
// of which is 3 or 5 real hours wide on a transition day. For every other
// timeframe this is a uniform tf-second stride, which is exactly correct
// since their grid boundaries don't depend on where the broker's trading
// day begins.
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

	// H4: fixed NY-local wall-clock opens within each real day (see
	// h4SlotsInDay).
	i := 0
	for i < n {
		next := nextDailyBoundary(day)
		for _, slot := range h4SlotsInDay(day, next) {
			if i >= n {
				break
			}
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

	// H4: walk the same per-day NY-local wall-clock slots SlotBoundaries
	// emits (h4SlotsInDay), so the two functions stay exact inverses even
	// across DST transitions, where the slots are not a fixed 4h stride.
	idx := 0
	for {
		next := nextDailyBoundary(day)
		slots := h4SlotsInDay(day, next)
		if t.Before(next) {
			pos := 0
			for i, s := range slots {
				if !t.Before(s) {
					pos = i
				}
			}
			return idx + pos
		}
		idx += len(slots)
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

// h4SlotsInDay returns the open times of every H4 candle within one broker
// day [dayOpen, nextOpen).
//
// OANDA's H4 candles open at fixed America/New_York WALL-CLOCK hours —
// 1:00, 5:00, 9:00, 13:00, 17:00, 21:00 local — NOT at fixed 4-hour UTC
// strides from the session open. On a normal day the two rules coincide,
// but on a DST transition day the wall-clock rule's UTC phase shifts at
// the transition instant (2am local), mid-session: the 1:00-5:00 local
// slot spans 3 real hours on spring-forward days and 5 on fall-back days,
// and every later slot that day sits an hour off the fixed-stride grid.
// Verified against the raw OANDA archive across 2005-2012 transition days
// (see issue #182 — the apparent "inconsistent compression position" was
// this rule viewed through the fixed-stride assumption).
//
// On fall-back days the 1:00 local hour occurs twice an hour apart in UTC;
// only the first occurrence opens a candle, so a candidate under 2h after
// the previous kept slot is skipped (legitimate spacing is never below 3h).
func h4SlotsInDay(dayOpen, nextOpen time.Time) []time.Time {
	loc := types.DailyAlignmentLocation()
	out := make([]time.Time, 0, 7)
	var prev time.Time
	// dayOpen is a 17:00-local boundary, so hourly steps stay on the hour.
	for t := dayOpen; t.Before(nextOpen); t = t.Add(time.Hour) {
		switch t.In(loc).Hour() {
		case 1, 5, 9, 13, 17, 21:
			if !prev.IsZero() && t.Sub(prev) < 2*time.Hour {
				continue
			}
			out = append(out, t)
			prev = t
		}
	}
	return out
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

	// H4: fixed NY-local wall-clock opens within each real day (see
	// h4SlotsInDay), keeping only slots whose own open falls within
	// [monthStart, monthEnd).
	var out []time.Time
	for day.Before(monthEnd) {
		next := nextDailyBoundary(day)
		for _, slot := range h4SlotsInDay(day, next) {
			if !slot.Before(monthStart) && slot.Before(monthEnd) {
				out = append(out, slot)
			}
		}
		day = next
	}
	return out
}
