package canonical

import (
	"bufio"
	"bytes"
	_ "embed" // usEquityCalendarCSV
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rustyeddy/trader/marketdata"
)

// PolicyUSEquityCloseV2 names the availability rule of US equity daily bars
// (ADR-095): a bar opens at 09:30 America/New_York on its exchange date and is
// available at the scheduled stock close, 16:00, or 13:00 on a day the calendar
// lists as an early close, except that a session before 2009-03-01 is available
// 15 minutes after that close. The funds built here traded until 16:15 on the
// American Stock Exchange (QQQ until December 2004, SPY until late February 2009),
// and which close a source's bar of those years holds is not established, so the
// later one is assumed: a bar is never usable before its prices could exist. v1,
// which had no delay, is retired. All times are modeled, not observed;
// unscheduled late openings and halts are not modeled. Changing the times means a
// new policy name; changing the dates means a new calendar version.
const PolicyUSEquityCloseV2 = "us-equity-close-v2"

// lateFundCloseDelay is how long after the stock close a session before
// lateFundCloseEnd is available.
const lateFundCloseDelay = 15 * time.Minute

// lateFundCloseEnd is the first exchange date (UTC midnight) without the delay.
var lateFundCloseEnd = time.Date(2009, time.March, 1, 0, 0, 0, 0, time.UTC)

// CalendarUSEquityV2 names the embedded table of full closures and early closes,
// 1999-01-01 to 2026-12-31. It carries the dates of v1 (2005 on) unchanged and
// adds the earlier years; files built under v1 are stale and are rebuilt.
const CalendarUSEquityV2 = "us-equity-calendar-v2"

// The years CalendarUSEquityV2 covers, inclusive.
const (
	equityFirstYear = 1999
	equityLastYear  = 2026
)

var (
	// ErrOutsideCalendar reports a date the calendar does not cover.
	ErrOutsideCalendar = errors.New("canonical: date is outside the calendar")
	// ErrNonSession reports a date on which the exchange held no session: a
	// weekend or a listed full closure.
	ErrNonSession = errors.New("canonical: no trading session on this date")
)

// Session is one regular trading session, in UTC.
type Session struct {
	// Open is when the session opens: the Time of its daily bar.
	Open time.Time
	// Close is when the stock session is scheduled to close.
	Close time.Time
	// Available is the AvailableAt of its daily bar under PolicyUSEquityCloseV2:
	// Close, or 15 minutes after it for a session before 2009-03-01.
	Available time.Time
	// Early reports a 13:00 close.
	Early bool
}

//go:embed us_equity_calendar_v2.csv
var usEquityCalendarCSV []byte

// dayKind is what the calendar says about a listed date.
type dayKind string

const (
	dayClosed dayKind = "closed"
	dayEarly  dayKind = "early"
)

var usEquityCalendar = sync.OnceValues(func() (map[string]dayKind, error) {
	return parseEquityCalendar(usEquityCalendarCSV)
})

