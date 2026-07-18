package types

import (
	"fmt"
	"strings"
	"time"
)

// Timestamp represents a trader domain type.
type Timestamp int64

// TimeMillis represents a trader domain type.
type TimeMillis int64

const (
	SecondInMS  TimeMillis = 1_000
	MinuteInSec Timestamp  = 60
	MinuteInMS  TimeMillis = 60_000
	HourInSec   Timestamp  = 3_600
	HourInMS    TimeMillis = 3_600_000
)

const dateLayout = "2006-01-02"

// FromTime is an internal helper for trader type processing.
func FromTime(t time.Time) Timestamp {
	return Timestamp(t.Unix())
}

// Int64 is an internal helper for trader type processing.
func (t Timestamp) Int64() int64 {
	return int64(t)
}

// Time is an internal helper for trader type processing.
func (t Timestamp) Time() time.Time {
	return time.Unix(t.Int64(), 0).UTC()
}

// IsZero is an internal helper for trader type processing.
func (t Timestamp) IsZero() bool {
	return t == 0
}

// Before is an internal helper for trader type processing.
func (t Timestamp) Before(ts Timestamp) bool {
	return t < ts
}

// After is an internal helper for trader type processing.
func (t Timestamp) After(ts Timestamp) bool {
	return t > ts
}

// Add is an internal helper for trader type processing.
func (t Timestamp) Add(d time.Duration) Timestamp {
	return t + Timestamp(d/time.Second)
}

// String is an internal helper for trader type processing.
func (t Timestamp) String() string {
	return time.Unix(t.Int64(), 0).
		UTC().
		Format(time.RFC3339)
}

// Milli is an internal helper for trader type processing.
func (t Timestamp) Milli() TimeMillis {
	return TimeMillis(t * 1000)
}

// Conversions
func (ms TimeMillis) Sec() Timestamp { return Timestamp(int64(ms) / 1_000) }

// MS is an internal helper for trader type processing.
func (s Timestamp) MS() TimeMillis { return TimeMillis(int64(s) * 1_000) }

// Flooring (bar opens)
func (s Timestamp) FloorToMinute() Timestamp { return (s / 60) * 60 }

// FloorToHour is an internal helper for trader type processing.
func (s Timestamp) FloorToHour() Timestamp { return (s / 3_600) * 3_600 }

// FloorToMinute is an internal helper for trader type processing.
func (ms TimeMillis) FloorToMinute() TimeMillis { return (ms / 60_000) * 60_000 }

// FloorToHour is an internal helper for trader type processing.
func (ms TimeMillis) FloorToHour() TimeMillis { return (ms / 3_600_000) * 3_600_000 }

// TimeMilliFromTime is an internal helper for trader type processing.
func TimeMilliFromTime(t time.Time) TimeMillis {
	return TimeMillis(t.UnixMilli())
}

// TimeRange represents a trader domain type.
type TimeRange struct {
	Start Timestamp // inclusive
	End   Timestamp // exclusive
	TF    Timeframe // m1, h1, d1
}

// NewTimeRange is an internal helper for trader type processing.
func NewTimeRange(start Timestamp, end Timestamp, tf Timeframe) TimeRange {
	r := TimeRange{
		Start: Timestamp(start),
		End:   Timestamp(end),
		TF:    tf,
	}
	return r
}

// ParseTimeRange parses a TimeRange from "YYYY-MM-DD" from/to strings and a
// timeframe string ("M1", "H1", "D1"). Exported for use by sibling packages.
func ParseTimeRange(from, to, tf string) (TimeRange, error) {
	return TimeRangeFromStrings(from, to, tf)
}

// TimeRangeFromStrings is an internal helper for trader type processing.
func TimeRangeFromStrings(fromStr, toStr, tfstr string) (tr TimeRange, err error) {
	return timeRangeLocation(fromStr, toStr, tfstr, time.UTC)
}

// timeRangeLocation is an internal helper for trader type processing.
func timeRangeLocation(fromStr, toStr, tfstr string, loc *time.Location) (TimeRange, error) {
	if loc == nil {
		loc = time.UTC
	}
	tf, err := ParseTimeframe(tfstr)
	if err != nil {
		return TimeRange{}, err
	}

	from, err := time.ParseInLocation(dateLayout, strings.TrimSpace(fromStr), loc)
	if err != nil {
		return TimeRange{}, fmt.Errorf("bad from date %q: %w", fromStr, err)
	}

	to, err := time.ParseInLocation(dateLayout, strings.TrimSpace(toStr), loc)
	if err != nil {
		return TimeRange{}, fmt.Errorf("bad to date %q: %w", toStr, err)
	}

	if !from.Before(to) {
		return TimeRange{}, fmt.Errorf("invalid date range: from %s must be before to %s", fromStr, toStr)
	}

	return TimeRange{
		Start: Timestamp(from.Unix()), // inclusive
		End:   Timestamp(to.Unix()),   // exclusive
		TF:    tf,
	}, nil
}

// Valid is an internal helper for trader type processing.
func (r TimeRange) Valid() bool {
	return r.Start > 0 && r.End > r.Start
}

// Contains is an internal helper for trader type processing.
func (r TimeRange) Contains(ts Timestamp) bool {
	return ts >= r.Start && ts < r.End
}

