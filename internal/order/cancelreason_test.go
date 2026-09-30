package order

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOrderCancelReasonOnlyOnCanceled(t *testing.T) {
	reason := &Rejection{Reason: ReasonInsufficientMargin, Detail: "required margin exceeds equity"}

	working := mustWorkingOrder(t, "1000")
	working.CancelReason = reason
	_, err := NewOrder(working)
	assert.ErrorIs(t, err, ErrInvalidOrder, "a working order may not carry a CancelReason")

	o := mustWorkingOrder(t, "1000")
	req, err := NewCancelRequest(CancelRequest{OrderID: o.Request.OrderID, Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	pending, err := ApplyCancelRequest(o, req)
	require.NoError(t, err)
	result, err := NewCancelResult(CancelResult{OrderID: o.Request.OrderID, Status: StatusCanceled, Metadata: resultMetadataFor(pending)})
	require.NoError(t, err)
	canceled, err := ApplyCancelResult(pending, result)
	require.NoError(t, err)
	assert.Nil(t, canceled.CancelReason, "a requested cancel carries no reason")

	canceled.CancelReason = reason
	got, err := NewOrder(canceled)
	require.NoError(t, err)
	assert.Equal(t, reason, got.CancelReason)
}
