package trader

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestTraderWithBalance(balance money) *trader {
	br := newBroker("t")
	act := br.addAccount("a", balance)
	return &trader{broker: br, account: act}
}

func newTestTrader() *trader { return newTestTraderWithBalance(usd(1000)) }

func TestTrader_HandleOrder(t *testing.T) {
	ctx := context.Background()
	captureLogs(t)
	openO := func() *order {
		return &order{Time: testStart, instrument: "EURUSD", direction: long, action: open, orderType: marketOrder, price: eurusd1_10, stopLoss: px(108900)}
	}

	t.Run("open is sized then submitted", func(t *testing.T) {
		o := openO()
		require.NoError(t, newTestTrader().handleOrder(ctx, o))
		assert.Equal(t, qty(909), o.units, "balance 1000: floor(1000 / 1.1)")
		assert.Equal(t, px(108900), o.stopLoss)
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
		o.orderType, o.price, o.stopLoss = limitOrder, px(100000), px(99000)
		require.NoError(t, newTestTrader().handleOrder(ctx, o))
		assert.Equal(t, qty(1000), o.units, "balance 1000: floor(1000 / 1.0)")
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
		require.NoError(t, newTestTraderWithBalance(usd(0)).handleOrder(ctx, o), "a rejection is recorded, not returned")
		assert.Equal(t, stateRejected, o.state)
		require.Len(t, o.history, 2)
		assert.Equal(t, stateRequested, o.history[0].state)
		assert.Equal(t, stateRejected, o.history[1].state)
		assert.Empty(t, o.id, "a rejected order never reaches the broker")
		assert.Equal(t, "a", o.accountID, "but it is still recorded against its account")
	})
	t.Run("open is rejected with negative margin", func(t *testing.T) {
		o := openO()
		require.NoError(t, newTestTraderWithBalance(mneg(usd(1))).handleOrder(ctx, o))
		assert.Equal(t, stateRejected, o.state)
		assert.Contains(t, o.history[len(o.history)-1].reason, "insufficient margin")
	})
	t.Run("add and reduce are not supported yet", func(t *testing.T) {
		tr := newTestTrader()
		require.ErrorIs(t, tr.handleOrder(ctx, &order{Time: testStart, instrument: "EURUSD", action: add, orderType: marketOrder}), errUnsupportedAction)
		require.ErrorIs(t, tr.handleOrder(ctx, &order{Time: testStart, instrument: "EURUSD", action: reduce, orderType: marketOrder}), errUnsupportedAction)
	})
	t.Run("unknown account at the broker is an error", func(t *testing.T) {
		tr := &trader{broker: newBroker("t"), account: newAccount("ghost", usd(1000))}
		require.ErrorIs(t, tr.handleOrder(ctx, openO()), errUnknownAccount)
	})
}

func TestTrader_HandleOrder_InvalidOrderType(t *testing.T) {
	captureLogs(t)
	for _, tt := range []struct {
		name string
		typ  orderType
	}{
		{"missing", noOrder},
		{"unknown", orderType(99)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tr := newTestTrader()
			o := openOrder()
			o.orderType = tt.typ

			err := tr.handleOrder(context.Background(), o)

			require.ErrorIs(t, err, errInvalidOrder)
			assert.False(t, isRejection(err), "an invalid request stops the run")
			assert.Empty(t, o.history, "never recorded or sized")
			assert.Empty(t, o.id)
			assert.Zero(t, o.units)
			assert.Empty(t, tr.broker.orders)
			reserved := reservedOf(t, tr.broker, tr.account.id)
			assert.True(t, reserved.IsZero())
			assert.Empty(t, tr.account.positions)
			assert.Equal(t, usd(1000), tr.account.balance)
		})
	}
}

func TestTrader_HandleOrder_ConflictingAccountIsAnError(t *testing.T) {
	captureLogs(t)
	o := &order{Time: testStart, accountID: "someone-else", instrument: "EURUSD", direction: long, action: open,
		orderType: marketOrder, price: eurusd1_10, stopLoss: px(108900)}

	err := newTestTrader().handleOrder(context.Background(), o)

	require.ErrorIs(t, err, errAccountConflict)
	assert.Equal(t, "someone-else", o.accountID, "an existing account is never overwritten")
	assert.Empty(t, o.history, "the order was neither requested, sized nor submitted")
}

func TestTrader_HandleOrder_MatchingAccountIsKept(t *testing.T) {
	captureLogs(t)
	o := &order{Time: testStart, accountID: "a", instrument: "EURUSD", direction: long, action: open,
		orderType: marketOrder, price: eurusd1_10, stopLoss: px(108900)}

	require.NoError(t, newTestTrader().handleOrder(context.Background(), o))

	assert.Equal(t, "a", o.accountID)
	assert.Equal(t, stateSubmitted, o.state)
}

func TestTrader_HandleOrder_KeepsOrdersRefusedBeforeTheBroker(t *testing.T) {
	captureLogs(t)
	tr := newTestTraderWithBalance(usd(0))
	o := openOrder()

	require.NoError(t, tr.handleOrder(context.Background(), o), "a refusal does not stop the run")

	assert.Equal(t, []*order{o}, tr.rejected)
	assert.Equal(t, stateRejected, o.state)
	assert.Empty(t, tr.broker.completed, "kept once, by the trader")
	assert.Empty(t, tr.broker.orders["EURUSD"])
}

func TestTrader_HandleOrder_KeepsASecondOpenRefusedWhileOneIsPending(t *testing.T) {
	captureLogs(t)
	tr := newTestTraderWithBalance(usd(10000))
	first, second := openOrder(), openOrder()

	require.NoError(t, tr.handleOrder(context.Background(), first))
	require.NoError(t, tr.handleOrder(context.Background(), second))

	assert.Equal(t, []*order{second}, tr.rejected, "the accepted open is not kept here")
	assert.Equal(t, []*order{first}, tr.broker.orders["EURUSD"])
}

func TestTrader_HandleOrder_FailureIsReturnedNotKeptAsRejected(t *testing.T) {
	captureLogs(t)
	tr := newTestTraderWithBalance(usd(10000))
	o := openOrder()
	o.instrument = "NOSUCH"

	require.ErrorIs(t, tr.handleOrder(context.Background(), o), errUnknownInstrument)
	assert.Empty(t, tr.rejected)
}
