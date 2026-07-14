package market

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestSundayBeforeHolidayIsClosed verifies that the Sunday evening session is
// marked closed when Monday is New Year's Day or Christmas — eliminating the
// false-positive missing-slot reports that previously appeared for those months.
func TestSundayBeforeHolidayIsClosed(t *testing.T) {
	t.Parallel()

	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	// Jan 1 2024 was a Monday → Dec 31 2023 Sunday evening should be closed.
	newYearEve := []time.Time{
		time.Date(2023, 12, 31, 17, 0, 0, 0, ny), // 5pm Sunday NY
		time.Date(2023, 12, 31, 20, 0, 0, 0, ny), // 8pm Sunday NY
		time.Date(2023, 12, 31, 23, 0, 0, 0, ny), // 11pm Sunday NY
	}
	for _, ts := range newYearEve {
		assert.True(t, isForexMarketClosed(ts), "expected closed: %v", ts)
	}

	// Dec 25 2023 was a Monday → Dec 24 2023 Sunday evening should be closed.
	christmasEve := []time.Time{
		time.Date(2023, 12, 24, 18, 0, 0, 0, ny),
		time.Date(2023, 12, 24, 22, 0, 0, 0, ny),
	}
	for _, ts := range christmasEve {
		assert.True(t, isForexMarketClosed(ts), "expected closed: %v", ts)
	}

	// Jan 1 2023 was itself a Sunday — evening session should be closed too.
	newYearSunday := []time.Time{
		time.Date(2023, 1, 1, 17, 0, 0, 0, ny),
		time.Date(2023, 1, 1, 20, 0, 0, 0, ny),
	}
	for _, ts := range newYearSunday {
		assert.True(t, isForexMarketClosed(ts), "expected closed (Jan 1 Sunday): %v", ts)
	}

	// Boxing Day (Dec 26) on a weekday should be closed.
	boxingDayWeekday := time.Date(2022, 12, 26, 10, 0, 0, 0, ny) // Monday
	assert.True(t, isForexMarketClosed(boxingDayWeekday), "expected closed (Boxing Day): %v", boxingDayWeekday)

	// Sunday before Boxing Day (Dec 25 Sun → Dec 26 Mon) evening should be closed.
	// Dec 25 2022 was a Sunday.
	sundayBeforeBoxingDay := time.Date(2022, 12, 25, 18, 0, 0, 0, ny)
	assert.True(t, isForexMarketClosed(sundayBeforeBoxingDay), "expected closed (Sunday before Boxing Day): %v", sundayBeforeBoxingDay)

	// A normal Sunday evening (not before a holiday) should be open.
	normalSundayEvening := time.Date(2024, 6, 2, 18, 0, 0, 0, ny)
	assert.False(t, isForexMarketClosed(normalSundayEvening), "expected open: %v", normalSundayEvening)

	// Sunday *before* the open (4pm NY) should still be closed regardless.
	sundayAfternoon := time.Date(2024, 6, 2, 16, 0, 0, 0, ny)
	assert.True(t, isForexMarketClosed(sundayAfternoon), "expected closed: %v", sundayAfternoon)
}
