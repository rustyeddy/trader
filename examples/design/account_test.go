package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccount_MarginAvailable(t *testing.T) {
	tests := []struct {
		name    string
		balance money
		want    money
	}{
		{"positive", 10000, 10000},
		{"zero", 0, 0},
		{"negative", -50, -50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, newAccount("a", tt.balance).marginAvailable())
		})
	}
}

func TestAccount_Size(t *testing.T) {
	t.Run("open gets the default units and is marked sized", func(t *testing.T) {
		o := &order{instrument: "EUR/USD", direction: short, action: open}
		require.NoError(t, newAccount("a", 500).size(o))
		assert.Equal(t, defaultOrderUnits, o.units)
		assert.Equal(t, stateSized, o.state)
	})
	t.Run("open is rejected without margin and left untouched", func(t *testing.T) {
		for _, bal := range []money{0, -10} {
			o := &order{instrument: "EUR/USD", direction: long, action: open}
			require.ErrorIs(t, newAccount("a", bal).size(o), errInsufficientMargin)
			assert.Zero(t, o.units)
			assert.Equal(t, stateRequested, o.state)
		}
	})
	t.Run("close is sized as the whole position", func(t *testing.T) {
		o := &order{instrument: "EUR/USD", action: close}
		require.NoError(t, newAccount("a", 0).size(o), "closing needs no margin")
		assert.Zero(t, o.units)
		assert.Equal(t, stateSized, o.state)
	})
	t.Run("add, reduce and no action are unsupported", func(t *testing.T) {
		for _, a := range []action{add, reduce, noAction, action(99)} {
			o := &order{instrument: "EUR/USD", action: a}
			require.ErrorIs(t, newAccount("a", 1000).size(o), errUnsupportedAction, a.String())
			assert.Equal(t, stateRequested, o.state)
		}
	})
}
