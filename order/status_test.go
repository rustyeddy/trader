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
