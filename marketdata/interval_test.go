package marketdata

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewIntervalValid(t *testing.T) {
	i, err := NewInterval(UnitHour, 4)
	require.NoError(t, err)
	assert.Equal(t, UnitHour, i.Unit())
	assert.Equal(t, 4, i.Count())
}

func TestNewIntervalRejectsInvalidCount(t *testing.T) {
	for _, count := range []int{0, -1, -100} {
		_, err := NewInterval(UnitMinute, count)
		assert.Error(t, err, "count %d", count)
	}
}

func TestNewIntervalRejectsUnknownUnit(t *testing.T) {
	_, err := NewInterval(Unit(200), 1)
	assert.Error(t, err)
}

func TestPredefinedIntervals(t *testing.T) {
	cases := []struct {
		name  string
		i     Interval
		unit  Unit
		count int
		str   string
	}{
		{"M1", M1, UnitMinute, 1, "M1"},
		{"H1", H1, UnitHour, 1, "H1"},
		{"H4", H4, UnitHour, 4, "H4"},
		{"D1", D1, UnitDay, 1, "D1"},
		{"W1", W1, UnitWeek, 1, "W1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.unit, tc.i.Unit())
			assert.Equal(t, tc.count, tc.i.Count())
			assert.Equal(t, tc.str, tc.i.String())
		})
	}
}

func TestUnitString(t *testing.T) {
	assert.Equal(t, "m", UnitMinute.String())
	assert.Equal(t, "h", UnitHour.String())
	assert.Equal(t, "d", UnitDay.String())
	assert.Equal(t, "w", UnitWeek.String())
	assert.Contains(t, Unit(200).String(), "200")
}
