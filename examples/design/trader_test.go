package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestTrader() *trader {
	return &trader{broker: &broker{id: "t"}, account: newAccount("a", 1000)}
}

func TestTrader_HandleIntent(t *testing.T) {
	ctx := context.Background()

	t.Run("hold is a no-op", func(t *testing.T) {
		assert.NoError(t, newTestTrader().handleIntent(ctx, intent{action: hold}))
	})
	t.Run("zero value intent is a no-op", func(t *testing.T) {
		assert.NoError(t, newTestTrader().handleIntent(ctx, intent{}))
	})
	t.Run("close succeeds", func(t *testing.T) {
		assert.NoError(t, newTestTrader().handleIntent(ctx, intent{action: close}))
	})
	t.Run("add and reduce are not handled yet", func(t *testing.T) {
		tr := newTestTrader()
		assert.NoError(t, tr.handleIntent(ctx, intent{action: add}))
		assert.NoError(t, tr.handleIntent(ctx, intent{action: reduce}))
	})
	t.Run("open is rejected while no margin is available", func(t *testing.T) {
		// marginAvailable is still a stub that reports zero margin.
		err := newTestTrader().handleIntent(ctx, intent{action: open})
		require.ErrorIs(t, err, errInsufficientMargin)
	})
}

func TestTrader_ImplementsIntentHandler(t *testing.T) {
	var _ intentHandler = (*trader)(nil)
}
