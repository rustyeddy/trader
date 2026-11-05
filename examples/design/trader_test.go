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

func newTestTrader() *trader { return newTestTraderWithBalance(usd(1000)) }

func TestTrader_HandleOrder(t *testing.T) {
	ctx := context.Background()
	captureLogs(t)
	openO := func() *order {
		return &order{Time: testStart, instrument: "EURUSD", direction: long, action: open, orderType: marketOrder, price: eurusd1_10, stopLoss: 108900}
	}

	t.Run("open is sized then submitted", func(t *testing.T) {
		o := openO()
		require.NoError(t, newTestTrader().handleOrder(ctx, o))
		assert.Equal(t, int64(909), o.units, "balance 1000: floor(1000 / 1.1)")
		assert.Equal(t, price(108900), o.stopLoss)
		assert.Equal(t, stateSubmitted, o.state)
		assert.NotEmpty(t, o.id)
		assert.Equal(t, "a", o.accountID, "the trader names the account")
		require.Len(t, o.history, 3)
		assert.Equal(t, stateRequested, o.history[0].state)
		assert.Equal(t, stateSized, o.history[1].state)
		assert.Equal(t, stateSubmitted, o.history[2].state)
	})
	t.Run("limit open is sized at its limit price", func(t *testing.T) {
		o := openO()
		o.orderType, o.price, o.stopLoss = limitOrder, 100000, 99000
		require.NoError(t, newTestTrader().handleOrder(ctx, o))
		assert.Equal(t, int64(1000), o.units, "balance 1000: floor(1000 / 1.0)")
		assert.Equal(t, stateSubmitted, o.state)
	})
	t.Run("close is submitted without units", func(t *testing.T) {
		o := &order{Time: testStart, instrument: "EURUSD", action: close, orderType: marketOrder}
		require.NoError(t, newTestTrader().handleOrder(ctx, o))
		assert.Zero(t, o.units, "units 0 means the whole position")
		assert.Equal(t, stateSubmitted, o.state)
		assert.Equal(t, "a", o.accountID)
	})
	t.Run("open is rejected with zero margin and never submitted", func(t *testing.T) {
		o := openO()
		require.NoError(t, newTestTraderWithBalance(0).handleOrder(ctx, o), "a rejection is recorded, not returned")
		assert.Equal(t, stateRejected, o.state)
		require.Len(t, o.history, 2)
		assert.Equal(t, stateRequested, o.history[0].state)
		assert.Equal(t, stateRejected, o.history[1].state)
		assert.Empty(t, o.id, "a rejected order never reaches the broker")
		assert.Equal(t, "a", o.accountID, "but it is still recorded against its account")
	})
	t.Run("open is rejected with negative margin", func(t *testing.T) {
		o := openO()
		require.NoError(t, newTestTraderWithBalance(-usd(1)).handleOrder(ctx, o))
		assert.Equal(t, stateRejected, o.state)
		assert.Contains(t, o.history[len(o.history)-1].reason, "insufficient margin")
	})
	t.Run("add and reduce are not supported yet", func(t *testing.T) {
		tr := newTestTrader()
		require.ErrorIs(t, tr.handleOrder(ctx, &order{Time: testStart, instrument: "EURUSD", action: add}), errUnsupportedAction)
		require.ErrorIs(t, tr.handleOrder(ctx, &order{Time: testStart, instrument: "EURUSD", action: reduce}), errUnsupportedAction)
	})
	t.Run("unknown account at the broker is an error", func(t *testing.T) {
		tr := &trader{broker: &broker{id: "t"}, account: newAccount("ghost", usd(1000))}
		require.ErrorIs(t, tr.handleOrder(ctx, openO()), errUnknownAccount)
	})
}

func TestTrader_HandleOrder_ConflictingAccountIsAnError(t *testing.T) {
	captureLogs(t)
	o := &order{Time: testStart, accountID: "someone-else", instrument: "EURUSD", direction: long, action: open,
		orderType: marketOrder, price: eurusd1_10, stopLoss: 108900}

	err := newTestTrader().handleOrder(context.Background(), o)

	require.ErrorIs(t, err, errAccountConflict)
	assert.Equal(t, "someone-else", o.accountID, "an existing account is never overwritten")
	assert.Empty(t, o.history, "the order was neither requested, sized nor submitted")
}

func TestTrader_HandleOrder_MatchingAccountIsKept(t *testing.T) {
	captureLogs(t)
	o := &order{Time: testStart, accountID: "a", instrument: "EURUSD", direction: long, action: open,
		orderType: marketOrder, price: eurusd1_10, stopLoss: 108900}

	require.NoError(t, newTestTrader().handleOrder(context.Background(), o))

	assert.Equal(t, "a", o.accountID)
	assert.Equal(t, stateSubmitted, o.state)
}
