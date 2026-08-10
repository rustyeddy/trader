package order

import (
	"testing"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func allStatuses() []Status {
	return []Status{
		StatusUnknown, StatusPendingSubmit, StatusWorking, StatusPartiallyFilled,
		StatusFilled, StatusPendingCancel, StatusCanceled, StatusPendingReplace,
		StatusRejected, StatusExpired,
	}
}

func TestCanTransitionToSameStatusIsIdempotentForKnownStatuses(t *testing.T) {
	for _, s := range allStatuses() {
		if s == StatusUnknown {
			continue
		}
		assert.True(t, s.CanTransitionTo(s), "%s -> %s should be idempotent", s, s)
	}
}

func TestCanTransitionToUnknownIsNeverLegal(t *testing.T) {
	for _, from := range allStatuses() {
		assert.False(t, from.CanTransitionTo(StatusUnknown), "%s -> Unknown should never be legal", from)
	}
	assert.False(t, StatusUnknown.CanTransitionTo(StatusUnknown), "Unknown -> Unknown should not be legal despite the same-status rule")
}

func TestCanTransitionToRejectsInvalidStatusValues(t *testing.T) {
	assert.False(t, Status(200).CanTransitionTo(StatusWorking))
	assert.False(t, StatusWorking.CanTransitionTo(Status(200)))
}

func TestCanTransitionToTerminalStatesRejectEverythingElse(t *testing.T) {
	for _, terminal := range []Status{StatusFilled, StatusCanceled, StatusRejected, StatusExpired} {
		for _, to := range allStatuses() {
			if to == terminal || to == StatusUnknown {
				continue
			}
			assert.False(t, terminal.CanTransitionTo(to), "%s -> %s should be illegal: terminal", terminal, to)
		}
	}
}

func TestCanTransitionToKnownGraph(t *testing.T) {
	cases := []struct {
		from, to Status
		want     bool
	}{
		{StatusPendingSubmit, StatusWorking, true},
		{StatusPendingSubmit, StatusRejected, true},
		{StatusPendingSubmit, StatusFilled, false},
		{StatusWorking, StatusPartiallyFilled, true},
		{StatusWorking, StatusFilled, true},
		{StatusWorking, StatusPendingCancel, true},
		{StatusWorking, StatusPendingReplace, true},
		{StatusWorking, StatusExpired, true},
		{StatusWorking, StatusCanceled, false},
		{StatusWorking, StatusRejected, false},
		{StatusPartiallyFilled, StatusFilled, true},
		{StatusPartiallyFilled, StatusPendingCancel, true},
		{StatusPartiallyFilled, StatusExpired, true},
		{StatusPartiallyFilled, StatusCanceled, false},
		{StatusPendingCancel, StatusCanceled, true},
		{StatusPendingCancel, StatusWorking, true},
		{StatusPendingCancel, StatusPartiallyFilled, true},
		{StatusPendingCancel, StatusFilled, true},
		{StatusPendingCancel, StatusRejected, false},
		{StatusPendingReplace, StatusWorking, true},
		{StatusPendingReplace, StatusPartiallyFilled, true},
		{StatusPendingReplace, StatusFilled, true},
		{StatusPendingReplace, StatusCanceled, true},
		{StatusPendingReplace, StatusRejected, false},
	}
	for _, tc := range cases {
		got := tc.from.CanTransitionTo(tc.to)
		assert.Equal(t, tc.want, got, "%s -> %s", tc.from, tc.to)
	}
}

func TestStatusTerminal(t *testing.T) {
	terminal := map[Status]bool{
		StatusUnknown:         false,
		StatusPendingSubmit:   false,
		StatusWorking:         false,
		StatusPartiallyFilled: false,
		StatusFilled:          true,
		StatusPendingCancel:   false,
		StatusCanceled:        true,
		StatusPendingReplace:  false,
		StatusRejected:        true,
		StatusExpired:         true,
	}
	for s, want := range terminal {
		assert.Equal(t, want, s.Terminal(), "%s", s)
	}
}

// --- ApplyAcceptance ---

func TestApplyAcceptanceValid(t *testing.T) {
	o := mustPendingSubmitOrder(t)
	accepted := num.MustParseQuantity("1000")
	o, err := ApplyAcceptance(o, "broker-1", accepted, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, StatusWorking, o.Status)
	assert.Equal(t, "broker-1", o.BrokerOrderID)
	require.NotNil(t, o.AcceptedQuantity)
	assert.True(t, o.AcceptedQuantity.Equal(accepted))
}

func TestApplyAcceptanceRejectsWrongSourceStatus(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	_, err := ApplyAcceptance(o, "broker-1", num.MustParseQuantity("1000"), nil, nil)
	assert.ErrorIs(t, err, ErrIllegalTransition)
}

// --- ApplyRejection ---

func TestApplyRejectionValid(t *testing.T) {
	o := mustPendingSubmitOrder(t)
	o, err := ApplyRejection(o, Rejection{Reason: ReasonMarketClosed})
	require.NoError(t, err)
	assert.Equal(t, StatusRejected, o.Status)
	require.NotNil(t, o.Rejection)
	assert.Equal(t, ReasonMarketClosed, o.Rejection.Reason)
}

func TestApplyRejectionRejectsWrongSourceStatus(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	_, err := ApplyRejection(o, Rejection{Reason: ReasonMarketClosed})
	assert.ErrorIs(t, err, ErrIllegalTransition)
}

// --- ApplyExpiration ---

func TestApplyExpirationValid(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	o, err := ApplyExpiration(o)
	require.NoError(t, err)
	assert.Equal(t, StatusExpired, o.Status)
}

func TestApplyExpirationRejectsPendingSubmit(t *testing.T) {
	o := mustPendingSubmitOrder(t)
	_, err := ApplyExpiration(o)
	assert.ErrorIs(t, err, ErrIllegalTransition)
}

func TestApplyExpirationIsTerminal(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	o, err := ApplyExpiration(o)
	require.NoError(t, err)
	_, err = ApplyExpiration(o)
	assert.ErrorIs(t, err, ErrIllegalTransition)
}

// --- ApplyCancelRequest / ApplyCancelResult ---

func TestApplyCancelRequestValid(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewCancelRequest(CancelRequest{OrderID: o.Request.OrderID, Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyCancelRequest(o, req)
	require.NoError(t, err)
	assert.Equal(t, StatusPendingCancel, o.Status)
	assert.Equal(t, req.Metadata.EventID, o.PendingCommandID)
}

func TestApplyCancelRequestRejectsMismatchedOrderID(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewCancelRequest(CancelRequest{OrderID: mustOrderID(t), Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	_, err = ApplyCancelRequest(o, req)
	assert.ErrorIs(t, err, ErrOrderMismatch)
}

func TestApplyCancelRequestRejectsZeroEventID(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewCancelRequest(CancelRequest{OrderID: o.Request.OrderID})
	require.NoError(t, err)
	_, err = ApplyCancelRequest(o, req)
	assert.ErrorIs(t, err, ErrIllegalTransition)
}

func TestApplyCancelRequestRejectsWrongSourceStatus(t *testing.T) {
	o := mustPendingSubmitOrder(t)
	req, err := NewCancelRequest(CancelRequest{OrderID: o.Request.OrderID, Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	_, err = ApplyCancelRequest(o, req)
	assert.ErrorIs(t, err, ErrIllegalTransition)
}

func TestApplyCancelResultSuccess(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewCancelRequest(CancelRequest{OrderID: o.Request.OrderID, Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyCancelRequest(o, req)
	require.NoError(t, err)

	result, err := NewCancelResult(CancelResult{OrderID: o.Request.OrderID, Status: StatusCanceled, Metadata: resultMetadataFor(o)})
	require.NoError(t, err)
	o, err = ApplyCancelResult(o, result)
	require.NoError(t, err)
	assert.Equal(t, StatusCanceled, o.Status)
	assert.Nil(t, o.Rejection, "a declined cancel must never set Order.Rejection")
	assert.True(t, o.PendingCommandID.IsZero(), "PendingCommandID must clear once the cycle resolves")
}

func TestApplyCancelResultDeclinedDoesNotSetOrderRejection(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewCancelRequest(CancelRequest{OrderID: o.Request.OrderID, Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyCancelRequest(o, req)
	require.NoError(t, err)

	// The order is not actually fully filled in this fixture, so
	// completing the transition also requires FilledQuantity to match.
	accepted := num.MustParseQuantity("1000")
	o.AcceptedQuantity = &accepted
	o.FilledQuantity = accepted

	// The cancel was declined because the order was already fully
	// filled by the time the broker processed it.
	result, err := NewCancelResult(CancelResult{
		OrderID:   o.Request.OrderID,
		Status:    StatusFilled,
		Rejection: &Rejection{Reason: ReasonUnknown, Detail: "already filled"},
		Metadata:  resultMetadataFor(o),
	})
	require.NoError(t, err)

	o, err = ApplyCancelResult(o, result)
	require.NoError(t, err)
	assert.Equal(t, StatusFilled, o.Status)
	assert.Nil(t, o.Rejection, "CancelResult.Rejection must not leak into Order.Rejection")
}

func TestApplyCancelResultRejectsMismatchedOrderID(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewCancelRequest(CancelRequest{OrderID: o.Request.OrderID, Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyCancelRequest(o, req)
	require.NoError(t, err)

	result, err := NewCancelResult(CancelResult{OrderID: mustOrderID(t), Status: StatusCanceled, Metadata: resultMetadataFor(o)})
	require.NoError(t, err)
	_, err = ApplyCancelResult(o, result)
	assert.ErrorIs(t, err, ErrOrderMismatch)
}

func TestApplyCancelResultRejectsIllegalResultStatus(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewCancelRequest(CancelRequest{OrderID: o.Request.OrderID, Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyCancelRequest(o, req)
	require.NoError(t, err)

	// StatusRejected is not reachable from StatusPendingCancel.
	result, err := NewCancelResult(CancelResult{
		OrderID:   o.Request.OrderID,
		Status:    StatusRejected,
		Rejection: &Rejection{Reason: ReasonUnknown},
		Metadata:  resultMetadataFor(o),
	})
	require.NoError(t, err)
	_, err = ApplyCancelResult(o, result)
	assert.ErrorIs(t, err, ErrIllegalTransition)
}

func TestApplyCancelResultRejectsWithoutPendingCancel(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	result, err := NewCancelResult(CancelResult{OrderID: o.Request.OrderID, Status: StatusCanceled})
	require.NoError(t, err)
	_, err = ApplyCancelResult(o, result)
	assert.ErrorIs(t, err, ErrIllegalTransition)
}

// Regression coverage for the delayed-result race a design review
// flagged: a stale CancelResult from an earlier cancel cycle on the
// same order must not be applied during a later cancel cycle.
func TestApplyCancelResultRejectsStaleResultFromEarlierCycle(t *testing.T) {
	o := mustWorkingOrder(t, "1000")

	// First cancel cycle: declined, order reverts to Working.
	firstReq, err := NewCancelRequest(CancelRequest{OrderID: o.Request.OrderID, Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyCancelRequest(o, firstReq)
	require.NoError(t, err)
	firstResult, err := NewCancelResult(CancelResult{
		OrderID:   o.Request.OrderID,
		Status:    StatusWorking,
		Rejection: &Rejection{Reason: ReasonUnknown},
		Metadata:  resultMetadataFor(o),
	})
	require.NoError(t, err)
	o, err = ApplyCancelResult(o, firstResult)
	require.NoError(t, err)
	assert.Equal(t, StatusWorking, o.Status)

	// Second cancel cycle begins.
	secondReq, err := NewCancelRequest(CancelRequest{OrderID: o.Request.OrderID, Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyCancelRequest(o, secondReq)
	require.NoError(t, err)

	// A delayed result from the *first* cycle finally arrives, correlated
	// to firstReq's EventID rather than the currently outstanding
	// secondReq's EventID.
	staleResult, err := NewCancelResult(CancelResult{
		OrderID:  o.Request.OrderID,
		Status:   StatusCanceled,
		Metadata: id.Metadata{CausationID: firstReq.Metadata.EventID},
	})
	require.NoError(t, err)
	_, err = ApplyCancelResult(o, staleResult)
	assert.ErrorIs(t, err, ErrStaleResult)
}

// Regression coverage for the race Copilot flagged: once ApplyFill has
// already overridden a pending cancel with a complete fill, a late or
// duplicate CancelResult confirming that same terminal status must be a
// safe no-op, not an error.
func TestApplyCancelResultIsIdempotentAfterFillOverridesPendingCancel(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewCancelRequest(CancelRequest{OrderID: o.Request.OrderID, Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyCancelRequest(o, req)
	require.NoError(t, err)

	o, err = ApplyFill(o, mustFillFor(t, o, "1000"))
	require.NoError(t, err)
	require.Equal(t, StatusFilled, o.Status)
	require.True(t, o.PendingCommandID.IsZero())

	lateResult, err := NewCancelResult(CancelResult{OrderID: o.Request.OrderID, Status: StatusFilled})
	require.NoError(t, err)
	unchanged, err := ApplyCancelResult(o, lateResult)
	require.NoError(t, err)
	assert.Equal(t, o, unchanged)

	// But a late result claiming something inconsistent with the actual
	// resulting state must still be rejected.
	wrongResult, err := NewCancelResult(CancelResult{OrderID: o.Request.OrderID, Status: StatusCanceled})
	require.NoError(t, err)
	_, err = ApplyCancelResult(o, wrongResult)
	assert.ErrorIs(t, err, ErrIllegalTransition)
}

// --- ApplyReplaceRequest / ApplyReplaceResult ---

func TestApplyReplaceRequestValid(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewReplaceRequest(ReplaceRequest{OrderID: o.Request.OrderID, NewQuantity: qty(t, "2000"), Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyReplaceRequest(o, req)
	require.NoError(t, err)
	assert.Equal(t, StatusPendingReplace, o.Status)
	assert.Equal(t, req.Metadata.EventID, o.PendingCommandID)
}

func TestApplyReplaceRequestRejectsMismatchedOrderID(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewReplaceRequest(ReplaceRequest{OrderID: mustOrderID(t), NewQuantity: qty(t, "2000"), Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	_, err = ApplyReplaceRequest(o, req)
	assert.ErrorIs(t, err, ErrOrderMismatch)
}

func TestApplyReplaceRequestRejectsZeroEventID(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewReplaceRequest(ReplaceRequest{OrderID: o.Request.OrderID, NewQuantity: qty(t, "2000")})
	require.NoError(t, err)
	_, err = ApplyReplaceRequest(o, req)
	assert.ErrorIs(t, err, ErrIllegalTransition)
}

func TestApplyReplaceRequestRejectsWrongSourceStatus(t *testing.T) {
	o := mustPendingSubmitOrder(t)
	req, err := NewReplaceRequest(ReplaceRequest{OrderID: o.Request.OrderID, NewQuantity: qty(t, "2000"), Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	_, err = ApplyReplaceRequest(o, req)
	assert.ErrorIs(t, err, ErrIllegalTransition)
}

func TestApplyReplaceResultSuccessAppliesNewLimitAndStopPrice(t *testing.T) {
	proposal, err := NewProposal(Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        Buy,
		Type:        StopLimit,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000"),
		LimitPrice:  price(t, "1.10000"),
		StopPrice:   price(t, "1.09500"),
	})
	require.NoError(t, err)
	request, err := NewRequest(proposal, mustOrderID(t))
	require.NoError(t, err)
	accepted := num.MustParseQuantity("1000")
	o, err := NewOrder(Order{
		Request:            request,
		AcceptedQuantity:   &accepted,
		AcceptedLimitPrice: price(t, "1.10000"),
		AcceptedStopPrice:  price(t, "1.09500"),
		Status:             StatusWorking,
	})
	require.NoError(t, err)

	req, err := NewReplaceRequest(ReplaceRequest{
		OrderID:       o.Request.OrderID,
		NewLimitPrice: price(t, "1.10500"),
		NewStopPrice:  price(t, "1.10000"),
		Metadata:      mustCommandMetadata(t),
	})
	require.NoError(t, err)
	o, err = ApplyReplaceRequest(o, req)
	require.NoError(t, err)

	result, err := NewReplaceResult(ReplaceResult{OrderID: o.Request.OrderID, Status: StatusWorking, Metadata: resultMetadataFor(o)})
	require.NoError(t, err)
	o, err = ApplyReplaceResult(o, req, result)
	require.NoError(t, err)
	require.NotNil(t, o.AcceptedLimitPrice)
	require.NotNil(t, o.AcceptedStopPrice)
	assert.True(t, o.AcceptedLimitPrice.Equal(num.MustParsePrice("1.10500")))
	assert.True(t, o.AcceptedStopPrice.Equal(num.MustParsePrice("1.10000")))
}

func TestApplyReplaceResultRejectsIllegalResultStatus(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewReplaceRequest(ReplaceRequest{OrderID: o.Request.OrderID, NewQuantity: qty(t, "2000"), Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyReplaceRequest(o, req)
	require.NoError(t, err)

	// StatusRejected is not reachable from StatusPendingReplace.
	result, err := NewReplaceResult(ReplaceResult{
		OrderID:   o.Request.OrderID,
		Status:    StatusRejected,
		Rejection: &Rejection{Reason: ReasonUnknown},
		Metadata:  resultMetadataFor(o),
	})
	require.NoError(t, err)
	_, err = ApplyReplaceResult(o, req, result)
	assert.ErrorIs(t, err, ErrIllegalTransition)
}

func TestApplyReplaceResultSuccessAppliesNewQuantity(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewReplaceRequest(ReplaceRequest{OrderID: o.Request.OrderID, NewQuantity: qty(t, "2000"), Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyReplaceRequest(o, req)
	require.NoError(t, err)

	result, err := NewReplaceResult(ReplaceResult{OrderID: o.Request.OrderID, Status: StatusWorking, Metadata: resultMetadataFor(o)})
	require.NoError(t, err)
	o, err = ApplyReplaceResult(o, req, result)
	require.NoError(t, err)
	assert.Equal(t, StatusWorking, o.Status)
	require.NotNil(t, o.AcceptedQuantity)
	assert.True(t, o.AcceptedQuantity.Equal(num.MustParseQuantity("2000")), "in-place amendment: same OrderID, updated accepted quantity")
	assert.True(t, o.PendingCommandID.IsZero())
}

func TestApplyReplaceResultDeclinedLeavesAcceptedValuesUnchanged(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewReplaceRequest(ReplaceRequest{OrderID: o.Request.OrderID, NewQuantity: qty(t, "2000"), Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyReplaceRequest(o, req)
	require.NoError(t, err)

	result, err := NewReplaceResult(ReplaceResult{
		OrderID:   o.Request.OrderID,
		Status:    StatusWorking,
		Rejection: &Rejection{Reason: ReasonUnsupportedCapability},
		Metadata:  resultMetadataFor(o),
	})
	require.NoError(t, err)
	o, err = ApplyReplaceResult(o, req, result)
	require.NoError(t, err)
	require.NotNil(t, o.AcceptedQuantity)
	assert.True(t, o.AcceptedQuantity.Equal(num.MustParseQuantity("1000")), "declined replace must not change accepted quantity")
}

// A non-rejected replace result whose Status is StatusCanceled must not
// apply the new accepted terms first: "the replacement took effect" is
// only true for a result that leaves the order live.
func TestApplyReplaceResultCanceledDoesNotApplyNewValues(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewReplaceRequest(ReplaceRequest{OrderID: o.Request.OrderID, NewQuantity: qty(t, "2000"), Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyReplaceRequest(o, req)
	require.NoError(t, err)

	result, err := NewReplaceResult(ReplaceResult{OrderID: o.Request.OrderID, Status: StatusCanceled, Metadata: resultMetadataFor(o)})
	require.NoError(t, err)
	o, err = ApplyReplaceResult(o, req, result)
	require.NoError(t, err)
	assert.Equal(t, StatusCanceled, o.Status)
	require.NotNil(t, o.AcceptedQuantity)
	assert.True(t, o.AcceptedQuantity.Equal(num.MustParseQuantity("1000")), "a Canceled result must not apply the new accepted quantity")
}

func TestApplyReplaceResultRejectsRequestOrderIDNotMatchingOrder(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	realReq, err := NewReplaceRequest(ReplaceRequest{OrderID: o.Request.OrderID, NewQuantity: qty(t, "2000"), Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyReplaceRequest(o, realReq)
	require.NoError(t, err)

	otherOrderID := mustOrderID(t)
	unrelatedReq, err := NewReplaceRequest(ReplaceRequest{OrderID: otherOrderID, NewQuantity: qty(t, "2000"), Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	unrelatedResult, err := NewReplaceResult(ReplaceResult{OrderID: otherOrderID, Status: StatusWorking})
	require.NoError(t, err)

	_, err = ApplyReplaceResult(o, unrelatedReq, unrelatedResult)
	assert.ErrorIs(t, err, ErrOrderMismatch)
}

func TestApplyReplaceResultRejectsMismatchedRequestResultOrderIDs(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewReplaceRequest(ReplaceRequest{OrderID: o.Request.OrderID, NewQuantity: qty(t, "2000"), Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyReplaceRequest(o, req)
	require.NoError(t, err)

	result, err := NewReplaceResult(ReplaceResult{OrderID: mustOrderID(t), Status: StatusWorking})
	require.NoError(t, err)
	_, err = ApplyReplaceResult(o, req, result)
	assert.ErrorIs(t, err, ErrOrderMismatch)
}

func TestApplyReplaceResultRejectsWithoutPendingReplace(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewReplaceRequest(ReplaceRequest{OrderID: o.Request.OrderID, NewQuantity: qty(t, "2000"), Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	// result.Status is not idempotent-equal to o.Status (Working) and
	// is not reachable directly from Working without having gone
	// through PendingReplace first.
	result, err := NewReplaceResult(ReplaceResult{OrderID: o.Request.OrderID, Status: StatusCanceled})
	require.NoError(t, err)
	_, err = ApplyReplaceResult(o, req, result)
	assert.ErrorIs(t, err, ErrIllegalTransition)
}

// Regression coverage for the delayed-result race a design review
// flagged: a stale ReplaceResult from an earlier replace cycle on the
// same order must not be applied during a later replace cycle.
func TestApplyReplaceResultRejectsStaleResultFromEarlierCycle(t *testing.T) {
	o := mustWorkingOrder(t, "1000")

	firstReq, err := NewReplaceRequest(ReplaceRequest{OrderID: o.Request.OrderID, NewQuantity: qty(t, "1500"), Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyReplaceRequest(o, firstReq)
	require.NoError(t, err)
	firstResult, err := NewReplaceResult(ReplaceResult{
		OrderID:   o.Request.OrderID,
		Status:    StatusWorking,
		Rejection: &Rejection{Reason: ReasonUnknown},
		Metadata:  resultMetadataFor(o),
	})
	require.NoError(t, err)
	o, err = ApplyReplaceResult(o, firstReq, firstResult)
	require.NoError(t, err)

	secondReq, err := NewReplaceRequest(ReplaceRequest{OrderID: o.Request.OrderID, NewQuantity: qty(t, "3000"), Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyReplaceRequest(o, secondReq)
	require.NoError(t, err)

	staleResult, err := NewReplaceResult(ReplaceResult{
		OrderID:  o.Request.OrderID,
		Status:   StatusWorking,
		Metadata: id.Metadata{CausationID: firstReq.Metadata.EventID},
	})
	require.NoError(t, err)
	_, err = ApplyReplaceResult(o, firstReq, staleResult)
	assert.ErrorIs(t, err, ErrStaleResult)
}

// Regression coverage for the race Copilot flagged: once ApplyFill has
// already overridden a pending replace with a complete fill, a late or
// duplicate ReplaceResult confirming that same terminal status must be a
// safe no-op, not an error.
func TestApplyReplaceResultIsIdempotentAfterFillOverridesPendingReplace(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewReplaceRequest(ReplaceRequest{OrderID: o.Request.OrderID, NewQuantity: qty(t, "2000"), Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyReplaceRequest(o, req)
	require.NoError(t, err)

	o, err = ApplyFill(o, mustFillFor(t, o, "1000"))
	require.NoError(t, err)
	require.Equal(t, StatusFilled, o.Status)
	require.True(t, o.PendingCommandID.IsZero())

	lateResult, err := NewReplaceResult(ReplaceResult{OrderID: o.Request.OrderID, Status: StatusFilled})
	require.NoError(t, err)
	unchanged, err := ApplyReplaceResult(o, req, lateResult)
	require.NoError(t, err)
	assert.Equal(t, o, unchanged)
}

// --- ApplyFill ---

func TestApplyFillPartialThenFull(t *testing.T) {
	o := mustWorkingOrder(t, "1000")

	o, err := ApplyFill(o, mustFillFor(t, o, "400"))
	require.NoError(t, err)
	assert.Equal(t, StatusPartiallyFilled, o.Status)
	assert.True(t, o.FilledQuantity.Equal(num.MustParseQuantity("400")))

	o, err = ApplyFill(o, mustFillFor(t, o, "600"))
	require.NoError(t, err)
	assert.Equal(t, StatusFilled, o.Status)
	assert.True(t, o.FilledQuantity.Equal(num.MustParseQuantity("1000")))
}

func TestApplyFillClearsAvgFillPrice(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	stale := num.MustParsePrice("1.00000")
	o.AvgFillPrice = &stale
	o, err := ApplyFill(o, mustFillFor(t, o, "400"))
	require.NoError(t, err)
	assert.Nil(t, o.AvgFillPrice, "AvgFillPrice must be cleared, never left stale")
}

func TestApplyFillRejectsOverfill(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	o, err := ApplyFill(o, mustFillFor(t, o, "600"))
	require.NoError(t, err)
	_, err = ApplyFill(o, mustFillFor(t, o, "500"))
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestApplyFillRejectsOrderWithNoAcceptedQuantity(t *testing.T) {
	// A malformed Order built as a bare struct literal, bypassing
	// NewOrder's invariant that a "live" Status requires a non-nil
	// AcceptedQuantity.
	o := mustWorkingOrder(t, "1000")
	o.AcceptedQuantity = nil
	_, err := ApplyFill(o, mustFillFor(t, o, "100"))
	assert.ErrorIs(t, err, ErrIllegalTransition)
}

func TestApplyFillRejectsAddOverflow(t *testing.T) {
	o := mustWorkingOrder(t, "90000000000")
	o.FilledQuantity = num.MustParseQuantity("50000000000")
	fill := mustFillFor(t, o, "50000000000")
	_, err := ApplyFill(o, fill)
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestApplyFillRejectsWrongSourceStatus(t *testing.T) {
	o := mustPendingSubmitOrder(t)
	fill, err := NewFill(Fill{
		FillID:    mustFillID(t),
		OrderID:   o.Request.OrderID,
		AccountID: o.Request.AccountID,
		Listing:   o.Request.Listing,
		Side:      o.Request.Side,
		Price:     num.MustParsePrice("1.10000"),
		Quantity:  num.MustParseQuantity("100"),
	})
	require.NoError(t, err)
	_, err = ApplyFill(o, fill)
	assert.ErrorIs(t, err, ErrIllegalTransition)
}

func TestApplyFillRejectsMismatchedOrderID(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	fill := mustFillFor(t, o, "100")
	fill.OrderID = mustOrderID(t)
	_, err := ApplyFill(o, fill)
	assert.ErrorIs(t, err, ErrOrderMismatch)
}

func TestApplyFillRejectsMismatchedAccountID(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	fill := mustFillFor(t, o, "100")
	fill.AccountID = mustAccountID(t)
	_, err := ApplyFill(o, fill)
	assert.ErrorIs(t, err, ErrOrderMismatch)
}

func TestApplyFillRejectsMismatchedListing(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	fill := mustFillFor(t, o, "100")
	apple, err := instrumentEquityListing(t)
	require.NoError(t, err)
	fill.Listing = apple
	_, err = ApplyFill(o, fill)
	assert.ErrorIs(t, err, ErrOrderMismatch)
}

func TestApplyFillRejectsMismatchedSide(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	fill := mustFillFor(t, o, "100")
	fill.Side = Sell
	require.NotEqual(t, o.Request.Side, fill.Side)
	_, err := ApplyFill(o, fill)
	assert.ErrorIs(t, err, ErrOrderMismatch)
}

func TestApplyFillRejectsMismatchedBrokerOrderID(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	fill := mustFillFor(t, o, "100")
	fill.BrokerOrderID = "some-other-broker-order"
	_, err := ApplyFill(o, fill)
	assert.ErrorIs(t, err, ErrOrderMismatch)
}

func TestApplyFillAllowsEmptyBrokerOrderIDOnEitherSide(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	o.BrokerOrderID = ""
	fill := mustFillFor(t, o, "100")
	fill.BrokerOrderID = "some-broker-order"
	_, err := ApplyFill(o, fill)
	assert.NoError(t, err, "an order with no recorded BrokerOrderID yet should not reject a fill that has one")
}

func TestApplyFillDetectsDuplicateByFillID(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	fill := mustFillFor(t, o, "400")

	o, err := ApplyFill(o, fill)
	require.NoError(t, err)

	unchanged, err := ApplyFill(o, fill)
	assert.ErrorIs(t, err, ErrDuplicateFill)
	assert.Equal(t, o, unchanged, "a duplicate fill must return the order unchanged")
}

func TestApplyFillDetectsDuplicateByBrokerFillIDEvenWithNewFillID(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	first := mustFillFor(t, o, "400")
	first.BrokerFillID = "broker-exec-1"
	first, err := NewFill(first)
	require.NoError(t, err)

	o, err = ApplyFill(o, first)
	require.NoError(t, err)

	// A redelivery of the same broker execution, but the adapter minted
	// a fresh Trader FillID for it.
	redelivered := mustFillFor(t, o, "400")
	redelivered.BrokerFillID = "broker-exec-1"
	redelivered, err = NewFill(redelivered)
	require.NoError(t, err)
	require.NotEqual(t, first.FillID, redelivered.FillID)

	unchanged, err := ApplyFill(o, redelivered)
	assert.ErrorIs(t, err, ErrDuplicateFill)
	assert.Equal(t, o, unchanged)
}

func TestApplyFillPreservesPendingCancelOnPartialFill(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewCancelRequest(CancelRequest{OrderID: o.Request.OrderID, Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyCancelRequest(o, req)
	require.NoError(t, err)

	o, err = ApplyFill(o, mustFillFor(t, o, "400"))
	require.NoError(t, err)
	assert.Equal(t, StatusPendingCancel, o.Status, "a partial fill must not discard the pending cancel")
	assert.True(t, o.FilledQuantity.Equal(num.MustParseQuantity("400")))
	assert.False(t, o.PendingCommandID.IsZero(), "the outstanding cancel command is still outstanding")
}

func TestApplyFillOverridesPendingCancelOnCompleteFill(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewCancelRequest(CancelRequest{OrderID: o.Request.OrderID, Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyCancelRequest(o, req)
	require.NoError(t, err)

	o, err = ApplyFill(o, mustFillFor(t, o, "1000"))
	require.NoError(t, err)
	assert.Equal(t, StatusFilled, o.Status, "a complete fill leaves nothing left to cancel")
	assert.True(t, o.PendingCommandID.IsZero(), "no command is outstanding once fully filled")
}

func TestApplyFillPreservesPendingReplaceOnPartialFill(t *testing.T) {
	o := mustWorkingOrder(t, "1000")
	req, err := NewReplaceRequest(ReplaceRequest{OrderID: o.Request.OrderID, NewQuantity: qty(t, "1500"), Metadata: mustCommandMetadata(t)})
	require.NoError(t, err)
	o, err = ApplyReplaceRequest(o, req)
	require.NoError(t, err)

	o, err = ApplyFill(o, mustFillFor(t, o, "400"))
	require.NoError(t, err)
	assert.Equal(t, StatusPendingReplace, o.Status, "a partial fill must not discard the pending replace")
	assert.False(t, o.PendingCommandID.IsZero(), "the outstanding replace command is still outstanding")
}
