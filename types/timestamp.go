package types

import "time"

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
	return ts < t
}

func (t Timestamp) After(ts Timestamp) bool {
	return t < ts
}

func (t Timestamp) Add(i time.Duration) Timestamp {
	return t + Timestamp(i)
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

// daysInMonth returns the number of days in a given month.
// month0 is 0-indexed: 0=Jan, 11=Dec.
func DaysInMonth(year int, month0 int) int {
	// Convert to Go's 1-indexed month
	month := time.Month(month0 + 1)

	// Trick: day 0 of next month = last day of this month
	t := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC)
	return t.Day()
}
