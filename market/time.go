package market

import (
	"fmt"
	"strings"
	"time"
)

// Timestamp represents a trader domain type.
type Timestamp int64

// timemilli represents a trader domain type.
type timemilli int64

const (
	SecondInMS  timemilli = 1_000
	MinuteInSec Timestamp = 60
	MinuteInMS  timemilli = 60_000
	HourInSec   Timestamp = 3_600
	HourInMS    timemilli = 3_600_000
)

const dateLayout = "2006-01-02"

// FromTime is an internal helper for trader type processing.
func FromTime(t time.Time) Timestamp {
	return Timestamp(t.Unix())
}

// ParseDateTimestamp parses a YYYY-MM-DD date string into a UTC midnight Timestamp.
func ParseDateTimestamp(s string) (Timestamp, error) {
	t, err := time.Parse(dateLayout, strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	return FromTime(t.UTC()), nil
}

// FromString is an internal helper for trader type processing.
func FromString(s string) Timestamp {
	t, err := ParseDateTimestamp(s)
	if err != nil {
		return 0
	}
	return t
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
func (t Timestamp) Milli() timemilli {
	return timemilli(t * 1000)
}

// Conversions
func (ms timemilli) Sec() Timestamp { return Timestamp(int64(ms) / 1_000) }

// MS is an internal helper for trader type processing.
func (s Timestamp) MS() timemilli { return timemilli(int64(s) * 1_000) }

// Flooring (bar opens)
func (s Timestamp) FloorToMinute() Timestamp { return (s / 60) * 60 }

// FloorToHour is an internal helper for trader type processing.
func (s Timestamp) FloorToHour() Timestamp { return (s / 3_600) * 3_600 }

// FloorToMinute is an internal helper for trader type processing.
func (ms timemilli) FloorToMinute() timemilli { return (ms / 60_000) * 60_000 }

// FloorToHour is an internal helper for trader type processing.
func (ms timemilli) FloorToHour() timemilli { return (ms / 3_600_000) * 3_600_000 }

// timeMilliFromTime is an internal helper for trader type processing.
func timeMilliFromTime(t time.Time) timemilli {
	return timemilli(t.UnixMilli())
}

// daysInMonth returns the number of days in a given month.
// month0 is 0-indexed: 0=Jan, 11=Dec.
func daysInMonth(year int, month0 int) int {
	// Convert to Go's 1-indexed month
	month := time.Month(month0 + 1)

	// Trick: day 0 of next month = last day of this month
	t := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC)
	return t.Day()
}

// TimeRange represents a trader domain type.
type TimeRange struct {
	Start Timestamp // inclusive
	End   Timestamp // exclusive
	TF    Timeframe // m1, h1, d1
}

// newTimeRange is an internal helper for trader type processing.
func newTimeRange(start Timestamp, end Timestamp, tf Timeframe) TimeRange {
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
	return timeRangeFromStrings(from, to, tf)
}

// timeRangeFromStrings is an internal helper for trader type processing.
func timeRangeFromStrings(fromStr, toStr, tfstr string) (tr TimeRange, err error) {
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

// monthRange will return the first day and last day of month.
func monthRange(year int, month int) TimeRange {
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

var newYorkLoc = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// isFXMarketClosed is retained for backward compatibility.
// Deprecated: use IsForexMarketClosed or isForexMarketClosed instead.
func isFXMarketClosed(t time.Time) bool {
	return isForexMarketClosed(t)
}

// IsForexMarketClosed is the exported form of isForexMarketClosed for
// use by sibling packages (e.g. data/dukascopy).
func IsForexMarketClosed(t time.Time) bool {
	return isForexMarketClosed(t)
}

// isForexMarketClosed is an internal helper for trader type processing.
func isForexMarketClosed(t time.Time) bool {
	nt := t.In(newYorkLoc)
	wd := nt.Weekday()
	h := nt.Hour()

	switch wd {
	case time.Saturday:
		return true
	case time.Sunday:
		if h < 17 {
			return true
		}
		// Sunday evening session is closed when Sunday itself is a major holiday
		// (e.g. Jan 1 or Dec 25 falls on Sunday — OANDA has no data that evening).
		sm, sd := nt.Month(), nt.Day()
		if (sm == time.January && sd == 1) ||
			(sm == time.December && sd == 25) ||
			(sm == time.December && sd == 26) {
			return true
		}
		// Also closed when Monday is a major holiday.
		nextDay := nt.AddDate(0, 0, 1)
		nm, nd := nextDay.Month(), nextDay.Day()
		return (nm == time.January && nd == 1) ||
			(nm == time.December && nd == 25) ||
			(nm == time.December && nd == 26)
	case time.Friday:
		return h >= 17
	default:
		return isMajorForexHolidayClosed(nt)
	}
}

// isMajorForexHolidayClosed is an internal helper for trader type processing.
func isMajorForexHolidayClosed(t time.Time) bool {
	month := t.Month()
	day := t.Day()
	h := t.Hour()

	if month == time.January && day == 1 {
		return true
	}
	if month == time.December && day == 25 {
		return true
	}
	if month == time.December && day == 26 {
		return true
	}
	if month == time.December && day == 24 && h >= 13 {
		return true
	}
	if month == time.December && day == 31 && h >= 13 {
		return true
	}

	return false
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
