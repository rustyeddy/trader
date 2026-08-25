package broker

import (
	"context"
	"testing"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVerticalSlice_ServiceComposesFullOrderLifecycle is issue
// #157/M3-14's own service-layer vertical slice: a multi-step scenario
// (Submit a fill, Submit a pending order, Cancel it, Snapshot the
// result) driven entirely through the public Service API rather than
// adapters/broker/sim directly, proving the ADR-022 service boundary
// composes correctly across a full sequence rather than only in the
// single-call isolation every other test in this package already
// covers.
//
// The deterministic, multi-account, cancel/replace/Advance-trigger
// scenario itself is adapters/broker/sim's own
// TestVerticalSlice_FullScenarioProducesExpectedState (and its
// determinism counterpart) -- this test does not repeat that scope,
// only proves Service forwards it faithfully. cmd/trader's own
// TestBrokerVerticalSlice (issue #155) already covers the CLI ->
// Service -> sim path for accounts/snapshot/submit, so this test does
// not duplicate that either.
func TestVerticalSlice_ServiceComposesFullOrderLifecycle(t *testing.T) {
	b, accountID, gen := testBroker(t)
	svc, err := New(b, nil)
	require.NoError(t, err)
	ctx := context.Background()

	fillReq := mustMarketRequest(t, gen, accountID)
	submitResp, err := svc.Submit(ctx, SubmitRequest{AccountRequest: AccountRequest{AccountID: accountID}, Order: fillReq})
	require.NoError(t, err)
	require.Equal(t, order.StatusFilled, submitResp.Order.Status)

	pendingReq := mustLimitRequest(t, gen, accountID)
	pendingResp, err := svc.Submit(ctx, SubmitRequest{AccountRequest: AccountRequest{AccountID: accountID}, Order: pendingReq})
	require.NoError(t, err)
	require.Equal(t, order.StatusWorking, pendingResp.Order.Status)

	eventID, err := id.GenerateEventID(gen)
	require.NoError(t, err)
	cancelReq, err := order.NewCancelRequest(order.CancelRequest{OrderID: pendingReq.OrderID, Metadata: id.Metadata{EventID: eventID}})
	require.NoError(t, err)
	cancelResp, err := svc.Cancel(ctx, CancelRequest{AccountRequest: AccountRequest{AccountID: accountID}, Cancel: cancelReq})
	require.NoError(t, err)
	assert.Equal(t, order.StatusCanceled, cancelResp.Result.Status)

	snapResp, err := svc.Snapshot(ctx, SnapshotRequest{AccountRequest: AccountRequest{AccountID: accountID}})
	require.NoError(t, err)
	assert.Empty(t, snapResp.Snapshot.OpenOrders(), "the only pending order was canceled")
	require.Len(t, snapResp.Snapshot.Positions(), 1, "the filled market order still holds its position")
}
