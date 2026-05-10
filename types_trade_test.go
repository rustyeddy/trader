package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTrade_AssignsTradeCommonPointer_Phase1(t *testing.T) {
	t.Parallel()

	common := &TradeCommon{
		ID:         "T-1",
		Instrument: "EURUSD",
		Side:       Long,
		Units:      1000,
	}

	tr := newTrade(common)
	require.NotNil(t, tr)
	assert.Same(t, common, tr.TradeCommon)
	assert.Equal(t, "T-1", tr.ID)
	assert.Equal(t, "EURUSD", tr.Instrument)
	assert.Equal(t, Units(1000), tr.Units)
}
