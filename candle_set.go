package trader

import (
	"fmt"
	"strings"
	"time"
)

// candleSet contains a dense set of candles.
type candleSet struct {
	Instrument string
	Start      Timestamp // unix seconds for candle open
	Timeframe  Timeframe
	Scale      Scale6
	Source     string
	Candles    []Candle
	Valid      []uint64

	Filepath string
	Gaps     []gap

	duplicates int
	outOfRange int
	badLines   int
}

// newMonthlyCandleSet is an internal helper for trader type processing.
func newMonthlyCandleSet(inst string, tf Timeframe, monthStart Timestamp,
	scale Scale6, source string) (*candleSet, error) {
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

	return &candleSet{
		Instrument: inst,
		Start:      monthStart,
		Timeframe:  tf,
		Scale:      scale,
		Source:     source,
		Candles:    make([]Candle, n),
		Valid:      make([]uint64, (n+63)/64),
	}, nil
}

// AddCandle is an internal helper for trader type processing.
func (cs *candleSet) AddCandle(ts Timestamp, c Candle) error {
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

	tf := Timestamp(cs.Timeframe)
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
func (cs *candleSet) Merge(src *candleSet) error {
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
		ts := src.Start + Timestamp(i)*Timestamp(src.Timeframe)
		if err := cs.AddCandle(ts, src.Candles[i]); err != nil {
			return err
		}
	}

	return nil
}

// SetValid is an internal helper for trader type processing.
func (cs *candleSet) SetValid(idx int) {
	bitSet(cs.Valid, idx)
}

// IsValid is an internal helper for trader type processing.
func (cs *candleSet) IsValid(idx int) bool {
	return bitIsSet(cs.Valid, idx)
}

// CountValid is an internal helper for trader type processing.
func (cs *candleSet) CountValid() int {
	n := 0
	for i := range cs.Candles {
		if cs.IsValid(i) {
			n++
		}
	}
	return n
}

// Time is an internal helper for trader type processing.
func (cs *candleSet) Time(idx int) time.Time {
	return time.Unix(int64(cs.Start)+int64(idx)*int64(cs.Timeframe), 0).UTC()
}

// Timestamp is an internal helper for trader type processing.
func (cs *candleSet) Timestamp(idx int) Timestamp {
	return Timestamp(int64(cs.Start) + int64(idx)*int64(cs.Timeframe))
}

// Filename is an internal helper for trader type processing.
func (cs *candleSet) Filename() string {
	inst := strings.ToLower(cs.Instrument)

	tfstr := strings.ToLower(cs.Timeframe.String())
	year := time.Unix(int64(cs.Start), 0).UTC().Year()

	if tfstr == "d1" {
		return fmt.Sprintf("%s-%s-all", inst, tfstr)
	}
	return fmt.Sprintf("%s-%s-%d", inst, tfstr, year)
}
