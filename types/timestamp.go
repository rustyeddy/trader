package types

import (
	"fmt"
	"time"
)

type Timestamp int64
type Timemilli int64

const (
	SecondInMS  Timemilli = 1_000
	MinuteInSec Timestamp = 60
	MinuteInMS  Timemilli = 60_000
	HourInSec   Timestamp = 3_600
	HourInMS    Timemilli = 3_600_000
)

func FromTime(t time.Time) Timestamp {
	return Timestamp(t.Unix())
}

func (t Timestamp) Int64() int64 {
	return int64(t)
}

func (t Timestamp) Time() time.Time {
	return time.Unix(t.Int64(), 0)
}

func (t Timestamp) IsZero() bool {
	return t == 0
}

func (t Timestamp) Before(ts Timestamp) bool {
	return t < ts
}

func (t Timestamp) After(ts Timestamp) bool {
	return t > ts
}

func (t Timestamp) Add(d time.Duration) Timestamp {
	return t + Timestamp(d/time.Second)
}

func (t Timestamp) String() string {
	return time.Unix(t.Int64(), 0).
		UTC().
		Format(time.RFC3339)
}

func (t Timestamp) Milli() Timemilli {
	return Timemilli(t * 1000)
}

// Conversions
func (ms Timemilli) Sec() Timestamp { return Timestamp(int64(ms) / 1_000) }
func (s Timestamp) MS() Timemilli   { return Timemilli(int64(s) * 1_000) }

// Flooring (bar opens)
func (s Timestamp) FloorToMinute() Timestamp { return (s / 60) * 60 }
func (s Timestamp) FloorToHour() Timestamp   { return (s / 3_600) * 3_600 }

func (ms Timemilli) FloorToMinute() Timemilli { return (ms / 60_000) * 60_000 }
func (ms Timemilli) FloorToHour() Timemilli   { return (ms / 3_600_000) * 3_600_000 }

func TimeMilliFromTime(t time.Time) Timemilli {
	return Timemilli(t.UnixMilli())
}

// daysInMonth returns the number of days in a given month.
// month0 is 0-indexed: 0=Jan, 11=Dec.
func DaysInMonth(year int, month0 int) int {
	// Convert to Go's 1-indexed month
	month := time.Month(month0 + 1)

	// Trick: day 0 of next month = last day of this month
	t := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC)
	return t.Day()
}

type TimeRange struct {
	Start Timestamp // inclusive
	End   Timestamp // exclusive
	TF    Timeframe // m1, h1, d1
}

func NewTimeRange(start Timestamp, end Timestamp) TimeRange {
	r := TimeRange{
		Start: Timestamp(start),
		End:   Timestamp(end),
	}
	return r
}

func (r TimeRange) Valid() bool {
	return r.Start > 0 && r.End > r.Start
}

func (r TimeRange) Contains(ts Timestamp) bool {
	return ts >= r.Start && ts < r.End
}

func (r TimeRange) Overlaps(other TimeRange) bool {
	return r.Start < other.End && other.Start < r.End
}

func (r TimeRange) Covers(other TimeRange) bool {
	return r.Start <= other.Start && r.End >= other.End
}

func (r TimeRange) String() string {
	return fmt.Sprintf("[%s, %s)",
		time.Unix(int64(r.Start), 0).UTC().Format(time.RFC3339),
		time.Unix(int64(r.End), 0).UTC().Format(time.RFC3339))
}

func YearRange(year int) TimeRange {
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)
	return TimeRange{
		Start: Timestamp(start.Unix()),
		End:   Timestamp(end.Unix()),
	}
}

type YearMonth struct {
	Year  int
	Month int
}

func MonthsInRange(r TimeRange) []YearMonth {
	start := time.Unix(int64(r.Start), 0).UTC()
	end := time.Unix(int64(r.End), 0).UTC()

	cur := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	last := time.Date(end.Year(), end.Month(), 1, 0, 0, -1, 0, time.UTC)

	var out []YearMonth
	for !cur.After(last) {
		out = append(out, YearMonth{
			Year:  cur.Year(),
			Month: int(cur.Month()),
		})
		cur = cur.AddDate(0, 1, 0)
	}
	return out
}

// func (r Range) Count() int
// func (r Range) Duration() int64
// func (r Range) Align() Range
// func (r Range) CandleStart(ts types.Timestamp) types.Timestamp
// func (r Range) IndexOf(ts types.Timestamp) (int, error)
// func (r Range) TimestampAt(i int) types.Timestamp

// func Floor(ts types.Timestamp, tf Timeframe) types.Timestamp
// func Ceil(ts types.Timestamp, tf Timeframe) types.Timestamp
// func AlignStart(ts types.Timestamp, tf Timeframe) types.Timestamp
// func Next(ts types.Timestamp, tf Timeframe) types.Timestamp
// func Prev(ts types.Timestamp, tf Timeframe) types.Timestamp

// func YearRange(year int, tf Timeframe) Range
// func MonthRange(year int, month time.Month, tf Timeframe) Range
// func DayRange(year int, month time.Month, day int, tf Timeframe) Range

var newYorkLoc = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.UTC
	}
	return loc
}()

func IsForexMarketClosed(t time.Time) bool {
	nt := t.In(newYorkLoc)
	wd := nt.Weekday()
	h := nt.Hour()

	switch wd {
	case time.Saturday:
		return true
	case time.Sunday:
		return h < 17
	case time.Friday:
		return h >= 17
	default:
		return isMajorForexHolidayClosed(nt)
	}
}

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
	if month == time.December && day == 24 && h >= 13 {
		return true
	}
	if month == time.December && day == 31 && h >= 13 {
		return true
	}

	return false
}
