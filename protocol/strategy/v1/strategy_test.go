package strategyv1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// roundTrip marshals m to wire bytes and unmarshals into a fresh value
// of the same concrete type, returning that fresh value. Every test
// below asserts proto.Equal(m, got) — the schema's own contract is
// that a message survives an actual wire encode/decode cycle
// unchanged, not merely that the Go struct literal looks right.
func roundTrip[M proto.Message](t *testing.T, m M) M {
	t.Helper()
	data, err := proto.Marshal(m)
	require.NoError(t, err)

	got := m.ProtoReflect().New().Interface()
	require.NoError(t, proto.Unmarshal(data, got))
	return got.(M)
}

func TestProtocolVersion(t *testing.T) {
	assert.Equal(t, "v1", ProtocolVersion)
}

func TestHandshakeRequest_RoundTrip(t *testing.T) {
	req := &HandshakeRequest{
		ProtocolVersion: ProtocolVersion,
		StrategyDescriptor: &StrategyDescriptor{
			Name:    "sma-trend",
			Version: "v4",
			Requirements: []*DataRequirement{
				{
					InstrumentId: "eq:NASDAQ:AAPL",
					Interval:     &Interval{Unit: IntervalUnit_INTERVAL_UNIT_DAY, Count: 1},
					WarmupBars:   200,
				},
			},
		},
		Capabilities: []Capability{Capability_CAPABILITY_FILL_HANDLER},
	}

	got := roundTrip(t, req)
	assert.True(t, proto.Equal(req, got))
	require.Len(t, got.StrategyDescriptor.Requirements, 1)
	assert.Equal(t, "eq:NASDAQ:AAPL", got.StrategyDescriptor.Requirements[0].InstrumentId)
	assert.Equal(t, IntervalUnit_INTERVAL_UNIT_DAY, got.StrategyDescriptor.Requirements[0].Interval.Unit)
	assert.Equal(t, []Capability{Capability_CAPABILITY_FILL_HANDLER}, got.Capabilities)
}

func TestHandshakeResponse_Accepted_RoundTrip(t *testing.T) {
	resp := &HandshakeResponse{
		Accepted:        true,
		ProtocolVersion: ProtocolVersion,
		Capabilities:    []Capability{Capability_CAPABILITY_FILL_HANDLER},
	}
	got := roundTrip(t, resp)
	assert.True(t, proto.Equal(resp, got))
	assert.Nil(t, got.RejectReason, "an accepted handshake must carry no reject reason")
}

func TestHandshakeResponse_Rejected_CarriesStructuredError(t *testing.T) {
	resp := &HandshakeResponse{
		Accepted:        false,
		ProtocolVersion: "v2",
		RejectReason: &Error{
			Code:    ErrorCode_ERROR_CODE_PROTOCOL_VERSION_MISMATCH,
			Message: "host does not support protocol v99",
		},
	}
	got := roundTrip(t, resp)
	assert.True(t, proto.Equal(resp, got))
	require.NotNil(t, got.RejectReason)
	assert.Equal(t, ErrorCode_ERROR_CODE_PROTOCOL_VERSION_MISMATCH, got.RejectReason.Code)
}

func TestSessionStart_RoundTrip(t *testing.T) {
	start := &SessionStart{
		RunId:              "run:01J8Z...",
		StartTimeUnixNanos: 1_700_000_000_000_000_000,
	}
	got := roundTrip(t, start)
	assert.True(t, proto.Equal(start, got))
}

func TestSessionEnd_NormalCompletion_ZeroValue(t *testing.T) {
	end := &SessionEnd{}
	got := roundTrip(t, end)
	assert.True(t, proto.Equal(end, got))
	assert.Equal(t, ErrorCode_ERROR_CODE_UNSPECIFIED, got.Code)
	assert.Empty(t, got.Reason)
}

func TestSessionEnd_AbnormalTermination_RoundTrip(t *testing.T) {
	end := &SessionEnd{
		Code:   ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED,
		Reason: "guest did not respond to bar 42 within the per-bar timeout",
	}
	got := roundTrip(t, end)
	assert.True(t, proto.Equal(end, got))
}

