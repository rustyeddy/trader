package marketdata

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testUSEquityCal(t *testing.T) *USEquityCalendar {
	t.Helper()
	years := make([]int, 0, 10)
	for y := 2015; y <= 2026; y++ {
		years = append(years, y)
	}
	return NewUSEquityCalendar(StandardUSEquityHolidays(years...))
}

// midSession returns 15:00 UTC on the given date — safely inside the
// real regular session (9:30-16:00 America/New_York) regardless of
// EST/EDT: 15:00 UTC is 10:00 EST or 11:00 EDT, either way well within
// [9:30, 16:00) local. Every "is this an ordinary open trading day"
// probe in this file uses this helper rather than noon UTC, which sits
// before the 9:30 open (14:30 UTC in winter, 13:30 UTC in summer) and
// would now honestly report StatusClosed even on a real trading day
// (PR #310 review).
func midSession(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 15, 0, 0, 0, time.UTC)
}

func TestUSEquityCalendar_OrdinaryWeekdayIsOpen(t *testing.T) {
	cal := testUSEquityCal(t)
	// 2020-01-02 is an ordinary Thursday.
	assert.Equal(t, StatusOpen, cal.Status(midSession(2020, 1, 2)))
}

// TestUSEquityCalendar_OutsideRegularHoursIsClosed is the central proof
// for PR #310's review finding: Status/Session must be honest about
// real regular-session clock hours, not merely about which calendar
// date t falls on. An ordinary Thursday's overnight/pre-market/
// after-hours instants must all report StatusClosed, even though the
// same calendar date is, at the day-label level, an open trading day.
func TestUSEquityCalendar_OutsideRegularHoursIsClosed(t *testing.T) {
	cal := testUSEquityCal(t)
	// 2020-01-08 is an ordinary Wednesday, far enough from New Year's
	// Day that neither it nor the preceding evening's NY-local date is
	// a holiday — isolating this test to the pure clock-hours question,
	// not a holiday-boundary interaction.
	tests := []struct {
		name string
		t    time.Time
	}{
		{"midnight UTC (Bar's own D1 label instant)", time.Date(2020, 1, 8, 0, 0, 0, 0, time.UTC)},
		{"3am America/New_York", time.Date(2020, 1, 8, 8, 0, 0, 0, time.UTC)},                           // 03:00 EST
		{"before the 9:30 open", time.Date(2020, 1, 8, 14, 0, 0, 0, time.UTC)},                          // 09:00 EST
		{"exactly the 16:00 close (half-open, excluded)", time.Date(2020, 1, 8, 21, 0, 0, 0, time.UTC)}, // 16:00 EST
		{"10pm America/New_York", time.Date(2020, 1, 9, 3, 0, 0, 0, time.UTC)},                          // 22:00 EST the same trading day
	}
	for _, tt := range tests {
		assert.Equal(t, StatusClosed, cal.Status(tt.t), tt.name)
		_, ok := cal.Session(tt.t)
		assert.False(t, ok, "%s: Session must report ok=false outside regular hours", tt.name)
	}
}

func TestUSEquityCalendar_WeekendIsClosed(t *testing.T) {
	cal := testUSEquityCal(t)
	assert.Equal(t, StatusClosed, cal.Status(midSession(2020, 1, 4))) // Saturday
	assert.Equal(t, StatusClosed, cal.Status(midSession(2020, 1, 5))) // Sunday
}

func TestUSEquityCalendar_FixedDateHolidays(t *testing.T) {
	cal := testUSEquityCal(t)
	tests := []struct {
		name string
		date time.Time
	}{
		{"New Year's Day 2020", midSession(2020, 1, 1)},
		{"Independence Day 2019", midSession(2019, 7, 4)},
		{"Christmas 2019", midSession(2019, 12, 25)},
		{"Juneteenth 2023", midSession(2023, 6, 19)},
	}
	for _, tt := range tests {
		assert.Equal(t, StatusHoliday, cal.Status(tt.date), tt.name)
	}
}

