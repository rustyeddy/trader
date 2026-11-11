package jsonl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/internal/broker"
	runtimeorder "github.com/rustyeddy/trader/internal/order"
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
	cases := map[string]runtimeorder.Type{"market": runtimeorder.Market, "limit": runtimeorder.Limit, "stop": runtimeorder.Stop, "stop_limit": runtimeorder.StopLimit}
	for s, want := range cases {
		got, err := parseType(s)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestParseTimeInForceEveryValue(t *testing.T) {
	cases := map[string]runtimeorder.TimeInForce{"gtc": runtimeorder.GTC, "day": runtimeorder.DAY, "ioc": runtimeorder.IOC, "fok": runtimeorder.FOK}
	for s, want := range cases {
		got, err := parseTimeInForce(s)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestParseStatusEveryValue(t *testing.T) {
	cases := map[string]runtimeorder.Status{
		"unknown": runtimeorder.StatusUnknown, "pending_submit": runtimeorder.StatusPendingSubmit,
		"working": runtimeorder.StatusWorking, "partially_filled": runtimeorder.StatusPartiallyFilled,
		"filled": runtimeorder.StatusFilled, "pending_cancel": runtimeorder.StatusPendingCancel,
		"canceled": runtimeorder.StatusCanceled, "pending_replace": runtimeorder.StatusPendingReplace,
		"rejected": runtimeorder.StatusRejected, "expired": runtimeorder.StatusExpired,
	}
	for s, want := range cases {
		got, err := parseStatus(s)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestParseRejectReasonEveryValue(t *testing.T) {
	cases := map[string]runtimeorder.RejectReason{
		"unknown": runtimeorder.ReasonUnknown, "insufficient_margin": runtimeorder.ReasonInsufficientMargin,
		"invalid_price": runtimeorder.ReasonInvalidPrice, "invalid_quantity": runtimeorder.ReasonInvalidQuantity,
		"market_closed": runtimeorder.ReasonMarketClosed, "unsupported_order_type": runtimeorder.ReasonUnsupportedOrderType,
		"duplicate_order_id": runtimeorder.ReasonDuplicateOrderID, "risk_rejected": runtimeorder.ReasonRiskRejected,
		"unsupported_capability": runtimeorder.ReasonUnsupportedCapability,
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
		"enter_with_stop": order.IntentEnterWithStop,
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
