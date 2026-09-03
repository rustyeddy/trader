// This file proves issue #273's own end-to-end requirement: a
// directionally-restricted (AllowedSide) run through the real
// service/backtest composition path never opens a position on the
// disallowed side, and the unrestricted default is unaffected —
// proven across the whole run's account/trade state, not only via
// strategy_test.go's own single-call actOnCross assertions.
package emacross_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/order"
	"github.com/rustyeddy/trader/strategy/emacross"
)

// TestEmacross_AllowedSideShortOnly_NeverOpensLong replays the same
// bullish-cross-then-bearish-reversal fixture EMA-06/EMA-10's own
// tests use, but with AllowedSide: SideShortOnly. The bar-7 bullish
// cross (which opens a long under the default SideBoth config,
// TestEmacross_EntryExitReversalThroughRealPipeline) must instead be a
// no-op, and the bar-12 bearish cross must open the short exactly as
// it does in the unrestricted case.
func TestEmacross_AllowedSideShortOnly_NeverOpensLong(t *testing.T) {
	resp, _ := runEMACrossoverFixtureWithStrategyConfig(t, emacross.Config{
		FastPeriod:  3,
		SlowPeriod:  5,
		AllowedSide: emacross.SideShortOnly,
	})

	assert.Empty(t, resp.Trades, "no long was ever opened, so no trade can have closed")
	require.Len(t, resp.OpenTrades, 1, "the bar-12 bearish cross must still open the (allowed) short")
	assert.Equal(t, order.Short, resp.OpenTrades[0].Side)

	require.Len(t, resp.Account.Positions(), 1)
	position := resp.Account.Positions()[0]
	assert.Equal(t, order.Short, position.Side)

	for _, p := range resp.Account.Positions() {
		assert.NotEqual(t, order.Long, p.Side, "AllowedSide: SideShortOnly must never open a long position")
	}
}

// TestEmacross_AllowedSideLongOnly_ExitsRatherThanReversesToShort is
// the long-only mirror: the bar-7 bullish cross opens the long exactly
// as the unrestricted case does, but the bar-12 bearish cross — which
// would normally reverse into a short — must instead close the long
// to flat and stop, since a short is now disallowed.
func TestEmacross_AllowedSideLongOnly_ExitsRatherThanReversesToShort(t *testing.T) {
	resp, _ := runEMACrossoverFixtureWithStrategyConfig(t, emacross.Config{
		FastPeriod:  3,
		SlowPeriod:  5,
		AllowedSide: emacross.SideLongOnly,
	})

	require.Len(t, resp.Trades, 1, "the bar-12 bearish cross must close the bar-7 long")
	assert.Equal(t, order.Long, resp.Trades[0].Side)

	assert.Empty(t, resp.OpenTrades, "no new short must have opened after the exit")
	assert.Empty(t, resp.Account.Positions(), "the account must end flat, not short")
}
