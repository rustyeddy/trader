package canonical

import (
	"fmt"
	"time"

	"github.com/rustyeddy/trader/marketdata"
)

// Closure says whether the FX market was expected to be trading when a bar of a
// given interval would have opened.
type Closure int

// The closures.
const (
	// Open: the market trades and a bar is expected.
	Open Closure = iota
	// Weekend: the weekly close, Friday 17:00 to Sunday 17:00 New York. Not
	// reported: every week has one.
	Weekend
	// Holiday: the year-end closure around December 25 and January 1, whose exact
	// hours differ from year to year. Reported, not an error.
	Holiday
	// KnownHole: a bar the provider is known not to have (known_holes.csv).
	// Reported, not an error. ClosureAt never returns it: it comes from the list,
	// not the calendar.
	KnownHole
)

// ClosureAt classifies the bar that would open at open (aligned to i). It is the
// calendar of ADR-090's gap policy: a small pure function, not an interface, and
// like the availability rule it uses the pinned New York zone.
//
// The weekly close is exact for the intervals canonical supports: an H1 bar does
// not open from Friday 17:00 until Sunday 17:00, and the D1 bar that would open on
// a Friday or Saturday at 17:00 does not exist.
//
// The holiday windows are deliberately loose. The archive's year-end gaps run from
// an early close on December 24 or 31 (as early as 14:00 in some years) to a late
// reopening on December 26 or January 2 (up to 07:00 in 2012), and a Sunday
// holiday delays the Sunday reopening by a day. A window therefore covers
// December 24 12:00 to December 26 17:59 and December 31 12:00 to January 2 17:59,
// New York time. A bar missing inside one is a known closure; one missing anywhere
// else is not, however short the gap. The windows are checked against the OANDA
// archive of 2005 to 2026 (every year-end gap of H1 and D1 falls inside one, and
// nothing else in 2013 to 2026 is a holiday); another provider's calendar is a new
// rule, not an edit to this one.
func ClosureAt(i Interval, open time.Time) (Closure, error) {
	ny, err := newYork()
	if err != nil {
		return Open, fmt.Errorf("canonical: %w", err)
	}
	l := open.In(ny)
	switch i {
	case H1:
		if w := l.Weekday(); w == time.Saturday || (w == time.Friday && l.Hour() >= sessionHour) || (w == time.Sunday && l.Hour() < sessionHour) {
			return Weekend, nil
		}
	case D1:
		if w := l.Weekday(); w == time.Friday || w == time.Saturday {
			return Weekend, nil
		}
	default:
		return Open, fmt.Errorf("%w: %q", ErrUnsupportedInterval, string(i))
	}
	// month, day and hour as one number orders the dates within a year.
	k := int(l.Month())*10000 + l.Day()*100 + l.Hour()
	if (k >= 122412 && k < 122618) || k >= 123112 || k < 10218 {
		return Holiday, nil
	}
	return Open, nil
}

// Gap is a run of bars that were expected and are absent between two bars that are
// present. A gap lies strictly between two stored bars: nothing before the first
// or after the last is judged, since a missing last bar may be a candle still
// forming (ADR-090 section 3).
type Gap struct {
	// Start is when the first missing bar would have opened. End is when the bar
	// after the last missing one would open: the next stored bar, or the start of
	// a weekend or of another kind of gap.
	Start, End time.Time
	// Bars counts the missing bars.
	Bars int
	// Closure is Holiday for a gap inside a holiday window and Open for one the
	// calendar does not explain.
	Closure Closure
}

// Gaps returns the interior gaps of bars, which must be in ascending order and
// aligned to i: every run of consecutive missing bars between two stored bars,
// after dropping the ones the weekly close explains. A run is split where its
// class changes, so a gap that begins in a holiday and ends outside one is two
// gaps.
func Gaps(i Interval, bars []marketdata.Bar) ([]Gap, error) {
	return GapsWith(i, bars, nil)
}

// GapsWith is Gaps where a missing bar the calendar does not explain is classified
// KnownHole when known says the provider does not have it (nil: none are). The
// calendar still wins: a missing bar in the weekly close or a holiday window is
// not a hole. known is asked with the opening time of each such bar.
func GapsWith(i Interval, bars []marketdata.Bar, known func(open time.Time) (bool, error)) ([]Gap, error) {
	var gaps []Gap
	for n := 1; n < len(bars); n++ {
		next := bars[n].Time
		open := false // the last missing bar extends gaps[len-1]
		t, err := nextOpen(i, bars[n-1].Time)
		for ; err == nil && t.Before(next); t, err = nextOpen(i, t) {
			c, cerr := ClosureAt(i, t)
			if cerr != nil {
				return nil, cerr
			}
			if c == Open && known != nil {
				hole, herr := known(t)
				if herr != nil {
					return nil, herr
				}
				if hole {
					c = KnownHole
				}
			}
			if c == Weekend {
				open = false
				continue
			}
			end, eerr := nextOpen(i, t)
			if eerr != nil {
				return nil, eerr
			}
			if last := len(gaps) - 1; open && gaps[last].Closure == c {
				gaps[last].Bars++
				gaps[last].End = end
				continue
			}
			gaps = append(gaps, Gap{Start: t, End: end, Bars: 1, Closure: c})
			open = true
		}
		if err != nil {
			return nil, err
		}
	}
	return gaps, nil
}

// nextOpen is when the bar after the one opening at open opens.
func nextOpen(i Interval, open time.Time) (time.Time, error) {
	if i == H1 {
		return open.Add(time.Hour).UTC(), nil
	}
	return AvailableAt(i, open)
}
