package marketdata

import (
	"fmt"
	"time"
)

// TimeRange is a half-open time interval [start, end): start is included,
// end is excluded. This matches the convention used throughout Trader's
// market-data queries and bar semantics.
type TimeRange struct {
	start time.Time
	end   time.Time
}

// NewTimeRange returns the half-open range [start, end). It reports an
// error unless end is strictly after start.
func NewTimeRange(start, end time.Time) (TimeRange, error) {
	if !end.After(start) {
		return TimeRange{}, fmt.Errorf("marketdata: invalid time range: end %s is not after start %s", end, start)
	}
	return TimeRange{start: start, end: end}, nil
}

// Start returns r's inclusive start.
func (r TimeRange) Start() time.Time {
	return r.start
}

// End returns r's exclusive end.
func (r TimeRange) End() time.Time {
	return r.end
}

// Duration returns the elapsed time between Start and End.
func (r TimeRange) Duration() time.Duration {
	return r.end.Sub(r.start)
}

// Contains reports whether t falls within r's half-open range: t is
// included at Start, excluded at End.
func (r TimeRange) Contains(t time.Time) bool {
	return !t.Before(r.start) && t.Before(r.end)
}

// Elapsed reports whether r has finished as of now: whether now is at or
// past r's End. It expresses bar completeness without introducing a
// separate concept — a bar is incomplete exactly when its range has not
// yet elapsed.
func (r TimeRange) Elapsed(now time.Time) bool {
	return !now.Before(r.end)
}
