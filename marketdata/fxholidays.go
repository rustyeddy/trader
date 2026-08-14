package marketdata

import "time"

// StandardFXHolidays returns the Holidays and PartialClosures for the
// given calendar years, ready to pass to NewFXCalendar: New Year's Day,
// Christmas Day, and Boxing Day as full-session closures, and Christmas
// Eve and New Year's Eve as partial closures from 13:00 New York time.
//
// This is Trader's accepted M2 FX holiday rule set (issue #74, ADR-020),
// carried forward unchanged from
// trader-first-try/market/forex_hours.go's isMajorForexHolidayClosed —
// the one holiday source Trader had already exercised against real
// OANDA timestamps. It is deliberately narrow: no Good Friday, no
// in-lieu observance when a holiday falls on a weekend, and no
// venue/asset-class holidays beyond FX. Extending it is a separate,
// documented decision, not a silent addition here.
//
// FXCalendar itself stays a pure calendar — it has no built-in holiday
// data of its own — so a caller who needs a custom or extended holiday
// set can build FXCalendarParams directly instead of calling this
// function, or call it and append further entries to the result.
func StandardFXHolidays(years ...int) FXCalendarParams {
	holidays := make([]time.Time, 0, 3*len(years))
	partial := make([]PartialClosure, 0, 2*len(years))
	for _, y := range years {
		holidays = append(holidays,
			civilNY(y, time.January, 1),
			civilNY(y, time.December, 25),
			civilNY(y, time.December, 26),
		)
		partial = append(partial,
			PartialClosure{Date: civilNY(y, time.December, 24), From: 13 * time.Hour},
			PartialClosure{Date: civilNY(y, time.December, 31), From: 13 * time.Hour},
		)
	}
	return FXCalendarParams{Holidays: holidays, PartialClosures: partial}
}

// civilNY returns a time.Time whose literal Year/Month/Day fields are
// year/month/day, for use with Holidays' and PartialClosure.Date's
// literal-date convention. The New York location is a natural, though
// not load-bearing, choice: Holidays and PartialClosure.Date deliberately
// ignore Location and read the calendar fields as given.
func civilNY(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, newYorkLocation)
}
