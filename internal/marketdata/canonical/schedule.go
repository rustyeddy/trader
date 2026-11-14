package canonical

import (
	_ "embed" // newYorkTZif
	"errors"
	"fmt"
	"sync"
	"time"
)

// Interval is a canonical bar interval token, lower case as in the layout and
// the raw store.
type Interval string

// The intervals canonical supports (ADR-090): the ones the archive has and the
// engine reads.
const (
	H1 Interval = "h1"
	D1 Interval = "d1"
)

// ErrUnsupportedInterval reports an interval other than H1 or D1.
var ErrUnsupportedInterval = errors.New("canonical: unsupported interval")

// ErrMisaligned reports a time that is not on its interval's boundary.
var ErrMisaligned = errors.New("canonical: time is not on the interval boundary")

// PolicyScheduledCloseV1 names the availability rule: a bar is available at its
// scheduled end, the next interval boundary after it opens. H1 boundaries are
// the hour; D1 boundaries are 17:00 America/New_York, so a day is 23 or 25 hours
// across a daylight-saving change. It is a modeled time, not an observed one.
// Changing the rule means a new policy name (ADR-090).
const PolicyScheduledCloseV1 = "scheduled-close-v1"

// Valid reports whether i is H1 or D1.
func (i Interval) Valid() bool { return i == H1 || i == D1 }

// sessionZone is the zone OANDA's daily bar is aligned to (17:00 local).
const sessionZone = "America/New_York"

// sessionHour is the local hour a daily bar opens.
const sessionHour = 17

// newYorkTZif is the America/New_York zone, pinned in this repository: the
// entry of Go 1.26's lib/time/zoneinfo.zip, 1744 bytes with the rule for years
// beyond the table in its footer (EST5EDT,M3.2.0,M11.1.0). It is loaded from
// these bytes, never by name, so neither the host's zoneinfo, ZONEINFO nor TZ can
// change an availability time. The rule's version is its policy name: replacing
// these bytes means a new PolicyScheduledCloseV2 and a rebuild (ADR-090), and a
// test pins their hash so they cannot change silently.
//
//go:embed newyork.tzif
var newYorkTZif []byte

func loadNewYork() (*time.Location, error) {
	return time.LoadLocationFromTZData(sessionZone, newYorkTZif)
}

var newYork = sync.OnceValues(loadNewYork)

// Aligned reports whether t is exactly on an interval boundary, where a bar of
// that interval opens: on the hour for H1, 17:00 America/New_York for D1.
func Aligned(i Interval, t time.Time) (bool, error) {
	switch i {
	case H1:
		return t.Equal(t.UTC().Truncate(time.Hour)), nil
	case D1:
		ny, err := newYork()
		if err != nil {
			return false, fmt.Errorf("canonical: %w", err)
		}
		l := t.In(ny)
		return l.Hour() == sessionHour && l.Minute() == 0 && l.Second() == 0 && l.Nanosecond() == 0, nil
	default:
		return false, fmt.Errorf("%w: %q", ErrUnsupportedInterval, string(i))
	}
}

// AvailableAt returns when a bar of interval i that opens at open becomes
// available under PolicyScheduledCloseV1: the next interval boundary. open must
// be aligned (ErrMisaligned otherwise).
func AvailableAt(i Interval, open time.Time) (time.Time, error) {
	ok, err := Aligned(i, open)
	if err != nil {
		return time.Time{}, err
	}
	if !ok {
		return time.Time{}, fmt.Errorf("%w: %s is not a %s boundary", ErrMisaligned, open.UTC().Format(time.RFC3339), string(i))
	}
	if i == H1 {
		return open.Add(time.Hour).UTC(), nil
	}
	ny, err := newYork()
	if err != nil {
		return time.Time{}, fmt.Errorf("canonical: %w", err)
	}
	l := open.In(ny)
	return time.Date(l.Year(), l.Month(), l.Day()+1, sessionHour, 0, 0, 0, ny).UTC(), nil
}
