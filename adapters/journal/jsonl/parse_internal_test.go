package jsonl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/broker"
	"github.com/rustyeddy/trader/order"
)

func TestParseSideEveryValue(t *testing.T) {
	cases := map[string]order.Side{"": order.Side(0), "buy": order.Buy, "sell": order.Sell}
	for s, want := range cases {
		got, err := parseSide(s)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestParseTypeEveryValue(t *testing.T) {
	cases := map[string]order.Type{"market": order.Market, "limit": order.Limit, "stop": order.Stop, "stop_limit": order.StopLimit}
	for s, want := range cases {
		got, err := parseType(s)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestParseTimeInForceEveryValue(t *testing.T) {
	cases := map[string]order.TimeInForce{"gtc": order.GTC, "day": order.DAY, "ioc": order.IOC, "fok": order.FOK}
	for s, want := range cases {
		got, err := parseTimeInForce(s)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestParseStatusEveryValue(t *testing.T) {
	cases := map[string]order.Status{
		"unknown": order.StatusUnknown, "pending_submit": order.StatusPendingSubmit,
		"working": order.StatusWorking, "partially_filled": order.StatusPartiallyFilled,
		"filled": order.StatusFilled, "pending_cancel": order.StatusPendingCancel,
		"canceled": order.StatusCanceled, "pending_replace": order.StatusPendingReplace,
		"rejected": order.StatusRejected, "expired": order.StatusExpired,
	}
	for s, want := range cases {
		got, err := parseStatus(s)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestParseRejectReasonEveryValue(t *testing.T) {
	cases := map[string]order.RejectReason{
		"unknown": order.ReasonUnknown, "insufficient_margin": order.ReasonInsufficientMargin,
		"invalid_price": order.ReasonInvalidPrice, "invalid_quantity": order.ReasonInvalidQuantity,
		"market_closed": order.ReasonMarketClosed, "unsupported_order_type": order.ReasonUnsupportedOrderType,
		"duplicate_order_id": order.ReasonDuplicateOrderID, "risk_rejected": order.ReasonRiskRejected,
		"unsupported_capability": order.ReasonUnsupportedCapability,
	}
	for s, want := range cases {
		got, err := parseRejectReason(s)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	_, err := parseRejectReason("bogus")
	assert.ErrorIs(t, err, ErrCorruptEntry)
}

func TestParsePositionSideEveryValue(t *testing.T) {
	cases := map[string]order.PositionSide{"flat": order.Flat, "long": order.Long, "short": order.Short}
	for s, want := range cases {
		got, err := parsePositionSide(s)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestParseIntentKindEveryValue(t *testing.T) {
	cases := map[string]order.IntentKind{
		"enter": order.IntentEnter, "exit": order.IntentExit,
		"adjust_stop": order.IntentAdjustStop, "target_exposure": order.IntentTargetExposure,
	}
	for s, want := range cases {
		got, err := parseIntentKind(s)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestParseAccountStatusEveryValue(t *testing.T) {
	cases := map[string]broker.AccountStatus{
		"unknown": broker.AccountStatusUnknown, "active": broker.AccountStatusActive,
		"degraded": broker.AccountStatusDegraded, "disconnected": broker.AccountStatusDisconnected,
	}
	for s, want := range cases {
		got, err := parseAccountStatus(s)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}
