package account

import (
	"testing"

	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTradeCommonClone(t *testing.T) {
	t.Parallel()

	tc := &TradeCommon{
		ID:         "t1",
		Instrument: "EURUSD",
		Side:       types.Long,
		Units:      1000,
		Stop:       types.PriceFromFloat(1.0900),
		Take:       types.PriceFromFloat(1.1100),
	}

	cp := tc.Clone()
	require.NotNil(t, cp)
	assert.Equal(t, *tc, *cp)

	cp.Instrument = "GBPUSD"
	cp.Stop = types.PriceFromFloat(1.0800)
	assert.Equal(t, "EURUSD", tc.Instrument)
	assert.Equal(t, types.PriceFromFloat(1.0900), tc.Stop)
}

func TestTradeCloneDeepCopy(t *testing.T) {
	t.Parallel()

	tr := &Trade{
		TradeCommon: &TradeCommon{
			ID:         "t1",
			Instrument: "EURUSD",
			Side:       types.Long,
			Units:      1000,
		},
		EntryPrice: types.PriceFromFloat(1.1000),
		EntryTime:  100,
		ExitPrice:  types.PriceFromFloat(1.1010),
		ExitTime:   200,
		PNL:        types.MoneyFromFloat(10),
	}

	cp := tr.Clone()
	require.NotNil(t, cp)
	require.NotNil(t, cp.TradeCommon)
	assert.Equal(t, *tr, *cp)

	cp.TradeCommon.Instrument = "GBPUSD"
	cp.PNL = types.MoneyFromFloat(20)
	assert.Equal(t, "EURUSD", tr.TradeCommon.Instrument)
	assert.Equal(t, types.MoneyFromFloat(10), tr.PNL)
}
