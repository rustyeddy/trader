package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBroker_Sim(t *testing.T) {
	br, ok := getBroker("sim")
	require.True(t, ok)
	assert.Equal(t, "sim", br.id)

	act, ok := br.getAccount("sim123")
	require.True(t, ok)
	assert.Equal(t, money(10000), act.balance)
}

func TestGetBroker_UnknownIsCreatedAndReused(t *testing.T) {
	t.Cleanup(func() { delete(brokers, "test-broker") })

	br, ok := getBroker("test-broker")
	require.True(t, ok)
	assert.Equal(t, "test-broker", br.id)
	assert.NotNil(t, br.accounts)

	again, ok := getBroker("test-broker")
	require.True(t, ok)
	assert.Same(t, br, again)
}

func TestBroker_AddAccountInitializesNilMap(t *testing.T) {
	b := &broker{id: "b"} // accounts map is nil
	act := b.addAccount("a1", 500)

	require.NotNil(t, act)
	assert.Equal(t, "a1", act.id)
	assert.Equal(t, money(500), act.balance)

	got, ok := b.getAccount("a1")
	require.True(t, ok)
	assert.Same(t, act, got)
}

func TestBroker_AddAccountReplacesSameID(t *testing.T) {
	b := &broker{id: "b"}
	b.addAccount("a1", 500)
	second := b.addAccount("a1", 900)

	got, ok := b.getAccount("a1")
	require.True(t, ok)
	assert.Same(t, second, got)
	assert.Equal(t, money(900), got.balance)
}

func TestBroker_GetAccountMissing(t *testing.T) {
	b := &broker{id: "b"}
	act, ok := b.getAccount("nope")
	assert.False(t, ok)
	assert.Nil(t, act)
}

func TestNewAccount(t *testing.T) {
	a := newAccount("x", 123)
	assert.Equal(t, "x", a.id)
	assert.Equal(t, money(123), a.balance)
}

func TestBroker_SubmitMarksSubmittedAssignsIDAndLogs(t *testing.T) {
	logs := captureLogs(t)
	b := &broker{id: "sim"}
	b.addAccount("acct", 1000)
	o := &order{accountID: "acct", instrument: "EUR/USD", direction: long, action: open, orderType: marketOrder, units: 1000}

	require.NoError(t, b.submit(context.Background(), o))

	assert.Equal(t, "sim-1", o.id)
	assert.Equal(t, stateSubmitted, o.state)
	require.Len(t, o.history, 1)
	assert.Equal(t, stateSubmitted, o.history[0].state)
	assert.False(t, o.history[0].time.IsZero())
	out := logs.String()
	assert.Contains(t, out, "to=submitted", "the state change is logged")
	assert.Contains(t, out, "order submitted")
	assert.Contains(t, out, "broker=sim")
	assert.Contains(t, out, "account=acct")
	assert.Contains(t, out, "order=sim-1")
	assert.Contains(t, out, "instrument=EUR/USD")
	assert.Contains(t, out, "action=open")
	assert.Contains(t, out, "direction=long")
	assert.Contains(t, out, "type=market")
	assert.Contains(t, out, "units=1000")
}

func TestBroker_SubmitIDsAreSequentialAndKept(t *testing.T) {
	captureLogs(t)
	b := &broker{id: "sim"}
	b.addAccount("acct", 1000)
	ctx := context.Background()

	o1, o2 := &order{accountID: "acct", action: open}, &order{accountID: "acct", action: close}
	require.NoError(t, b.submit(ctx, o1))
	require.NoError(t, b.submit(ctx, o2))
	assert.Equal(t, "sim-1", o1.id)
	assert.Equal(t, "sim-2", o2.id)

	pre := &order{id: "mine", accountID: "acct", action: open}
	require.NoError(t, b.submit(ctx, pre))
	assert.Equal(t, "mine", pre.id, "an existing id is not overwritten")
}

func TestBroker_SubmitUnknownAccount(t *testing.T) {
	logs := captureLogs(t)
	b := &broker{id: "sim"}
	o := &order{accountID: "ghost", action: open}

	err := b.submit(context.Background(), o)

	require.ErrorIs(t, err, errUnknownAccount)
	assert.Equal(t, stateRequested, o.state)
	assert.Empty(t, o.id)
	assert.Empty(t, o.history)
	assert.NotContains(t, logs.String(), "order submitted")
}

func TestBroker_SubmitCancelledContext(t *testing.T) {
	captureLogs(t)
	b := &broker{id: "sim"}
	b.addAccount("acct", 1000)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	o := &order{accountID: "acct", action: open}

	require.ErrorIs(t, b.submit(ctx, o), context.Canceled)
	assert.Empty(t, o.id)
	assert.Equal(t, stateRequested, o.state)
	assert.Empty(t, o.history)
}

func TestBroker_SubmitOrderWithoutAnAccountIsRejectedAsUnknown(t *testing.T) {
	logs := captureLogs(t)
	b := &broker{id: "sim"}
	b.addAccount("acct", 1000)
	o := &order{action: open} // the trader never set an account

	err := b.submit(context.Background(), o)

	require.ErrorIs(t, err, errUnknownAccount)
	assert.Contains(t, err.Error(), "no account")
	assert.Empty(t, o.id)
	assert.Empty(t, o.history)
	assert.NotContains(t, logs.String(), "order submitted")
}

func TestBroker_SubmitUsesTheOrdersAccountForTheLog(t *testing.T) {
	logs := captureLogs(t)
	b := &broker{id: "sim"}
	b.addAccount("one", 1000)
	b.addAccount("two", 1000)

	require.NoError(t, b.submit(context.Background(), &order{accountID: "two", action: open}))

	assert.Contains(t, logs.String(), "account=two")
	assert.NotContains(t, logs.String(), "account=one")
}