func mustAccountSnapshot() *AccountSnapshot {
	return &AccountSnapshot{
		AccountId:     "acct:01J8Z...",
		Currency:      "USD",
		AsOfUnixNanos: 1_700_000_100_000_000_000,
		Equity:        &Money{Amount: "100523.45", Currency: "USD"},
		RealizedPnl:   &Money{Amount: "523.45", Currency: "USD"},
		UnrealizedPnl: &Money{Amount: "0", Currency: "USD"},
		Positions: []*PositionSnapshot{
			{
				InstrumentId: "eq:NASDAQ:AAPL",
				Side:         PositionSide_POSITION_SIDE_LONG,
				Quantity:     "100",
				AvgPrice:     "150.25",
			},
		},
	}
}

func TestAccountSnapshot_RoundTrip(t *testing.T) {
	snap := mustAccountSnapshot()
	got := roundTrip(t, snap)
	assert.True(t, proto.Equal(snap, got))
	require.Len(t, got.Positions, 1)
	assert.Equal(t, PositionSide_POSITION_SIDE_LONG, got.Positions[0].Side)
}

func TestAccountSnapshot_FlatPosition_EmptyAvgPrice(t *testing.T) {
	// order.Position's own invariant: AvgPrice is nil exactly when Side
	// is Flat. The wire mirror is an empty string, the zero value for
	// PositionSnapshot.avg_price, matching PositionSide's own zero
	// value (POSITION_SIDE_FLAT) so both fields default together.
	snap := &AccountSnapshot{
		Positions: []*PositionSnapshot{
			{InstrumentId: "fx:EUR/USD", Quantity: "0"},
		},
	}
	got := roundTrip(t, snap)
	assert.True(t, proto.Equal(snap, got))
	assert.Equal(t, PositionSide_POSITION_SIDE_FLAT, got.Positions[0].Side)
	assert.Empty(t, got.Positions[0].AvgPrice)
}

func TestBarEvent_RoundTrip(t *testing.T) {
	event := &BarEvent{
		Sequence:     42,
		InstrumentId: "eq:NASDAQ:AAPL",
		Interval:     &Interval{Unit: IntervalUnit_INTERVAL_UNIT_DAY, Count: 1},
		Bar: &Bar{
			TimeUnixNanos: 1_700_000_000_000_000_000,
			Open:          "150.00",
			High:          "151.50",
			Low:           "149.75",
			Close:         "150.90",
			AvgSpread:     "0.01",
			MaxSpread:     "0.03",
			Ticks:         12345,
		},
		Account: mustAccountSnapshot(),
	}
	got := roundTrip(t, event)
	assert.True(t, proto.Equal(event, got))
	assert.Equal(t, uint64(42), got.Sequence)
	assert.Equal(t, "150.90", got.Bar.Close)
}

func TestOnBarResponse_EmptyIsExplicitNoIntents(t *testing.T) {
	resp := &OnBarResponse{Sequence: 7}
	got := roundTrip(t, resp)
	assert.True(t, proto.Equal(resp, got))
	assert.Empty(t, got.Intents, "an empty intents list is a valid, explicit \"no intents this bar\" response")
	assert.Empty(t, got.Signals)
}

func TestOnBarResponse_DescribedIntentsAndSignals_RoundTrip(t *testing.T) {
	resp := &OnBarResponse{
		Sequence: 7,
		Intents: []*DescribedIntent{
			{
				Kind:             IntentKind_INTENT_KIND_ENTER_WITH_STOP,
				InstrumentId:     "eq:NASDAQ:AAPL",
				Side:             Side_SIDE_BUY,
				StopPrice:        "148.00",
				CorrelationToken: "bracket-1",
			},
			{
				Kind:             IntentKind_INTENT_KIND_ADJUST_STOP,
				InstrumentId:     "eq:NASDAQ:AAPL",
				StopPrice:        "149.10",
				CorrelationToken: "bracket-1",
			},
		},
		Signals: []*DescribedSignal{
			{
				Strategy: "sma-trend",
				Values: map[string]string{
					"sma_200":   "148.32",
					"close":     "150.90",
					"above_sma": "true",
				},
			},
		},
	}
	got := roundTrip(t, resp)
	assert.True(t, proto.Equal(resp, got))
	require.Len(t, got.Intents, 2)
	assert.Equal(t, "bracket-1", got.Intents[0].CorrelationToken)
	assert.Equal(t, got.Intents[0].CorrelationToken, got.Intents[1].CorrelationToken,
		"two described intents sharing one correlation token must round-trip identically")
	require.Len(t, got.Signals, 1)
	assert.Equal(t, "148.32", got.Signals[0].Values["sma_200"])
}