// Overlaps is an internal helper for trader type processing.
func (r TimeRange) Overlaps(other TimeRange) bool {
	return r.Start < other.End && other.Start < r.End
}

// Covers is an internal helper for trader type processing.
func (r TimeRange) Covers(other TimeRange) bool {
	return r.Start <= other.Start && r.End >= other.End
}

// String is an internal helper for trader type processing.
func (r TimeRange) String() string {
	return fmt.Sprintf("[%s, %s)",
		time.Unix(int64(r.Start), 0).UTC().Format(time.RFC3339),
		time.Unix(int64(r.End), 0).UTC().Format(time.RFC3339))
}

// MonthRange will return the first day and last day of month.
func MonthRange(year int, month int) TimeRange {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(year, time.Month(month)+1, 1, 0, 0, 0, 0, time.UTC)
	return TimeRange{
		Start: Timestamp(start.Unix()),
		End:   Timestamp(end.Unix()),
	}
}

// yearMonth represents a trader domain type.
type yearMonth struct {
	Year  int
	Month int
}

// MonthsInRange is an internal helper for trader type processing.
func (r TimeRange) MonthsInRange() []yearMonth {
	if !r.Valid() {
		return nil
	}

	start := time.Unix(int64(r.Start), 0).UTC()
	endExclusive := time.Unix(int64(r.End), 0).UTC()

	cur := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)

	// last included month is the month containing End-1 second
	lastInstant := endExclusive.Add(-time.Second)
	last := time.Date(lastInstant.Year(), lastInstant.Month(), 1, 0, 0, 0, 0, time.UTC)

	var out []yearMonth
	for !cur.After(last) {
		out = append(out, yearMonth{
			Year:  cur.Year(),
			Month: int(cur.Month()),
		})
		cur = cur.AddDate(0, 1, 0)
	}
	return out
}

// ********************************************************************
// Timeframe
// ********************************************************************
type Timeframe int64

const (
	TF0   Timeframe = 0
	Ticks Timeframe = 1
	M1    Timeframe = 60
	H1    Timeframe = 3600
	H4    Timeframe = 14400
	D1    Timeframe = 86400
)

// ParseTimeframe parses a timeframe string into its canonical Timeframe value.
// It accepts common aliases and returns an error for unknown values.
func ParseTimeframe(s string) (Timeframe, error) {
	switch normalizeTF(s) {
	case "1", "tick", "ticks":
		return Ticks, nil
	case "m1":
		return M1, nil
	case "h1":
		return H1, nil
	case "h4":
		return H4, nil
	case "d", "d1":
		return D1, nil
	default:
		return TF0, fmt.Errorf("unsupported timeframe %q", s)
	}
}

// normalizeTF is an internal helper for trader type processing.
func normalizeTF(tf string) string {
	tf = strings.ToLower(strings.TrimSpace(tf))
	// allow "60" etc if you ever pass seconds
	switch tf {
	case "60":
		return "m1"
	case "3600":
		return "h1"
	case "14400":
		return "h4"
	case "86400":
		return "d1"
	}
	return tf
}

// String is an internal helper for trader type processing.
func (tf Timeframe) String() string {
	switch tf {
	case TF0:
		return "tf0"

	case Ticks:
		return "ticks"

	case M1:
		return "m1"

	case H1:
		return "h1"

	case H4:
		return "h4"

	case D1:
		return "d1"

	default:
		return fmt.Sprintf("timeframe(%d)", tf)
	}
}

// dailyAlignmentLocation is OANDA's default alignmentTimezone. Daily-aligned
// granularities (D1, and by subdivision H4) open at 17:00 in this zone, not
// at UTC midnight — the UTC offset shifts by an hour across DST
// transitions, so this must never be approximated with a fixed offset.
var dailyAlignmentLocation = mustLoadLocation("America/New_York")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		// Should never happen with the Go stdlib's embedded tzdata; if it
		// somehow does, fail loud rather than silently mislabel every
		// daily-aligned candle from UTC, which is exactly the bug this
		// helper exists to prevent.
		panic(fmt.Sprintf("types: load location %q: %v", name, err))
	}
	return loc
}

// DailyAlignmentLocation returns the broker alignment timezone
// (America/New_York). Exposed for callers that need to evaluate local
// wall-clock rules beyond the 17:00 day boundary itself — e.g. H4 candle
// opens, which OANDA anchors to fixed local hours (1/5/9/13/17/21:00),
// so their UTC phase shifts at the DST transition instant.
func DailyAlignmentLocation() *time.Location {
	return dailyAlignmentLocation
}

// DailyAlignmentBoundary returns the most recent broker daily-alignment
// boundary at or before t: 17:00 in America/New_York, DST-aware. This is
// OANDA's default dailyAlignment/alignmentTimezone (this repo never
// overrides either), and it is the true boundary D1 candles — and, by
// subdivision, H4 candles — are anchored to. It is not UTC midnight.
func DailyAlignmentBoundary(t time.Time) time.Time {
	nyT := t.In(dailyAlignmentLocation)
	boundary := time.Date(nyT.Year(), nyT.Month(), nyT.Day(), 17, 0, 0, 0, dailyAlignmentLocation)
	if boundary.After(nyT) {
		boundary = boundary.AddDate(0, 0, -1)
	}
	return boundary.UTC()
}