// TestUSEquityCalendar_JuneteenthNotObservedBeforeNYSEAdoption confirms
// Juneteenth is not backdated to years before NYSE actually began
// observing it (2022): 2021-06-18 (the Friday NYSE was actually open,
// since June 19 2021 fell on a Saturday and there was no observance
// that year) must be an ordinary trading day.
func TestUSEquityCalendar_JuneteenthNotObservedBeforeNYSEAdoption(t *testing.T) {
	cal := testUSEquityCal(t)
	assert.Equal(t, StatusOpen, cal.Status(midSession(2021, 6, 18)))
}

func TestUSEquityCalendar_FloatingHolidays(t *testing.T) {
	cal := testUSEquityCal(t)
	tests := []struct {
		name string
		date time.Time
	}{
		{"MLK Day 2020 (Jan 20)", midSession(2020, 1, 20)},
		{"Washington's Birthday 2020 (Feb 17)", midSession(2020, 2, 17)},
		{"Good Friday 2020 (Apr 10)", midSession(2020, 4, 10)},
		{"Memorial Day 2020 (May 25)", midSession(2020, 5, 25)},
		{"Labor Day 2020 (Sep 7)", midSession(2020, 9, 7)},
		{"Thanksgiving 2020 (Nov 26)", midSession(2020, 11, 26)},
	}
	for _, tt := range tests {
		assert.Equal(t, StatusHoliday, cal.Status(tt.date), tt.name)
	}
}

// TestUSEquityCalendar_ObservedWeekendShift confirms the standard
// "observed" rule for a holiday that is *not* New Year's Day:
// Independence Day 2021 fell on a Sunday, so it was observed the
// following Monday (2021-07-05).
func TestUSEquityCalendar_ObservedWeekendShift(t *testing.T) {
	cal := testUSEquityCal(t)
	assert.Equal(t, StatusHoliday, cal.Status(midSession(2021, 7, 5)),
		"Independence Day 2021 (Sunday) observed the following Monday")
}

// TestUSEquityCalendar_NewYearSaturdayNotObservedOnPrecedingFriday is
// the corrected version of what PR #310's review found wrong: NYSE
// does *not* observe a Saturday New Year's Day on the preceding
// Friday, unlike every other holiday (see observed's own doc comment
// in usequityholidays.go for the year-end-accounting reason). January
// 1, 2022 fell on a Saturday; NYSE traded normally on Friday,
// December 31, 2021.
func TestUSEquityCalendar_NewYearSaturdayNotObservedOnPrecedingFriday(t *testing.T) {
	cal := testUSEquityCal(t)
	assert.Equal(t, StatusOpen, cal.Status(midSession(2021, 12, 31)),
		"December 31, 2021 was a real, ordinary NYSE trading day, not an observed New Year's closure")
	assert.Equal(t, StatusClosed, cal.Status(midSession(2022, 1, 1)),
		"January 1, 2022 (Saturday) is an ordinary weekend closure (StatusClosed), not StatusHoliday")
}

// TestUSEquityCalendar_OrdinarySaturdayHolidayStillShiftsToFriday
// confirms the New Year's exception is genuinely New-Year-specific and
// does not disturb the ordinary Saturday->Friday shift for any other
// holiday: Independence Day 2020 fell on a Saturday, and was observed
// on Friday, July 3, 2020.
func TestUSEquityCalendar_OrdinarySaturdayHolidayStillShiftsToFriday(t *testing.T) {
	cal := testUSEquityCal(t)
	require.Equal(t, time.Saturday, time.Date(2020, 7, 4, 0, 0, 0, 0, time.UTC).Weekday(),
		"test assumption: July 4, 2020 is a Saturday")
	assert.Equal(t, StatusHoliday, cal.Status(midSession(2020, 7, 3)),
		"Independence Day 2020 (Saturday) observed the preceding Friday")
	assert.Equal(t, StatusClosed, cal.Status(midSession(2020, 7, 4)),
		"the actual Saturday date itself is an ordinary weekend closure, not StatusHoliday twice")
}

