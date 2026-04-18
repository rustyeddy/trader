package trader

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenOrders_Add(t *testing.T) {
	t.Parallel()
	oo := &OpenOrders{Orders: make(map[string]*Order)}
	order := &Order{TradeCommon: &TradeCommon{ID: NewULID()}}
	oo.Add(order)
	assert.Equal(t, 1, len(oo.Orders))
}

func TestOpenOrders_Get_Missing(t *testing.T) {
	t.Parallel()
	oo := &OpenOrders{Orders: make(map[string]*Order)}
	retrieved := oo.Get("nonexistent")
	assert.Nil(t, retrieved)
}

func TestOpenOrders_Get_Existing(t *testing.T) {
	t.Parallel()
	oo := &OpenOrders{Orders: make(map[string]*Order)}
	order := &Order{TradeCommon: &TradeCommon{ID: NewULID()}}
	oo.Add(order)
	retrieved := oo.Get(order.ID)
	require.NotNil(t, retrieved)
	assert.Equal(t, order.ID, retrieved.ID)
}
