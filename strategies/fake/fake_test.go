package fake

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader"
	"github.com/rustyeddy/trader/execution"
)

func fakeRun(instrument string) *trader.Backtest {
	return &trader.Backtest{
		Request: &trader.BacktestRequest{Instrument: instrument},
		State:   &trader.BacktestRun{Lots: &execution.LotBook{}},
	}
}

func fakeCandle(ts trader.Timestamp, close, high, low float64) *trader.CandleTime {
	return &trader.CandleTime{
		Timestamp: ts,
		Candle: trader.Candle{
			Close: trader.PriceFromFloat(close),
			High:  trader.PriceFromFloat(high),
			Low:   trader.PriceFromFloat(low),
		},
	}
}

func TestFake_NameResetReady(t *testing.T) {
	t.Parallel()

	f := &Fake{CandleCount: 2, candles: []*trader.CandleTime{{}, {}}, highest: 10, lowest: 5}
	assert.Equal(t, "Fake", f.Name())
	assert.True(t, f.Ready())

	f.Reset()
	assert.Len(t, f.candles, 2)
	assert.Nil(t, f.candles[0])
	assert.Nil(t, f.candles[1])
	assert.Equal(t, trader.Price(0), f.highest)
	assert.Equal(t, trader.Price(0), f.lowest)
}

func TestFake_Update_OpensAfterWarmupOnHigherHigh(t *testing.T) {
	t.Parallel()

	f := &Fake{CandleCount: 2}
	run := fakeRun("EURUSD")

	plan1 := f.Update(context.Background(), fakeCandle(1, 1.1000, 1.1010, 1.0990), run)
	require.NotNil(t, plan1)
	assert.Empty(t, plan1.Opens)

	plan2 := f.Update(context.Background(), fakeCandle(2, 1.1020, 1.1030, 1.1000), run)
	require.NotNil(t, plan2)
	require.Len(t, plan2.Opens, 1)
	assert.Equal(t, trader.Long, plan2.Opens[0].Side)
	assert.Equal(t, "EURUSD", plan2.Opens[0].Instrument)
}

func TestFake_Update_MissingInstrumentReturnsNil(t *testing.T) {
	t.Parallel()

	f := &Fake{CandleCount: 1}
	run := fakeRun("NOPE")

	plan := f.Update(context.Background(), fakeCandle(1, 1.1000, 1.1010, 1.0990), run)
	assert.Nil(t, plan)
}

func TestFake_Update_ClosesOpenPositionOnStopBreak(t *testing.T) {
	t.Parallel()

	f := &Fake{CandleCount: 1, highest: trader.PriceFromFloat(2.0)}
	run := fakeRun("EURUSD")

	lot := &execution.Lot{
		TradeCommon: &execution.TradeCommon{
			ID:         trader.NewULID(),
			Instrument: "EURUSD",
			Side:       trader.Long,
			Units:      1000,
			Stop:       trader.PriceFromFloat(1.0950),
		},
		OriginalUnits:  1000,
		RemainingUnits: 1000,
		State:          execution.LotOpen,
	}
	run.State.Lots.Add(lot)

	plan := f.Update(context.Background(), fakeCandle(10, 1.0940, 1.0900, 1.0890), run)
	require.NotNil(t, plan)
	require.Len(t, plan.Closes, 1)
	assert.Equal(t, execution.CloseStopLoss, plan.Closes[0].CloseCause)
	assert.Equal(t, lot.ID, plan.Closes[0].Lot.ID)
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

	openPlan := f.Update(context.Background(), fakeCandle(1, 1.1000, 1.1010, 1.0990), run)
	require.NotNil(t, openPlan)
	require.Len(t, openPlan.Opens, 1)
	assert.Equal(t, "fake-02-open", openPlan.Reason)

	openLot := &execution.Lot{TradeCommon: openPlan.Opens[0].TradeCommon, OriginalUnits: openPlan.Opens[0].Units, RemainingUnits: openPlan.Opens[0].Units, State: execution.LotOpen}
	run.State.Lots.Add(openLot)

	holdPlan := f.Update(context.Background(), fakeCandle(2, 1.1005, 1.1015, 1.0995), run)
	require.NotNil(t, holdPlan)
	assert.Empty(t, holdPlan.Closes)

	closePlan := f.Update(context.Background(), fakeCandle(3, 1.1002, 1.1012, 1.0992), run)
	require.NotNil(t, closePlan)
	require.Len(t, closePlan.Closes, 1)
	assert.Equal(t, "fake-02-close", closePlan.Reason)
	assert.Equal(t, execution.CloseManual, closePlan.Closes[0].CloseCause)
}

func TestFake02_Update_MissingInstrument(t *testing.T) {
	t.Parallel()

	f := &Fake02{WaitBars: 1, HoldBars: 2, StopPips: 10}
	run := fakeRun("NOPE")

	plan := f.Update(context.Background(), fakeCandle(1, 1.1000, 1.1010, 1.0990), run)
	require.NotNil(t, plan)
	assert.Equal(t, "fake-02-missing-instrument", plan.Reason)
	assert.Empty(t, plan.Opens)
}
