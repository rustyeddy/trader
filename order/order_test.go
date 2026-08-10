package order

import (
	"testing"

	"github.com/rustyeddy/trader/id"
	"github.com/rustyeddy/trader/num"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOrderValidPendingSubmit(t *testing.T) {
	o, err := NewOrder(Order{
		Request: mustRequest(t),
		Status:  StatusPendingSubmit,
	})
	require.NoError(t, err)
	assert.Nil(t, o.AcceptedQuantity)
}

func TestNewOrderValidAccepted(t *testing.T) {
	accepted := num.MustParseQuantity("1000")
	o, err := NewOrder(Order{
		Request:          mustRequest(t),
		BrokerOrderID:    "broker-123",
		AcceptedQuantity: &accepted,
		Status:           StatusWorking,
	})
	require.NoError(t, err)
	require.NotNil(t, o.AcceptedQuantity)
	assert.True(t, o.AcceptedQuantity.Equal(accepted))
}

func TestNewOrderRejectsUnconstructedRequest(t *testing.T) {
	_, err := NewOrder(Order{})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

// NewOrder must fully revalidate its embedded Request (and, through it,
// the embedded Proposal), not merely trust it by provenance: Request's
// exported fields let a caller build one as a bare struct literal,
// bypassing both NewProposal and NewRequest entirely.
func TestNewOrderRevalidatesRequestBuiltAsStructLiteral(t *testing.T) {
	bypassed := Request{
		Proposal: Proposal{
			Listing: mustEurUsdListing(t),
			// AccountID deliberately left zero.
			Side:        Buy,
			Type:        Market,
			TimeInForce: GTC,
			Quantity:    num.MustParseQuantity("1000"),
		},
		OrderID: mustOrderID(t),
	}
	_, err := NewOrder(Order{Request: bypassed})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestNewOrderRejectsInvalidStatus(t *testing.T) {
	_, err := NewOrder(Order{
		Request: mustRequest(t),
		Status:  Status(200),
	})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestNewOrderRejectsWorkingStatusWithoutAcceptedQuantity(t *testing.T) {
	_, err := NewOrder(Order{
		Request: mustRequest(t),
		Status:  StatusWorking,
	})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestNewOrderRejectsPendingSubmitWithAcceptedQuantity(t *testing.T) {
	accepted := num.MustParseQuantity("1000")
	_, err := NewOrder(Order{
		Request:          mustRequest(t),
		AcceptedQuantity: &accepted,
		Status:           StatusPendingSubmit,
	})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestNewOrderRejectsRejectedWithAcceptedQuantity(t *testing.T) {
	accepted := num.MustParseQuantity("1000")
	_, err := NewOrder(Order{
		Request:          mustRequest(t),
		AcceptedQuantity: &accepted,
		Status:           StatusRejected,
		Rejection:        &Rejection{Reason: ReasonRiskRejected},
	})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestNewOrderRejectsZeroAcceptedQuantity(t *testing.T) {
	zero := num.Quantity{}
	_, err := NewOrder(Order{
		Request:          mustRequest(t),
		AcceptedQuantity: &zero,
	})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestNewOrderRejectsFilledQuantityWithoutAcceptedQuantity(t *testing.T) {
	_, err := NewOrder(Order{
		Request:        mustRequest(t),
		FilledQuantity: num.MustParseQuantity("100"),
	})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestNewOrderRejectsAcceptedPricesWithoutAcceptedQuantity(t *testing.T) {
	_, err := NewOrder(Order{
		Request:            mustRequest(t),
		AcceptedLimitPrice: price(t, "1.10000"),
	})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestNewOrderRejectsFilledQuantityExceedingAcceptedQuantity(t *testing.T) {
	accepted := num.MustParseQuantity("1000")
	_, err := NewOrder(Order{
		Request:          mustRequest(t),
		AcceptedQuantity: &accepted,
		FilledQuantity:   num.MustParseQuantity("1000.00000001"),
	})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestNewOrderAllowsFullyFilled(t *testing.T) {
	accepted := num.MustParseQuantity("1000")
	o, err := NewOrder(Order{
		Request:          mustRequest(t),
		AcceptedQuantity: &accepted,
		FilledQuantity:   num.MustParseQuantity("1000"),
		Status:           StatusFilled,
	})
	require.NoError(t, err)
	remaining, err := o.RemainingQuantity()
	require.NoError(t, err)
	assert.True(t, remaining.IsZero())
}

func TestNewOrderRejectsWrongAcceptedPricePresenceForType(t *testing.T) {
	proposal, err := NewProposal(Proposal{
		Listing:     mustEurUsdListing(t),
		AccountID:   mustAccountID(t),
		Side:        Buy,
		Type:        Limit,
		TimeInForce: GTC,
		Quantity:    num.MustParseQuantity("1000"),
		LimitPrice:  price(t, "1.10000"),
	})
	require.NoError(t, err)
	req, err := NewRequest(proposal, mustOrderID(t))
	require.NoError(t, err)

	accepted := num.MustParseQuantity("1000")
	// A Limit order's accepted state must also carry an accepted limit
	// price.
	_, err = NewOrder(Order{
		Request:          req,
		AcceptedQuantity: &accepted,
	})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestNewOrderRejectsAcceptedQuantityNotOnIncrement(t *testing.T) {
	accepted := num.MustParseQuantity("1000.5")
	_, err := NewOrder(Order{
		Request:          mustRequest(t),
		AcceptedQuantity: &accepted,
	})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestNewOrderRejectsRejectedStatusWithoutRejection(t *testing.T) {
	_, err := NewOrder(Order{
		Request: mustRequest(t),
		Status:  StatusRejected,
	})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestNewOrderRejectsRejectionOnNonRejectedStatus(t *testing.T) {
	accepted := num.MustParseQuantity("1000")
	_, err := NewOrder(Order{
		Request:          mustRequest(t),
		AcceptedQuantity: &accepted,
		Status:           StatusWorking,
		Rejection:        &Rejection{Reason: ReasonMarketClosed},
	})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestNewOrderAllowsRejectedWithRejection(t *testing.T) {
	o, err := NewOrder(Order{
		Request:   mustRequest(t),
		Status:    StatusRejected,
		Rejection: &Rejection{Reason: ReasonRiskRejected},
	})
	require.NoError(t, err)
	require.NotNil(t, o.Rejection)
	assert.Equal(t, ReasonRiskRejected, o.Rejection.Reason)
}

func TestOrderRemainingQuantityRequiresAcceptance(t *testing.T) {
	o, err := NewOrder(Order{
		Request: mustRequest(t),
		Status:  StatusPendingSubmit,
	})
	require.NoError(t, err)
	_, err = o.RemainingQuantity()
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestNewOrderRejectsZeroAppliedFillID(t *testing.T) {
	_, err := NewOrder(Order{
		Request:        mustRequest(t),
		Status:         StatusPendingSubmit,
		AppliedFillIDs: []id.FillID{{}},
	})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestNewOrderRejectsDuplicateAppliedFillID(t *testing.T) {
	fid := mustFillID(t)
	_, err := NewOrder(Order{
		Request:        mustRequest(t),
		Status:         StatusPendingSubmit,
		AppliedFillIDs: []id.FillID{fid, fid},
	})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestNewOrderRejectsEmptyAppliedBrokerFillID(t *testing.T) {
	_, err := NewOrder(Order{
		Request:              mustRequest(t),
		Status:               StatusPendingSubmit,
		AppliedBrokerFillIDs: []string{""},
	})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestNewOrderRejectsDuplicateAppliedBrokerFillID(t *testing.T) {
	_, err := NewOrder(Order{
		Request:              mustRequest(t),
		Status:               StatusPendingSubmit,
		AppliedBrokerFillIDs: []string{"broker-exec-1", "broker-exec-1"},
	})
	assert.ErrorIs(t, err, ErrInvalidOrder)
}

func TestOrderRemainingQuantityPartialFill(t *testing.T) {
	accepted := num.MustParseQuantity("1000")
	o, err := NewOrder(Order{
		Request:          mustRequest(t),
		AcceptedQuantity: &accepted,
		FilledQuantity:   num.MustParseQuantity("400"),
		Status:           StatusPartiallyFilled,
	})
	require.NoError(t, err)
	remaining, err := o.RemainingQuantity()
	require.NoError(t, err)
	assert.True(t, remaining.Equal(num.MustParseQuantity("600")))
}
