package fake

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/backtest"
	"github.com/rustyeddy/trader/execution"
	"github.com/rustyeddy/trader/idgen"
	"github.com/rustyeddy/trader/market"
	"github.com/rustyeddy/trader/types"
)

func fakeRun(instrument string) *backtest.Backtest {
	return &backtest.Backtest{
		Request: &backtest.BacktestRequest{Instrument: instrument},
		State:   &backtest.BacktestRun{Lots: &execution.LotBook{}},
	}
}

func fakeCandle(ts types.Timestamp, close, high, low float64) *market.CandleTime {
	return &market.CandleTime{
		Timestamp: ts,
		Candle: market.Candle{
			Close: types.PriceFromFloat(close),
			High:  types.PriceFromFloat(high),
			Low:   types.PriceFromFloat(low),
		},
	}
}

func TestFake_NameResetReady(t *testing.T) {
	t.Parallel()

	f := &Fake{CandleCount: 2, candles: []*market.CandleTime{{}, {}}, highest: 10, lowest: 5}
	assert.Equal(t, "Fake", f.Name())
	assert.True(t, f.Ready())

	f.Reset()
	assert.Len(t, f.candles, 2)
	assert.Nil(t, f.candles[0])
	assert.Nil(t, f.candles[1])
	assert.Equal(t, types.Price(0), f.highest)
	assert.Equal(t, types.Price(0), f.lowest)
}

func TestFake_Update_OpensAfterWarmupOnHigherHigh(t *testing.T) {
	t.Parallel()

	f := &Fake{CandleCount: 2}
	run := fakeRun("EURUSD")

	sig1 := f.Update(context.Background(), fakeCandle(1, 1.1000, 1.1010, 1.0990), run)
	assert.Equal(t, types.Flat, sig1.Side, "warmup bar should hold")

	sig2 := f.Update(context.Background(), fakeCandle(2, 1.1020, 1.1030, 1.1000), run)
	assert.Equal(t, types.Long, sig2.Side)
	assert.Equal(t, "higher highs", sig2.Reason)
}

func TestFake_Update_MissingInstrumentHolds(t *testing.T) {
	t.Parallel()

	f := &Fake{CandleCount: 1}
	run := fakeRun("NOPE")

	// With signals, unknown instrument just emits a Long (stop handled by planner).
	sig := f.Update(context.Background(), fakeCandle(1, 1.1000, 1.1010, 1.0990), run)
	assert.Equal(t, types.Long, sig.Side)
}

func TestFake_Update_HoldsWhenInPosition(t *testing.T) {
	t.Parallel()

	f := &Fake{CandleCount: 1, highest: types.PriceFromFloat(1.0)}
	run := fakeRun("EURUSD")

	lot := &execution.Lot{
		TradeCommon: &execution.TradeCommon{
			ID:    idgen.NewULID(),
			Side:  types.Long,
			Units: 1000,
			Stop:  types.PriceFromFloat(1.0950),
		},
		OriginalUnits:  1000,
		RemainingUnits: 1000,
		State:          execution.LotOpen,
	}
	run.State.Lots.Add(lot)

	// New higher high while in position should hold (not open another).
	sig := f.Update(context.Background(), fakeCandle(10, 1.1050, 1.1060, 1.1030), run)
	assert.Equal(t, types.Flat, sig.Side, "should hold when already in position")
}

func TestFake02_NameResetReady(t *testing.T) {
	t.Parallel()

	f := &Fake02{bar: 10, nextOpenAt: 7, openedAt: 3, longNext: true}
	assert.Equal(t, "Fake02", f.Name())
	assert.True(t, f.Ready())

	f.Reset()
	assert.Equal(t, 0, f.bar)
	assert.Equal(t, 0, f.nextOpenAt)
	assert.Equal(t, 0, f.openedAt)
	assert.False(t, f.longNext)
}

func TestFake02_Update_OpenThenCloseCycle(t *testing.T) {
	t.Parallel()

	f := &Fake02{WaitBars: 1, HoldBars: 2, StopPips: 10}
	run := fakeRun("EURUSD")

	openSig := f.Update(context.Background(), fakeCandle(1, 1.1000, 1.1010, 1.0990), run)
	assert.Equal(t, types.Long, openSig.Side)
	assert.Equal(t, "fake-02-open", openSig.Reason)

	tc := &execution.TradeCommon{ID: idgen.NewULID(), Side: types.Long, Units: 1000, Instrument: "EURUSD"}
	openLot := &execution.Lot{TradeCommon: tc, OriginalUnits: 1000, RemainingUnits: 1000, State: execution.LotOpen}
	run.State.Lots.Add(openLot)

	holdSig := f.Update(context.Background(), fakeCandle(2, 1.1005, 1.1015, 1.0995), run)
	assert.Equal(t, types.Flat, holdSig.Side)

	closeSig := f.Update(context.Background(), fakeCandle(3, 1.1002, 1.1012, 1.0992), run)
	require.True(t, closeSig.CloseAll)
	assert.Equal(t, "fake-02-close", closeSig.Reason)
}

func TestFake02_Update_MissingInstrument(t *testing.T) {
	t.Parallel()

	f := &Fake02{WaitBars: 1, HoldBars: 2, StopPips: 10}
	run := fakeRun("NOPE")

	// With signals, unknown instrument doesn't matter — planner handles stop.
	sig := f.Update(context.Background(), fakeCandle(1, 1.1000, 1.1010, 1.0990), run)
	assert.Equal(t, types.Long, sig.Side)
}
