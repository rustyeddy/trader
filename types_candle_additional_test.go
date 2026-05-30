package trader

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewMonthlyCandleSet_Guards performs TestNewMonthlyCandleSet_Guards.
func TestNewMonthlyCandleSet_Guards(t *testing.T) {
	t.Parallel()

	monthStart := Timestamp(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Unix())

	_, err := newMonthlyCandleSet("", M1, monthStart, PriceScale, SourceCandles)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blank instrument")

	_, err = newMonthlyCandleSet("EURUSD", TF0, monthStart, PriceScale, SourceCandles)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timeframe")

	badMinute := Timestamp(time.Date(2026, time.January, 1, 0, 0, 30, 0, time.UTC).Unix())
	_, err = newMonthlyCandleSet("EURUSD", M1, badMinute, PriceScale, SourceCandles)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "minute boundary")

	badMonthStart := Timestamp(time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC).Unix())
	_, err = newMonthlyCandleSet("EURUSD", M1, badMonthStart, PriceScale, SourceCandles)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start of month")
}

// TestCandleSetAddCandle_Branches performs TestCandleSetAddCandle_Branches.
func TestCandleSetAddCandle_Branches(t *testing.T) {
	t.Parallel()

	c := Candle{Open: 1, High: 2, Low: 1, Close: 2, Ticks: 1}

	var nilSet *candleSet
	err := nilSet.AddCandle(1, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil CandleSet")

	cs := &candleSet{Start: 100, Timeframe: M1, Candles: make([]Candle, 2), Valid: make([]uint64, 1)}

	err = cs.AddCandle(99, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "before set start")
	assert.Equal(t, 1, cs.outOfRange)

	err = cs.AddCandle(101, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not aligned")

	err = cs.AddCandle(100+2*Timestamp(M1), c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
	assert.Equal(t, 2, cs.outOfRange)

	err = cs.AddCandle(100, c)
	require.NoError(t, err)
	assert.True(t, cs.IsValid(0))

	err = cs.AddCandle(100, c)
	require.NoError(t, err)
	assert.Equal(t, 1, cs.duplicates)

	badTF := &candleSet{Start: 100, Timeframe: 0, Candles: make([]Candle, 1), Valid: make([]uint64, 1)}
	err = badTF.AddCandle(100, c)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timeframe")
}
