package strategysdk_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategysdk"
)

func TestEnter(t *testing.T) {
	inst := eurUSD(t)
	d := strategysdk.Enter(inst, order.Buy)
	require.Equal(t, order.IntentEnter, d.Kind)
	require.True(t, d.Instrument.Equal(inst))
	require.Equal(t, order.Buy, d.Side)
	require.Nil(t, d.Quantity)
	require.Nil(t, d.StopPrice)
}

func TestExit(t *testing.T) {
	inst := eurUSD(t)
	d := strategysdk.Exit(inst)
	require.Equal(t, order.IntentExit, d.Kind)
	require.True(t, d.Instrument.Equal(inst))
	require.Zero(t, d.Side)
	require.Nil(t, d.Quantity)
	require.Nil(t, d.StopPrice)
}

func TestAdjustStop(t *testing.T) {
	inst := eurUSD(t)
	stop := num.MustParsePrice("1.0950")
	d := strategysdk.AdjustStop(inst, stop)
	require.Equal(t, order.IntentAdjustStop, d.Kind)
	require.NotNil(t, d.StopPrice)
	require.True(t, d.StopPrice.Equal(stop))
	require.Zero(t, d.Side)
	require.Nil(t, d.Quantity)
}

func TestEnterWithStop(t *testing.T) {
	inst := eurUSD(t)
	stop := num.MustParsePrice("1.0900")
	d := strategysdk.EnterWithStop(inst, order.Buy, stop)
	require.Equal(t, order.IntentEnterWithStop, d.Kind)
	require.Equal(t, order.Buy, d.Side)
	require.NotNil(t, d.StopPrice)
	require.True(t, d.StopPrice.Equal(stop))
	require.Nil(t, d.Quantity)
}

func TestTargetExposure(t *testing.T) {
	inst := eurUSD(t)
	qty := num.MustParseQuantity("500")
	d := strategysdk.TargetExposure(inst, order.Sell, qty)
	require.Equal(t, order.IntentTargetExposure, d.Kind)
	require.Equal(t, order.Sell, d.Side)
	require.NotNil(t, d.Quantity)
	require.True(t, d.Quantity.Equal(qty))
	require.Nil(t, d.StopPrice)
}

func TestDescribedIntent_WithCorrelation(t *testing.T) {
	inst := eurUSD(t)
	d := strategysdk.Enter(inst, order.Buy).WithCorrelation("grp-1")
	require.Equal(t, "grp-1", d.CorrelationToken)

	// The original is unmodified — WithCorrelation returns a copy.
	original := strategysdk.Enter(inst, order.Buy)
	require.Empty(t, original.CorrelationToken)
}
