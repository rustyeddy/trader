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

func TestUSEquityCalendar_OrdinaryWeekdayIsOpen(t *testing.T) {
	cal := testUSEquityCal(t)
	// 2020-01-02 is an ordinary Thursday.
	assert.Equal(t, StatusOpen, cal.Status(time.Date(2020, 1, 2, 15, 0, 0, 0, time.UTC)))
}

func TestUSEquityCalendar_WeekendIsClosed(t *testing.T) {
	cal := testUSEquityCal(t)
	assert.Equal(t, StatusClosed, cal.Status(time.Date(2020, 1, 4, 12, 0, 0, 0, time.UTC))) // Saturday
	assert.Equal(t, StatusClosed, cal.Status(time.Date(2020, 1, 5, 12, 0, 0, 0, time.UTC))) // Sunday
}

func TestUSEquityCalendar_FixedDateHolidays(t *testing.T) {
	cal := testUSEquityCal(t)
	tests := []struct {
		name string
		date time.Time
	}{
		{"New Year's Day 2020", time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)},
		{"Independence Day 2019", time.Date(2019, 7, 4, 12, 0, 0, 0, time.UTC)},
		{"Christmas 2019", time.Date(2019, 12, 25, 12, 0, 0, 0, time.UTC)},
		{"Juneteenth 2023", time.Date(2023, 6, 19, 12, 0, 0, 0, time.UTC)},
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
	assert.Equal(t, StatusOpen, cal.Status(time.Date(2021, 6, 18, 12, 0, 0, 0, time.UTC)))
}

func TestUSEquityCalendar_FloatingHolidays(t *testing.T) {
	cal := testUSEquityCal(t)
	tests := []struct {
		name string
		date time.Time
	}{
		{"MLK Day 2020 (Jan 20)", time.Date(2020, 1, 20, 12, 0, 0, 0, time.UTC)},
		{"Washington's Birthday 2020 (Feb 17)", time.Date(2020, 2, 17, 12, 0, 0, 0, time.UTC)},
		{"Good Friday 2020 (Apr 10)", time.Date(2020, 4, 10, 12, 0, 0, 0, time.UTC)},
		{"Memorial Day 2020 (May 25)", time.Date(2020, 5, 25, 12, 0, 0, 0, time.UTC)},
		{"Labor Day 2020 (Sep 7)", time.Date(2020, 9, 7, 12, 0, 0, 0, time.UTC)},
		{"Thanksgiving 2020 (Nov 26)", time.Date(2020, 11, 26, 12, 0, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		assert.Equal(t, StatusHoliday, cal.Status(tt.date), tt.name)
	}
}

// TestUSEquityCalendar_ObservedWeekendShift confirms the standard
// "observed" rule: New Year's Day 2022 fell on a Saturday, so it was
// observed the preceding Friday (2021-12-31), and Independence Day
// 2021 fell on a Sunday, so it was observed the following Monday
// (2021-07-05).
func TestUSEquityCalendar_ObservedWeekendShift(t *testing.T) {
	cal := testUSEquityCal(t)
	assert.Equal(t, StatusHoliday, cal.Status(time.Date(2021, 12, 31, 12, 0, 0, 0, time.UTC)),
		"New Year's Day 2022 (Saturday) observed the preceding Friday")
	assert.Equal(t, StatusClosed, cal.Status(time.Date(2022, 1, 1, 12, 0, 0, 0, time.UTC)),
		"the actual Saturday date itself is an ordinary weekend closure (StatusClosed), not StatusHoliday twice")
	assert.Equal(t, StatusHoliday, cal.Status(time.Date(2021, 7, 5, 12, 0, 0, 0, time.UTC)),
		"Independence Day 2021 (Sunday) observed the following Monday")
}

// TestUSEquityCalendar_HalfDayIsOpenNotHoliday confirms a half day
// (the day after Thanksgiving) is a full, open trading day at this
// calendar's D1 granularity — not conflated with a full closure — while
// still being distinguishable as a half day via IsHalfDay (issue #296's
// own "early-close/half-day behavior" requirement).
func TestUSEquityCalendar_HalfDayIsOpenNotHoliday(t *testing.T) {
	cal := testUSEquityCal(t)
	dayAfterThanksgiving2020 := time.Date(2020, 11, 27, 12, 0, 0, 0, time.UTC)

	assert.Equal(t, StatusOpen, cal.Status(dayAfterThanksgiving2020))
	closeAt, ok := cal.IsHalfDay(dayAfterThanksgiving2020)
	require.True(t, ok)
	assert.Equal(t, 13*time.Hour, closeAt)

	// An ordinary trading day is not a half day.
	_, ok = cal.IsHalfDay(time.Date(2020, 11, 30, 12, 0, 0, 0, time.UTC))
	assert.False(t, ok)
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

// TestUSEquityCalendar_SessionContainsBarSpan is the exact
// interval_state.go ClassifyInterval containment requirement
// (uniformStatus) this calendar's design depends on: Session must
// never be narrower than Bar's own D1 span, on every open day this
// suite already exercises, or every D1 coverage classification would
// fail with ErrIntervalStraddlesBoundary instead of classifying
// correctly.
func TestUSEquityCalendar_SessionContainsBarSpan(t *testing.T) {
	cal := testUSEquityCal(t)
	openDays := []time.Time{
		time.Date(2020, 1, 2, 15, 0, 0, 0, time.UTC),
		time.Date(2020, 11, 27, 15, 0, 0, 0, time.UTC), // half day
		time.Date(2020, 5, 4, 15, 0, 0, 0, time.UTC),
	}
	for _, t0 := range openDays {
		barSpan, err := cal.Bar(t0, D1)
		require.NoError(t, err)
		session, ok := cal.Session(t0)
		require.True(t, ok)
		assert.False(t, session.Start().After(barSpan.Start()), "session start after bar start for %v", t0)
		assert.False(t, session.End().Before(barSpan.End()), "session end before bar end for %v", t0)
	}
}

func TestUSEquityCalendar_SessionFalseWhenClosed(t *testing.T) {
	cal := testUSEquityCal(t)
	_, ok := cal.Session(time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC))
	assert.False(t, ok)
}

func TestUSEquityCalendar_NoHolidayDataByDefault(t *testing.T) {
	cal := NewUSEquityCalendar(USEquityCalendarParams{})
	// With no configured holidays, every weekday is open — the
	// calendar itself carries no built-in holiday data (mirroring
	// FXCalendar's own "no holiday data ships with Trader itself"
	// split).
	assert.Equal(t, StatusOpen, cal.Status(time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)))
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
		assert.Equal(t, StatusHoliday, cal.Status(wantDate), year)
		dayBefore := wantDate.AddDate(0, 0, -1)
		if dayBefore.Weekday() != time.Saturday && dayBefore.Weekday() != time.Sunday {
			assert.Equal(t, StatusOpen, cal.Status(dayBefore), "day before Good Friday %d", year)
		}
	}
}
