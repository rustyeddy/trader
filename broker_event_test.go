package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEventTypeString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		typ  EventType
		want string
	}{
		{name: "order accepted", typ: EventOrderAccepted, want: "OrderAccepted"},
		{name: "order rejected", typ: EventOrderRejected, want: "OrderRejected"},
		{name: "order filled", typ: EventOrderFilled, want: "OrderFilled"},
		{name: "order partially filled", typ: EventOrderPartiallyFilled, want: "OrderPartiallyFilled"},
		{name: "order canceled", typ: EventOrderCanceled, want: "OrderCanceled"},
		{name: "position closed", typ: EventPositionClosed, want: "PositionClosed"},
		{name: "account updated", typ: EventAccountUpdated, want: "AccountUpdated"},
		{name: "unknown zero", typ: EventType(0), want: "UknownEventType"},
		{name: "unknown out of range", typ: EventType(999), want: "UknownEventType"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.typ.String())
		})
	}
}