// TestDescribedIntent_EveryKindIsRepresentable proves every current
// order.IntentKind (including IntentEnterWithStop, ADR-059 — PR #386
// review on the preceding ADR caught its omission from an earlier
// draft) has a distinct, round-trippable wire value. This schema does
// not itself validate which of side/quantity/stop_price a given kind
// requires (issue #378's own job); it only proves every kind is
// representable at all.
func TestDescribedIntent_EveryKindIsRepresentable(t *testing.T) {
	kinds := []IntentKind{
		IntentKind_INTENT_KIND_ENTER,
		IntentKind_INTENT_KIND_EXIT,
		IntentKind_INTENT_KIND_ADJUST_STOP,
		IntentKind_INTENT_KIND_TARGET_EXPOSURE,
		IntentKind_INTENT_KIND_ENTER_WITH_STOP,
	}
	seen := map[int32]bool{}
	for _, k := range kinds {
		require.False(t, seen[int32(k)], "duplicate wire value for %v", k)
		seen[int32(k)] = true

		intent := &DescribedIntent{Kind: k, InstrumentId: "fx:EUR/USD"}
		got := roundTrip(t, intent)
		assert.True(t, proto.Equal(intent, got))
	}
	assert.Len(t, seen, 5, "every defined IntentKind must have a distinct wire value")
}

func TestFillEvent_And_OnFillResponse_RoundTrip(t *testing.T) {
	fill := &FillEvent{
		Sequence:      9,
		OrderId:       "order:01J8Z...",
		InstrumentId:  "eq:NASDAQ:AAPL",
		Side:          Side_SIDE_BUY,
		Price:         "150.10",
		Quantity:      "100",
		CorrelationId: "corr:01J8Z...",
		CausationId:   "cause:01J8Z...",
		Account:       mustAccountSnapshot(),
	}
	got := roundTrip(t, fill)
	assert.True(t, proto.Equal(fill, got))

	// OnFill returns only error (ADR-060's own deliberate signature) —
	// the response never carries described intents or signals.
	ack := &OnFillResponse{Sequence: 9}
	gotAck := roundTrip(t, ack)
	assert.True(t, proto.Equal(ack, gotAck))
	assert.Nil(t, gotAck.Error)

	nack := &OnFillResponse{
		Sequence: 9,
		Error:    &Error{Code: ErrorCode_ERROR_CODE_UNSPECIFIED, Message: "guest-side handler panicked"},
	}
	gotNack := roundTrip(t, nack)
	assert.True(t, proto.Equal(nack, gotNack))
}

func TestGetHistoryBars_RoundTrip(t *testing.T) {
	req := &GetHistoryBarsRequest{
		CallbackSequence: 42,
		InstrumentId:     "eq:NASDAQ:AAPL",
		Interval:         &Interval{Unit: IntervalUnit_INTERVAL_UNIT_DAY, Count: 1},
		Count:            20,
	}
	got := roundTrip(t, req)
	assert.True(t, proto.Equal(req, got))

	resp := &GetHistoryBarsResponse{
		Bars: []*Bar{
			{TimeUnixNanos: 1, Open: "1", High: "1", Low: "1", Close: "1"},
			{TimeUnixNanos: 2, Open: "2", High: "2", Low: "2", Close: "2"},
		},
	}
	gotResp := roundTrip(t, resp)
	assert.True(t, proto.Equal(resp, gotResp))
	assert.Len(t, gotResp.Bars, 2)
}

