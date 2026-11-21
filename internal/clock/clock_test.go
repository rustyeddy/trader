package clock

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestClock(t *testing.T) {
	before := time.Now()
	got := Clock()
	after := time.Now()

	assert.False(t, got.Before(before.Truncate(0)), "not before the call began")
	assert.False(t, got.After(after), "not after the call returned")
	assert.Equal(t, time.UTC, got.Location(), "UTC")
	assert.Equal(t, got, got.Round(0), "no monotonic reading: comparing it with a stored time compares wall time")
}
