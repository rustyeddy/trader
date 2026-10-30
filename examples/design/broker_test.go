package main

import (
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
