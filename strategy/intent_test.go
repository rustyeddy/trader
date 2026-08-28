package strategy

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
)

var testStart = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

func testFactory(t *testing.T) (IntentFactory, *id.Generator) {
	t.Helper()
	c := clock.NewSimulated(testStart)
	gen := id.NewGenerator(c, id.NewDeterministic(1, 2))
	return NewIntentFactory(c, gen, "strategy.test"), gen
}

func testInstrumentID(t *testing.T) instrument.ID {
	t.Helper()
	inst, err := instrument.NewCurrencyPair(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))
	require.NoError(t, err)
	return inst.ID()
}

func TestIntentFactory_Enter(t *testing.T) {
	f, _ := testFactory(t)
	instID := testInstrumentID(t)

	in, err := f.Enter(instID, order.Buy)
	require.NoError(t, err)
	assert.Equal(t, order.IntentEnter, in.Kind)
	assert.Equal(t, instID, in.Instrument)
	assert.Equal(t, order.Buy, in.Side)
	assert.Nil(t, in.Quantity)
	assert.Nil(t, in.StopPrice)
	assert.False(t, in.IntentID.IsZero())
	assert.False(t, in.Metadata.EventID.IsZero())
	assert.False(t, in.Metadata.CorrelationID.IsZero())
	assert.True(t, in.Metadata.CausationID.IsZero(), "an Intent is the first stage of its own workflow")
	assert.Equal(t, testStart, in.Metadata.Timestamp)
	assert.Equal(t, id.Source("strategy.test"), in.Metadata.Source)
}

func TestIntentFactory_Exit(t *testing.T) {
	f, _ := testFactory(t)
	instID := testInstrumentID(t)

	in, err := f.Exit(instID)
	require.NoError(t, err)
	assert.Equal(t, order.IntentExit, in.Kind)
	assert.Equal(t, instID, in.Instrument)
}

func TestIntentFactory_AdjustStop(t *testing.T) {
	f, _ := testFactory(t)
	instID := testInstrumentID(t)
	stop := num.MustParsePrice("1.05000")

	in, err := f.AdjustStop(instID, stop)
	require.NoError(t, err)
	assert.Equal(t, order.IntentAdjustStop, in.Kind)
	require.NotNil(t, in.StopPrice)
	assert.True(t, in.StopPrice.Equal(stop))
}

func TestIntentFactory_TargetExposure(t *testing.T) {
	f, _ := testFactory(t)
	instID := testInstrumentID(t)
	qty := num.MustParseQuantity("1000")

	in, err := f.TargetExposure(instID, order.Sell, qty)
	require.NoError(t, err)
	assert.Equal(t, order.IntentTargetExposure, in.Kind)
	assert.Equal(t, order.Sell, in.Side)
	require.NotNil(t, in.Quantity)
	assert.True(t, in.Quantity.Equal(qty))
}

// TestIntentFactory_DefaultCorrelationIsFreshPerCall proves the
// documented default: two independent calls, neither grouped via
// WithCorrelation, get their own distinct CorrelationID.
func TestIntentFactory_DefaultCorrelationIsFreshPerCall(t *testing.T) {
	f, _ := testFactory(t)
	instID := testInstrumentID(t)

	first, err := f.Enter(instID, order.Buy)
	require.NoError(t, err)
	second, err := f.Exit(instID)
	require.NoError(t, err)

	assert.NotEqual(t, first.Metadata.CorrelationID, second.Metadata.CorrelationID)
}

// TestIntentFactory_WithCorrelationGroupsIntents proves a strategy can
// explicitly group multiple intents (for example a reversal expressed
// as an exit plus an enter) under one shared CorrelationID.
func TestIntentFactory_WithCorrelationGroupsIntents(t *testing.T) {
	f, _ := testFactory(t)
	instID := testInstrumentID(t)

	corr, err := f.NewCorrelationID()
	require.NoError(t, err)
	grouped := f.WithCorrelation(corr)

	exit, err := grouped.Exit(instID)
	require.NoError(t, err)
	enter, err := grouped.Enter(instID, order.Buy)
	require.NoError(t, err)

	assert.Equal(t, corr, exit.Metadata.CorrelationID)
	assert.Equal(t, corr, enter.Metadata.CorrelationID)
	assert.NotEqual(t, exit.IntentID, enter.IntentID, "grouped intents still get distinct identities")
	assert.NotEqual(t, exit.Metadata.EventID, enter.Metadata.EventID)
}

// TestIntentFactory_WithCorrelationDoesNotMutateOriginal proves
// WithCorrelation returns an independent factory: the original keeps
// minting a fresh CorrelationID per call.
func TestIntentFactory_WithCorrelationDoesNotMutateOriginal(t *testing.T) {
	f, _ := testFactory(t)
	instID := testInstrumentID(t)

	corr, err := f.NewCorrelationID()
	require.NoError(t, err)
	_ = f.WithCorrelation(corr)

	first, err := f.Enter(instID, order.Buy)
	require.NoError(t, err)
	second, err := f.Exit(instID)
	require.NoError(t, err)

	assert.NotEqual(t, corr, first.Metadata.CorrelationID)
	assert.NotEqual(t, first.Metadata.CorrelationID, second.Metadata.CorrelationID)
}

// TestIntentFactory_DeterministicAcrossIndependentInstances proves two
// factories built from identical initial deterministic inputs produce
// identical Intent values — the same cross-instance determinism
// guarantee execution.Planner and risk.Sizer already establish.
func TestIntentFactory_DeterministicAcrossIndependentInstances(t *testing.T) {
	instID := testInstrumentID(t)

	build := func() order.Intent {
		c := clock.NewSimulated(testStart)
		gen := id.NewGenerator(c, id.NewDeterministic(1, 2))
		f := NewIntentFactory(c, gen, "strategy.test")
		in, err := f.Enter(instID, order.Buy)
		require.NoError(t, err)
		return in
	}

	first := build()
	second := build()
	assert.Equal(t, first, second)
}
