package order

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRejectReasonZeroValueIsUnknown(t *testing.T) {
	var r RejectReason
	assert.Equal(t, ReasonUnknown, r)
}

func TestRejectReasonString(t *testing.T) {
	cases := map[RejectReason]string{
		ReasonUnknown:               "unknown",
		ReasonInsufficientMargin:    "insufficient_margin",
		ReasonInvalidPrice:          "invalid_price",
		ReasonInvalidQuantity:       "invalid_quantity",
		ReasonMarketClosed:          "market_closed",
		ReasonUnsupportedOrderType:  "unsupported_order_type",
		ReasonDuplicateOrderID:      "duplicate_order_id",
		ReasonRiskRejected:          "risk_rejected",
		ReasonUnsupportedCapability: "unsupported_capability",
	}
	for reason, want := range cases {
		assert.Equal(t, want, reason.String())
	}
	assert.Contains(t, RejectReason(200).String(), "200")
}

func TestRejectionPreservesBrokerCodeEvenWhenUnknown(t *testing.T) {
	r := Rejection{Reason: ReasonUnknown, BrokerCode: "MARKET_HALTED_XYZ", Detail: "unrecognized broker code"}
	assert.Equal(t, ReasonUnknown, r.Reason)
	assert.Equal(t, "MARKET_HALTED_XYZ", r.BrokerCode)
	assert.Equal(t, "unrecognized broker code", r.Detail)
}
