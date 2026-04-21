package trader

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testIterator struct {
	nextFn       func() bool
	candleTimeFn func() candleTime
	err          error
	closeErr     error
	closed       bool
}

func (it *testIterator) Next() bool {
	if it.nextFn == nil {
		return false
	}
	return it.nextFn()
}

func (it *testIterator) Candle() Candle {
	return it.CandleTime().Candle
}

func (it *testIterator) CandleTime() candleTime {
	if it.candleTimeFn == nil {
		return candleTime{}
	}
	return it.candleTimeFn()
}

func (it *testIterator) NextCandle() (Candle, bool) {
	if !it.Next() {
		return Candle{}, false
	}
	return it.Candle(), true
}

func (it *testIterator) Timestamp() Timestamp {
	return it.CandleTime().Timestamp
}

func (it *testIterator) Err() error {
	return it.err
}

func (it *testIterator) Close() error {
	it.closed = true
	return it.closeErr
}

type testStrategy struct {
	name     string
	updateFn func(context.Context, *candleTime, *Positions) *StrategyPlan
}

func (s testStrategy) Name() string {
	if s.name == "" {
		return "test"
	}
	return s.name
}

func (s testStrategy) Update(ctx context.Context, candle *candleTime, positions *Positions) *StrategyPlan {
	if s.updateFn == nil {
		return &DefaultStrategyPlan
	}
	return s.updateFn(ctx, candle, positions)
}

func newTestTrader() *Trader {
	am := NewAccountManager()
	return &Trader{
		Account: am.CreateAccount("test", 1000),
		Broker: &Broker{
			ID: NewULID(),
			OpenOrders: OpenOrders{
				Orders: make(map[string]*order),
			},
		},
	}
}

func TestTrader(t *testing.T) {
	trader := newTestTrader()

	bars := []candleTime{
		{
			Candle:    Candle{Open: 1100000, High: 1101000, Low: 1099000, Close: 1100500, Ticks: 10},
			Timestamp: FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		},
		{
			Candle:    Candle{Open: 1100500, High: 1102000, Low: 1100000, Close: 1101500, Ticks: 11},
			Timestamp: FromTime(time.Date(2026, time.January, 1, 1, 0, 0, 0, time.UTC)),
		},
		{
			Candle:    Candle{Open: 1101500, High: 1103000, Low: 1101000, Close: 1102500, Ticks: 12},
			Timestamp: FromTime(time.Date(2026, time.January, 1, 2, 0, 0, 0, time.UTC)),
		},
	}

	idx := -1
	iter := &testIterator{
		nextFn: func() bool {
			idx++
			return idx < len(bars)
		},
		candleTimeFn: func() candleTime {
			if idx < 0 || idx >= len(bars) {
				return candleTime{}
			}
			return bars[idx]
		},
	}

	cfg := &ConfigBackTest{Instrument: "EURUSD", Strategy: "noop", TimeFrame: M1}
	err := trader.backTestWithIterator(context.Background(), cfg, testStrategy{name: "noop"}, iter)
	assert.NoError(t, err)
	assert.True(t, iter.closed)
}

func TestBackTestRejectsUnknownStrategy(t *testing.T) {
	trader := newTestTrader()
	trader.DataManager = &DataManager{}

	cfg := &ConfigBackTest{
		Instrument: "EURUSD",
		Strategy:   "does-not-exist",
		TimeFrame:  M1,
		Start:      time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		End:        time.Date(2026, time.January, 2, 0, 0, 0, 0, time.UTC),
		Account:    "test",
	}

	err := trader.BackTest(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported strategy")
}

func TestBackTestWithIteratorReturnsContextCancellation(t *testing.T) {
	trader := newTestTrader()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	iter := &testIterator{
		nextFn: func() bool {
			cancel()
			return true
		},
		candleTimeFn: func() candleTime {
			return candleTime{
				Candle:    Candle{Close: Price(1100000)},
				Timestamp: FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
			}
		},
	}

	cfg := &ConfigBackTest{Instrument: "EURUSD", Strategy: "noop", TimeFrame: M1}
	err := trader.backTestWithIterator(ctx, cfg, testStrategy{name: "noop"}, iter)
	require.ErrorIs(t, err, context.Canceled)
	assert.True(t, iter.closed)
}

func TestBackTestWithIteratorReturnsIteratorError(t *testing.T) {
	trader := newTestTrader()
	iterErr := errors.New("iterator failed")
	iter := &testIterator{
		nextFn: func() bool { return false },
		err:    iterErr,
	}

	cfg := &ConfigBackTest{Instrument: "EURUSD", Strategy: "noop", TimeFrame: M1}
	err := trader.backTestWithIterator(context.Background(), cfg, testStrategy{name: "noop"}, iter)
	require.ErrorIs(t, err, iterErr)
	assert.True(t, iter.closed)
}

func TestBackTestWithIteratorReturnsBrokerEventError(t *testing.T) {
	trader := newTestTrader()
	iter := &testIterator{
		nextFn: func() bool { return false },
	}

	position := &Position{
		TradeCommon: NewTradeHistory("EURUSD").TradeCommon,
		FillPrice:   Price(1100000),
		State:       PositionOpen,
	}

	strategy := testStrategy{
		name: "bad-close",
		updateFn: func(ctx context.Context, candle *candleTime, positions *Positions) *StrategyPlan {
			return &StrategyPlan{
				Closes: []*closeRequest{{
					Request: Request{
						TradeCommon: position.TradeCommon,
						RequestType: RequestClose,
						Price:       Price(1090000),
						Timestamp:   FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
					},
				}},
			}
		},
	}

	iter.nextFn = func() bool {
		if iter.closed {
			return false
		}
		iter.nextFn = func() bool { return false }
		return true
	}
	iter.candleTimeFn = func() candleTime {
		return candleTime{
			Candle:    Candle{Close: Price(1100000)},
			Timestamp: FromTime(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)),
		}
	}

	cfg := &ConfigBackTest{Instrument: "EURUSD", Strategy: "fake", TimeFrame: M1}
	err := trader.backTestWithIterator(context.Background(), cfg, strategy, iter)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing position")
}

