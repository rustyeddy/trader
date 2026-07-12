package datamanager

import (
	"fmt"
	"strings"
	"time"

	"github.com/rustyeddy/trader/market"
)

// CandleSet contains a dense set of candles.
type CandleSet struct {
	Instrument string
	Start      market.Timestamp // unix seconds for candle open
	Timeframe  market.Timeframe
	Scale      market.Scale6
	Source     string
	Candles    []market.Candle
	Valid      []uint64

	Filepath string
	Gaps     []gap

	duplicates int
	outOfRange int
}

// NewMonthlyCandleSet is an internal helper for trader type processing.
func NewMonthlyCandleSet(inst string, tf market.Timeframe, monthStart market.Timestamp,
	scale market.Scale6, source string) (*CandleSet, error) {
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

	return &CandleSet{
		Instrument: inst,
		Start:      monthStart,
		Timeframe:  tf,
		Scale:      scale,
		Source:     source,
		Candles:    make([]market.Candle, n),
		Valid:      make([]uint64, (n+63)/64),
	}, nil
}

// AddCandle is an internal helper for trader type processing.
func (cs *CandleSet) AddCandle(ts market.Timestamp, c market.Candle) error {
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

	tf := market.Timestamp(cs.Timeframe)
	off := ts - cs.Start
	if off%tf != 0 {
		return fmt.Errorf("timestamp %d not aligned to timeframe %d", ts, cs.Timeframe)
	}

	idx := int(off / tf)
	if idx < 0 || idx >= len(cs.Candles) {
		cs.outOfRange++
		return fmt.Errorf("timestamp %d out of range for set starting %d", ts, cs.Start)
	}

	if cs.IsValid(idx) {
		cs.duplicates++
	}

	cs.Candles[idx] = c
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
		ts := src.Start + market.Timestamp(i)*market.Timestamp(src.Timeframe)
		if err := cs.AddCandle(ts, src.Candles[i]); err != nil {
			return err
		}
	}

	return nil
}

// SetValid is an internal helper for trader type processing.
func (cs *CandleSet) SetValid(idx int) {
	market.BitSet(cs.Valid, idx)
}

// IsValid is an internal helper for trader type processing.
func (cs *CandleSet) IsValid(idx int) bool {
	return market.BitIsSet(cs.Valid, idx)
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
	return time.Unix(int64(cs.Start)+int64(idx)*int64(cs.Timeframe), 0).UTC()
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
func (cs *CandleSet) Timestamp(idx int) market.Timestamp {
	return market.Timestamp(int64(cs.Start) + int64(idx)*int64(cs.Timeframe))
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
