package market

import "time"

var newYorkLoc = func() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.UTC
	}
	return loc
}()

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