type testCycleStrategy struct {
	instrument string
	waitBars   int
	holdBars   int
	stopPips   float64
	totalBars  int

	bar        int
	nextOpenAt int
	openedAt   int
	longNext   bool
}

func (s *testCycleStrategy) Name() string {
	return "test-cycle"
}

func (s *testCycleStrategy) Update(ctx context.Context, candle *candleTime, positions *Positions) *StrategyPlan {
	_ = ctx

	if candle == nil {
		return &DefaultStrategyPlan
	}

	if s.waitBars <= 0 {
		s.waitBars = 10
	}
	if s.holdBars <= 0 {
		s.holdBars = 6
	}
	if s.stopPips <= 0 {
		s.stopPips = 20
	}
	if s.bar == 0 && s.nextOpenAt == 0 {
		s.nextOpenAt = 1
		s.longNext = true
	}

	s.bar++

	plan := &StrategyPlan{
		Reason: "hold",
	}

	// If a position is open, close it after holdBars or on the very last bar.
	if positions.Len() > 0 {
		shouldClose := (s.bar - s.openedAt) >= s.holdBars
		if s.totalBars > 0 && s.bar >= s.totalBars {
			shouldClose = true
		}

		if shouldClose {
			positions.Range(func(pos *Position) error {
				cl := &closeRequest{
					Request: Request{
						TradeCommon: pos.TradeCommon,
						Reason:      "CycleClose",
						Candle:      candle.Candle,
						RequestType: RequestClose,
						Price:       candle.Close,
						Timestamp:   candle.Timestamp,
					},
					// Keep the existing enum simple for now.
					CloseCause: CloseStopLoss,
					Position:   pos,
				}
				plan.Closes = append(plan.Closes, cl)
				return nil
			})

			s.nextOpenAt = s.bar + s.waitBars
			s.longNext = !s.longNext
			return plan
		}

		return plan
	}

	// Flat: open on schedule.
	if s.bar < s.nextOpenAt {
		return plan
	}

	side := Long
	if !s.longNext {
		side = Short
	}

	inst := GetInstrument(s.instrument)
	if inst == nil {
		plan.Reason = "missing instrument"
		return plan
	}

	var stop Price
	if side == Long {
		stop = inst.SubPips(candle.Close, pipsFromFloat(s.stopPips))
	} else {
		stop = inst.AddPips(candle.Close, pipsFromFloat(s.stopPips))
	}

	op := newOpenRequest(s.instrument, candle, side, stop, Price(0), "cycle-open")
	plan.Opens = append(plan.Opens, op)
	plan.Reason = "cycle-open"

	s.openedAt = s.bar
	return plan
}

func flattenValidCandles(sets []*candleSet) []candleTime {
	var out []candleTime

	for _, cs := range sets {
		for i := range cs.Candles {
			if !cs.IsValid(i) {
				continue
			}
			out = append(out, candleTime{
				Candle:    cs.Candles[i],
				Timestamp: cs.Timestamp(i),
			})
		}
	}

	return out
}

func TestTraderFakeStrategyYearlyLifecycle(t *testing.T) {
	trader := newTestTrader()

	cfg := DefaultSyntheticConfig("EURUSD")
	cfg.Timeframe = H1
	cfg.Seed = 4242
	cfg.Trend = 0.00002
	cfg.Volatility = 0.0015
	cfg.TicksPerBar = 20

	sets, err := cfg.GenerateSyntheticYearlyCandles(2024)
	require.NoError(t, err)

	bars := flattenValidCandles(sets)
	require.NotEmpty(t, bars)
	require.Greater(t, len(bars), 1000)

	idx := -1
	iter := &testIterator{
		nextFn: func() bool {
			idx++
			return idx < len(bars)
		},
		candleTimeFn: func() candleTime {
			if idx < 0 || idx >= len(bars) {
				return candleTime{}
			}
			return bars[idx]
		},
	}

	strategy := &testCycleStrategy{
		instrument: "EURUSD",
		waitBars:   8,
		holdBars:   6,
		stopPips:   20,
		totalBars:  len(bars),
	}

	cfgBacktest := &ConfigBackTest{
		Instrument: "EURUSD",
		Strategy:   "fake",
		TimeFrame:  H1,
	}

	err = trader.backTestWithIterator(context.Background(), cfgBacktest, strategy, iter)
	require.NoError(t, err)
	assert.True(t, iter.closed)

	// We want repeated opens/closes over a long run.
	assert.Greater(t, len(trader.Account.Trades), 20)

	// The cycle strategy should force-close by the end.
	assert.Equal(t, 0, trader.Account.Positions.Len())

	// Once all positions are closed, equity should equal balance.
	assert.Equal(t, trader.Account.Balance, trader.Account.Equity)
}
