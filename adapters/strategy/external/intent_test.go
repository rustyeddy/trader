package external_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/adapters/strategy/external"
	"github.com/rustyeddy/trader/order"
	v1 "github.com/rustyeddy/trader/protocol/strategy/v1"
)

func TestIntentsFromWire_Enter(t *testing.T) {
	factory := newIntentFactory(t, "external_test")
	inst := eurUSD(t)

	intents, tokens, err := external.IntentsFromWire([]*v1.DescribedIntent{
		{Kind: v1.IntentKind_INTENT_KIND_ENTER, InstrumentId: inst.String(), Side: v1.Side_SIDE_BUY},
	}, factory)
	require.NoError(t, err)
	require.Empty(t, tokens)
	require.Len(t, intents, 1)

	in := intents[0]
	require.Equal(t, order.IntentEnter, in.Kind)
	require.True(t, in.Instrument.Equal(inst))
	require.Equal(t, order.Buy, in.Side)
	require.Nil(t, in.Quantity)
	require.Nil(t, in.StopPrice)
	require.False(t, in.IntentID.IsZero())
	require.False(t, in.Metadata.CorrelationID.IsZero())
}

func TestIntentsFromWire_Exit(t *testing.T) {
	factory := newIntentFactory(t, "external_test")
	inst := eurUSD(t)

	intents, _, err := external.IntentsFromWire([]*v1.DescribedIntent{
		{Kind: v1.IntentKind_INTENT_KIND_EXIT, InstrumentId: inst.String()},
	}, factory)
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentExit, intents[0].Kind)
}

func TestIntentsFromWire_AdjustStop(t *testing.T) {
	factory := newIntentFactory(t, "external_test")
	inst := eurUSD(t)

	intents, _, err := external.IntentsFromWire([]*v1.DescribedIntent{
		{Kind: v1.IntentKind_INTENT_KIND_ADJUST_STOP, InstrumentId: inst.String(), StopPrice: "1.0950"},
	}, factory)
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentAdjustStop, intents[0].Kind)
	require.Equal(t, "1.095", intents[0].StopPrice.String())
}

func TestIntentsFromWire_TargetExposure(t *testing.T) {
	factory := newIntentFactory(t, "external_test")
	inst := eurUSD(t)

	intents, _, err := external.IntentsFromWire([]*v1.DescribedIntent{
		{Kind: v1.IntentKind_INTENT_KIND_TARGET_EXPOSURE, InstrumentId: inst.String(), Side: v1.Side_SIDE_SELL, Quantity: "500"},
	}, factory)
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentTargetExposure, intents[0].Kind)
	require.Equal(t, order.Sell, intents[0].Side)
	require.Equal(t, "500", intents[0].Quantity.String())
}

func TestIntentsFromWire_EnterWithStop(t *testing.T) {
	factory := newIntentFactory(t, "external_test")
	inst := eurUSD(t)

	intents, _, err := external.IntentsFromWire([]*v1.DescribedIntent{
		{Kind: v1.IntentKind_INTENT_KIND_ENTER_WITH_STOP, InstrumentId: inst.String(), Side: v1.Side_SIDE_BUY, StopPrice: "1.0900"},
	}, factory)
	require.NoError(t, err)
	require.Len(t, intents, 1)
	require.Equal(t, order.IntentEnterWithStop, intents[0].Kind)
	require.Equal(t, order.Buy, intents[0].Side)
	require.Equal(t, "1.09", intents[0].StopPrice.String())
}

