package data

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/rustyeddy/trader/market"
)

func TestBuildDataSets(t *testing.T) {
	start := time.Date(2003, 01, 01, 0, 0, 0, 0, time.UTC)
	end := time.Date(2003, 01, 03, 0, 0, 0, 0, time.UTC)
	dm := &DataManager{
		Instruments: market.InstrumentList,
		Start:       start,
		End:         end,
	}
	assert.NotNil(t, dm)

	// Cancel immediately to avoid blocking and network calls.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dm.BuildDatasets(ctx)
	assert.Equal(t, len(market.InstrumentList), len(dm.data))

	for sym, ds := range dm.data {
		assert.Equal(t, sym, ds.symbol)
		assert.NotNil(t, ds)
	}
}