// parseEquityCalendar reads the table: comment lines starting with '#', among
// them one "# source <key> <url>" per publication, then the header
// date,kind,source and one row per date. It is strict, since a wrong entry would
// silently move or excuse a bar: dates must be weekdays inside the covered years,
// ascending and listed once, the kind known and the source declared.
func parseEquityCalendar(data []byte) (map[string]dayKind, error) {
	out := map[string]dayKind{}
	sources := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	var prev string
	sawHeader := false
	n := 0
	for sc.Scan() {
		n++
		bad := func(format string, args ...any) error {
			return fmt.Errorf("equity calendar line %d: %s", n, fmt.Sprintf(format, args...))
		}
		line := strings.TrimSpace(sc.Text())
		if rest, ok := strings.CutPrefix(line, "# source "); ok {
			f := strings.Fields(rest)
			if len(f) != 2 || sources[f[0]] {
				return nil, bad("a source needs a new key and a URL")
			}
			sources[f[0]] = true
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !sawHeader {
			if line != "date,kind,source" {
				return nil, bad("want the header date,kind,source")
			}
			sawHeader = true
			continue
		}
		f := strings.Split(line, ",")
		if len(f) != 3 {
			return nil, bad("want 3 fields, got %d", len(f))
		}
		date, err := time.Parse(time.DateOnly, f[0])
		if err != nil {
			return nil, bad("date: %v", err)
		}
		switch kind := dayKind(f[1]); {
		case date.Year() < equityFirstYear || date.Year() > equityLastYear:
			return nil, bad("%s is outside %d to %d", f[0], equityFirstYear, equityLastYear)
		case isWeekend(date):
			return nil, bad("%s is a weekend", f[0])
		case f[0] <= prev:
			return nil, bad("%s is not after the date before it", f[0])
		case kind != dayClosed && kind != dayEarly:
			return nil, bad("kind %q is not closed or early", f[1])
		case !sources[f[2]]:
			return nil, bad("source %q is not declared", f[2])
		default:
			out[f[0]], prev = kind, f[0]
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if !sawHeader {
		return nil, errors.New("equity calendar: no header")
	}
	return out, nil
}

func isWeekend(t time.Time) bool {
	w := t.Weekday()
	return w == time.Saturday || w == time.Sunday
}

// USEquitySession returns the session of the exchange date year-month-day under
// PolicyUSEquityCloseV2 and CalendarUSEquityV2. It reports ErrOutsideCalendar
// for a date the calendar does not cover, including one that does not exist, and
// ErrNonSession for a weekend or full closure.
func USEquitySession(year int, month time.Month, day int) (Session, error) {
	date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	text := date.Format(time.DateOnly)
	if date.Year() != year || date.Month() != month || date.Day() != day || year < equityFirstYear || year > equityLastYear {
		return Session{}, fmt.Errorf("%w: %04d-%02d-%02d, %s covers %d to %d", ErrOutsideCalendar,
			year, int(month), day, CalendarUSEquityV2, equityFirstYear, equityLastYear)
	}
	days, err := usEquityCalendar()
	if err != nil {
		return Session{}, fmt.Errorf("canonical: %w", err)
	}
	if isWeekend(date) || days[text] == dayClosed {
		return Session{}, fmt.Errorf("%w: %s", ErrNonSession, text)
	}
	ny, err := newYork()
	if err != nil {
		return Session{}, fmt.Errorf("canonical: %w", err)
	}
	s := Session{Open: time.Date(year, month, day, 9, 30, 0, 0, ny).UTC(), Early: days[text] == dayEarly}
	closeHour := 16
	if s.Early {
		closeHour = 13
	}
	s.Close = time.Date(year, month, day, closeHour, 0, 0, 0, ny).UTC()
	s.Available = s.Close
	if date.Before(lateFundCloseEnd) {
		s.Available = s.Close.Add(lateFundCloseDelay)
	}
	return s, nil
}

// USEquitySessionAt returns the session whose daily bar opens at open. It reports
// ErrMisaligned when open is not 09:30 New York on its date, and the errors of
// USEquitySession for that date.
func USEquitySessionAt(open time.Time) (Session, error) {
	ny, err := newYork()
	if err != nil {
		return Session{}, fmt.Errorf("canonical: %w", err)
	}
	y, m, d := open.In(ny).Date()
	s, err := USEquitySession(y, m, d)
	if err != nil {
		return Session{}, err
	}
	if !open.Equal(s.Open) {
		return Session{}, fmt.Errorf("%w: %s is not the session open %s", ErrMisaligned,
			open.UTC().Format(time.RFC3339), s.Open.Format(time.RFC3339))
	}
	return s, nil
}

// USEquityGaps returns the interior gaps of US equity daily bars, which must be
// in ascending order and open at their sessions' opens: every run of sessions
// with no bar between two stored bars. Weekends and full closures are not gaps
// and do not split a run. Each gap's Closure is Open, meaning the calendar does
// not explain it, and its Bars counts the missing sessions; it starts at the
// first missing session's open and ends at the next stored bar's. As with Gaps,
// nothing before the first or after the last bar is judged. Bars out of order are
// not detected.
func USEquityGaps(bars []marketdata.Bar) ([]Gap, error) {
	var gaps []Gap
	ny, err := newYork()
	if err != nil {
		return nil, fmt.Errorf("canonical: %w", err)
	}
	for n := range bars {
		if _, err := USEquitySessionAt(bars[n].Time); err != nil {
			return nil, err
		}
		if n == 0 {
			continue
		}
		var gap Gap
		y, m, d := bars[n-1].Time.In(ny).Date()
		for day := time.Date(y, m, d+1, 12, 0, 0, 0, ny); ; day = day.AddDate(0, 0, 1) {
			s, err := USEquitySession(day.Year(), day.Month(), day.Day())
			if errors.Is(err, ErrNonSession) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if !s.Open.Before(bars[n].Time) {
				break
			}
			if gap.Bars == 0 {
				gap = Gap{Start: s.Open, Closure: Open}
			}
			gap.Bars++
		}
		if gap.Bars > 0 {
			gap.End = bars[n].Time.UTC()
			gaps = append(gaps, gap)
		}
	}
	return gaps, nil
}
