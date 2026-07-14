package indicator

import (
	"math"
	"testing"

	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bbCandle builds a flat candle with the given close (all OHLC = close).
func bbCandle(close types.Price) market.Candle {
	return market.Candle{Open: close, High: close, Low: close, Close: close}
}

func TestBollingerBands_NotReadyUntilNBars(t *testing.T) {
	t.Parallel()
	b, err := NewBollingerBands(3, 2.0, types.PriceScale)
	require.NoError(t, err)
	assert.False(t, b.Ready())
	b.Update(bbCandle(100000))
	assert.False(t, b.Ready())
	b.Update(bbCandle(100000))
	assert.False(t, b.Ready())
	b.Update(bbCandle(100000))
	assert.True(t, b.Ready())
}

func TestBollingerBands_MiddleIsCorrectSMA(t *testing.T) {
	t.Parallel()
	b, err := NewBollingerBands(3, 2.0, types.PriceScale)
	require.NoError(t, err)
	// Feed 1.00, 1.02, 1.04 → mean = 1.02  (types.PriceScale=100_000)
	b.Update(bbCandle(100_000))
	b.Update(bbCandle(102_000))
	b.Update(bbCandle(104_000))
	require.True(t, b.Ready())
	assert.Equal(t, types.Price(102_000), b.MiddlePrice())
	assert.InDelta(t, 1.02, b.Middle(), 1e-6)
}

func TestBollingerBands_BandsSymmetric(t *testing.T) {
	t.Parallel()
	b, err := NewBollingerBands(3, 2.0, types.PriceScale)
	require.NoError(t, err)
	b.Update(bbCandle(100_000))
	b.Update(bbCandle(102_000))
	b.Update(bbCandle(104_000))
	require.True(t, b.Ready())
	assert.InDelta(t, b.Upper()-b.Middle(), b.Middle()-b.Lower(), 1e-10)
}

func TestBollingerBands_StdDevCorrect(t *testing.T) {
	t.Parallel()
	// values: 1.0, 1.02, 1.04 → mean=1.02, pop variance=((−0.02)²+0²+(0.02)²)/3
	b, err := NewBollingerBands(3, 2.0, types.PriceScale)
	require.NoError(t, err)
	b.Update(bbCandle(100_000))
	b.Update(bbCandle(102_000))
	b.Update(bbCandle(104_000))
	require.True(t, b.Ready())
	expected := math.Sqrt((0.02*0.02 + 0 + 0.02*0.02) / 3)
	assert.Equal(t, types.Price(1_633), b.StdDevPrice())
	assert.InDelta(t, expected, b.StdDev(), 1.0/float64(types.PriceScale))
}

func TestBollingerBands_FlatLineZeroWidth(t *testing.T) {
	t.Parallel()
	b, err := NewBollingerBands(5, 2.0, types.PriceScale)
	require.NoError(t, err)
	for range 5 {
		b.Update(bbCandle(100_000))
	}
	require.True(t, b.Ready())
	assert.Equal(t, 0.0, b.StdDev())
	assert.Equal(t, b.Upper(), b.Middle())
	assert.Equal(t, b.Lower(), b.Middle())
}

func TestBollingerBands_PriceAccessors(t *testing.T) {
	t.Parallel()
	b, err := NewBollingerBands(3, 2.0, types.PriceScale)
	require.NoError(t, err)
	b.Update(bbCandle(100_000))
	b.Update(bbCandle(102_000))
	b.Update(bbCandle(104_000))
	require.True(t, b.Ready())
	assert.Equal(t, b.MiddlePrice(), types.Price(math.Round(b.Middle()*float64(types.PriceScale))))
	assert.Equal(t, b.UpperPrice(), types.Price(math.Round(b.Upper()*float64(types.PriceScale))))
	assert.Equal(t, b.LowerPrice(), types.Price(math.Round(b.Lower()*float64(types.PriceScale))))
}

func TestBollingerBands_PercentB(t *testing.T) {
	t.Parallel()
	b, err := NewBollingerBands(3, 2.0, types.PriceScale)
	require.NoError(t, err)
	b.Update(bbCandle(100_000))
	b.Update(bbCandle(102_000))
	b.Update(bbCandle(104_000))
	require.True(t, b.Ready())
	assert.InDelta(t, 0.5, b.PercentB(b.Middle()), 1e-9)
	assert.InDelta(t, 1.0, b.PercentB(b.Upper()), 1e-9)
	assert.InDelta(t, 0.0, b.PercentB(b.Lower()), 1e-9)
}

func TestBollingerBands_PercentBFlatLine(t *testing.T) {
	t.Parallel()
	b, err := NewBollingerBands(3, 2.0, types.PriceScale)
	require.NoError(t, err)
	for range 3 {
		b.Update(bbCandle(100_000))
	}
	require.True(t, b.Ready())
	// Zero-width bands → always returns 0.5
	assert.Equal(t, 0.5, b.PercentB(1.0))
}

func TestBollingerBands_BandWidth(t *testing.T) {
	t.Parallel()
	b, err := NewBollingerBands(3, 2.0, types.PriceScale)
	require.NoError(t, err)
	b.Update(bbCandle(100_000))
	b.Update(bbCandle(102_000))
	b.Update(bbCandle(104_000))
	require.True(t, b.Ready())
	expected := (b.Upper() - b.Lower()) / b.Middle()
	assert.InDelta(t, expected, b.BandWidth(), 1.0/float64(types.PriceScale))
}

func TestBollingerBands_Reset(t *testing.T) {
	t.Parallel()
	b, err := NewBollingerBands(3, 2.0, types.PriceScale)
	require.NoError(t, err)
	for range 3 {
		b.Update(bbCandle(100_000))
	}
	require.True(t, b.Ready())
	b.Reset()
	assert.False(t, b.Ready())
	assert.Equal(t, 0.0, b.Middle())
	assert.Equal(t, 0.0, b.Upper())
	assert.Equal(t, 0.0, b.Lower())
}

func TestBollingerBands_RollingWindowEvictsOldValues(t *testing.T) {
	t.Parallel()
	// Feed 5 bars at 1.0 then 1 bar at 1.1; period=3 so window is [1.0, 1.0, 1.1].
	b, err := NewBollingerBands(3, 2.0, types.PriceScale)
	require.NoError(t, err)
	for range 5 {
		b.Update(bbCandle(100_000))
	}
	b.Update(bbCandle(110_000))
	require.True(t, b.Ready())
	// mean = (1.0 + 1.0 + 1.1) / 3
	assert.Equal(t, types.Price(103_333), b.MiddlePrice())
	assert.InDelta(t, (1.0+1.0+1.1)/3.0, b.Middle(), 1.0/float64(types.PriceScale))
}

func TestBollingerBands_Name(t *testing.T) {
	t.Parallel()
	b, err := NewBollingerBands(20, 2.0, types.PriceScale)
	require.NoError(t, err)
	assert.Equal(t, "BB(20,2.0)", b.Name())
}
