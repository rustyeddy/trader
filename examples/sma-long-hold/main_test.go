package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/clock"
	"github.com/rustyeddy/trader/instrument"
	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategysdk"
)

// These are the same in-process-level tests strategy/smatrend's own
// exitrule_test.go/crossstate tests would run for the identical
// logic; the cross-process equivalence proof lives in cmd/trader/
// backtest/sma_long_hold_equivalence_test.go, which needs a real
// subprocess and is therefore comparatively slow and heavyweight —
// these exist so the core decision logic has fast, direct coverage
// too, not just the end-to-end proof.

func TestNewLongHold_RejectsInvalidConfig(t *testing.T) {
	valid := fileConfig{Base: "EUR", Quote: "USD", IntervalUnit: "hour", IntervalCount: 1, SMAPeriod: 5, TrailingStopPercent: "0.10"}

	tests := []struct {
		name   string
		modify func(fileConfig) fileConfig
	}{
		{"non-positive sma period", func(c fileConfig) fileConfig { c.SMAPeriod = 0; return c }},
		{"bad base currency", func(c fileConfig) fileConfig { c.Base = "???"; return c }},
		{"bad quote currency", func(c fileConfig) fileConfig { c.Quote = "???"; return c }},
		{"base equals quote", func(c fileConfig) fileConfig { c.Quote = c.Base; return c }},
		{"bad interval unit", func(c fileConfig) fileConfig { c.IntervalUnit = "fortnight"; return c }},
		{"bad interval count", func(c fileConfig) fileConfig { c.IntervalCount = -1; return c }},
		{"bad trailing stop percent", func(c fileConfig) fileConfig { c.TrailingStopPercent = "not-a-rate"; return c }},
		{"zero trailing stop percent", func(c fileConfig) fileConfig { c.TrailingStopPercent = "0"; return c }},
		{"negative trailing stop percent", func(c fileConfig) fileConfig { c.TrailingStopPercent = "-0.01"; return c }},
		{"trailing stop percent equal to 1", func(c fileConfig) fileConfig { c.TrailingStopPercent = "1"; return c }},
		{"trailing stop percent above 1", func(c fileConfig) fileConfig { c.TrailingStopPercent = "1.50"; return c }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newLongHold(tt.modify(valid))
			require.Error(t, err)
		})
	}
}

func TestNewLongHold_AcceptsValidConfig(t *testing.T) {
	strat, err := newLongHold(fileConfig{
		Base: "EUR", Quote: "USD", IntervalUnit: "hour", IntervalCount: 1,
		SMAPeriod: 5, TrailingStopPercent: "0.10",
	})
	require.NoError(t, err)
	require.NotNil(t, strat)

	d := strat.Describe()
	assert.Equal(t, "sma-long-hold", d.Name)
	require.Len(t, d.Requirements, 1)
	assert.Equal(t, 5, d.Requirements[0].WarmupBars)
	assert.True(t, d.Requirements[0].Instrument.Equal(strat.inst))
}

