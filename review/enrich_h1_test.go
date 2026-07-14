package review

import (
	"testing"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
)

// decliningCandles builds a monotonic downtrend, the mirror image of
// trendingCandles, for exercising a countertrend H1 series.
func decliningCandles(n int) []market.Candle {
	candles := make([]market.Candle, 0, n)
	price := 1.10000
	for range n {
		open := price
		price -= 0.00150
		close := price
		high := open + 0.00005
		low := close - 0.00005
		candles = append(candles, market.Candle{
			Open:  types.PriceFromFloat(open),
			High:  types.PriceFromFloat(high),
			Low:   types.PriceFromFloat(low),
			Close: types.PriceFromFloat(close),
		})
	}
	return candles
}

func tradeableResult(instrument string) ReviewResult {
	return ReviewResult{
		Instrument: instrument,
		Bucket:     "tradeable",
		H4:         H4Snapshot{Close: 1.10500, EMA20: 1.10000}, // H4 bias: long
	}
}

func TestEnrichWithH1_NoopForNonTradeableBucket(t *testing.T) {
	for _, bucket := range []string{"watch", "hot"} {
		result := ReviewResult{Instrument: "EURUSD", Bucket: bucket}
		before := result

		got := EnrichWithH1(result, trendingCandles(80))

		assert.Equal(t, before, got, "bucket %q must be returned unchanged", bucket)
	}
}

func TestEnrichWithH1_UnavailableAddsNoteButKeepsTradeable(t *testing.T) {
	result := tradeableResult("EURUSD")

	got := EnrichWithH1(result, nil)

	assert.Equal(t, "tradeable", got.Bucket)
	assert.Equal(t, H1Snapshot{}, got.H1)
	assert.Zero(t, got.Setup.H1Aligned)
	assert.Zero(t, got.Setup.H1EntryDist)
	assert.Contains(t, got.Notes, "H1 unavailable")
}

func TestEnrichWithH1_UnknownInstrumentIsNoop(t *testing.T) {
	result := tradeableResult("NOTAPAIR")
	before := result

	got := EnrichWithH1(result, trendingCandles(80))

	assert.Equal(t, before, got)
}

func TestEnrichWithH1_AlignedTrue(t *testing.T) {
	result := tradeableResult("EURUSD") // H4 bias: long

	got := EnrichWithH1(result, trendingCandles(80)) // H1 bias: long, matches H4

	assert.True(t, got.Setup.H1Aligned)
	assert.NotZero(t, got.H1.EMA20)
	assert.NotEqual(t, 0.0, got.Setup.H1EntryDist)
}

func TestEnrichWithH1_AlignedFalse(t *testing.T) {
	result := tradeableResult("EURUSD") // H4 bias: long

	got := EnrichWithH1(result, decliningCandles(80)) // H1 bias: short, conflicts with H4

	assert.False(t, got.Setup.H1Aligned)
}

func TestComputeH1_NotReadyOnEmptyCandles(t *testing.T) {
	inst := market.GetInstrument("EURUSD")
	_, _, ok := computeH1(inst, nil)
	assert.False(t, ok)
}

func TestComputeH1_NotReadyOnInsufficientCandles(t *testing.T) {
	inst := market.GetInstrument("EURUSD")
	// 10 candles is well short of the EMA(50) warmup requirement.
	_, _, ok := computeH1(inst, trendingCandles(10))
	assert.False(t, ok)
}

func TestComputeH1_ReadyProducesBiasAndSnapshot(t *testing.T) {
	inst := market.GetInstrument("EURUSD")
	snap, bias, ok := computeH1(inst, trendingCandles(80))
	assert.True(t, ok)
	assert.Equal(t, "long", bias)
	assert.Greater(t, snap.EMA20, 0.0)
	assert.Greater(t, snap.EMA50, 0.0)
}
