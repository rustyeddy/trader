package smatrend

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/marketdata"
	"github.com/rustyeddy/trader/num"
)

func mustBar(t *testing.T, open, high, low, close string) marketdata.Bar {
	t.Helper()
	return marketdata.Bar{
		Time:  testStart,
		Open:  num.MustParsePrice(open),
		High:  num.MustParsePrice(high),
		Low:   num.MustParsePrice(low),
		Close: num.MustParsePrice(close),
	}
}

func TestExitRuleRegistry_KnowsBothNames(t *testing.T) {
	for _, name := range []string{"trailing-stop", "sma-cross"} {
		_, ok := exitRuleRegistry[name]
		assert.True(t, ok, name)
	}
}

func TestTrailingStopExitRule_RatchetsMonotonicallyUpward(t *testing.T) {
	cfg := Config{TrailingStopPercent: num.MustParseRate("0.10")}
	rule, err := newTrailingStopExitRule(cfg)
	require.NoError(t, err)

	rule.OnEntry(mustBar(t, "100", "110", "99", "105"))

	decision, err := rule.OnLongBar(mustBar(t, "103", "110", "102", "105"), 90)
	require.NoError(t, err)
	require.NotNil(t, decision.NewStop)
	assert.Equal(t, "99", decision.NewStop.String(), "90%% of the 110 high-water mark")
	assert.False(t, decision.ExitNow)

	// A lower High must not move the stop at all.
	decision, err = rule.OnLongBar(mustBar(t, "106", "108", "100", "101"), 90)
	require.NoError(t, err)
	assert.Nil(t, decision.NewStop, "a lower bar high must never move the stop")

	// A higher High ratchets it up.
	decision, err = rule.OnLongBar(mustBar(t, "106", "120", "105", "118"), 90)
	require.NoError(t, err)
	require.NotNil(t, decision.NewStop)
	assert.Equal(t, "108", decision.NewStop.String())
}

func TestSMACrossExitRule_ExitsOnCloseAtOrBelowSMA(t *testing.T) {
	rule, err := newSMACrossExitRule(Config{})
	require.NoError(t, err)
	rule.OnEntry(mustBar(t, "100", "105", "99", "102"))

	decision, err := rule.OnLongBar(mustBar(t, "103", "106", "102", "105"), 100)
	require.NoError(t, err)
	assert.False(t, decision.ExitNow, "close (105) above SMA (100) must not exit")
	assert.Nil(t, decision.NewStop, "smaCrossExitRule never manages a resting stop")

	decision, err = rule.OnLongBar(mustBar(t, "104", "105", "98", "99"), 100)
	require.NoError(t, err)
	assert.True(t, decision.ExitNow, "close (99) at or below SMA (100) must exit")
}
