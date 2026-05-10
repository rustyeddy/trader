package trader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultPlan(t *testing.T) {
	assert.Equal(t, "hold", DefaultStrategyPlan.Reason)
	assert.Empty(t, DefaultStrategyPlan.Opens)
	assert.Empty(t, DefaultStrategyPlan.Closes)
	assert.Empty(t, DefaultStrategyPlan.Cancel)
}

func TestStrategyRuntimeContextHelpers_NoContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	assert.Equal(t, "", StrategyInstrument(ctx))
	assert.Equal(t, 0, StrategyBarIndex(ctx))
	assert.Equal(t, 0, StrategyGapBars(ctx))
	assert.Nil(t, StrategyAccount(ctx))

	_, ok := strategyRuntimeFromContext(ctx)
	assert.False(t, ok)
}

func TestWithStrategyRuntimeAndRetrieveValues(t *testing.T) {
	t.Parallel()

	acct := NewAccount("trading", MoneyFromFloat(50_000))
	ctx := context.Background()

	ctx = withStrategyRuntime(ctx, "EURUSD", 42, 3, acct)

	assert.Equal(t, "EURUSD", StrategyInstrument(ctx))
	assert.Equal(t, 42, StrategyBarIndex(ctx))
	assert.Equal(t, 3, StrategyGapBars(ctx))
	assert.Same(t, acct, StrategyAccount(ctx))

	rt, ok := strategyRuntimeFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, "EURUSD", rt.instrument)
	assert.Equal(t, 42, rt.barIndex)
	assert.Equal(t, 3, rt.gapBars)
	assert.Same(t, acct, rt.account)
}

func TestStrategyRuntimeZeroValues(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	ctx = withStrategyRuntime(ctx, "", 0, 0, nil)

	assert.Equal(t, "", StrategyInstrument(ctx))
	assert.Equal(t, 0, StrategyBarIndex(ctx))
	assert.Equal(t, 0, StrategyGapBars(ctx))
	assert.Nil(t, StrategyAccount(ctx))
}

func TestStrategyAccountReplace(t *testing.T) {
	t.Parallel()

	acct1 := NewAccount("first", MoneyFromFloat(1_000))
	acct2 := NewAccount("second", MoneyFromFloat(2_000))

	ctx := context.Background()
	ctx = withStrategyRuntime(ctx, "AAAA", 1, 1, acct1)
	assert.Same(t, acct1, StrategyAccount(ctx))

	ctx = withStrategyRuntime(ctx, "BBBB", 2, 2, acct2)
	assert.Same(t, acct2, StrategyAccount(ctx))
	assert.Equal(t, "BBBB", StrategyInstrument(ctx))
}