func TestParseIntervalUnit(t *testing.T) {
	tests := map[string]marketdata.Unit{
		"minute": marketdata.UnitMinute,
		"hour":   marketdata.UnitHour,
		"day":    marketdata.UnitDay,
		"week":   marketdata.UnitWeek,
	}
	for name, want := range tests {
		got, err := parseIntervalUnit(name)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	_, err := parseIntervalUnit("fortnight")
	require.Error(t, err)
}

func TestCrossState_OnlyReportsGenuineCrossAbove(t *testing.T) {
	var s crossState

	// The very first observation can never itself report a cross,
	// regardless of whether it is already "above" — mirroring
	// strategy/smatrend's own crossState identically.
	assert.False(t, s.update(true))

	// Staying above produces no further signal.
	assert.False(t, s.update(true))

	// Dropping to at-or-below, then back above, is a genuine cross.
	assert.False(t, s.update(false))
	assert.True(t, s.update(true))

	// Staying above again produces nothing further.
	assert.False(t, s.update(true))
}

// fakeView is a minimal strategysdk.View for driving OnBar directly,
// without a real host process.
type fakeView struct {
	inst     instrument.ID
	side     order.PositionSide
	avgPrice *num.Price
}

func (v fakeView) Account() strategysdk.AccountSnapshot {
	if v.side == order.Flat {
		return strategysdk.AccountSnapshot{}
	}
	return strategysdk.AccountSnapshot{
		Positions: []strategysdk.PositionSnapshot{{Instrument: v.inst, Side: v.side, AvgPrice: v.avgPrice}},
	}
}

func (v fakeView) HistoryBars(instrument.ID, marketdata.Interval, int) ([]marketdata.Bar, bool, error) {
	return nil, false, nil
}

func testBar(t *testing.T, hour int, o, h, l, c string) marketdata.Bar {
	t.Helper()
	return marketdata.Bar{
		Time:      time.Date(2024, time.June, 3, hour, 0, 0, 0, time.UTC),
		Open:      num.MustParsePrice(o),
		High:      num.MustParsePrice(h),
		Low:       num.MustParsePrice(l),
		Close:     num.MustParsePrice(c),
		AvgSpread: num.MustParsePrice("0.00020"),
		MaxSpread: num.MustParsePrice("0.00020"),
		Ticks:     100,
	}
}

// TestOnBar_EntersOnCrossThenRatchetsThenIgnoresOtherInstrument walks
// this strategy through the same shape of episode
// sma_long_hold_equivalence_test.go's fixture exercises end to end,
// driven directly here for fast, deterministic, single-process
// coverage of the actual decision logic: below-SMA warm-up, no
// action; a cross-above while Flat emits Enter; once Long, a new
// bar High above the ratcheted high-water mark emits AdjustStop; an
// unchanged/lower High emits nothing; and a bar for a different
// instrument is ignored outright.
func TestOnBar_EntersOnCrossThenRatchetsThenIgnoresOtherInstrument(t *testing.T) {
	strat, err := newLongHold(fileConfig{
		Base: "EUR", Quote: "USD", IntervalUnit: "hour", IntervalCount: 1,
		SMAPeriod: 3, TrailingStopPercent: "0.02",
	})
	require.NoError(t, err)
	ctx := context.Background()

	// Warm-up: three declining closes, ending below their own 3-bar
	// average, and Flat throughout.
	bars := []marketdata.Bar{
		testBar(t, 0, "1.10500", "1.10550", "1.10450", "1.10500"),
		testBar(t, 1, "1.10500", "1.10550", "1.10350", "1.10400"),
		testBar(t, 2, "1.10400", "1.10450", "1.10250", "1.10300"),
	}
	for _, bar := range bars {
		intents, signals, err := strat.OnBar(ctx, strategysdk.BarEvent{Instrument: strat.inst, Interval: strat.interval, Bar: bar}, fakeView{inst: strat.inst, side: order.Flat})
		require.NoError(t, err)
		assert.Empty(t, signals)
		assert.Empty(t, intents, "no entry expected during warm-up/below-SMA bars")
	}

	// A bar for a different instrument must be ignored outright, even
	// though it would otherwise look like a fresh cross.
	other := instrument.CurrencyPairID(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))
	intents, _, err := strat.OnBar(ctx, strategysdk.BarEvent{Instrument: other, Interval: strat.interval, Bar: testBar(t, 3, "1.20000", "1.30000", "1.19000", "1.29000")}, fakeView{inst: strat.inst, side: order.Flat})
	require.NoError(t, err)
	assert.Empty(t, intents, "a bar for a different instrument must never be acted on")

	// A sharp rise crosses above the SMA: Enter, while still Flat.
	crossBar := testBar(t, 3, "1.10300", "1.11050", "1.10250", "1.11000")
	intents, signals, err := strat.OnBar(ctx, strategysdk.BarEvent{Instrument: strat.inst, Interval: strat.interval, Bar: crossBar}, fakeView{inst: strat.inst, side: order.Flat})
	require.NoError(t, err)
	require.Len(t, intents, 1)
	assert.Equal(t, order.IntentEnter, intents[0].Kind)
	assert.Equal(t, order.Buy, intents[0].Side)
	require.Len(t, signals, 1, "an entry must also record its own decision evidence")
	assert.Equal(t, "enter-long", signals[0].Values["action"])
	assert.Equal(t, intents[0].CorrelationToken, signals[0].CorrelationToken,
		"the signal must share the entry intent's own correlation token")

	// Position now observed Long (as if the Enter filled): the entry
	// bar's own High seeds the high-water mark, and the first
	// AdjustStop is emitted immediately — mirroring
	// trailingStopExitRule.OnEntry followed by OnLongBar on the same
	// bar the position is first observed Long.
	entryFillBar := testBar(t, 4, "1.11000", "1.11200", "1.10900", "1.11100")
	entryPrice := num.MustParsePrice("1.11000")
	intents, signals, err = strat.OnBar(ctx, strategysdk.BarEvent{Instrument: strat.inst, Interval: strat.interval, Bar: entryFillBar}, fakeView{inst: strat.inst, side: order.Long, avgPrice: &entryPrice})
	require.NoError(t, err)
	require.Len(t, intents, 1)
	assert.Equal(t, order.IntentAdjustStop, intents[0].Kind)
	firstStop := *intents[0].StopPrice
	wantFirstStop, err := num.MustParsePrice("1.11200").MulRate(num.MustParseRate("0.98"))
	require.NoError(t, err)
	assert.True(t, firstStop.Equal(wantFirstStop), "expected %s, got %s", wantFirstStop, firstStop)
	require.Len(t, signals, 1)
	assert.Equal(t, "adjust-stop", signals[0].Values["action"])
	assert.Equal(t, firstStop.String(), signals[0].Values["stop_price"])

	// A higher High ratchets the stop upward.
	higherBar := testBar(t, 5, "1.11100", "1.11500", "1.11000", "1.11400")
	intents, _, err = strat.OnBar(ctx, strategysdk.BarEvent{Instrument: strat.inst, Interval: strat.interval, Bar: higherBar}, fakeView{inst: strat.inst, side: order.Long, avgPrice: &entryPrice})
	require.NoError(t, err)
	require.Len(t, intents, 1)
	assert.Equal(t, order.IntentAdjustStop, intents[0].Kind)
	secondStop := *intents[0].StopPrice
	assert.True(t, secondStop.Cmp(firstStop) > 0, "the stop must only ever ratchet upward")

	// A High no greater than the existing high-water mark emits
	// nothing — the stop never moves down.
	flatBar := testBar(t, 6, "1.11400", "1.11450", "1.11200", "1.11300")
	intents, _, err = strat.OnBar(ctx, strategysdk.BarEvent{Instrument: strat.inst, Interval: strat.interval, Bar: flatBar}, fakeView{inst: strat.inst, side: order.Long, avgPrice: &entryPrice})
	require.NoError(t, err)
	assert.Empty(t, intents)

	// A sharp reversal closes the position (broker-triggered stop,
	// already reflected in the account snapshot by the time OnBar
	// observes it — see OnBar's own doc comment): back to Flat, no new
	// entry on this same bar since there was no fresh cross.
	afterStopBar := testBar(t, 7, "1.11300", "1.11350", "1.09000", "1.09500")
	intents, _, err = strat.OnBar(ctx, strategysdk.BarEvent{Instrument: strat.inst, Interval: strat.interval, Bar: afterStopBar}, fakeView{inst: strat.inst, side: order.Flat})
	require.NoError(t, err)
	assert.Empty(t, intents)
}

