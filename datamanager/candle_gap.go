package datamanager

import (
	"fmt"
	"io"
	"time"

	"github.com/rustyeddy/trader/types"
)

// gap represents a trader domain type.
type gap struct {
	StartIdx int32  // first missing candle index
	Len      int32  // number of missing intervals
	Kind     string // weekend vs suspicious
}

// gapStats represents a trader domain type.
type gapStats struct {
	TotalBars      int
	PresentBars    int
	MissingBars    int
	GapCount       int
	WeekendGaps    int
	SuspiciousGaps int
	LongestGapBars int
	LongestGapKind string
}

// BuildGapReport is an internal helper for trader type processing.
func (cs *CandleSet) BuildGapReport() {
	cs.Gaps = cs.Gaps[:0]

	n := len(cs.Candles)
	if n == 0 {
		return
	}

	i := 0
	for i < n {
		if types.BitIsSet(cs.Valid, i) {
			i++
			continue
		}

		start := i
		for i < n && !types.BitIsSet(cs.Valid, i) {
			i++
		}
		length := i - start

		kind := cs.classifyGap(start, length)
		cs.Gaps = append(cs.Gaps, gap{
			StartIdx: int32(start),
			Len:      int32(length),
			Kind:     kind,
		})
	}
}

// classifyGap is an internal helper for trader type processing.
func (cs *CandleSet) classifyGap(startIdx, length int) string {
	tf := int64(cs.Timeframe)

	startUnix := int64(cs.Start) + int64(startIdx)*tf
	t := time.Unix(startUnix, 0).UTC()
	wd := t.Weekday()

	gapSeconds := int64(length) * tf
	gapMinutes := gapSeconds / 60

	if gapMinutes >= 60*24 {
		if wd == time.Friday || wd == time.Saturday || wd == time.Sunday {
			return "weekend"
		}
		return "suspicious"
	}

	if gapMinutes >= 10 {
		return "suspicious"
	}

	return "minor"
}

// Stats is an internal helper for trader type processing.
func (cs *CandleSet) Stats() gapStats {
	var s gapStats

	if len(cs.Gaps) == 0 {
		cs.BuildGapReport()
	}

	n := len(cs.Candles)
	s.TotalBars = n

	for i := 0; i < n; i++ {
		if types.BitIsSet(cs.Valid, i) {
			s.PresentBars++
		}
	}

	s.MissingBars = n - s.PresentBars

	for _, g := range cs.Gaps {
		s.GapCount++
		if int(g.Len) > s.LongestGapBars {
			s.LongestGapBars = int(g.Len)
			s.LongestGapKind = g.Kind
		}
		switch g.Kind {
		case "weekend":
			s.WeekendGaps++
		case "suspicious":
			s.SuspiciousGaps++
		}
	}

	return s
}

// PrintStats is an internal helper for trader type processing.
func (cs *CandleSet) PrintStats(w io.Writer) {
	cs.BuildGapReport()
	s := cs.Stats()

	fmt.Fprintln(w, "---- CandleSet Stats ----")
	fmt.Fprintf(w, "Range: %s → %s\n", cs.Time(0), cs.Time(len(cs.Candles)-1))
	fmt.Fprintf(w, "             Timeframe: %s\n", cs.Timeframe)
	fmt.Fprintf(w, "            Total Bars: %d\n", s.TotalBars)
	fmt.Fprintf(w, "          Present Bars: %d\n", s.PresentBars)
	fmt.Fprintf(w, "          Missing Bars: %d\n", s.MissingBars)
	fmt.Fprintf(w, "             Total Gaps: %d\n", s.GapCount)
	fmt.Fprintf(w, "           Weekend Gaps: %d\n", s.WeekendGaps)
	fmt.Fprintf(w, "        Suspicious Gaps: %d\n", s.SuspiciousGaps)
	fmt.Fprintf(w, "Longest Gap: %d bars (%s)\n", s.LongestGapBars, s.LongestGapKind)
	fmt.Fprintln(w, "--------------------------")
}
