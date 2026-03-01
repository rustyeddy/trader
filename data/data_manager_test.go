package data

import (
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
	assert.Equal(t, len(market.InstrumentList), len(dm.Instruments))
	assert.Equal(t, start, dm.Start)
	assert.Equal(t, end, dm.End)
}
