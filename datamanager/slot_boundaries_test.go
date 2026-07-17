package datamanager

import (
	"context"
	"testing"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/require"
)

// TestSlotIndexForTime_H4MatchesSlotBoundaries_AcrossManyDays is the
// regression case for a bug found while validating #179's regen:
// SlotIndexForTime's per-day increment for H4 used floor(dayWidth/step),
// undercounting by one on the 23-hour spring-forward day (floor(23h/4h)=5),
// while SlotBoundaries' emission loop (a slot counts if it starts before
// the day ends, even if it doesn't finish before the day ends) actually
// emits 6 slots for that same day. The mismatch permanently shifted every
// later H4 index in the file by -1 relative to SlotBoundaries, causing
// DeriveCanonicalFromRaw to misplace every raw row after a March DST
// transition — silently dropping one real candle and colliding two
// different slots onto the same timestamp.
//
// This walks 400 days (more than a year, crossing both the March
// spring-forward and November fall-back transitions) and asserts
// SlotIndexForTime(start, tf, SlotBoundaries(start, tf, n)[i]) == i for
// every single slot — i.e. the two functions must stay exact inverses of
// each other for as long as the underlying data might span.
func TestSlotIndexForTime_MatchesSlotBoundaries_AcrossManyDays(t *testing.T) {
	t.Parallel()

	for _, tf := range []types.Timeframe{types.D1, types.H4} {
		start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		n := 400
		if tf == types.H4 {
			n = 400 * 6
		}
		boundaries := SlotBoundaries(start, tf, n)

		for i, b := range boundaries {
			got := SlotIndexForTime(start, tf, b)
			require.Equal(t, i, got, "tf=%v slot %d (%s): SlotIndexForTime must invert SlotBoundaries", tf, i, b)
		}
	}
}

// TestDeriveCanonicalFromRaw_H4SpringForwardNoCollision is an end-to-end
// regression test for the same bug via the real production path: a raw
// H4 month spanning the March 2020 spring-forward transition must derive
// with every real candle in its own slot, no collisions, and no
// off-by-one loss of a real trading day's candle.
func TestDeriveCanonicalFromRaw_H4SpringForwardNoCollision(t *testing.T) {
	rawDir := t.TempDir()
	UseTempDataDir(t)

	key := Key{
		Kind:       KindCandle,
		Source:     market.SourceOanda,
		Instrument: "EURUSD",
		TF:         types.H4,
		Year:       2020,
		Month:      3,
	}

	monthStart := time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	boundaries := SlotBoundaries(monthStart, types.H4, 186) // full March, 31 days * 6

	// March 31's trading day (by daily-alignment convention) runs into
	// April 1 for its last few H4 slots — writeRawMonth only preserves
	// rows within [monthStart, monthEnd), same as the real raw archive,
	// so those trailing slots are expected to have no raw row and no
	// canonical data; this test only asserts on the slots writeRawMonth
	// actually keeps.
	var rows []RawCandleRow
	var keptIdx []int
	for i, b := range boundaries {
		if b.Before(monthStart) || !b.Before(monthEnd) {
			continue
		}
		keptIdx = append(keptIdx, i)
		rows = append(rows, RawCandleRow{
			Time:    b,
			BidOpen: 1.1000, BidHigh: 1.1010, BidLow: 1.0990, BidClose: 1.1005,
			AskOpen: 1.1002, AskHigh: 1.1012, AskLow: 1.0992, AskClose: 1.1007,
			Volume:   100,
			Complete: true,
		})
	}
	require.NoError(t, writeRawMonth(rawDir, key, monthStart, rows))
	rawPath := monthlyCandle(rawDir, key)

	dm := NewDataManager([]string{"EURUSD"}, monthStart, monthEnd)
	_, err := dm.DeriveCanonicalFromRaw(context.Background(), rawPath, key)
	require.NoError(t, err)

	cs, err := getStore().ReadCSV(key)
	require.NoError(t, err)
	require.Equal(t, len(keptIdx), cs.CountValid(), "every slot with a raw row must land in its own slot, no collisions or losses")
	for _, i := range keptIdx {
		require.True(t, cs.IsValid(i), "slot %d", i)
		require.Equal(t, boundaries[i], cs.Time(i), "slot %d", i)
	}
}
