package datamanager

import (
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

// CandleSet contains a dense set of candles. Each element carries its own
// authoritative open Timestamp (market.CandleTime) rather than having it
// reconstructed from Start+idx*Timeframe on every read — that
// reconstruction assumed every slot is exactly Timeframe seconds apart,
// which is false for D1 across a DST transition (OANDA's true daily
// boundary is 17:00 America/New_York, so the transition day is 23 or 25
// wall-clock hours). Start/Timeframe are still used to place candles into
// slots (see SlotIndexForTime) and to size the array, but the reported
// time of any candle — valid or a gap placeholder — always comes from its
// own Timestamp field.
type CandleSet struct {
	Instrument string
	Start      types.Timestamp // unix seconds for slot 0's open
	Timeframe  types.Timeframe
	Scale      types.Scale6
	Source     string
	Candles    []market.CandleTime
	Valid      []uint64

	Filepath string
	Gaps     []gap

	duplicates int
	outOfRange int
}

// NewMonthlyCandleSet is an internal helper for trader type processing.
func NewMonthlyCandleSet(inst string, tf types.Timeframe, monthStart types.Timestamp,
	scale types.Scale6, source string) (*CandleSet, error) {
	if inst == "" {
		return nil, fmt.Errorf("blank instrument")
	}
	if tf <= 0 {
		return nil, fmt.Errorf("invalid timeframe: %d", tf)
	}

	startTime := time.Unix(int64(monthStart), 0).UTC()
	if startTime.Second() != 0 || startTime.Nanosecond() != 0 {
		return nil, fmt.Errorf("monthStart not aligned to minute boundary: %d", monthStart)
	}
	if startTime.Day() != 1 || startTime.Hour() != 0 || startTime.Minute() != 0 {
		return nil, fmt.Errorf("monthStart not aligned to start of month: %s", startTime.Format(time.RFC3339))
	}

	endTime := startTime.AddDate(0, 1, 0)
	spanSec := int64(endTime.Sub(startTime).Seconds())
	n := int(spanSec / int64(tf))
	if n <= 0 {
		return nil, fmt.Errorf("computed invalid candle count: %d", n)
	}

	boundaries := SlotBoundaries(startTime, tf, n)
	candles := make([]market.CandleTime, n)
	for i, b := range boundaries {
		candles[i].Timestamp = types.FromTime(b)
	}

	return &CandleSet{
		Instrument: inst,
		Start:      types.FromTime(boundaries[0]),
		Timeframe:  tf,
		Scale:      scale,
		Source:     source,
		Candles:    candles,
		Valid:      make([]uint64, (n+63)/64),
	}, nil
}

// AddCandle is an internal helper for trader type processing.
func (cs *CandleSet) AddCandle(ts types.Timestamp, c market.Candle) error {
	if cs == nil {
		return fmt.Errorf("nil CandleSet")
	}
	if cs.Timeframe <= 0 {
		return fmt.Errorf("invalid timeframe: %d", cs.Timeframe)
	}
	if ts < cs.Start {
		cs.outOfRange++
		return fmt.Errorf("timestamp %d before set start %d", ts, cs.Start)
	}

	var idx int
	if dailyAligned(cs.Timeframe) {
		// D1/H4 slots aren't evenly spaced from cs.Start across a DST
		// transition (see SlotIndexForTime), so a fixed offset%tf check
		// would reject legitimately-aligned timestamps on the far side of
		// one.
		idx = SlotIndexForTime(time.Unix(int64(cs.Start), 0).UTC(), cs.Timeframe, time.Unix(int64(ts), 0).UTC())
	} else {
		tf := types.Timestamp(cs.Timeframe)
		off := ts - cs.Start
		if off%tf != 0 {
			return fmt.Errorf("timestamp %d not aligned to timeframe %d", ts, cs.Timeframe)
		}
		idx = int(off / tf)
	}

	if idx < 0 || idx >= len(cs.Candles) {
		cs.outOfRange++
		return fmt.Errorf("timestamp %d out of range for set starting %d", ts, cs.Start)
	}

	if cs.IsValid(idx) {
		cs.duplicates++
	}

	cs.Candles[idx] = market.CandleTime{Candle: c, Timestamp: ts}
	cs.SetValid(idx)
	return nil
}

// Merge is an internal helper for trader type processing.
func (cs *CandleSet) Merge(src *CandleSet) error {
	if cs == nil || src == nil {
		return fmt.Errorf("nil CandleSet in merge")
	}
	if cs.Timeframe != src.Timeframe {
		return fmt.Errorf("timeframe mismatch dst=%d src=%d", cs.Timeframe, src.Timeframe)
	}
	if cs.Scale != src.Scale {
		return fmt.Errorf("scale mismatch dst=%d src=%d", cs.Scale, src.Scale)
	}
	if cs.Instrument == "" || src.Instrument == "" {
		return fmt.Errorf("nil instrument in merge")
	}
	if cs.Instrument != src.Instrument {
		return fmt.Errorf("instrument mismatch dst=%q src=%q", cs.Instrument, src.Instrument)
	}

	for i := range src.Candles {
		if !src.IsValid(i) {
			continue
		}
		if err := cs.AddCandle(src.Candles[i].Timestamp, src.Candles[i].Candle); err != nil {
			return err
		}
	}

	return nil
}

// SetValid is an internal helper for trader type processing.
func (cs *CandleSet) SetValid(idx int) {
	types.BitSet(cs.Valid, idx)
}

// IsValid is an internal helper for trader type processing.
func (cs *CandleSet) IsValid(idx int) bool {
	return types.BitIsSet(cs.Valid, idx)
}

// CountValid is an internal helper for trader type processing.
func (cs *CandleSet) CountValid() int {
	n := 0
	for i := range cs.Candles {
		if cs.IsValid(i) {
			n++
		}
	}
	return n
}

// Time is an internal helper for trader type processing.
func (cs *CandleSet) Time(idx int) time.Time {
	return time.Unix(int64(cs.Candles[idx].Timestamp), 0).UTC()
}

// LastValidTime returns the UTC calendar day of the last valid (non-gap)
// candle in the set, or false if the set has no valid candles.
func (cs *CandleSet) LastValidTime() (time.Time, bool) {
	if cs == nil {
		return time.Time{}, false
	}
	for i := len(cs.Candles) - 1; i >= 0; i-- {
		if cs.IsValid(i) {
			t := cs.Time(i)
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), true
		}
	}
	return time.Time{}, false
}

// Timestamp is an internal helper for trader type processing.
func (cs *CandleSet) Timestamp(idx int) types.Timestamp {
	return cs.Candles[idx].Timestamp
}

// Filename is an internal helper for trader type processing.
func (cs *CandleSet) Filename() string {
	inst := strings.ToLower(cs.Instrument)

	tfstr := strings.ToLower(cs.Timeframe.String())
	year := time.Unix(int64(cs.Start), 0).UTC().Year()

	if tfstr == "d1" {
		return fmt.Sprintf("%s-%s-all", inst, tfstr)
	}
	return fmt.Sprintf("%s-%s-%d", inst, tfstr, year)
}
