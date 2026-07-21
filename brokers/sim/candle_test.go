package sim

import (
	"testing"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
)

func TestTickFromCandle_SplitsSpreadAroundClose(t *testing.T) {
	candle := market.Candle{
		Close:     types.PriceFromFloat(1.1000),
		AvgSpread: types.PriceFromFloat(0.0002),
		Timestamp: 500,
	}

	tick := TickFromCandle("EURUSD", candle)

	assert.Equal(t, "EURUSD", tick.Instrument)
	assert.Equal(t, candle.Timestamp, tick.Timestamp)
	assert.Equal(t, candle.Close-candle.AvgSpread/2, tick.Bid)
	assert.Equal(t, candle.Close+candle.AvgSpread/2, tick.Ask)
	assert.Equal(t, candle.AvgSpread, tick.Ask-tick.Bid, "synthesized spread must equal the candle's AvgSpread")
}

func TestTickFromCandle_ZeroSpreadCollapsesToClose(t *testing.T) {
	candle := market.Candle{Close: types.PriceFromFloat(1.1000)}

	tick := TickFromCandle("EURUSD", candle)

	assert.Equal(t, candle.Close, tick.Bid)
	assert.Equal(t, candle.Close, tick.Ask)
}