// TestRunServerMessage_OneofVariants proves each of RunServerMessage's
// four payload variants round-trips independently and that
// GetXxx()-style accessors report nil for every variant not set —
// the discriminated-envelope shape journal.Record/broker.Event already
// establish elsewhere in this codebase (strategy.proto's own doc
// comment on RunServerMessage).
func TestRunServerMessage_OneofVariants(t *testing.T) {
	cases := []struct {
		name string
		msg  *RunServerMessage
	}{
		{"session_start", &RunServerMessage{Payload: &RunServerMessage_SessionStart{
			SessionStart: &SessionStart{RunId: "run:1"},
		}}},
		{"bar_event", &RunServerMessage{Payload: &RunServerMessage_BarEvent{
			BarEvent: &BarEvent{Sequence: 1, InstrumentId: "fx:EUR/USD"},
		}}},
		{"fill_event", &RunServerMessage{Payload: &RunServerMessage_FillEvent{
			FillEvent: &FillEvent{Sequence: 2, InstrumentId: "fx:EUR/USD"},
		}}},
		{"session_end", &RunServerMessage{Payload: &RunServerMessage_SessionEnd{
			SessionEnd: &SessionEnd{},
		}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := roundTrip(t, tc.msg)
			assert.True(t, proto.Equal(tc.msg, got))

			// Exactly one accessor returns non-nil.
			nonNil := 0
			if got.GetSessionStart() != nil {
				nonNil++
			}
			if got.GetBarEvent() != nil {
				nonNil++
			}
			if got.GetFillEvent() != nil {
				nonNil++
			}
			if got.GetSessionEnd() != nil {
				nonNil++
			}
			assert.Equal(t, 1, nonNil, "exactly one RunServerMessage payload variant must be set")
		})
	}
}

func TestRunClientMessage_OneofVariants(t *testing.T) {
	cases := []struct {
		name string
		msg  *RunClientMessage
	}{
		{"on_bar_response", &RunClientMessage{Payload: &RunClientMessage_OnBarResponse{
			OnBarResponse: &OnBarResponse{Sequence: 1},
		}}},
		{"on_fill_response", &RunClientMessage{Payload: &RunClientMessage_OnFillResponse{
			OnFillResponse: &OnFillResponse{Sequence: 2},
		}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := roundTrip(t, tc.msg)
			assert.True(t, proto.Equal(tc.msg, got))

			nonNil := 0
			if got.GetOnBarResponse() != nil {
				nonNil++
			}
			if got.GetOnFillResponse() != nil {
				nonNil++
			}
			assert.Equal(t, 1, nonNil, "exactly one RunClientMessage payload variant must be set")
		})
	}
}

// TestZeroValueEnumsAreUnspecified proves every enum's proto3 zero
// value is an explicit "unspecified" sentinel, never a silently
// meaningful default (for example PositionSide_POSITION_SIDE_FLAT is
// the one deliberate exception, mirroring order.PositionSide's own
// Flat-is-the-zero-value contract — every other enum here uses
// UNSPECIFIED=0).
func TestZeroValueEnumsAreUnspecified(t *testing.T) {
	assert.Equal(t, IntervalUnit(0), IntervalUnit_INTERVAL_UNIT_UNSPECIFIED)
	assert.Equal(t, Side(0), Side_SIDE_UNSPECIFIED)
	assert.Equal(t, IntentKind(0), IntentKind_INTENT_KIND_UNSPECIFIED)
	assert.Equal(t, Capability(0), Capability_CAPABILITY_UNSPECIFIED)
	assert.Equal(t, ErrorCode(0), ErrorCode_ERROR_CODE_UNSPECIFIED)

	// The one deliberate exception: PositionSide's zero value is FLAT,
	// not UNSPECIFIED, mirroring order.PositionSide's own Flat-zero
	// contract exactly.
	assert.Equal(t, PositionSide(0), PositionSide_POSITION_SIDE_FLAT)
}
