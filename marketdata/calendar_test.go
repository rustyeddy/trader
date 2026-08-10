package marketdata

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatusString(t *testing.T) {
	assert.Equal(t, "unknown", StatusUnknown.String())
	assert.Equal(t, "open", StatusOpen.String())
	assert.Equal(t, "closed", StatusClosed.String())
	assert.Equal(t, "holiday", StatusHoliday.String())
	assert.Contains(t, Status(200).String(), "200")
}

func TestStatusZeroValueIsUnknownNotOpen(t *testing.T) {
	var s Status
	assert.Equal(t, StatusUnknown, s)
	assert.NotEqual(t, StatusOpen, s)
}
