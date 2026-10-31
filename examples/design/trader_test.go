package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestTraderWithBalance(balance money) *trader {
	br := &broker{id: "t"}
	act := br.addAccount("a", balance)
	return &trader{broker: br, account: act}
}

func newTestTrader() *trader { return newTestTraderWithBalance(1000) }

func TestTrader_HandleOrder(t *testing.T) {
	ctx := context.Background()
	captureLogs(t)
	openO := func() *order {
		return &order{instrument: "EURUSD", direction: long, action: open, orderType: marketOrder}
	}

	t.Run("open is sized then submitted", func(t *testing.T) {
		o := openO()
		require.NoError(t, newTestTrader().handleOrder(ctx, o))
		assert.Equal(t, defaultOrderUnits, o.units)
		assert.Equal(t, stateSubmitted, o.state)
		assert.NotEmpty(t, o.id)
		require.Len(t, o.history, 2)
		assert.Equal(t, stateSized, o.history[0].state)
		assert.Equal(t, stateSubmitted, o.history[1].state)
	})
	t.Run("close is submitted without units", func(t *testing.T) {
		o := &order{instrument: "EURUSD", action: close, orderType: marketOrder}
		require.NoError(t, newTestTrader().handleOrder(ctx, o))
		assert.Zero(t, o.units, "units 0 means the whole position")
		assert.Equal(t, stateSubmitted, o.state)
	})
	t.Run("open is rejected with zero margin and never submitted", func(t *testing.T) {
		o := openO()
		require.ErrorIs(t, newTestTraderWithBalance(0).handleOrder(ctx, o), errInsufficientMargin)
		assert.Equal(t, stateRejected, o.state)
		require.Len(t, o.history, 1)
		assert.Equal(t, stateRejected, o.history[0].state)
		assert.Empty(t, o.id, "a rejected order never reaches the broker")
	})
	t.Run("open is rejected with negative margin", func(t *testing.T) {
		require.ErrorIs(t, newTestTraderWithBalance(-1).handleOrder(ctx, openO()), errInsufficientMargin)
	})
	t.Run("add and reduce are not supported yet", func(t *testing.T) {
		tr := newTestTrader()
		require.ErrorIs(t, tr.handleOrder(ctx, &order{instrument: "EURUSD", action: add}), errUnsupportedAction)
		require.ErrorIs(t, tr.handleOrder(ctx, &order{instrument: "EURUSD", action: reduce}), errUnsupportedAction)
	})
	t.Run("unknown account at the broker is an error", func(t *testing.T) {
		tr := &trader{broker: &broker{id: "t"}, account: newAccount("ghost", 1000)}
		require.ErrorIs(t, tr.handleOrder(ctx, openO()), errUnknownAccount)
	})
}

func TestTrader_ImplementsOrderHandler(t *testing.T) {
	var _ orderHandler = (*trader)(nil)
}
