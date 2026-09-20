package order

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCancelRequestValid(t *testing.T) {
	_, err := NewCancelRequest(CancelRequest{OrderID: mustOrderID(t)})
	require.NoError(t, err)
}

func TestNewCancelRequestRejectsZeroOrderID(t *testing.T) {
	_, err := NewCancelRequest(CancelRequest{})
	assert.ErrorIs(t, err, ErrInvalidCancelRequest)
}

func TestNewCancelResultValid(t *testing.T) {
	r, err := NewCancelResult(CancelResult{OrderID: mustOrderID(t), Status: StatusCanceled})
	require.NoError(t, err)
	assert.Nil(t, r.Rejection)
}

func TestNewCancelResultRejectsZeroOrderID(t *testing.T) {
	_, err := NewCancelResult(CancelResult{})
	assert.ErrorIs(t, err, ErrInvalidCancelResult)
}

func TestNewCancelResultDeclinedCarriesRejection(t *testing.T) {
	r, err := NewCancelResult(CancelResult{
		OrderID:   mustOrderID(t),
		Status:    StatusFilled,
		Rejection: &Rejection{Reason: ReasonUnknown, Detail: "already filled"},
	})
	require.NoError(t, err)
	require.NotNil(t, r.Rejection)
	assert.Equal(t, "already filled", r.Rejection.Detail)
}
