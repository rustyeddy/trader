package backtest

import (
	"testing"

	"github.com/rustyeddy/trader/account"
	"github.com/rustyeddy/trader/engine"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshotLots(t *testing.T) {
	t.Parallel()

	// SnapshotLots is in trader.go — test via indirect usage through BacktestRun.
	// Directly we can test LotBook copying behavior.
	src := &account.LotBook{}
	lot := &account.Lot{TradeCommon: &account.TradeCommon{ID: "p1", Instrument: "EURUSD", Side: types.Long, Units: 10}, EntryPrice: types.PriceFromFloat(1.1), OriginalUnits: 10, RemainingUnits: 10, State: account.LotOpen}
	src.Add(lot)

	// Use SnapshotLots function from the engine package.
	cp := engine.SnapshotLots(src)
	require.NotNil(t, cp)
	assert.Equal(t, 1, cp.Len())

	src.Delete("p1")
	assert.Equal(t, 0, src.Len())
	assert.Equal(t, 1, cp.Len(), "snapshot should not change after source map mutation")
}
