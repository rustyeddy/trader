package marketdata

import "time"

// StandardUSEquityHolidays returns the Holidays and HalfDays for the
// given calendar years, ready to pass to NewUSEquityCalendar: the ten
// standard NYSE/Nasdaq full-closure holidays (New Year's Day, Martin
// Luther King Jr. Day, Washington's Birthday, Good Friday, Memorial
// Day, Juneteenth National Independence Day, Independence Day, Labor
// Day, Thanksgiving Day, and Christmas Day), each shifted per the
// standard "observed" rule (a holiday falling on Saturday is observed
// the preceding Friday; falling on Sunday, the following Monday), plus
// one representative half day (the day after Thanksgiving) — issue
// #296's own "at least representative early-close dates" requirement.
//
// Juneteenth is included only for years >= 2022, the first year NYSE
// actually observed it as a market holiday — it must not be backdated
// to earlier years this function is asked to cover.
//
// This is deliberately not exhaustive: Christmas Eve and July 3rd
// early closes (when they fall on a business day) are real,
// occasionally-observed NYSE half days too, but their application has
// varied by year and is not a fixed rule the way the day after
// Thanksgiving is — they are a named, deferred refinement, not silently
// assumed either way. USEquityCalendar itself has no built-in holiday
// data (mirroring FXCalendar/StandardFXHolidays' own split) — a caller
// needing a different or extended rule set can build
// USEquityCalendarParams directly.
func StandardUSEquityHolidays(years ...int) USEquityCalendarParams {
	holidays := make([]time.Time, 0, 10*len(years))
	halfDays := make([]HalfDay, 0, len(years))

	for _, y := range years {
		holidays = append(holidays,
			observed(y, time.January, 1),
			nthWeekday(y, time.January, time.Monday, 3),  // MLK Day
			nthWeekday(y, time.February, time.Monday, 3), // Washington's Birthday
			goodFriday(y),
			lastWeekday(y, time.May, time.Monday), // Memorial Day
		)
		if y >= 2022 {
			holidays = append(holidays, observed(y, time.June, 19)) // Juneteenth
		}
		holidays = append(holidays,
			observed(y, time.July, 4),
			nthWeekday(y, time.September, time.Monday, 1), // Labor Day
		)
		thanksgiving := nthWeekday(y, time.November, time.Thursday, 4)
		holidays = append(holidays, thanksgiving, observed(y, time.December, 25))

		halfDays = append(halfDays, HalfDay{
			Date:    thanksgiving.AddDate(0, 0, 1), // the Friday after Thanksgiving
			CloseAt: 13 * time.Hour,
		})
	}
	return USEquityCalendarParams{Holidays: holidays, HalfDays: halfDays}
}

// civilUTC returns a time.Time whose literal Year/Month/Day fields are
// year/month/day, for use with Holidays' and HalfDay.Date's
// literal-date convention — USEquityCalendar's UTC-anchored analogue of
// FXCalendar's civilNY.
func civilUTC(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// observed returns the given calendar date, shifted per the standard
// "observed" rule: a date falling on Saturday is observed the
// preceding Friday, and one falling on Sunday is observed the
// following Monday.
func observed(year int, month time.Month, day int) time.Time {
	d := civilUTC(year, month, day)
	switch d.Weekday() {
	case time.Saturday:
		return d.AddDate(0, 0, -1)
	case time.Sunday:
		return d.AddDate(0, 0, 1)
	default:
		return d
	}
}

// nthWeekday returns the nth occurrence of weekday in (year, month) —
// for example nthWeekday(2024, time.November, time.Thursday, 4) is
// Thanksgiving Day 2024.
func nthWeekday(year int, month time.Month, weekday time.Weekday, n int) time.Time {
	first := civilUTC(year, month, 1)
	offset := (int(weekday) - int(first.Weekday()) + 7) % 7
	return first.AddDate(0, 0, offset+7*(n-1))
}

// lastWeekday returns the last occurrence of weekday in (year, month) —
// for example lastWeekday(2024, time.May, time.Monday) is Memorial Day
// 2024.
func lastWeekday(year int, month time.Month, weekday time.Weekday) time.Time {
	// The first day of the following month, minus one day, is the
	// month's last day; walking backward from there to weekday can
	// never cross into the previous month, since a month always holds
	// at least four full weeks.
	next := civilUTC(year, month+1, 1)
	if month == time.December {
		next = civilUTC(year+1, time.January, 1)
	}
	last := next.AddDate(0, 0, -1)
	offset := (int(last.Weekday()) - int(weekday) + 7) % 7
	return last.AddDate(0, 0, -offset)
}

// goodFriday returns Good Friday's date for year: two days before
// Easter Sunday, computed via the anonymous Gregorian algorithm
// (Meeus/Jones/Butcher), the standard integer-arithmetic method for
// the Gregorian Easter date.
func goodFriday(year int) time.Time {
	a := year % 19
	b := year / 100
	c := year % 100
	d := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i := c / 4
	k := c % 4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1

	easter := civilUTC(year, time.Month(month), day)
	return easter.AddDate(0, 0, -2)
}