// TestUSEquityCalendar_HalfDayIsOpenNotHoliday confirms a half day
// (the day after Thanksgiving) is a full, open trading day at the D1
// bar-label level — not conflated with a full closure — while Status/
// Session honor its early close directly, and IsHalfDay still exposes
// it explicitly (issue #296's own "early-close/half-day behavior"
// requirement).
func TestUSEquityCalendar_HalfDayIsOpenNotHoliday(t *testing.T) {
	cal := testUSEquityCal(t)
	dayAfterThanksgiving2020 := midSession(2020, 11, 27)

	assert.Equal(t, StatusOpen, cal.Status(dayAfterThanksgiving2020))
	closeAt, ok := cal.IsHalfDay(dayAfterThanksgiving2020)
	require.True(t, ok)
	assert.Equal(t, 13*time.Hour, closeAt)

	// An ordinary trading day is not a half day.
	_, ok = cal.IsHalfDay(midSession(2020, 11, 30))
	assert.False(t, ok)
}

// TestUSEquityCalendar_HalfDayClosesEarly confirms Status/Session
// actually honor a configured HalfDay's early close, not merely
// report it via IsHalfDay: 13:30 EST (30 minutes after the 1:00pm half
// day close) must be StatusClosed, even though the same clock time on
// an ordinary full session day is well within regular hours.
func TestUSEquityCalendar_HalfDayClosesEarly(t *testing.T) {
	cal := testUSEquityCal(t)
	afterHalfDayClose := time.Date(2020, 11, 27, 18, 30, 0, 0, time.UTC) // 13:30 EST
	assert.Equal(t, StatusClosed, cal.Status(afterHalfDayClose))

	beforeHalfDayClose := time.Date(2020, 11, 27, 17, 0, 0, 0, time.UTC) // 12:00 EST
	assert.Equal(t, StatusOpen, cal.Status(beforeHalfDayClose))
	session, ok := cal.Session(beforeHalfDayClose)
	require.True(t, ok)
	assert.True(t, session.End().Equal(time.Date(2020, 11, 27, 18, 0, 0, 0, time.UTC)),
		"session must end at the half day's 13:00 EST close (18:00 UTC), not the ordinary 16:00 close")
}

// TestUSEquityCalendar_BarD1AlignsToMidnightUTC confirms the deliberate
// midnight-UTC anchor (not America/New_York, unlike FXCalendar): this
// is what lets a Stooq bar's own Time (also midnight UTC, per
// stooq/reader.go's time.Parse("2006-01-02", ...)) fall exactly on
// Bar's own returned boundary.
func TestUSEquityCalendar_BarD1AlignsToMidnightUTC(t *testing.T) {
	cal := testUSEquityCal(t)
	span, err := cal.Bar(time.Date(2020, 1, 2, 15, 30, 0, 0, time.UTC), D1)
	require.NoError(t, err)
	assert.True(t, span.Start().Equal(time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)))
	assert.True(t, span.End().Equal(time.Date(2020, 1, 3, 0, 0, 0, 0, time.UTC)))
}

func TestUSEquityCalendar_BarRejectsUnsupportedIntervals(t *testing.T) {
	cal := testUSEquityCal(t)
	for _, iv := range []Interval{M1, H1, H4, W1} {
		_, err := cal.Bar(time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC), iv)
		assert.Error(t, err, iv.String())
	}
}