func TestIntentsFromWire_UnspecifiedKindRejected(t *testing.T) {
	factory := newIntentFactory(t, "external_test")
	_, _, err := external.IntentsFromWire([]*v1.DescribedIntent{
		{Kind: v1.IntentKind_INTENT_KIND_UNSPECIFIED, InstrumentId: eurUSD(t).String()},
	}, factory)
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestIntentsFromWire_InvalidInstrumentIDRejected(t *testing.T) {
	factory := newIntentFactory(t, "external_test")
	_, _, err := external.IntentsFromWire([]*v1.DescribedIntent{
		{Kind: v1.IntentKind_INTENT_KIND_EXIT, InstrumentId: "garbage"},
	}, factory)
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestIntentsFromWire_MissingRequiredSideRejected(t *testing.T) {
	factory := newIntentFactory(t, "external_test")
	_, _, err := external.IntentsFromWire([]*v1.DescribedIntent{
		{Kind: v1.IntentKind_INTENT_KIND_ENTER, InstrumentId: eurUSD(t).String()},
	}, factory)
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestIntentsFromWire_MissingRequiredStopPriceRejected(t *testing.T) {
	factory := newIntentFactory(t, "external_test")
	_, _, err := external.IntentsFromWire([]*v1.DescribedIntent{
		{Kind: v1.IntentKind_INTENT_KIND_ADJUST_STOP, InstrumentId: eurUSD(t).String()},
	}, factory)
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestIntentsFromWire_MissingRequiredQuantityRejected(t *testing.T) {
	factory := newIntentFactory(t, "external_test")
	_, _, err := external.IntentsFromWire([]*v1.DescribedIntent{
		{Kind: v1.IntentKind_INTENT_KIND_TARGET_EXPOSURE, InstrumentId: eurUSD(t).String(), Side: v1.Side_SIDE_BUY},
	}, factory)
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestIntentsFromWire_NilDescribedIntentRejected(t *testing.T) {
	factory := newIntentFactory(t, "external_test")
	_, _, err := external.IntentsFromWire([]*v1.DescribedIntent{nil}, factory)
	require.ErrorIs(t, err, external.ErrInvalidWireValue)
}

func TestIntentsFromWire_EmptyIsValid(t *testing.T) {
	factory := newIntentFactory(t, "external_test")
	intents, tokens, err := external.IntentsFromWire(nil, factory)
	require.NoError(t, err)
	require.Empty(t, intents)
	require.Empty(t, tokens)
}

// TestIntentsFromWire_CorrelationTokenGrouping is the correctness
// property ADR-062's own "Intent construction ownership" section
// requires: two described intents sharing one correlation_token get
// the identical real CorrelationID, minted exactly once via
// factory.NewCorrelationID(), never a hand-rolled or guest-supplied
// value.
func TestIntentsFromWire_CorrelationTokenGrouping(t *testing.T) {
	factory := newIntentFactory(t, "external_test")
	inst := eurUSD(t)

	intents, tokens, err := external.IntentsFromWire([]*v1.DescribedIntent{
		{Kind: v1.IntentKind_INTENT_KIND_EXIT, InstrumentId: inst.String(), CorrelationToken: "reversal"},
		{Kind: v1.IntentKind_INTENT_KIND_ENTER, InstrumentId: inst.String(), Side: v1.Side_SIDE_SELL, CorrelationToken: "reversal"},
	}, factory)
	require.NoError(t, err)
	require.Len(t, intents, 2)

	require.Len(t, tokens, 1)
	grouped, ok := tokens["reversal"]
	require.True(t, ok)
	require.False(t, grouped.IsZero())

	require.True(t, intents[0].Metadata.CorrelationID.Equal(grouped))
	require.True(t, intents[1].Metadata.CorrelationID.Equal(grouped))
}

// TestIntentsFromWire_UngroupedIntentsGetDistinctCorrelationIDs is
// IntentFactory's own documented default: an empty correlation_token
// never shares a CorrelationID with anything else.
func TestIntentsFromWire_UngroupedIntentsGetDistinctCorrelationIDs(t *testing.T) {
	factory := newIntentFactory(t, "external_test")
	inst := eurUSD(t)

	intents, tokens, err := external.IntentsFromWire([]*v1.DescribedIntent{
		{Kind: v1.IntentKind_INTENT_KIND_EXIT, InstrumentId: inst.String()},
		{Kind: v1.IntentKind_INTENT_KIND_ENTER, InstrumentId: inst.String(), Side: v1.Side_SIDE_BUY},
	}, factory)
	require.NoError(t, err)
	require.Empty(t, tokens)
	require.False(t, intents[0].Metadata.CorrelationID.Equal(intents[1].Metadata.CorrelationID))
}

// TestIntentsFromWire_DistinctTokensGetDistinctCorrelationIDs is the
// review finding that closes the gap TestIntentsFromWire_
// CorrelationTokenGrouping alone leaves open: two distinct non-empty
// tokens must mint two distinct CorrelationIDs, not silently collapse
// onto whichever one is minted first.
func TestIntentsFromWire_DistinctTokensGetDistinctCorrelationIDs(t *testing.T) {
	factory := newIntentFactory(t, "external_test")
	inst := eurUSD(t)

	intents, tokens, err := external.IntentsFromWire([]*v1.DescribedIntent{
		{Kind: v1.IntentKind_INTENT_KIND_EXIT, InstrumentId: inst.String(), CorrelationToken: "group-a"},
		{Kind: v1.IntentKind_INTENT_KIND_ENTER, InstrumentId: inst.String(), Side: v1.Side_SIDE_BUY, CorrelationToken: "group-b"},
	}, factory)
	require.NoError(t, err)
	require.Len(t, intents, 2)
	require.Len(t, tokens, 2)

	require.False(t, tokens["group-a"].Equal(tokens["group-b"]))
	require.True(t, intents[0].Metadata.CorrelationID.Equal(tokens["group-a"]))
	require.True(t, intents[1].Metadata.CorrelationID.Equal(tokens["group-b"]))
}

// TestIntentsFromWire_ForbiddenFieldsRejected is the review's blocking
// finding: a field this kind forbids must fail explicitly, never be
// silently ignored, mirroring order.NewIntent's own per-Kind contract.
func TestIntentsFromWire_ForbiddenFieldsRejected(t *testing.T) {
	inst := eurUSD(t)

	tests := []struct {
		name string
		di   *v1.DescribedIntent
	}{
		{"exit with side", &v1.DescribedIntent{Kind: v1.IntentKind_INTENT_KIND_EXIT, InstrumentId: inst.String(), Side: v1.Side_SIDE_BUY}},
		{"exit with stop_price", &v1.DescribedIntent{Kind: v1.IntentKind_INTENT_KIND_EXIT, InstrumentId: inst.String(), StopPrice: "1.1000"}},
		{"exit with quantity", &v1.DescribedIntent{Kind: v1.IntentKind_INTENT_KIND_EXIT, InstrumentId: inst.String(), Quantity: "100"}},
		{"enter with quantity", &v1.DescribedIntent{Kind: v1.IntentKind_INTENT_KIND_ENTER, InstrumentId: inst.String(), Side: v1.Side_SIDE_BUY, Quantity: "100"}},
		{"enter with stop_price", &v1.DescribedIntent{Kind: v1.IntentKind_INTENT_KIND_ENTER, InstrumentId: inst.String(), Side: v1.Side_SIDE_BUY, StopPrice: "1.1000"}},
		{"adjust_stop with side", &v1.DescribedIntent{Kind: v1.IntentKind_INTENT_KIND_ADJUST_STOP, InstrumentId: inst.String(), Side: v1.Side_SIDE_BUY, StopPrice: "1.1000"}},
		{"adjust_stop with quantity", &v1.DescribedIntent{Kind: v1.IntentKind_INTENT_KIND_ADJUST_STOP, InstrumentId: inst.String(), StopPrice: "1.1000", Quantity: "100"}},
		{"target_exposure with stop_price", &v1.DescribedIntent{Kind: v1.IntentKind_INTENT_KIND_TARGET_EXPOSURE, InstrumentId: inst.String(), Side: v1.Side_SIDE_BUY, Quantity: "100", StopPrice: "1.1000"}},
		{"enter_with_stop with quantity", &v1.DescribedIntent{Kind: v1.IntentKind_INTENT_KIND_ENTER_WITH_STOP, InstrumentId: inst.String(), Side: v1.Side_SIDE_BUY, StopPrice: "1.1000", Quantity: "100"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory := newIntentFactory(t, "external_test")
			_, _, err := external.IntentsFromWire([]*v1.DescribedIntent{tt.di}, factory)
			require.ErrorIs(t, err, external.ErrInvalidWireValue)
		})
	}
}

func TestIntentsFromWire_NilFactoryRejected(t *testing.T) {
	_, _, err := external.IntentsFromWire(nil, nil)
	require.Error(t, err)
}
