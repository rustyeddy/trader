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

func TestTrader_HandleOrder(t *testing.T) {
	ctx := context.Background()
	openO := func() *order { return &order{instrument: "EUR/USD", direction: long, action: open} }

	t.Run("order with no action is ignored", func(t *testing.T) {
		// Rejecting malformed orders is the run loop's job (validate).
		assert.NoError(t, newTestTrader().handleOrder(ctx, &order{}))
	})
	t.Run("close succeeds", func(t *testing.T) {
		assert.NoError(t, newTestTrader().handleOrder(ctx, &order{instrument: "EUR/USD", action: close}))
	})
	t.Run("add and reduce are not handled yet", func(t *testing.T) {
		tr := newTestTrader()
		assert.NoError(t, tr.handleOrder(ctx, &order{instrument: "EUR/USD", action: add}))
		assert.NoError(t, tr.handleOrder(ctx, &order{instrument: "EUR/USD", action: reduce}))
	})
	t.Run("open is accepted when margin is available", func(t *testing.T) {
		assert.NoError(t, newTestTrader().handleOrder(ctx, openO()))
	})
	t.Run("open is rejected with zero margin", func(t *testing.T) {
		tr := &trader{broker: &broker{id: "t"}, account: newAccount("a", 0)}
		require.ErrorIs(t, tr.handleOrder(ctx, openO()), errInsufficientMargin)
	})
	t.Run("open is rejected with negative margin", func(t *testing.T) {
		tr := &trader{broker: &broker{id: "t"}, account: newAccount("a", -1)}
		require.ErrorIs(t, tr.handleOrder(ctx, openO()), errInsufficientMargin)
	})
}

func TestTrader_ImplementsOrderHandler(t *testing.T) {
	var _ orderHandler = (*trader)(nil)
}
