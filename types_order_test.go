package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestOrderTypeString_AllValuesAndUnknown_Phase1 verifies expected behavior for this component.
func TestOrderTypeString_AllValuesAndUnknown_Phase1(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   orderType
		want string
	}{
		{OrderNone, "none"},
		{OrderMarket, "market"},
		{OrderLimit, "limit"},
		{OrderStop, "stop"},
		{OrderStopLimit, "stop-limit"},
		{OrderTrailingStop, "trailing-stop"},
		{orderType(255), "<unknown>"},
	}

	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.in.String())
	}
}

// TestOrderStatusString_AllValuesAndUnknown_Phase1 verifies expected behavior for this component.
func TestOrderStatusString_AllValuesAndUnknown_Phase1(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   orderStatus
		want string
	}{
		{OrderStatusNone, "none"},
		{OrderPending, "pending"},
		{OrderAccepted, "accepted"},
		{OrderFilled, "filled"},
		{OrderRejected, "rejected"},
		{OrderCanceled, "canceled"},
		{orderStatus(255), "<unknown>"},
	}

	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.in.String())
	}
}