// TestUSEquityCalendar_ClassifyBarSpan is the actual mechanism
// interval_state.go's uniformStatus uses in place of endpoint sampling
// for this calendar (PR #310's fix for the review's architectural
// finding): it classifies a whole Bar(t, D1)-shaped labeled day
// directly, using the labeled UTC calendar date, not Status sampled at
// the bar's own (out-of-session) literal endpoints.
func TestUSEquityCalendar_ClassifyBarSpan(t *testing.T) {
	cal := testUSEquityCal(t)

	// 2020-01-08 (Wednesday), not 2020-01-02: far enough from New
	// Year's Day that the bar's own midnight-UTC instant does not fall
	// on the preceding evening of a holiday (see
	// TestUSEquityCalendar_OutsideRegularHoursIsClosed's own doc
	// comment for the same reasoning).
	openDay, err := cal.Bar(time.Date(2020, 1, 8, 0, 0, 0, 0, time.UTC), D1)
	require.NoError(t, err)
	status, err := cal.ClassifyBarSpan(openDay)
	require.NoError(t, err)
	assert.Equal(t, StatusOpen, status, "an ordinary Wednesday's whole labeled day is Open")

	weekend, err := cal.Bar(time.Date(2020, 1, 4, 0, 0, 0, 0, time.UTC), D1) // Saturday
	require.NoError(t, err)
	status, err = cal.ClassifyBarSpan(weekend)
	require.NoError(t, err)
	assert.Equal(t, StatusClosed, status)

	holiday, err := cal.Bar(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), D1) // New Year's Day
	require.NoError(t, err)
	status, err = cal.ClassifyBarSpan(holiday)
	require.NoError(t, err)
	assert.Equal(t, StatusHoliday, status)

	halfDay, err := cal.Bar(time.Date(2020, 11, 27, 0, 0, 0, 0, time.UTC), D1) // day after Thanksgiving
	require.NoError(t, err)
	status, err = cal.ClassifyBarSpan(halfDay)
	require.NoError(t, err)
	assert.Equal(t, StatusOpen, status, "a half day is still one ordinary open labeled trading day")

	// Sampling Status at the bar span's own literal endpoints would
	// have reported StatusClosed for every one of the open cases above
	// (they are all out-of-session instants) — confirm that directly,
	// so this test fails loudly if ClassifyBarSpan is ever accidentally
	// bypassed rather than merely happening to agree with Status today.
	assert.Equal(t, StatusClosed, cal.Status(openDay.Start()),
		"sanity check: the bar's own literal start (midnight UTC) is genuinely out of session")
}

func TestUSEquityCalendar_SessionFalseWhenClosed(t *testing.T) {
	cal := testUSEquityCal(t)
	_, ok := cal.Session(midSession(2020, 1, 1))
	assert.False(t, ok)
}

func TestUSEquityCalendar_NoHolidayDataByDefault(t *testing.T) {
	cal := NewUSEquityCalendar(USEquityCalendarParams{})
	// With no configured holidays, every weekday is open during regular
	// hours — the calendar itself carries no built-in holiday data
	// (mirroring FXCalendar's own "no holiday data ships with Trader
	// itself" split).
	assert.Equal(t, StatusOpen, cal.Status(midSession(2020, 1, 1)))
}

func TestStandardUSEquityHolidays_GoodFridayKnownDates(t *testing.T) {
	// Independently-verifiable real Good Friday dates.
	want := map[int]time.Time{
		2024: time.Date(2024, 3, 29, 0, 0, 0, 0, time.UTC),
		2025: time.Date(2025, 4, 18, 0, 0, 0, 0, time.UTC),
		2026: time.Date(2026, 4, 3, 0, 0, 0, 0, time.UTC),
	}
	for year, wantDate := range want {
		cal := NewUSEquityCalendar(StandardUSEquityHolidays(year))
		assert.Equal(t, StatusHoliday, cal.Status(midSession(wantDate.Year(), wantDate.Month(), wantDate.Day())), year)
		dayBefore := wantDate.AddDate(0, 0, -1)
		if dayBefore.Weekday() != time.Saturday && dayBefore.Weekday() != time.Sunday {
			assert.Equal(t, StatusOpen, cal.Status(midSession(dayBefore.Year(), dayBefore.Month(), dayBefore.Day())), "day before Good Friday %d", year)
		}
	}
}
