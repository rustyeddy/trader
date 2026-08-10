package order

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewReplaceRequestValidQuantityOnly(t *testing.T) {
	_, err := NewReplaceRequest(ReplaceRequest{OrderID: mustOrderID(t), NewQuantity: qty(t, "2000")})
	require.NoError(t, err)
}

func TestNewReplaceRequestValidPricesOnly(t *testing.T) {
	_, err := NewReplaceRequest(ReplaceRequest{
		OrderID:       mustOrderID(t),
		NewLimitPrice: price(t, "1.10500"),
	})
	require.NoError(t, err)
}

func TestNewReplaceRequestRejectsZeroOrderID(t *testing.T) {
	_, err := NewReplaceRequest(ReplaceRequest{NewQuantity: qty(t, "2000")})
	assert.ErrorIs(t, err, ErrInvalidReplaceRequest)
}

func TestNewReplaceRequestRejectsNoChanges(t *testing.T) {
	_, err := NewReplaceRequest(ReplaceRequest{OrderID: mustOrderID(t)})
	assert.ErrorIs(t, err, ErrInvalidReplaceRequest)
}

func TestNewReplaceResultValid(t *testing.T) {
	_, err := NewReplaceResult(ReplaceResult{OrderID: mustOrderID(t), Status: StatusWorking})
	require.NoError(t, err)
}

func TestNewReplaceResultRejectsZeroOrderID(t *testing.T) {
	_, err := NewReplaceResult(ReplaceResult{})
	assert.ErrorIs(t, err, ErrInvalidReplaceRequest)
}