func TestNewFromEnv_DefaultsWhenUnset(t *testing.T) {
	t.Setenv("TRADER_STRATEGY_CONFIG", "")
	strat, err := newFromEnv()
	require.NoError(t, err)
	assert.True(t, strat.inst.Equal(instrument.CurrencyPairID(num.MustParseCurrency("EUR"), num.MustParseCurrency("USD"))))
	assert.Equal(t, 20, strat.sma.Period())
}

func TestNewFromEnv_ParsesConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"base": "GBP", "quote": "USD",
		"interval_unit": "day", "interval_count": 1,
		"sma_period": 7,
		"trailing_stop_percent": "0.05"
	}`), 0o600))
	t.Setenv("TRADER_STRATEGY_CONFIG", path)

	strat, err := newFromEnv()
	require.NoError(t, err)
	assert.True(t, strat.inst.Equal(instrument.CurrencyPairID(num.MustParseCurrency("GBP"), num.MustParseCurrency("USD"))))
	assert.Equal(t, 7, strat.sma.Period())
}

func TestNewFromEnv_MissingFileIsAnError(t *testing.T) {
	t.Setenv("TRADER_STRATEGY_CONFIG", filepath.Join(t.TempDir(), "does-not-exist.json"))
	_, err := newFromEnv()
	require.Error(t, err)
}

func TestNewFromEnv_MalformedJSONIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))
	t.Setenv("TRADER_STRATEGY_CONFIG", path)

	_, err := newFromEnv()
	require.Error(t, err)
}

func TestStart_LogsWithoutError(t *testing.T) {
	strat, err := newLongHold(defaultConfig())
	require.NoError(t, err)

	err = strat.Start(context.Background(), strategysdk.Environment{
		Clock:  clock.NewSimulated(time.Date(2024, time.June, 3, 0, 0, 0, 0, time.UTC)),
		RunID:  "run_test",
		Logger: slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)
}

func TestOnBar_UnexpectedShortPositionIsAnError(t *testing.T) {
	strat, err := newLongHold(fileConfig{
		Base: "EUR", Quote: "USD", IntervalUnit: "hour", IntervalCount: 1,
		SMAPeriod: 3, TrailingStopPercent: "0.10",
	})
	require.NoError(t, err)
	ctx := context.Background()

	for _, bar := range []marketdata.Bar{
		testBar(t, 0, "1.10500", "1.10550", "1.10450", "1.10500"),
		testBar(t, 1, "1.10500", "1.10550", "1.10350", "1.10400"),
		testBar(t, 2, "1.10400", "1.10450", "1.10250", "1.10300"),
	} {
		_, _, err := strat.OnBar(ctx, strategysdk.BarEvent{Instrument: strat.inst, Interval: strat.interval, Bar: bar}, fakeView{inst: strat.inst, side: order.Flat})
		require.NoError(t, err)
	}

	_, _, err = strat.OnBar(ctx, strategysdk.BarEvent{
		Instrument: strat.inst, Interval: strat.interval,
		Bar: testBar(t, 3, "1.10300", "1.10350", "1.10250", "1.10300"),
	}, fakeView{inst: strat.inst, side: order.Short})
	require.Error(t, err)
}
