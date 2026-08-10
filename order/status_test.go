package order

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatusZeroValueIsUnknown(t *testing.T) {
	var s Status
	assert.Equal(t, StatusUnknown, s)
}

func TestStatusString(t *testing.T) {
	cases := map[Status]string{
		StatusUnknown:         "unknown",
		StatusPendingSubmit:   "pending_submit",
		StatusWorking:         "working",
		StatusPartiallyFilled: "partially_filled",
		StatusFilled:          "filled",
		StatusPendingCancel:   "pending_cancel",
		StatusCanceled:        "canceled",
		StatusPendingReplace:  "pending_replace",
		StatusRejected:        "rejected",
		StatusExpired:         "expired",
	}
	for status, want := range cases {
		assert.Equal(t, want, status.String())
	}
	assert.Contains(t, Status(200).String(), "200")
}

func TestStatusValid(t *testing.T) {
	for _, s := range []Status{
		StatusUnknown, StatusPendingSubmit, StatusWorking, StatusPartiallyFilled,
		StatusFilled, StatusPendingCancel, StatusCanceled, StatusPendingReplace,
		StatusRejected, StatusExpired,
	} {
		assert.True(t, s.valid(), "%s should be valid", s)
	}
	assert.False(t, Status(200).valid())
}

func TestStatusRequiresAcceptance(t *testing.T) {
	for _, s := range []Status{
		StatusWorking, StatusPartiallyFilled, StatusFilled, StatusPendingCancel,
		StatusCanceled, StatusPendingReplace, StatusExpired,
	} {
		assert.True(t, s.requiresAcceptance(), "%s should require acceptance", s)
	}
	for _, s := range []Status{StatusUnknown, StatusPendingSubmit, StatusRejected} {
		assert.False(t, s.requiresAcceptance(), "%s should not require acceptance", s)
	}
}

func TestStatusPrecludesAcceptance(t *testing.T) {
	assert.True(t, StatusPendingSubmit.precludesAcceptance())
	assert.True(t, StatusRejected.precludesAcceptance())
	assert.False(t, StatusUnknown.precludesAcceptance())
	assert.False(t, StatusWorking.precludesAcceptance())
}
